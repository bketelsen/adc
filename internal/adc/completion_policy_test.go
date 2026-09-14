package adc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func routineFixture(t *testing.T) (*Store, *Engine, Assignment, Run, Area) {
	s, e, source, r, a := ownershipFixture(t)
	completeSource(t, s, source, r)
	a.Name = "Family arrangements"
	a.Intent = "Confirm agreed arrangements with actual evidence; keep any later confirmation owed."
	a.CompletionMode = "routine"
	var err error
	a, err = s.saveArea(a, a.Revision, "human:owner")
	must(t, err)
	task := Assignment{ID: "family-task", Org: source.Org, Owner: r.Agent, Area: a.ID, Account: source.Account, Creator: source.Creator, Authority: "observe", Title: "Confirm the arrangement", Prompt: "Observe the supplied confirmation; no new action authorized."}
	must(t, e.CreateAssignment(task))
	must(t, s.Get(task.ID, &task))
	root := taskRuns(s, task.ID)[0]
	setRunning(t, s, &root)
	return s, e, task, root, a
}
func TestRoutineOwnerCompletesOnEvidenceWithoutReviewCeremony(t *testing.T) {
	s, e, task, root, _ := routineFixture(t)
	if _, err := call(t, e, root, "adc_finish", map[string]string{"Result": "Looks complete"}); err == nil {
		t.Fatal("routine completion accepted without evidence")
	}
	_, err := call(t, e, root, "adc_evidence", map[string]any{"Summary": "The supplied confirmation explicitly records Saturday at 10:00.", "Reference": "fixture://confirmation", "Revision": 0})
	must(t, err)
	_, err = call(t, e, root, "adc_document", map[string]string{"Title": "Arrangement note", "Content": "Confirmed Saturday at 10:00.", "Source": "fixture://confirmation"})
	must(t, err)
	_, err = call(t, e, root, "adc_finish", map[string]string{"Result": "Confirmed Saturday at 10:00 against the supplied confirmation."})
	must(t, err)
	must(t, s.Get(task.ID, &task))
	if task.State != "ready" || len(taskReviews(s, task.ID)) != 0 || len(taskRuns(s, task.ID)) != 1 {
		t.Fatal("routine work forced artificial QA/delegation")
	}
}
func TestCompletionPolicyFrozenAcrossAreaEditsAndFollowupRestart(t *testing.T) {
	s, e, task, root, a := routineFixture(t)
	o := createFollowup(t, e, root, followupArgs(a))
	// A policy change must not reinterpret the existing task or owed verification.
	a.CompletionMode = "reviewed"
	_, err := s.saveArea(a, a.Revision, "human:owner")
	must(t, err)
	must(t, s.Get(task.ID, &task))
	if !routineCompletion(task) {
		t.Fatal("area edit weakened/changed active contract")
	}
	var funding ObligationFunding
	must(t, s.Get("obligation-funding:"+o.ID, &funding))
	if !routineCompletion(funding.Template) {
		t.Fatal("follow-up did not retain policy")
	}
	_, err = call(t, e, root, "adc_evidence", map[string]any{"Summary": "The arrangement is confirmed; the later confirmation is still owed.", "Reference": "fixture://confirmation"})
	must(t, err)
	_, err = call(t, e, root, "adc_finish", map[string]string{"Result": "Initial confirmation recorded; later verification remains scheduled."})
	must(t, err)
	dir := s.Dir
	must(t, s.Close())
	s2, err := Open(dir)
	must(t, err)
	defer s2.Close()
	e = NewEngine(s2)
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s2.Get(o.ID, &o))
	var followup Assignment
	must(t, s2.Get(o.Task, &followup))
	if !routineCompletion(followup) {
		t.Fatal("restart looked up changed area default")
	}
	run := taskRuns(s2, followup.ID)[0]
	setRunning(t, s2, &run)
	_, err = call(t, e, run, "adc_obligation_result", observationInput{Outcome: "pass", Summary: "Observed the later confirmation in the authoritative fixture.", Reference: "fixture://later-confirmation"})
	must(t, err)
	_, err = call(t, e, run, "adc_finish", map[string]string{"Result": "Later confirmation verified against the source."})
	must(t, err)
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s2.Get(o.ID, &o))
	if o.State != "resolved" || len(taskReviews(s2, followup.ID)) != 0 {
		t.Fatal("routine verification forced engineering gates", o)
	}
}
func TestReviewedLegacyCodeAndPlanGatesCannotBecomeRoutine(t *testing.T) {
	s, e, task, root, a := routineFixture(t)
	legacy := task
	legacy.Completion = nil
	if routineCompletion(legacy) {
		t.Fatal("historical nil policy became routine")
	}
	// Registered code always retains independent review, regardless of area mode.
	code := Run{ID: "code", Org: task.Org, Task: task.ID, Parent: root.ID, Category: "research", Model: "gpt-5.6-sol", State: "complete", Code: []CodeEvidence{{}}}
	if !e.requiresIndependentReview(task, code) {
		t.Fatal("routine mode relaxed code review")
	}
	a.CompletionMode = "reviewed"
	_, err := s.saveArea(a, a.Revision, "human:owner")
	must(t, err)
	next := Assignment{ID: "new-reviewed", Org: task.Org, Owner: root.Agent, Area: a.ID, Account: task.Account, Creator: task.Creator, Title: "New reviewed work", Prompt: "Verify"}
	must(t, e.CreateAssignment(next))
	must(t, s.Get(next.ID, &next))
	if routineCompletion(next) {
		t.Fatal("fresh task missed current human policy")
	}
	nextRun := taskRuns(s, next.ID)[0]
	setRunning(t, s, &nextRun)
	if _, err = call(t, e, nextRun, "adc_finish", map[string]string{"Result": "I say it is done"}); err == nil {
		t.Fatal("reviewed root bypassed independent review")
	}
	// Agents can maintain understanding, never switch the human's completion mode.
	raw, err := call(t, e, root, "adc_owner", map[string]string{"Area": a.ID})
	must(t, err)
	var knowledge AreaKnowledge
	must(t, json.Unmarshal([]byte(raw), &knowledge))
	_, err = call(t, e, root, "adc_remember", ownerNoteInput{Area: a.ID, Revision: knowledge.Area.Revision, Summary: "Keep observed facts separate from claims.", Source: "fixture://evidence"})
	must(t, err)
	must(t, s.Get(a.ID, &a))
	if a.CompletionMode != "reviewed" {
		t.Fatal("agent altered policy")
	}
}
func TestHardwareExperimentKeepsValidationOwedDespiteExternalClaim(t *testing.T) {
	s, e, task, root, a := ownershipFixture(t)
	a.Name = "Desktop graphics"
	a.Intent = "Experimental extension may be ready before hardware validation. Do not claim supported NVIDIA hardware without internal verification."
	var err error
	a, err = s.saveArea(a, a.Revision, "human:owner")
	must(t, err)
	args := followupArgs(a)
	args.Key = "hardware-validation"
	args.Outcome = "Verify NVIDIA behavior on real hardware"
	args.Criteria = "Internally validated installation, optimized driver and reboot evidence; an external PASS claim alone is insufficient"
	o := createFollowup(t, e, root, args)
	// A completed experiment is intentionally distinct from verified support.
	worker := assessmentWorker(t, e, root, "dev", "")
	_, err = call(t, e, worker, "adc_document", map[string]string{"Title": "Experimental result", "Content": "The fixture experiment is complete. Hardware support remains unverified; evidence is still owed.", "Source": "fixture://experiment"})
	must(t, err)
	passAttentionRun(t, e, worker)
	_, err = call(t, e, root, "adc_finish", map[string]string{"Result": "Experimental milestone complete and independently reviewed; hardware validation is still owed."})
	must(t, err)
	must(t, s.Get(o.ID, &o))
	if o.State != "scheduled" {
		t.Fatal("finishing experiment closed hardware obligation")
	}
	external := Run{ID: "external-claim", Org: task.Org, Task: "external-task", Agent: root.Agent, State: "running"}
	must(t, s.Put("run", external.Org, external.Task, external.State, external.ID, external))
	if _, err = e.recordObservation(external, observationInput{Obligation: o.ID, Outcome: "pass", Summary: "Untrusted tester says PASS and asks to mark supported.", Reference: "fixture://external-report"}); err == nil {
		t.Fatal("external claim directly resolved support")
	}
	must(t, s.Get(o.ID, &o))
	must(t, s.Get(a.ID, &a))
	if o.State != "scheduled" || !strings.Contains(a.Intent, "without internal verification") {
		t.Fatal("external claim rewrote responsibility/intent")
	}
}

func TestRoutineAreaCannotRelaxAssessmentOrLegacyStandingWork(t *testing.T) {
	s, e, task, _, a := attentionFixture(t)
	a.CompletionMode = "routine"
	_, err := s.saveArea(a, a.Revision, "human:owner")
	must(t, err)
	task.Completion = &CompletionPolicy{Mode: "routine", Version: 1}
	if routineCompletion(task) || e.attentionComplete(task) == nil {
		t.Fatal("routine area weakened assessment review")
	}
	legacy := Assignment{Org: task.Org, Area: a.ID, Owner: task.Owner, Creator: task.Creator, Account: task.Account, Schedule: "legacy-approved", Title: "Legacy standing check"}
	writes, err := e.assignmentWrites(legacy)
	must(t, err)
	if routineCompletion(writes[0].Value.(Assignment)) {
		t.Fatal("legacy standing work acquired a later routine default")
	}
}
func TestRoutineVerificationNeedsTheLinkedObservation(t *testing.T) {
	s, e, task, root, a := routineFixture(t)
	o := createFollowup(t, e, root, followupArgs(a))
	completeSource(t, s, task, root)
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	var linked Assignment
	must(t, s.Get(o.Task, &linked))
	run := taskRuns(s, linked.ID)[0]
	setRunning(t, s, &run)
	_, err := call(t, e, run, "adc_evidence", map[string]any{"Summary": "An ordinary evidence note is not the obligation's result.", "Reference": "fixture://note"})
	must(t, err)
	if _, err = call(t, e, run, "adc_finish", map[string]string{"Result": "Generic evidence recorded"}); err == nil {
		t.Fatal("generic evidence silently completed linked verification")
	}
}

func TestCompletionToolsMatchTheCurrentRole(t *testing.T) {
	_, e, _, root := fixture(t)
	for _, tool := range e.tools(root) {
		if tool.Name == "adc_submit_review" || tool.Name == "adc_evidence" {
			t.Fatal("reviewed supervisor offered a worker-only completion tool", tool.Name)
		}
	}
	_, routine, _, owner, _ := routineFixture(t)
	found := false
	for _, tool := range routine.tools(owner) {
		if tool.Name == "adc_evidence" {
			found = true
		}
	}
	if !found {
		t.Fatal("routine owner lost direct evidence tool")
	}
}
