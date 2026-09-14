package adc

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func attentionFixture(t *testing.T) (*Store, *Engine, Assignment, Run, Area) {
	s, e, source, r, a := ownershipFixture(t)
	completeSource(t, s, source, r)
	a.Owner = "dev"
	var err error
	a, err = s.saveArea(a, a.Revision, "human:owner")
	must(t, err)
	task := Assignment{ID: "assessment-task", Org: a.Org, Owner: "boss", Creator: source.Creator, Account: source.Account, Authority: "observe", Title: "Fixture bounded assessment", Prompt: "Use only supplied fixture evidence. No infrastructure action.", Attention: &AttentionPolicy{Areas: []string{a.ID}, Scan: 3, Investigation: 6, Review: 4, MaxProposals: 1, Minutes: 10, Concurrent: 2}}
	must(t, e.CreateAssignment(task))
	must(t, s.Get(task.ID, &task))
	root := taskRuns(s, task.ID)[0]
	setRunning(t, s, &root)
	return s, e, task, root, a
}
func assessmentWorker(t *testing.T, e *Engine, root Run, agent, stage string) Run {
	raw, err := call(t, e, root, "adc_delegate", map[string]any{"Agent": agent, "Title": "Assess " + agent, "Prompt": "Use fixture evidence for " + agent, "AttentionStage": stage})
	must(t, err)
	var r Run
	must(t, json.Unmarshal([]byte(raw), &r))
	setRunning(t, e.Store, &r)
	return r
}
func passAttentionRun(t *testing.T, e *Engine, r Run) {
	r.State = "complete"
	must(t, e.Store.Put("run", r.Org, r.Task, r.State, r.ID, r))
	rev := Review{ID: ID(), Org: r.Org, Task: r.Task, Target: r.ID, Revision: e.revision(r), Model: "claude-opus-5", Verdict: "pass"}
	must(t, e.Store.Put("review", r.Org, r.Task, "pass", rev.ID, rev))
}
func TestAttentionReplacementPreservesFundingCadenceAndCoalesces(t *testing.T) {
	s, e, old, at := scheduleFixture(t)
	a, err := s.saveArea(Area{Org: old.Org, Owner: "dev", Name: "NAS", Intent: "Observe existing scope"}, 0, "human:owner")
	must(t, err)
	p := WorkProposal{ID: "replace-proposal", Org: old.Org, Title: "Bounded weekly observation", Cadence: old.Cadence, Attention: &AttentionPolicy{Areas: []string{a.ID}, Scan: 2, Investigation: 4, Review: 3, MaxProposals: 1, Minutes: 10, Concurrent: 2, ReplacesSchedule: old.ID, ReplacesRevision: old.Revision}}
	template := old.Template
	next, writes, err := e.approveSchedule(p, template, old.Scope, at.Add(time.Minute))
	must(t, err)
	var before StandingSchedule
	must(t, s.Get(old.ID, &before))
	if before.State != "active" {
		t.Fatal("replacement mutated before commit")
	}
	must(t, s.Batch(writes...))
	must(t, s.Get(old.ID, &before))
	if before.State != "replaced" || next.NextAt != old.NextAt || next.Template.Account != old.Template.Account || next.Template.Creator != old.Template.Creator || next.Scope != old.Scope || next.Template.Attention.Owners[a.ID] != "dev" {
		t.Fatal("replacement lost snapshot")
	}
	if _, _, err = e.approveSchedule(p, template, old.Scope, at); err == nil {
		t.Fatal("replacement replay accepted")
	}
	e.dispatchSchedules(at.Add(4 * time.Hour))
	must(t, s.Get(next.ID, &next))
	first := next.LastTask
	var task Assignment
	must(t, s.Get(first, &task))
	if task.AttentionStarted != "" || task.Attention == nil || task.Authority != "observe" {
		t.Fatal("not a fresh bounded read-only occurrence")
	}
	// Budget-stopped assessment and a separate unresolved repair do not prevent a new scan.
	task.State = "paused"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	repair := Assignment{ID: "repair", Org: task.Org, State: "needs input", Title: "Long repair"}
	must(t, s.Put("assignment", repair.Org, "", repair.State, repair.ID, repair))
	dir := s.Dir
	must(t, s.Close())
	s2, err := Open(dir)
	must(t, err)
	defer s2.Close()
	e = NewEngine(s2)
	e.dispatchSchedules(at.Add(24 * time.Hour))
	e.dispatchSchedules(at.Add(24 * time.Hour))
	must(t, s2.Get(next.ID, &next))
	if next.Runs != 2 || next.LastTask == first {
		t.Fatal("catch-up replay or suppressed assessment", next.Runs)
	}
	must(t, s2.Get(repair.ID, &repair))
	if repair.State != "needs input" {
		t.Fatal("assessment reinterpreted repair")
	}
}
func TestAttentionBudgetsClaimsDeadlineAndRevocation(t *testing.T) {
	s, e, task, root, a := attentionFixture(t)
	worker := assessmentWorker(t, e, root, "dev", "scan")
	if e.attentionRunCapacity(task, Run{Task: task.ID, AttentionStage: "scan"}) {
		t.Fatal("concurrency ignored")
	}
	root.State = "waiting"
	worker.State = "queued"
	worker.Activations = task.Attention.Scan
	must(t, s.Batch(Write{"run", root.Org, root.Task, root.State, root.ID, root}, Write{"run", worker.Org, worker.Task, worker.State, worker.ID, worker}))
	if e.attentionRunCapacity(task, worker) {
		t.Fatal("stage budget reset")
	}
	e.boundAttentionCycles(time.Now())
	must(t, s.Get(task.ID, &task))
	if task.State != "paused" {
		t.Fatal("exhausted stage stranded queued work")
	}
	task.State = "running"
	task.AttentionStarted = time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339Nano)
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if s.attentionRunProblem(task) == "" {
		t.Fatal("expired active tools allowed")
	}
	e.boundAttentionCycles(time.Now())
	must(t, s.Get(task.ID, &task))
	if task.State != "paused" {
		t.Fatal("deadline ignored")
	}
	task.AttentionStarted = ""
	a.Owner = "boss"
	_, err := s.saveArea(a, a.Revision, "human:owner")
	must(t, err)
	if s.attentionRunProblem(task) == "" {
		t.Fatal("owner transfer silently changed standing policy")
	}
}
func TestAttentionEvidenceReviewPinsAndProposalBound(t *testing.T) {
	s, e, task, root, a := attentionFixture(t)
	worker := assessmentWorker(t, e, root, "dev", "scan")
	if _, err := s.saveAssessment(root, a.ID, "unknown", "fixture://source", "unknown", 0); err == nil {
		t.Fatal("supervisor impersonated owner")
	}
	v, err := s.saveAssessment(worker, a.ID, "Fixture source contains no restore proof; live health is unknown.", "fixture://source", "unknown", 0)
	must(t, err)
	b, err := s.saveSupervisorBrief(worker, "No change recommended without restore evidence. Existing review remains open.", "fixture://source", 0)
	must(t, err)
	if e.attentionComplete(task) == nil {
		t.Fatal("unreviewed brief finished")
	}
	passAttentionRun(t, e, worker)
	must(t, e.attentionComplete(task))
	oldRevision := e.revision(worker)
	v, err = s.saveAssessment(worker, a.ID, "Updated observation reports a failed restore.", "fixture://new-source", "changed", v.Revision)
	must(t, err)
	if e.revision(worker) == oldRevision || e.attentionComplete(task) == nil {
		t.Fatal("changed evidence retained review")
	}
	passAttentionRun(t, e, worker)
	must(t, e.attentionComplete(task))
	worker.Superseded = true
	must(t, s.Put("run", worker.Org, worker.Task, worker.State, worker.ID, worker))
	if e.attentionComplete(task) == nil {
		t.Fatal("superseded observation satisfies assessment")
	}
	_ = b
	p := proposalInput{Title: "Restore coverage", Rationale: "One shared gap", Scope: "Verify restore coverage", Criteria: "Observed restore proof", Evidence: "fixture://gap", Owner: "dev"}
	first, err := e.proposeWork(root, p)
	must(t, err)
	first.Proposal.State = "declined"
	must(t, s.Put("proposal", task.Org, task.ID, "declined", first.Proposal.ID, first.Proposal))
	again, err := e.proposeWork(root, p)
	must(t, err)
	if !again.Duplicate || again.Proposal.State != "declined" {
		t.Fatal("declined suggestion reproduced")
	}
	p.Title = "Renamed suggestion"
	renamed, err := e.proposeWork(root, p)
	must(t, err)
	if !renamed.Duplicate {
		t.Fatal("renaming recreated declined work")
	}
	p.Scope = "Investigate another unrelated improvement"
	if _, err = e.proposeWork(root, p); err == nil {
		t.Fatal("proposal cap bypassed")
	}
	if _, err = e.registerFollowup(root, followupArgs(a), time.Now()); err == nil {
		t.Fatal("assessment created an unapproved timer")
	}
}
func TestAttentionBriefingIsReadOnlyAndCurrentStatePrecedesNarrative(t *testing.T) {
	s, e, task, root, a := attentionFixture(t)
	worker := assessmentWorker(t, e, root, "dev", "scan")
	_, err := s.saveAssessment(worker, a.ID, "No new observations", "fixture://source", "no-change", 0)
	must(t, err)
	_, err = s.saveSupervisorBrief(worker, "Nothing changed in this dated fixture assessment.", "fixture://source", 0)
	must(t, err)
	passAttentionRun(t, e, worker)
	task.State = "ready"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	o := Obligation{ID: "retained", Org: task.Org, Area: a.ID, Owner: a.Owner, Outcome: "Restore evidence still owed", State: "blocked", Note: "Missing observed restore proof"}
	must(t, s.Put("obligation", o.Org, "", o.State, o.ID, o))
	_, err = s.db.Exec("INSERT INTO sessions VALUES(?, 'owner','2099-01-01T00:00:00Z')", digest("attention-fixture"))
	must(t, err)
	web := NewWeb(s, e, false)
	before := len(taskRuns(s, task.ID))
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/?org=org", nil)
		req.Header.Set("Cookie", "adc_session=attention-fixture")
		out := httptest.NewRecorder()
		web.Handler().ServeHTTP(out, req)
		body := out.Body.String()
		if out.Code != 200 || !strings.Contains(body, o.Outcome) || !strings.Contains(body, "Nothing changed in this dated") {
			t.Fatal("briefing missing current state", out.Code)
		}
		if strings.Index(body, o.Outcome) > strings.Index(body, "Nothing changed in this dated") {
			t.Fatal("stale narrative hides blocker")
		}
	}
	if len(taskRuns(s, task.ID)) != before {
		t.Fatal("rendering created work")
	}
}

func TestAttentionNeverStartedExpiresAndRejectsSpentDelegation(t *testing.T) {
	s, e, task, root, _ := attentionFixture(t)
	root.State = "queued"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	e.boundAttentionCycles(time.Now().Add(21 * time.Minute))
	must(t, s.Get(task.ID, &task))
	if task.State != "paused" {
		t.Fatal("never-started attention suppressed future cycles")
	}
	task.State = "running"
	task.Created = now()
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	setRunning(t, s, &root)
	worker := assessmentWorker(t, e, root, "dev", "scan")
	worker.State = "complete"
	worker.Activations = task.Attention.Scan
	must(t, s.Put("run", worker.Org, worker.Task, worker.State, worker.ID, worker))
	_, err := call(t, e, root, "adc_delegate", map[string]any{"Agent": "dev", "Title": "Another scan", "Prompt": "Different scan", "AttentionStage": "scan"})
	if err == nil {
		t.Fatal("spent stage accepted delegation")
	}
	if len(taskRuns(s, task.ID)) != 2 {
		t.Fatal("rejected delegation still queued work")
	}
	if !e.attentionRunCapacity(task, Run{Task: task.ID, AttentionStage: "investigate"}) {
		t.Fatal("scan exhaustion consumed investigation budget")
	}
}
func TestAttentionWorkerMustPersistObservationBeforeFinishing(t *testing.T) {
	s, e, task, root, a := attentionFixture(t)
	worker := assessmentWorker(t, e, root, "dev", "scan")
	repeated := assessmentWorker(t, e, root, "dev", "scan")
	if repeated.ID != worker.ID {
		t.Fatal("scan retry duplicated a worker after instruction injection")
	}
	if _, err := call(t, e, worker, "adc_finish", map[string]string{"Result": "Found nothing"}); err == nil {
		t.Fatal("unrecorded scan claimed completion")
	}
	_, err := s.saveAssessment(worker, a.ID, "No new observed change", "fixture://source", "no-change", 0)
	must(t, err)
	_, err = call(t, e, worker, "adc_finish", map[string]string{"Result": "No new change; stored observation"})
	must(t, err)
	must(t, s.Get(worker.ID, &worker))
	if worker.State != "complete" || len(e.reviewNeeds(task.ID)) != 1 {
		t.Fatal("stored observation bypassed independent review")
	}
}

func TestAttentionBudgetEditRetainsPreviousApprovalProposal(t *testing.T) {
	s, e, task, root, _ := attentionFixture(t)
	cadence := Cadence{Frequency: "daily", Timezone: "UTC", At: "09:00"}
	proposal, err := e.proposeWork(root, proposalInput{Title: "Daily area review", Rationale: "Retain ownership", Scope: "Observe only", Criteria: "Review observed facts", Evidence: "fixture://scope", Owner: "boss", Cadence: &cadence, Attention: task.Attention})
	must(t, err)
	p := proposal.Proposal
	form := url.Values{"id": {p.ID}, "revision": {strconv.Itoa(p.Revision)}, "action": {"edit"}, "title": {p.Title}, "rationale": {p.Rationale}, "scope": {p.Scope}, "criteria": {p.Criteria}, "evidence": {p.Evidence}, "owner": {p.Owner}, "attention_scan": {"4"}, "attention_investigate": {"6"}, "attention_review": {"4"}, "attention_proposals": {"1"}, "attention_minutes": {"10"}, "attention_concurrent": {"2"}}
	req := httptest.NewRequest("POST", "/proposal-action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	must(t, NewWeb(s, e, false).proposalAction(req, Page{Org: Organization{ID: p.Org}, User: User{ID: task.Creator}}))
	must(t, s.Get(p.ID, &p))
	if p.Attention.Scan != 4 {
		t.Fatal("budget not edited")
	}
	history := list[WorkProposal](s, "proposal-revision", p.Org)
	if len(history) != 1 {
		t.Fatal("previous proposal revision missing")
	}
	for _, v := range history {
		if v.ID == p.ID && v.Attention.Scan != 3 {
			t.Fatal("editing budget rewrote old proposal history")
		}
	}
	// Explicitly choosing one-off removes the recurring policy; approval remains pending.
	updated := proposalInput{ID: p.ID, Revision: p.Revision, Title: p.Title, Rationale: p.Rationale, Scope: p.Scope, Criteria: p.Criteria, Evidence: p.Evidence, Owner: p.Owner, Cadence: &Cadence{}}
	result, err := e.proposeWork(root, updated)
	must(t, err)
	if result.Proposal.Attention != nil || result.Proposal.State != "pending" {
		t.Fatal("one-off refinement retained a recurring policy or auto-approved")
	}
}
