package adc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func planFixtureInput() planInput {
	return planInput{Title: "Repogen safety fixture", Source: "Fixture based on core Plan 0007 R1–R5; no real repository work", Steps: []PlanStepSpec{
		{Key: "R1", Title: "Contract", Agent: "dev", Reviewer: "qa", Prompt: "Record the fixture contract", Criteria: "The bounded contract is recorded"},
		{Key: "R2", Title: "Validation", Agent: "dev", Reviewer: "qa", Prompt: "Validate fixture inputs", Criteria: "Invalid input is rejected", DependsOn: []string{"R1"}},
		{Key: "R3", Title: "Restore", Agent: "dev", Reviewer: "qa", Prompt: "Restore fixture state", Criteria: "Invalid state fails closed", DependsOn: []string{"R2"}},
		{Key: "R4", Title: "Pool", Agent: "dev", Reviewer: "qa", Prompt: "Check fixture pool", Criteria: "Different bytes cannot overwrite", DependsOn: []string{"R2"}},
		{Key: "R5", Title: "Canary preparation", Agent: "dev", Reviewer: "qa", Prompt: "Prepare fixture evidence only", Criteria: "Both prerequisites checked; no publication", DependsOn: []string{"R3", "R4"}},
	}}
}
func planDispatch(e *Engine) { e.Store.mu.Lock(); defer e.Store.mu.Unlock(); e.dispatchPlans() }
func planStepByKey(t *testing.T, s *Store, key string) PlanStep {
	t.Helper()
	for _, step := range s.taskPlan("task").Steps {
		if step.Key == key {
			return step
		}
	}
	t.Fatal("missing step", key)
	return PlanStep{}
}
func completePlanWorker(t *testing.T, e *Engine, step PlanStep) {
	t.Helper()
	var r Run
	must(t, e.Store.Get(step.Run, &r))
	r.State = "running"
	must(t, e.Store.Put("run", r.Org, r.Task, r.State, r.ID, r))
	_, err := call(t, e, r, "adc_finish", map[string]any{"Result": "Fixture acceptance evidence for " + step.Key})
	must(t, err)
}
func reviewPlanStep(t *testing.T, e *Engine, step PlanStep, verdict string) {
	t.Helper()
	var r Run
	must(t, e.Store.Get(step.Review, &r))
	if !e.prepareReview(&r) {
		t.Fatal("review target not ready")
	}
	r.State = "running"
	must(t, e.Store.Put("run", r.Org, r.Task, r.State, r.ID, r))
	_, err := call(t, e, r, "adc_review", map[string]any{"Verdict": verdict, "Findings": "Independently checked fixture criterion for " + step.Key})
	must(t, err)
}
func TestExecutionPlanDependencyReviewRecovery(t *testing.T) {
	s, e, task, root := fixture(t)
	input := planFixtureInput()
	p, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	if len(taskRuns(s, task.ID)) != 1 {
		t.Fatal("draft dispatched")
	}
	input.Revision = p.Revision
	input.Start = true
	p, err = e.saveExecutionPlan(root, input)
	must(t, err)
	_, err = call(t, e, root, "adc_wait", map[string]any{})
	must(t, err)
	planDispatch(e)
	r1 := planStepByKey(t, s, "R1")
	if r1.Run == "" || r1.Review == "" {
		t.Fatal("missing worker/reviewer")
	}
	if planStepByKey(t, s, "R2").Run != "" {
		t.Fatal("dependent started early")
	}
	// Reconstruct the engine from persisted records, then reconcile repeatedly.
	e = NewEngine(s)
	planDispatch(e)
	if len(taskRuns(s, task.ID)) != 3 {
		t.Fatal("recovery duplicated workers")
	}
	completePlanWorker(t, e, r1)
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run != "" {
		t.Fatal("unreviewed worker unlocked dependent")
	}
	reviewPlanStep(t, e, r1, "changes")
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run != "" {
		t.Fatal("failed review unlocked dependent")
	}
	completePlanWorker(t, e, r1)
	reviewPlanStep(t, e, r1, "pass")
	planDispatch(e)
	r2 := planStepByKey(t, s, "R2")
	if r2.Run == "" {
		t.Fatal("passing review did not dispatch")
	}
	var run Run
	must(t, s.Get(r2.Run, &run))
	if !strings.Contains(run.Prompt, r1.Run) || len(r2.Inputs) != 1 {
		t.Fatal("lost upstream evidence")
	}
	completePlanWorker(t, e, r2)
	reviewPlanStep(t, e, r2, "pass")
	planDispatch(e)
	r3, r4 := planStepByKey(t, s, "R3"), planStepByKey(t, s, "R4")
	if r3.Run == "" || r4.Run == "" {
		t.Fatal("eligible siblings did not dispatch")
	}
	completePlanWorker(t, e, r3)
	reviewPlanStep(t, e, r3, "pass")
	planDispatch(e)
	if planStepByKey(t, s, "R5").Run != "" {
		t.Fatal("join ignored second prerequisite")
	}
	completePlanWorker(t, e, r4)
	reviewPlanStep(t, e, r4, "pass")
	planDispatch(e)
	r5 := planStepByKey(t, s, "R5")
	completePlanWorker(t, e, r5)
	reviewPlanStep(t, e, r5, "pass")
	planDispatch(e)
	if s.taskPlan(task.ID).State != "complete" {
		t.Fatal("plan never completed")
	}
	must(t, s.Get(root.ID, &root))
	root.State = "running"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	_, err = call(t, e, root, "adc_finish", map[string]any{"Result": "All five fixture steps independently verified"})
	must(t, err)
	must(t, s.Get(task.ID, &task))
	if task.State != "ready" {
		t.Fatal("supervisor did not complete")
	}
}
func TestExecutionPlanValidationAndAuthority(t *testing.T) {
	tests := []struct {
		name   string
		change func(*planInput)
	}{
		{"cycle", func(p *planInput) { p.Steps[0].DependsOn = []string{"R5"} }},
		{"missing", func(p *planInput) { p.Steps[1].DependsOn = []string{"missing"} }},
		{"duplicate", func(p *planInput) { p.Steps[1].Key = "R1" }},
		{"same family", func(p *planInput) { p.Steps[0].Reviewer = "boss" }},
		{"foreign owner", func(p *planInput) { p.Steps[0].Agent = "foreign" }},
		{"missing tool", func(p *planInput) { p.Steps[0].RequiredTools = []string{"unavailable"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, e, _, root := fixture(t)
			p := planFixtureInput()
			test.change(&p)
			_, err := e.saveExecutionPlan(root, p)
			if err == nil || s.taskPlan(root.Task).ID != "" {
				t.Fatal("invalid plan persisted", err)
			}
		})
	}
	s, e, task, root := fixture(t)
	input := planFixtureInput()
	input.Start = true
	child := root
	child.Parent = "boss"
	if _, err := e.saveExecutionPlan(child, input); err == nil {
		t.Fatal("worker defined plan")
	}
	root.Execution = "protected"
	root.Authority = "observe"
	root.Tools = nil
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	p, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	for _, id := range []string{step.Run, step.Review} {
		var run Run
		must(t, s.Get(id, &run))
		if run.Authority != "observe" || run.Execution != "protected" || len(run.Tools) > 0 || run.Account != task.Account {
			t.Fatal("plan expanded authority", run)
		}
	}
	input.Revision = p.Revision
	if _, err = e.saveExecutionPlan(root, input); err == nil {
		t.Fatal("active plan replaced")
	}
}
func TestExecutionPlanPauseBlockAndDuplicateDispatch(t *testing.T) {
	s, e, task, root := fixture(t)
	input := planFixtureInput()
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	task.State = "paused"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	planDispatch(e)
	if len(taskRuns(s, task.ID)) != 1 {
		t.Fatal("paused plan ran")
	}
	task.State = "running"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); planDispatch(e) }()
	}
	wg.Wait()
	if len(taskRuns(s, task.ID)) != 3 {
		t.Fatal("duplicate dispatch")
	}
	r1 := planStepByKey(t, s, "R1")
	completePlanWorker(t, e, r1)
	reviewPlanStep(t, e, r1, "pass")
	planDispatch(e)
	r2 := planStepByKey(t, s, "R2")
	completePlanWorker(t, e, r2)
	reviewPlanStep(t, e, r2, "pass")
	planDispatch(e)
	r3 := planStepByKey(t, s, "R3")
	var run Run
	must(t, s.Get(r3.Run, &run))
	run.State = "blocked"
	run.Error = "missing fixture access"
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	r4 := planStepByKey(t, s, "R4")
	completePlanWorker(t, e, r4)
	reviewPlanStep(t, e, r4, "pass")
	planDispatch(e)
	if e.inspectPlan(s.taskPlan(task.ID)).Steps[3].State != "complete" || planStepByKey(t, s, "R5").Run != "" {
		t.Fatal("blocked sibling mishandled")
	}
	_, err = call(t, e, root, "adc_finish", map[string]any{"Result": "Premature"})
	if err == nil {
		t.Fatal("finished incomplete plan")
	}
	task.State = "cancelled"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	before := len(taskRuns(s, task.ID))
	planDispatch(e)
	if len(taskRuns(s, task.ID)) != before {
		t.Fatal("cancelled plan dispatched")
	}
}
func TestExecutionPlanRejectsStalePrerequisite(t *testing.T) {
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	r1 := planStepByKey(t, s, "R1")
	completePlanWorker(t, e, r1)
	reviewPlanStep(t, e, r1, "pass")
	planDispatch(e)
	r2 := planStepByKey(t, s, "R2")
	var upstream Run
	must(t, s.Get(r1.Run, &upstream))
	upstream.Result = "Changed evidence"
	must(t, s.Put("run", upstream.Org, upstream.Task, upstream.State, upstream.ID, upstream))
	var dependent Run
	must(t, s.Get(r2.Run, &dependent))
	if e.planAllowsDispatch(dependent) {
		t.Fatal("stale dependent dispatched")
	}
	planDispatch(e)
	if s.taskPlan(root.Task).Steps[1].State != "blocked" {
		t.Fatal("staleness hidden")
	}
}
func TestExecutionPlanHTTPActivationBoundaries(t *testing.T) {
	s, e, task, root := fixture(t)
	transcriptLogin(t, s)
	var account Account
	must(t, s.Get(task.Account, &account))
	account.User = task.Creator
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	p, err := e.saveExecutionPlan(root, planFixtureInput())
	must(t, err)
	w := NewWeb(s, e, false)
	req := httptest.NewRequest("GET", "/task?org=org&id=task", nil)
	req.AddCookie(&http.Cookie{Name: "adc_session", Value: "transcript-fixture"})
	rw := httptest.NewRecorder()
	w.Handler().ServeHTTP(rw, req)
	if rw.Code != 200 || !strings.Contains(rw.Body.String(), "Start plan within assignment scope") {
		t.Fatal("plan UI absent", rw.Code)
	}
	page := Page{Org: Organization{ID: "org"}, User: User{ID: "owner", Name: "Fixture"}}
	form := func(revision int) *http.Request {
		v := url.Values{"task": {task.ID}, "action": {"start"}, "revision": {fmt.Sprint(revision)}}
		r := httptest.NewRequest("POST", "/plan-action", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	if err := w.executionPlanAction(form(p.Revision+1), page); err == nil {
		t.Fatal("stale form accepted")
	}
	foreign := page
	foreign.Org.ID = "other"
	if err := w.executionPlanAction(form(p.Revision), foreign); err == nil {
		t.Fatal("cross-org activation")
	}
	rw = httptest.NewRecorder()
	req = form(p.Revision)
	req.AddCookie(&http.Cookie{Name: "adc_session", Value: "transcript-fixture"})
	w.Handler().ServeHTTP(rw, req)
	if rw.Code != http.StatusForbidden {
		t.Fatal("CSRF missing", rw.Code)
	}
	must(t, w.executionPlanAction(form(p.Revision), page))
	must(t, w.executionPlanAction(form(p.Revision), page))
	planDispatch(e)
	if len(taskRuns(s, task.ID)) != 3 {
		t.Fatal("activation duplicated work")
	}
}
func TestExecutionPlanSchedulerRunsReadyBranches(t *testing.T) {
	s, e, task, root := fixture(t)
	input := planFixtureInput()
	input.Steps = input.Steps[:1]
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	_, err = call(t, e, root, "adc_wait", map[string]any{})
	must(t, err)
	e.runActivation = func(ctx context.Context, r Run, task Assignment, a Account) {
		if r.Parent == "" {
			_, err := call(t, e, r, "adc_finish", map[string]any{"Result": "Fixture done"})
			must(t, err)
		} else if r.ReviewOf != "" {
			_, err := call(t, e, r, "adc_review", map[string]any{"Verdict": "pass", "Findings": "Verified the fixture result"})
			must(t, err)
		} else {
			_, err := call(t, e, r, "adc_finish", map[string]any{"Result": "Fixture acceptance met"})
			must(t, err)
		}
	}
	for range 5 {
		e.tick(context.Background())
		e.wg.Wait()
	}
	must(t, s.Get(task.ID, &task))
	if task.State != "ready" {
		t.Fatal("scheduler failed to carry plan through review and root completion", task.State)
	}
}

func TestExecutionPlanPersistsAcrossDatabaseReopen(t *testing.T) {
	s, e, task, root := fixture(t)
	input := planFixtureInput()
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	dir := s.Dir
	must(t, s.Close())
	restored, err := Open(dir)
	must(t, err)
	defer restored.Close()
	fresh := NewEngine(restored)
	planDispatch(fresh)
	if got := planStepByKey(t, restored, "R1"); got.Run != step.Run || got.Review != step.Review {
		t.Fatal("reopen duplicated or lost step mapping")
	}
	if len(taskRuns(restored, task.ID)) != 3 {
		t.Fatal("restart changed run count")
	}
	completePlanWorker(t, fresh, step)
	reviewPlanStep(t, fresh, step, "pass")
	planDispatch(fresh)
	if planStepByKey(t, restored, "R2").Run == "" {
		t.Fatal("reopened scheduler did not continue")
	}
}
func TestExecutionPlanDraftRevisionAndBlockedReviewer(t *testing.T) {
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	p, err := e.saveExecutionPlan(root, input)
	must(t, err)
	input.Title = "Revised fixture"
	if _, err = e.saveExecutionPlan(root, input); err == nil {
		t.Fatal("stale edit accepted")
	}
	input.Revision = p.Revision
	p, err = e.saveExecutionPlan(root, input)
	must(t, err)
	history := list[ExecutionPlan](s, "execution-plan-history", root.Org)
	if len(history) != 1 || history[0].Title != "Repogen safety fixture" {
		t.Fatal("draft history missing")
	}
	input.Revision = p.Revision
	input.Start = true
	_, err = e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	completePlanWorker(t, e, step)
	var reviewer Run
	must(t, s.Get(step.Review, &reviewer))
	reviewer.State = "cancelled"
	must(t, s.Put("run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer))
	planDispatch(e)
	if got := s.taskPlan(root.Task).Steps[0]; got.State != "blocked" || !strings.Contains(got.Reason, "review") {
		t.Fatal("cancelled review left plan silently waiting")
	}
	_, err = call(t, e, root, "adc_wait", map[string]any{})
	if err == nil {
		t.Fatal("supervisor silently waited on blocked plan")
	}
}

func TestExecutionPlanChangedPrerequisiteReplaysAfterYield(t *testing.T) {
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Steps = input.Steps[:2]
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	r1 := planStepByKey(t, s, "R1")
	completePlanWorker(t, e, r1)
	reviewPlanStep(t, e, r1, "pass")
	planDispatch(e)
	old := planStepByKey(t, s, "R2")
	var running Run
	must(t, s.Get(old.Run, &running))
	running.State = "running"
	must(t, s.Put("run", running.Org, running.Task, running.State, running.ID, running))
	// A changed prerequisite output (here a document) invalidates dependents
	// once it is reviewed again; result text alone does not.
	changed := Document{ID: "r1-output", Org: root.Org, Task: root.Task, Run: r1.Run, Title: "Contract", Content: "Refreshed prerequisite output", Revision: 1}
	must(t, s.Put("document", changed.Org, changed.Task, "", changed.ID, changed))
	reviewPlanStep(t, e, r1, "pass")
	planDispatch(e)
	if got := planStepByKey(t, s, "R2"); got.Run != old.Run || got.State != "blocked" {
		t.Fatal("replaced an in-flight attempt")
	}
	completePlanWorker(t, e, old)
	planDispatch(e)
	fresh := planStepByKey(t, s, "R2")
	if fresh.Run == old.Run || fresh.Run == "" || len(fresh.Attempts) != 1 || fresh.Inputs["R1"] == old.Inputs["R1"] {
		t.Fatal("fresh reviewed inputs did not rearm", fresh)
	}
	must(t, s.Get(old.Run, &running))
	if running.State != "cancelled" || !running.Superseded || e.planAllowsDispatch(running) {
		t.Fatal("obsolete attempt can run")
	}
	if _, err = e.Reassign(root, old.Run, "dev", "Try old attempt"); err == nil {
		t.Fatal("resurrected superseded run")
	}
	completePlanWorker(t, e, fresh)
	reviewPlanStep(t, e, fresh, "pass")
	planDispatch(e)
	if s.taskPlan(root.Task).State != "complete" {
		t.Fatal("recovered plan cannot complete")
	}
	if len(taskRuns(s, root.Task)) != 7 {
		t.Fatal("recovery duplicated additional work")
	}
}
func TestExecutionPlanRequiresDesignatedReview(t *testing.T) {
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	completePlanWorker(t, e, step)
	var run Run
	must(t, s.Get(step.Run, &run))
	extra := Review{ID: "extra-review", Org: run.Org, Task: run.Task, Run: "other-reviewer", Target: run.ID, Revision: e.revision(run), Model: "claude-opus-5", Family: "anthropic-claude", Verdict: "pass", Findings: "Another review"}
	must(t, s.Put("review", extra.Org, extra.Task, extra.Verdict, extra.ID, extra))
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run != "" {
		t.Fatal("unplanned reviewer bypassed designated reviewer")
	}
	reviewPlanStep(t, e, step, "changes")
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run != "" {
		t.Fatal("extra pass masked designated changes")
	}
	completePlanWorker(t, e, step)
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run == "" {
		t.Fatal("designated pass did not unlock")
	}
}
func TestExecutionPlanSupervisorAndPreDispatchBlockers(t *testing.T) {
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	root.State = "blocked"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	planDispatch(e)
	if planStepByKey(t, s, "R1").Run != "" {
		t.Fatal("dispatched without accountable supervisor")
	}
	// Pending supervisor questions are covered separately: they no longer
	// freeze the already-authorized graph. A blocked supervisor still does.
	root.State = "running"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	// Removing a previously funded portfolio is caught before any worker exists.
	var a Account
	must(t, s.Get("account", &a))
	a.User = "other-human"
	must(t, s.Put("account", "", a.User, "", a.ID, a))
	planDispatch(e)
	if step := e.inspectPlan(s.taskPlan(root.Task)).Steps[0]; step.State != "blocked" || step.Run != "" {
		t.Fatal("pre-dispatch blocker lost", step)
	}
	_, err = call(t, e, root, "adc_wait", map[string]any{})
	if err == nil {
		t.Fatal("root slept on blocker with no run")
	}
}
func TestExecutionPlanSeparateReviewToolRequirements(t *testing.T) {
	s, e, _, root := fixture(t)
	c := Connection{ID: "storage", Org: root.Org, Name: "Fixture", Transport: "stdio", Command: "fixture"}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	var qa Agent
	must(t, s.Get("qa", &qa))
	qa.Tools = nil
	must(t, s.Put("agent", qa.Org, "", "", qa.ID, qa))
	input := planFixtureInput()
	input.Steps = input.Steps[:1]
	input.Steps[0].RequiredTools = []string{"storage"}
	p, err := e.saveExecutionPlan(root, input)
	must(t, err)
	input.Revision = p.Revision
	input.Steps[0].ReviewRequiredTools = []string{"storage"}
	if _, err = e.saveExecutionPlan(root, input); err == nil {
		t.Fatal("missing explicitly required reviewer connection accepted")
	}
}

func TestExecutionPlanOutstandingReviewChangesBlockGate(t *testing.T) {
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	completePlanWorker(t, e, step)
	reviewPlanStep(t, e, step, "pass")
	var run Run
	must(t, s.Get(step.Run, &run))
	extra := Review{ID: "extra-changes", Org: run.Org, Task: run.Task, Run: "extra-reviewer", Target: run.ID, Revision: e.revision(run), Model: "claude-opus-5", Family: "anthropic-claude", Verdict: "changes", Findings: "An additional acceptance finding remains"}
	must(t, s.Put("review", extra.Org, extra.Task, extra.Verdict, extra.ID, extra))
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run != "" {
		t.Fatal("outstanding changes bypassed")
	}
	extra.ID = "extra-pass"
	extra.Verdict = "pass"
	extra.Findings = "Finding resolved and verified"
	must(t, s.Put("review", extra.Org, extra.Task, extra.Verdict, extra.ID, extra))
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run == "" {
		t.Fatal("resolved current review remained blocked")
	}
}

func TestRoutinePlanDependentsPinOutputNotResultText(t *testing.T) {
	s, e, task, root := fixture(t)
	task.Completion = selectedCompletion("routine")
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	input := planFixtureInput()
	input.Steps = input.Steps[:2]
	for i := range input.Steps {
		input.Steps[i].Reviewer = ""
	}
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	r1 := planStepByKey(t, s, "R1")
	completePlanWorker(t, e, r1)
	planDispatch(e)
	r2 := planStepByKey(t, s, "R2")
	if r2.Run == "" {
		t.Fatal("routine dependent not dispatched")
	}
	var upstream Run
	must(t, s.Get(r1.Run, &upstream))
	upstream.Result = "Reworded result text"
	must(t, s.Put("run", upstream.Org, upstream.Task, upstream.State, upstream.ID, upstream))
	var dependent Run
	must(t, s.Get(r2.Run, &dependent))
	if !e.planAllowsDispatch(dependent) || planStepByKey(t, s, "R2").State == "blocked" {
		t.Fatal("result text change invalidated the dependent")
	}
	completePlanWorker(t, e, r2)
	planDispatch(e)
	if s.taskPlan(task.ID).State != "complete" {
		t.Fatal("routine plan did not complete")
	}
	// A real output change on the prerequisite still supersedes the dependent.
	changed := Document{ID: "r1-doc", Org: root.Org, Task: root.Task, Run: r1.Run, Title: "Contract", Content: "New contract", Revision: 1}
	must(t, s.Put("document", changed.Org, changed.Task, "", changed.ID, changed))
	planDispatch(e)
	fresh := planStepByKey(t, s, "R2")
	if fresh.Run == r2.Run || len(fresh.Attempts) != 1 {
		t.Fatal("changed prerequisite output did not supersede finished dependent", fresh.State, fresh.Reason)
	}
}
