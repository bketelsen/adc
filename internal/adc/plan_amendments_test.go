package adc

import (
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestHumanCanRemoveInformationGateWithoutFabricatingEvidence(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "human-evidence")
	r.State = "blocked"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	plan := s.taskPlan(r.Task)
	must(t, e.removePlanRequirement(plan, step.Key, "gate", "This information is disproportionate to the project.", "owner"))
	after := s.taskPlan(r.Task)
	if after.Revision != plan.Revision+1 || len(after.Steps[0].Requirements) != 0 || len(plan.Steps[0].Requirements) != 1 {
		t.Fatal("scope or history damaged")
	}
	if len(list[ExecutionPlan](s, "plan-history", r.Org)) != 1 || s.milestoneEvidence(r.ID, "gate").ID != "" {
		t.Fatal("lost history or manufactured evidence")
	}
	must(t, s.Get(r.ID, &r))
	if r.State != "queued" || !strings.Contains(r.Prompt, "do not ask for it again") {
		t.Fatal("worker not resumed with new scope")
	}
	setRunning(t, s, &r)
	_, err := call(t, e, r, "adc_finish", map[string]string{"Result": "The human removed the information requirement; remaining scope is complete."})
	must(t, err)
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State == "complete" {
		t.Fatal("scope change bypassed independent review")
	}
	planDispatch(e)
	reviewPlanStep(t, e, step, "pass")
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State != "complete" {
		t.Fatal("reviewed amended step did not finish")
	}
	if e.removePlanRequirement(plan, step.Key, "gate", "stale", "owner") == nil {
		t.Fatal("stale change accepted")
	}
}

func TestPlanAmendmentBoundaryAndHumanControls(t *testing.T) {
	for _, kind := range []string{"human-evidence", "merged-pr"} {
		t.Run(kind, func(t *testing.T) {
			s, e, r, step := milestoneFixture(t, kind)
			plan := s.taskPlan(r.Task)
			r.State = "blocked"
			must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			w := NewWeb(s, e, false)
			values := url.Values{"task": {r.Task}, "revision": {strconv.Itoa(plan.Revision)}, "action": {"remove-requirement"}, "step": {step.Key}, "requirement": {"gate"}, "reason": {"Not required"}}
			req := httptest.NewRequest("POST", "/plan-action", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			must(t, req.ParseForm())
			if w.executionPlanAction(req, Page{Org: Organization{ID: "foreign"}, User: User{ID: "owner"}}) == nil {
				t.Fatal("cross-org amendment accepted")
			}
			if e.removePlanRequirement(plan, step.Key, "gate", "why", "") == nil {
				t.Fatal("missing actor accepted")
			}
			r.State = "running"
			must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			if e.removePlanRequirement(plan, step.Key, "gate", "why", "owner") == nil {
				t.Fatal("active worker scope changed")
			}
			r.State = "blocked"
			must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			if kind != "human-evidence" && e.removePlanRequirement(plan, step.Key, "gate", "why", "owner") == nil {
				t.Fatal("operational evidence gate removed")
			}
		})
	}
}

func TestPlanAmendmentPreservesPendingExecutorApproval(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "human-evidence")
	r.State = "blocked"
	r.NextAt = "2999-01-01T00:00:00Z"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	d := Decision{ID: "action-hold", Org: r.Org, Task: r.Task, Run: r.Parent, State: "pending", Action: &DecisionAction{decisionActionInput: decisionActionInput{Run: r.ID}}}
	must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
	must(t, e.removePlanRequirement(s.taskPlan(r.Task), step.Key, "gate", "No longer required", "owner"))
	must(t, s.Get(r.ID, &r))
	must(t, s.Get(d.ID, &d))
	if r.State != "waiting" || r.NextAt != "" || d.State != "pending" {
		t.Fatal("pending action or old retry delay mishandled", r.State, r.NextAt, d.State)
	}
}

func TestOmittedStepUnblocksDependentsWithoutEmptyWorkOrReview(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "human-evidence")
	r.State = "waiting"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	p := s.taskPlan(r.Task)
	must(t, e.omitPlanStep(p, step.Key, "This whole exercise is out of scope", "owner"))
	planDispatch(e)
	p = e.inspectPlan(s.taskPlan(r.Task))
	first := p.Steps[0]
	second := p.Steps[1]
	if first.State != "omitted" || second.Run == "" || second.Inputs[first.Key] != omittedRevision(first) {
		t.Fatal("omission did not unblock exact downstream graph", first.State, second.Run, second.Inputs)
	}
	must(t, s.Get(r.ID, &r))
	if r.State != "cancelled" || !r.Superseded || e.planAllowsDispatch(r) {
		t.Fatal("omitted work still dispatches")
	}
	var reviewer Run
	must(t, s.Get(step.Review, &reviewer))
	if reviewer.State != "cancelled" || !reviewer.Superseded {
		t.Fatal("empty exercise still reviewed")
	}
	if len(e.reviewNeeds(r.Task)) != 0 || s.milestoneEvidence(r.ID, "gate").ID != "" {
		t.Fatal("omitted work needs review or gained fake evidence")
	}
	graph := planGraph(p)
	if graph.Omitted != 1 || graph.Complete != 0 {
		t.Fatal("omitted counted as successful", graph)
	}
	var worker Run
	must(t, s.Get(second.Run, &worker))
	setRunning(t, s, &worker)
	_, err := call(t, e, worker, "adc_finish", map[string]string{"Result": "Remaining authorized fixture work completed"})
	must(t, err)
	planDispatch(e)
	reviewPlanStep(t, e, second, "pass")
	if e.inspectPlan(s.taskPlan(r.Task)).State != "complete" {
		t.Fatal("omission prevented remaining plan completion")
	}
	// Omission and cancelled execution survive reconstruction, with no fake review.
	e = NewEngine(s)
	planDispatch(e)
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State != "omitted" {
		t.Fatal("restart lost omission")
	}
}

func TestOmissionRefusesActiveWorkAndAlreadyStartedDependents(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "human-evidence")
	p := s.taskPlan(r.Task)
	if e.omitPlanStep(p, step.Key, "Drop it", "owner") == nil {
		t.Fatal("active worker omitted")
	}
	r.State = "waiting"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	p.Steps[1].Run = "already-started"
	must(t, s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p))
	if e.omitPlanStep(p, step.Key, "Drop it", "owner") == nil {
		t.Fatal("already-started dependent changed")
	}
	p.Steps[1].Run = ""
	must(t, s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p))
	old := p
	p.Revision++
	if e.omitPlanStep(p, step.Key, "Drop it", "owner") == nil {
		t.Fatal("stale revision accepted")
	}
	if e.omitPlanStep(old, step.Key, "Drop it", "") == nil {
		t.Fatal("missing human accepted")
	}
}

func TestSupervisorQuestionDoesNotFreezeOtherAuthorizedPlanSteps(t *testing.T) {
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Steps = input.Steps[:2]
	input.Start = true
	input.Steps[1].DependsOn = nil
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	d := Decision{ID: "root-question", Org: root.Org, Task: root.Task, Run: root.ID, State: "pending", Question: "A separate scope question"}
	must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
	planDispatch(e)
	p := s.taskPlan(root.Task)
	if p.Steps[0].Run == "" || p.Steps[1].Run == "" {
		t.Fatal("root question froze previously authorized graph")
	}
	must(t, s.Get(d.ID, &d))
	if d.State != "pending" {
		t.Fatal("decision implicitly answered")
	}
}
