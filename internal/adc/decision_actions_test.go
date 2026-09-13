package adc

import (
	"strings"
	"testing"
)

func actionFixture(t *testing.T) (*Store, *Engine, Run, Decision) {
	s, e, r, _ := milestoneFixture(t, "merged-pr")
	transcriptLogin(t, s)
	var root Run
	must(t, s.Get(r.Parent, &root))
	r.State = "waiting"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	d, err := e.submitDecision(root, decisionInput{Brief: "Merge the reviewed fixture PR; the agent will verify and record it.", Question: "Concrete reviewed fixture change with passing checks.", Action: &decisionActionInput{Run: r.ID, Action: "Squash merge fixture PR #1 at its reviewed head", Target: "fixture/project#1", Reference: "fixture:pr/1", Validation: "Verify exact merge tree and successful checks", Rollback: "Revert reviewed change through a separate approved PR", Requirement: "gate"}})
	must(t, err)
	return s, e, r, d
}
func TestApprovedActionExecutesAndRecordsOnceAcrossRecovery(t *testing.T) {
	s, e, r, d := actionFixture(t)
	w := NewWeb(s, e, false)
	must(t, decisionPost(t, w, d, "approve", "", nil))
	must(t, s.Get(d.ID, &d))
	must(t, s.Get(r.ID, &r))
	if d.Action.State != "approved" || r.State != "queued" || !strings.Contains(r.Prompt, "adc_action_result") {
		t.Fatal("approval did not dispatch")
	}
	if s.milestoneEvidence(r.ID, "gate").ID != "" {
		t.Fatal("approval fabricated merge")
	}
	// Restart between approval and execution; outcome remains pending and bound.
	e = NewEngine(s)
	setRunning(t, s, &r)
	p := actionResultInput{Decision: d.ID, Summary: "Fixture API reports merged, exact reviewed tree", Reference: "fixture:pr/1/merged", ObservedAt: now()}
	_, err := call(t, e, r, "adc_action_result", p)
	must(t, err)
	v := s.milestoneEvidence(r.ID, "gate")
	if v.Revision != 1 || v.Actor != "run:"+r.ID || !strings.Contains(v.Reference, d.ID) {
		t.Fatal(v)
	}
	// Lost outcome response + retry must not replace the recorded observation.
	e = NewEngine(s)
	setRunning(t, s, &r)
	_, err = call(t, e, r, "adc_action_result", p)
	must(t, err)
	if s.milestoneEvidence(r.ID, "gate").Revision != 1 {
		t.Fatal("duplicated external outcome")
	}
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State == "complete" {
		t.Fatal("action bypassed final review")
	}
	if decisionPost(t, w, d, "approve", "", nil) == nil {
		t.Fatal("approved twice")
	}
}
func TestActionRejectRefineStaleAndForeign(t *testing.T) {
	for _, change := range []string{"reject", "refine", "stale", "foreign", "rollback"} {
		t.Run(change, func(t *testing.T) {
			s, e, r, d := actionFixture(t)
			w := NewWeb(s, e, false)
			if change == "reject" || change == "refine" {
				must(t, decisionPost(t, w, d, change, "Use a different approach", nil))
				must(t, s.Get(d.ID, &d))
				if d.Action.State == "approved" {
					t.Fatal("unauthorized dispatch")
				}
				return
			}
			if change == "stale" {
				doc := Document{ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, Content: "changed candidate", Revision: 1}
				must(t, s.Put("document", doc.Org, doc.Task, "", doc.ID, doc))
				if decisionPost(t, w, d, "approve", "", nil) == nil {
					t.Fatal("stale action approved")
				}
				return
			}
			if change == "rollback" {
				_, err := s.db.Exec(`CREATE TRIGGER reject_action BEFORE UPDATE ON records WHEN NEW.kind='decision' BEGIN SELECT RAISE(ABORT,'fixture'); END`)
				must(t, err)
				if decisionPost(t, w, d, "approve", "", nil) == nil {
					t.Fatal("database error ignored")
				}
				must(t, s.Get(r.ID, &r))
				if r.State != "waiting" {
					t.Fatal("partial dispatch")
				}
				return
			}
			must(t, decisionPost(t, w, d, "approve", "", nil))
			other := r
			other.ID = "foreign"
			other.State = "running"
			must(t, s.Put("run", other.Org, other.Task, other.State, other.ID, other))
			if _, err := e.recordActionResult(other, actionResultInput{Decision: d.ID, Summary: "fake", Reference: "fake", ObservedAt: now()}); err == nil {
				t.Fatal("wrong executor recorded outcome")
			}
		})
	}
}

func TestActionOutcomeSurvivesEvidenceChanges(t *testing.T) {
	s, e, r, d := actionFixture(t)
	must(t, decisionPost(t, NewWeb(s, e, false), d, "approve", "", nil))
	// An execution-time observation can change the evidence after approval. The
	// action happened; recording that fact must not ask for approval again.
	doc := Document{ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, Content: "Observed post-execution output", Revision: 1}
	must(t, s.Put("document", doc.Org, doc.Task, "", doc.ID, doc))
	setRunning(t, s, &r)
	_, err := call(t, e, r, "adc_action_result", actionResultInput{Decision: d.ID, Summary: "Observed exact approved merge", Reference: "fixture:merge", ObservedAt: now()})
	must(t, err)
	must(t, s.Get(d.ID, &d))
	if d.Action.State != "done" || d.Action.OutcomeArtifact == d.Action.Artifact || s.milestoneEvidence(r.ID, "gate").Revision != 1 {
		t.Fatal("post-action observation lost or reprompted")
	}
}
