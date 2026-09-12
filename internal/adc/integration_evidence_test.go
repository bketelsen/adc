package adc

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func integrationFixture(t *testing.T) (*Store, *Engine, Run, PlanStep) {
	t.Helper()
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Start = true
	input.Steps = input.Steps[:3]
	input.Steps[0].Repositories = []string{"fixture:producer"}
	input.Steps[0].Checks = []ValidationRequirement{{Key: "unit", Kind: "command", Verifier: "make test", Criteria: "All fixture tests pass"}}
	input.Steps[1].Checks = []ValidationRequirement{{Key: "together", Kind: "integration", Verifier: "fixture integration verifier", Criteria: "Validate the exact producer/consumer combination"}}
	input.Steps[2].DependsOn = nil // Unaffected sibling, not globally serialized.
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	var r Run
	must(t, s.Get(step.Run, &r))
	r.State = "running"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	return s, e, r, step
}
func integrationRepo(t *testing.T, e *Engine, r *Run) RepositoryEvidence {
	t.Helper()
	dir := filepath.Join(r.Workspace, "repo")
	must(t, os.MkdirAll(dir, 0700))
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	git("init", "-q")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	must(t, os.WriteFile(filepath.Join(dir, "file"), []byte("base"), 0600))
	git("add", "file")
	git("commit", "-qm", "Base")
	base, err := gitOutput(dir, "rev-parse", "HEAD")
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, "file"), []byte("output"), 0600))
	git("commit", "-qam", "Output")
	code, err := e.captureCode(r, dir)
	must(t, err)
	must(t, e.Store.Put("run", r.Org, r.Task, r.State, r.ID, *r))
	return RepositoryEvidence{Identity: "fixture:producer", Path: code.Path, Base: base, Commit: code.Commit, PullRequest: "fixture:pr/1", Release: "fixture:v1"}
}
func integrationCheck(t *testing.T, e *Engine, r Run, key, outcome string) IntegrationEvidence {
	t.Helper()
	v, err := e.recordValidation(r, validationInput{Key: key, ArtifactRevision: e.artifactRevision(r), Revision: e.Store.integrationEvidence(r.ID).Revision, Outcome: outcome, Output: "Fixture verifier output"}, false)
	must(t, err)
	return v
}
func TestIntegrationChecksPinArtifactsAndSurviveRecovery(t *testing.T) {
	s, e, r, step := integrationFixture(t)
	repo := integrationRepo(t, e, &r)
	if _, err := call(t, e, r, "adc_finish", map[string]any{"Result": "done"}); err == nil {
		t.Fatal("missing ledger accepted")
	}
	v, err := e.saveIntegration(r, integrationInput{Repositories: []RepositoryEvidence{repo}, Environment: []EnvironmentVersion{{Name: "compiler", Version: "v1"}}})
	must(t, err)
	for _, outcome := range []string{"failed", "inapplicable"} {
		integrationCheck(t, e, r, "unit", outcome)
		if e.milestoneMissing(r) == "" {
			t.Fatal("nonpassing check accepted")
		}
	}
	v = integrationCheck(t, e, r, "unit", "pass")
	pin := e.artifactRevision(r)
	reviewPin := e.revision(r)
	completePlanWorker(t, e, step)
	must(t, s.Get(r.ID, &r))
	if e.artifactRevision(r) != pin {
		t.Fatal("completion message invalidates its own checks")
	}
	if e.revision(r) == reviewPin {
		t.Fatal("completion result excluded from review")
	}
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	down := planStepByKey(t, s, "R2")
	if down.Run == "" {
		t.Fatal("valid reviewed inputs did not unlock successor")
	}
	sibling := planStepByKey(t, s, "R3")
	completePlanWorker(t, e, sibling)
	reviewPlanStep(t, e, sibling, "pass")
	// A material environment change invalidates checks and only the affected branch.
	r.State = "running"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	v, err = e.saveIntegration(r, integrationInput{Revision: v.Revision, Repositories: []RepositoryEvidence{repo}, Environment: []EnvironmentVersion{{Name: "compiler", Version: "v2"}}})
	must(t, err)
	if e.validationViews(r)[0].State != "stale" {
		t.Fatal("environment did not stale validation")
	}
	if _, err = e.recordValidation(r, validationInput{Key: "unit", ArtifactRevision: pin, Revision: v.Revision, Outcome: "pass", Output: "old check"}, false); err == nil {
		t.Fatal("stale report accepted")
	}
	var dependent Run
	must(t, s.Get(down.Run, &dependent))
	if e.planAllowsDispatch(dependent) {
		t.Fatal("stale input dispatched")
	}
	view := e.inspectPlan(s.taskPlan(r.Task))
	if view.Steps[2].State != "complete" {
		t.Fatal("unaffected work invalidated")
	}
	dir := s.Dir
	must(t, s.Close())
	reopened, err := Open(dir)
	must(t, err)
	t.Cleanup(func() { reopened.Close() })
	e = NewEngine(reopened)
	if e.validationViews(r)[0].State != "stale" || len(reopened.integrationEvidence(r.ID).Checks) != 1 {
		t.Fatal("recovery lost stale evidence")
	}
	integrationCheck(t, e, r, "unit", "pass")
	completePlanWorker(t, e, step)
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	if planStepByKey(t, reopened, "R2").Run == down.Run {
		t.Fatal("affected attempt not replaced")
	}
	if planStepByKey(t, reopened, "R3").Run != sibling.Run {
		t.Fatal("unaffected attempt replaced")
	}
}

func TestIntegrationConsumesExactUpstreamAndReviewBrief(t *testing.T) {
	s, e, r, step := integrationFixture(t)
	repo := integrationRepo(t, e, &r)
	_, err := e.saveIntegration(r, integrationInput{Repositories: []RepositoryEvidence{repo}})
	must(t, err)
	integrationCheck(t, e, r, "unit", "pass")
	completePlanWorker(t, e, step)
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	down := planStepByKey(t, s, "R2")
	var worker Run
	must(t, s.Get(down.Run, &worker))
	worker.State = "running"
	must(t, s.Put("run", worker.Org, worker.Task, worker.State, worker.ID, worker))
	for _, bad := range []ConsumedArtifact{{Step: "R3", Repository: repo.Identity, Commit: repo.Commit}, {Step: "R1", Repository: repo.Identity, Commit: repo.Base}, {Step: "R1", Repository: "fixture:wrong", Commit: repo.Commit}} {
		if _, err := e.saveIntegration(worker, integrationInput{Consumes: []ConsumedArtifact{bad}}); err == nil {
			t.Fatal("wrong consumed input accepted", bad)
		}
	}
	integrationCheck(t, e, worker, "together", "pass")
	if !strings.Contains(e.milestoneMissing(worker), "consumed") {
		t.Fatal("integration passed without declared combination")
	}
	old := s.integrationEvidence(worker.ID)
	_, err = e.saveIntegration(worker, integrationInput{Revision: old.Revision, Consumes: []ConsumedArtifact{{Step: "R1", Repository: repo.Identity, Commit: repo.Commit}}, Environment: []EnvironmentVersion{{Name: "consumer", Version: "v2"}}})
	must(t, err)
	if e.validationViews(worker)[0].State != "stale" {
		t.Fatal("consumed version did not invalidate old check")
	}
	integrationCheck(t, e, worker, "together", "pass")
	completePlanWorker(t, e, down)
	var reviewer Run
	must(t, s.Get(down.Review, &reviewer))
	e.prepareReview(&reviewer)
	b, _ := json.Marshal(e.reviewerBrief(reviewer))
	for _, want := range []string{repo.Commit, "consumer", "criteria", "unrelated", "weakened", "agent-reported"} {
		if !strings.Contains(string(b), want) {
			t.Fatal("review brief missing", want)
		}
	}
	// An artifact document added after checks makes those checks stale.
	d := Document{ID: "new-artifact", Org: worker.Org, Task: worker.Task, Run: worker.ID, Content: "Changed integration instructions", Revision: 1}
	must(t, s.Put("document", d.Org, d.Task, "", d.ID, d))
	if e.validationViews(worker)[0].State != "stale" {
		t.Fatal("document did not invalidate checks")
	}
	if len(list[IntegrationEvidence](s, "integration-evidence-history", worker.Org)) < 2 {
		t.Fatal("replacement history missing")
	}
}

func TestIntegrationRejectsWrongBaseStaleWritesAndOtherWorkers(t *testing.T) {
	s, e, r, _ := integrationFixture(t)
	repo := integrationRepo(t, e, &r)
	bad := repo
	bad.Base = strings.Repeat("0", 40)
	if _, err := e.saveIntegration(r, integrationInput{Repositories: []RepositoryEvidence{bad}}); err == nil {
		t.Fatal("unknown base accepted")
	}
	bad = repo
	bad.Commit = repo.Base
	if _, err := e.saveIntegration(r, integrationInput{Repositories: []RepositoryEvidence{bad}}); err == nil {
		t.Fatal("unregistered output accepted")
	}
	bad = repo
	bad.Identity = "https://user:secret@example.invalid/repo"
	if _, err := e.saveIntegration(r, integrationInput{Repositories: []RepositoryEvidence{bad}}); err == nil {
		t.Fatal("credential URL accepted")
	}
	_, err := e.saveIntegration(r, integrationInput{Repositories: []RepositoryEvidence{repo}})
	must(t, err)
	if _, err := e.saveIntegration(r, integrationInput{}); err == nil {
		t.Fatal("stale replacement accepted")
	}
	step := planStepByKey(t, s, "R1")
	var reviewer Run
	must(t, s.Get(step.Review, &reviewer))
	reviewer.State = "running"
	must(t, s.Put("run", r.Org, r.Task, "running", reviewer.ID, reviewer))
	if _, err := e.saveIntegration(reviewer, integrationInput{}); err == nil {
		t.Fatal("reviewer writes worker evidence")
	}
	r.Superseded = true
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	if _, err := e.saveIntegration(r, integrationInput{Revision: 1}); err == nil {
		t.Fatal("superseded attempt writes evidence")
	}
}

func TestIntegrationVisualFailureAndPausedLateResult(t *testing.T) {
	s, e, task, root := fixture(t)
	p := planFixtureInput()
	p.Start = true
	p.Steps = p.Steps[:1]
	p.Steps[0].Checks = []ValidationRequirement{{Key: "phone", Kind: "visual", Verifier: "browser viewport 390x844", Criteria: "Phone form can submit, no horizontal overflow or browser errors; retain desktop/phone and baseline evidence"}}
	_, err := e.saveExecutionPlan(root, p)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	var r Run
	must(t, s.Get(step.Run, &r))
	r.State = "running"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	input := validationInput{Key: "phone", ArtifactRevision: e.artifactRevision(r), Outcome: "pass", Output: "Fixture browser check"}
	if _, err := e.recordValidation(r, input, false); err == nil {
		t.Fatal("visual pass without evidence accepted")
	}
	input.Outcome = "failed"
	input.Output = "Submit is outside viewport; desktop checks pass, but phone flow is broken"
	input.References = []string{"fixture:phone-before.png"}
	v, err := e.recordValidation(r, input, false)
	must(t, err)
	if _, err := call(t, e, r, "adc_finish", map[string]any{"Result": "Code tests passed"}); err == nil {
		t.Fatal("passing code bypasses failed phone requirement")
	}
	input.Revision = v.Revision
	input.Outcome = "pass"
	input.Output = "Late fixture result"
	task.State = "paused"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := e.recordValidation(r, input, true); err == nil {
		t.Fatal("late result accepted after pause")
	}
	if s.integrationEvidence(r.ID).Revision != v.Revision {
		t.Fatal("late result mutated evidence")
	}
}

func TestIntegrationPlanPersistenceExcludesDerivedViews(t *testing.T) {
	s, e, r, _ := integrationFixture(t)
	repo := integrationRepo(t, e, &r)
	_, err := e.saveIntegration(r, integrationInput{Repositories: []RepositoryEvidence{repo}})
	must(t, err)
	_, err = e.recordValidation(r, validationInput{Key: "unit", ArtifactRevision: e.artifactRevision(r), Revision: 1, Outcome: "pass", Output: strings.Repeat("OUTPUT_SENTINEL", 2000)}, false)
	must(t, err)
	view := e.inspectPlan(s.taskPlan(r.Task))
	if len(view.Steps[0].Validation) != 1 {
		t.Fatal("live view lost checks")
	}
	// Both write paths strip derived fields without mutating the live view.
	for _, batch := range []bool{false, true} {
		if batch {
			must(t, s.Batch(Write{"execution-plan", view.Org, view.Task, view.State, view.ID, view}))
		} else {
			must(t, s.Put("execution-plan", view.Org, view.Task, view.State, view.ID, view))
		}
		stored := s.taskPlan(r.Task)
		b, _ := json.Marshal(stored)
		if strings.Contains(string(b), "OUTPUT_SENTINEL") || stored.Steps[0].Integration.ID != "" || len(stored.Steps[0].Validation) != 0 || stored.Steps[0].ArtifactRevision != "" {
			t.Fatal("derived evidence persisted")
		}
		if len(view.Steps[0].Validation) != 1 {
			t.Fatal("persistence mutated live view")
		}
	}
	planDispatch(e)
	var before, after string
	must(t, s.db.QueryRow(`SELECT updated FROM records WHERE id=?`, view.ID).Scan(&before))
	planDispatch(e)
	must(t, s.db.QueryRow(`SELECT updated FROM records WHERE id=?`, view.ID).Scan(&after))
	if before != after {
		t.Fatal("unchanged derived output caused plan rewrite")
	}
}

func TestStaleReviewNoticeRemainsBounded(t *testing.T) {
	s, e, r, step := integrationFixture(t)
	var reviewer Run
	must(t, s.Get(step.Review, &reviewer))
	for i := 0; i < 12; i++ {
		_, err := e.restartStaleReview(reviewer)
		must(t, err)
		must(t, s.Get(reviewer.ID, &reviewer))
		if i == 3 {
			reviewer.Prompt += "\nA later human clarification"
		}
	}
	if strings.Count(reviewer.Prompt, "ADC detected that the review target changed") != 1 {
		t.Fatal("repeated notice grew stored prompt")
	}
	if reviewer.State != "waiting" || reviewer.Task != r.Task {
		t.Fatal("recovery state changed")
	}
}
