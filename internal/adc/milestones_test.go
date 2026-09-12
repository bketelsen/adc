package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func milestoneFixture(t *testing.T, kind string) (*Store, *Engine, Run, PlanStep) {
	t.Helper()
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Steps = input.Steps[:2]
	input.Start = true
	input.Steps[0].Requirements = []PlanRequirement{{Key: "gate", Kind: kind, Target: "fixture:release/v1", Criteria: "Observe the exact fixture target and record primary evidence"}}
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
func milestonePayload(kind string) milestoneInput {
	return milestoneInput{Requirement: "gate", Kind: kind, Target: "fixture:release/v1", Summary: "Fixture v1 observed in the specified state", Reference: "fixture:primary-observation/123", ObservedAt: now()}
}
func TestMilestoneKindsRequireEvidence(t *testing.T) {
	for _, kind := range []string{"reviewed-code", "merged-pr", "published-release", "verified-canary", "human-evidence"} {
		t.Run(kind, func(t *testing.T) {
			s, e, r, step := milestoneFixture(t, kind)
			if _, err := call(t, e, r, "adc_finish", map[string]any{"Result": "The code review passed"}); err == nil {
				t.Fatal("generic success bypassed", kind)
			}
			if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State == "complete" {
				t.Fatal("missing evidence accepted")
			}
			// Even a legacy/incorrectly persisted complete worker cannot receive a pass.
			r.State = "complete"
			must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			var review Run
			must(t, s.Get(step.Review, &review))
			if !e.prepareReview(&review) {
				t.Fatal("review not ready")
			}
			review.State = "running"
			must(t, s.Put("run", review.Org, review.Task, review.State, review.ID, review))
			if _, err := call(t, e, review, "adc_review", map[string]any{"Verdict": "pass", "Findings": "Generic review"}); err == nil {
				t.Fatal("review bypassed missing milestone")
			}
			planDispatch(e)
			if planStepByKey(t, s, "R2").Run != "" {
				t.Fatal("dependent dispatched without required evidence")
			}
		})
	}
}
func TestMilestoneObservationReviewAndInvalidation(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "published-release")
	input := milestonePayload("published-release")
	for _, change := range []func(*milestoneInput){func(p *milestoneInput) { p.Kind = "merged-pr" }, func(p *milestoneInput) { p.Target = "fixture:wrong" }, func(p *milestoneInput) { p.ObservedAt = "2999-01-01T00:00:00Z" }, func(p *milestoneInput) { p.Reference = "" }, func(p *milestoneInput) { p.Revision = 9 }} {
		bad := input
		change(&bad)
		if _, err := e.submitMilestone(r, bad, ""); err == nil {
			t.Fatal("invalid evidence accepted", bad)
		}
	}
	if _, err := call(t, e, r, "adc_milestone", input); err != nil {
		t.Fatal(err)
	}
	firstPin := e.revision(r)
	completePlanWorker(t, e, step)
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run != "" {
		t.Fatal("unreviewed evidence unlocked step")
	}
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	oldDependent := planStepByKey(t, s, "R2").Run
	if oldDependent == "" {
		t.Fatal("reviewed release did not unlock dependent")
	}
	// Reopen the author to correct an observation, as normal steering/review can do.
	must(t, s.Get(r.ID, &r))
	r.State = "running"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	input.Revision = 1
	input.Summary = "Corrected fixture observation"
	_, err := e.submitMilestone(r, input, "")
	must(t, err)
	var reopenedReviewer Run
	must(t, s.Get(step.Review, &reopenedReviewer))
	if reopenedReviewer.State != "waiting" {
		t.Fatal("evidence change failed to reopen reviewer")
	}
	if e.revision(r) == firstPin {
		t.Fatal("evidence excluded from revision")
	}
	completePlanWorker(t, e, step)
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State == "complete" {
		t.Fatal("old review survived changed evidence")
	}
	var stale Run
	must(t, s.Get(oldDependent, &stale))
	if e.planAllowsDispatch(stale) {
		t.Fatal("stale dependent dispatched")
	}
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	current := planStepByKey(t, s, "R2")
	if current.Run == oldDependent || len(current.Attempts) != 1 {
		t.Fatal("new evidence did not supersede dependent", current)
	}
	if len(list[MilestoneEvidence](s, "milestone-evidence-history", r.Org)) != 1 {
		t.Fatal("evidence history lost")
	}
	// A stale packet cannot overwrite the newer record, including after engine recreation.
	e = NewEngine(s)
	input.Revision = 1
	if _, err = e.submitMilestone(r, input, ""); err == nil {
		t.Fatal("stale evidence accepted after recovery")
	}
}
func TestMilestoneHumanBoundaryAndResume(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "human-evidence")
	input := milestonePayload("human-evidence")
	if _, err := call(t, e, r, "adc_milestone", input); err == nil {
		t.Fatal("agent impersonated human")
	}
	_, err := call(t, e, r, "adc_wait", map[string]any{})
	must(t, err)
	// No activation while the human input is absent, even with a fresh engine.
	e = NewEngine(s)
	e.runActivation = func(_ context.Context, active Run, _ Assignment, _ Account) {
		t.Errorf("unexpected activation %s", active.ID)
	}
	var root Run
	must(t, s.Get(s.taskPlan(r.Task).Supervisor, &root))
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	e.tick(context.Background())
	e.wg.Wait()
	must(t, s.Get(r.ID, &r))
	if r.State != "waiting" {
		t.Fatal("human waiter consumed capacity")
	}
	var waitingTask Assignment
	must(t, s.Get(r.Task, &waitingTask))
	if waitingTask.State != "needs input" {
		t.Fatal("human evidence request hidden on work board", waitingTask.State)
	}
	oldAuthority, oldTools := r.Authority, strings.Join(r.Tools, ",")
	_, err = e.submitMilestone(r, input, "owner")
	must(t, err)
	must(t, s.Get(r.ID, &r))
	if r.State != "queued" || r.Authority != oldAuthority || strings.Join(r.Tools, ",") != oldTools {
		t.Fatal("human evidence did not safely wake worker")
	}
	completePlanWorker(t, e, step)
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	dependent := planStepByKey(t, s, "R2")
	completePlanWorker(t, e, dependent)
	reviewPlanStep(t, e, dependent, "pass")
	planDispatch(e)
	root.State = "running"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	_, err = call(t, e, root, "adc_finish", map[string]any{"Result": "Fixture all gates passed"})
	must(t, err)
	// Human withdrawal reopens ready assignments atomically and invalidates all downstream gates.
	must(t, s.Get(r.ID, &r))
	input.Revision = 1
	input.Withdraw = true
	_, err = e.submitMilestone(r, input, "owner")
	must(t, err)
	var task Assignment
	must(t, s.Get(r.Task, &task))
	must(t, s.Get(root.ID, &root))
	must(t, s.Get(r.ID, &r))
	if task.State == "ready" || root.State == "complete" || r.State != "queued" {
		t.Fatal("withdrawal left a completed assignment")
	}
	if e.inspectPlan(s.taskPlan(r.Task)).State != "active" {
		t.Fatal("withdrawal left plan complete")
	}
	var child Run
	must(t, s.Get(dependent.Run, &child))
	if e.planAllowsDispatch(child) {
		t.Fatal("withdrawal left child usable")
	}
	input.Revision = 2
	input.Withdraw = false
	_, err = e.submitMilestone(r, input, "owner")
	must(t, err)
	completePlanWorker(t, e, step)
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run == dependent.Run {
		t.Fatal("dependent not regenerated")
	}
}
func TestMilestoneHTTPAndOwnership(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "human-evidence")
	transcriptLogin(t, s)
	w := NewWeb(s, e, false)
	p := s.taskPlan(r.Task)
	values := url.Values{"task": {r.Task}, "revision": {fmt.Sprint(p.Revision)}, "action": {"milestone"}, "run": {r.ID}, "requirement": {"gate"}, "kind": {"human-evidence"}, "target": {"fixture:release/v1"}, "evidence_revision": {"0"}, "summary": {"Observed fixture"}, "reference": {"fixture:source"}, "observed_at": {now()}}
	form := func() *http.Request {
		req := httptest.NewRequest("POST", "/plan-action?org=org", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}
	req := form()
	req.AddCookie(&http.Cookie{Name: "adc_session", Value: "transcript-fixture"})
	rw := httptest.NewRecorder()
	w.Handler().ServeHTTP(rw, req)
	if rw.Code != http.StatusForbidden {
		t.Fatal("CSRF bypass", rw.Code)
	}
	if err := w.executionPlanAction(form(), Page{Org: Organization{ID: "foreign"}, User: User{ID: "owner"}}); err == nil {
		t.Fatal("cross-org evidence write")
	}
	page := Page{Org: Organization{ID: "org"}, User: User{ID: "owner"}}
	values.Set("run", step.Review)
	if err := w.executionPlanAction(form(), page); err == nil {
		t.Fatal("reviewer wrote owner evidence")
	}
	values.Set("run", r.ID)
	must(t, w.executionPlanAction(form(), page))
	if err := w.executionPlanAction(form(), page); err == nil {
		t.Fatal("stale form overwrote evidence")
	}
	var task Assignment
	must(t, s.Get(r.Task, &task))
	task.State = "cancelled"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	values.Set("evidence_revision", "1")
	if err := w.executionPlanAction(form(), page); err == nil {
		t.Fatal("cancelled assignment resurrected")
	}
}
func TestMilestoneRequirementValidation(t *testing.T) {
	_, e, _, root := fixture(t)
	for _, requirements := range [][]PlanRequirement{
		{{Key: "a", Kind: "success", Target: "x", Criteria: "y"}},
		{{Key: "a", Kind: "published-release", Criteria: "y"}},
		{{Key: "a", Kind: "published-release", Target: "x"}},
		{{Key: "a", Kind: "published-release", Target: "x", Criteria: "y"}, {Key: "a", Kind: "human-evidence", Target: "x", Criteria: "y"}},
	} {
		input := planFixtureInput()
		input.Steps[0].Requirements = requirements
		if _, err := e.saveExecutionPlan(root, input); err == nil {
			t.Fatal("bad requirement accepted", requirements)
		}
	}
}

func TestMilestoneStatusExposesCurrentEvidence(t *testing.T) {
	_, e, r, _ := milestoneFixture(t, "published-release")
	_, err := e.submitMilestone(r, milestonePayload("published-release"), "")
	must(t, err)
	response, err := call(t, e, r, "adc_status", map[string]any{})
	must(t, err)
	var status struct{ Plan ExecutionPlan }
	must(t, json.Unmarshal([]byte(response), &status))
	if len(status.Plan.Steps) == 0 || len(status.Plan.Steps[0].Evidence) != 1 || status.Plan.Steps[0].Evidence[0].Revision != 1 {
		t.Fatal("status omitted current requirement evidence", response)
	}
}

func TestMilestoneChangeDuringIndependentReview(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "human-evidence")
	input := milestonePayload("human-evidence")
	_, err := e.submitMilestone(r, input, "owner")
	must(t, err)
	completePlanWorker(t, e, step)
	var reviewer Run
	must(t, s.Get(step.Review, &reviewer))
	if !e.prepareReview(&reviewer) {
		t.Fatal("target not ready")
	}
	reviewer.State = "running"
	must(t, s.Put("run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer))
	oldPin := reviewer.ReviewedRevision
	must(t, s.Get(r.ID, &r))
	input.Revision = 1
	input.Summary = "A newer observation during independent review"
	_, err = e.submitMilestone(r, input, "owner")
	must(t, err)
	// Even if the author finishes before the old reviewer replies, that old verdict is stale.
	completePlanWorker(t, e, step)
	result, err := call(t, e, reviewer, "adc_review", map[string]any{"Verdict": "pass", "Findings": "Old observation passed"})
	must(t, err)
	if !strings.Contains(result, "Review target changed") || len(taskReviews(s, r.Task)) != 0 {
		t.Fatal("stale in-flight pass recorded", result)
	}
	must(t, s.Get(reviewer.ID, &reviewer))
	if reviewer.State != "waiting" || e.revision(r) == oldPin {
		t.Fatal("stale reviewer was not returned for a new inspection")
	}
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run == "" {
		t.Fatal("fresh review failed to resume dependents")
	}
}
