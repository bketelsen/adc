package adc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdvisoryPreflightOwnsDirectoryAndRecovers(t *testing.T) {
	s, e, r, _ := preflightPlan(t)
	r.Preflight = &PreflightSpec{Directories: []string{"build"}, Ports: []string{"http"}}
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	ready := checkPreflight(t, e, r)
	if ready.State != "ready" {
		t.Fatalf("%+v", ready)
	}
	resources := s.runResources(r.ID)
	expected := filepath.Join(r.Workspace, ".adc-test", "build")
	if resources.Directories["build"] != expected || resources.Ports["http"] == 0 {
		t.Fatal(resources)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatal(err)
	}
	fresh := NewEngine(s)
	must(t, fresh.prepareRunResources(context.Background(), r))
	if s.runResources(r.ID).Ports["http"] != resources.Ports["http"] {
		t.Fatal("resource changed on retry")
	}
	other := r
	other.ID = "other-run"
	other.Workspace = filepath.Join(s.Dir, "workspaces", r.Org, r.Task, other.ID)
	must(t, s.Put("run", other.Org, other.Task, other.State, other.ID, other))
	must(t, fresh.prepareRunResources(context.Background(), other))
	if s.runResources(other.ID).Directories["build"] == expected || s.runResources(other.ID).Ports["http"] == resources.Ports["http"] {
		t.Fatal("shared resources")
	}
	must(t, os.RemoveAll(filepath.Join(r.Workspace, ".adc-test")))
	outside := t.TempDir()
	must(t, os.Symlink(outside, filepath.Join(r.Workspace, ".adc-test")))
	if fresh.prepareRunResources(context.Background(), r) == nil {
		t.Fatal("followed escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "build")); !os.IsNotExist(err) {
		t.Fatal("wrote outside workspace")
	}
}

func TestCandidateReviewCorrectionDeliveryAndFinalReview(t *testing.T) {
	s, e, r, _ := milestoneFixture(t, "merged-pr")
	step := s.taskPlan(r.Task).Steps[0]
	setRunning(t, s, &r)
	_, err := call(t, e, r, "adc_submit_review", map[string]string{"Result": "Candidate v1 validated"})
	must(t, err)
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State != "review" {
		t.Fatal("candidate hidden")
	}
	reviewPlanStep(t, e, step, "changes")
	must(t, s.Get(r.ID, &r))
	if r.State != "queued" {
		t.Fatal(r.State)
	}
	setRunning(t, s, &r)
	_, err = call(t, e, r, "adc_submit_review", map[string]string{"Result": "Candidate v2 corrected and validated"})
	must(t, err)
	// Reconstruct the engine between handoff and review, as at deployment.
	e = NewEngine(s)
	reviewPlanStep(t, e, step, "pass")
	must(t, s.Get(r.ID, &r))
	if r.State != "queued" || !eCandidateReviewed(s, r) || e.hasCurrentReview(r, taskReviews(s, r.Task)) {
		t.Fatal("candidate pass did not resume delivery or bypassed final review")
	}
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State == "complete" {
		t.Fatal("candidate bypassed external gate")
	}
	setRunning(t, s, &r)
	_, err = e.submitMilestone(r, milestonePayload("merged-pr"), "")
	must(t, err)
	_, err = call(t, e, r, "adc_finish", map[string]string{"Result": "Candidate merged and observed"})
	must(t, err)
	planDispatch(e)
	reviewPlanStep(t, e, step, "pass")
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State != "complete" {
		t.Fatal("final review did not complete step")
	}
}

func TestWaitKeepsIndependentWorkMoving(t *testing.T) {
	s, e, _, root := fixture(t)
	blocked := Run{ID: "blocked", Org: root.Org, Task: root.Task, Parent: root.ID, State: "blocked"}
	working := blocked
	working.ID = "working"
	working.State = "queued"
	for _, r := range []Run{blocked, working} {
		must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	}
	_, err := call(t, e, root, "adc_wait", map[string]any{})
	must(t, err)
	must(t, s.Get(root.ID, &root))
	if root.State != "waiting" {
		t.Fatal("supervisor consumed slot")
	}
}
func TestRepairStepPreservesGatesAndEvidence(t *testing.T) {
	s, e, r, _ := milestoneFixture(t, "merged-pr")
	p := s.taskPlan(r.Task)
	r.State = "blocked"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	var root Run
	must(t, s.Get(p.Supervisor, &root))
	before := p.Steps[0].Requirements[0]
	repaired, err := e.repairStep(root, stepRepairInput{Key: p.Steps[0].Key, Revision: p.Revision, Notes: "Agent performs already approved action; human only approves.", Preflight: &PreflightSpec{Directories: []string{"build"}}})
	must(t, err)
	if repaired.Revision != p.Revision+1 || repaired.Steps[0].Run != r.ID || repaired.Steps[0].Requirements[0].Kind != before.Kind || !strings.Contains(repaired.Steps[0].Prompt, "Agent performs") {
		t.Fatal("repair lost gates or run")
	}
	if _, err = e.repairStep(root, stepRepairInput{Key: p.Steps[0].Key, Revision: p.Revision, Notes: "stale"}); err == nil {
		t.Fatal("stale plan repair")
	}
}
func TestRuntimeToolCapabilities(t *testing.T) {
	_, e, _, r := fixture(t)
	for _, tool := range e.tools(r) {
		if tool.Name == "adc_validate" || tool.Name == "adc_review" {
			t.Fatal("unusable tool exposed", tool.Name)
		}
	}
}

func TestRepairCannotRaceCandidateReview(t *testing.T) {
	s, e, r, _ := milestoneFixture(t, "merged-pr")
	_, err := call(t, e, r, "adc_submit_review", map[string]string{"Result": "Ready candidate"})
	must(t, err)
	var root Run
	must(t, s.Get(r.Parent, &root))
	p := s.taskPlan(r.Task)
	if _, err = e.repairStep(root, stepRepairInput{Key: p.Steps[0].Key, Revision: p.Revision, Notes: "new guidance"}); err == nil {
		t.Fatal("author restarted during candidate review")
	}
	must(t, s.Get(r.ID, &r))
	if r.State != "waiting" || r.CandidateRevision == "" {
		t.Fatal("candidate altered")
	}
}

func TestInvalidPreflightReturnsOnceToSupervisor(t *testing.T) {
	s, e, r, _ := preflightPlan(t)
	r.Preflight = &PreflightSpec{Directories: []string{"../escape"}}
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	ready := checkPreflight(t, e, r)
	must(t, s.Get(r.ID, &r))
	var root Run
	must(t, s.Get(r.Parent, &root))
	if ready.State != "blocked" || r.State != "blocked" || root.State != "queued" || !strings.Contains(root.Prompt, "adc_repair_step") {
		t.Fatal("invalid definition left in blind retry", ready.State, r.State, root.State)
	}
}
func TestPlanRelatedWorkAndDecisionsRemainAProjection(t *testing.T) {
	s, e, r, step := milestoneFixture(t, "merged-pr")
	extra := Run{ID: "extra-review", Org: r.Org, Task: r.Task, Parent: r.Parent, ReviewOf: r.ID, Title: "Recheck candidate", State: "running"}
	must(t, s.Put("run", r.Org, r.Task, extra.State, extra.ID, extra))
	d := Decision{ID: "current-question", Org: r.Org, Task: r.Task, Run: extra.ID, State: "pending", Brief: "Approve the next concrete action"}
	must(t, s.Put("decision", r.Org, r.Task, d.State, d.ID, d))
	view := e.inspectPlan(s.taskPlan(r.Task))
	got := view.Steps[0]
	if len(got.Related) != 1 || got.Related[0].ID != extra.ID || len(got.Decisions) != 1 || got.Run != step.Run || got.State == "complete" {
		t.Fatal("related evidence missing or changed completion")
	}
	stored := durablePlan(view)
	if len(stored.Steps[0].Related) != 0 || len(stored.Steps[0].Decisions) != 0 {
		t.Fatal("UI projection persisted")
	}
}
