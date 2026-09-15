package adc

import (
	"strings"
	"testing"
)

func TestUnaskedHumanEvidenceDoesNotKeepSupervisorAsleep(t *testing.T) {
	s, e, worker, _ := milestoneFixture(t, "human-evidence")
	worker.State = "waiting"
	must(t, s.Put("run", worker.Org, worker.Task, worker.State, worker.ID, worker))
	var root Run
	must(t, s.Get(worker.Parent, &root))
	setRunning(t, s, &root)
	if _, err := call(t, e, root, "adc_wait", map[string]any{}); err == nil {
		t.Fatal("unasked human requirement counted as progress")
	}
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	e.recoverStalledSupervisors()
	must(t, s.Get(root.ID, &root))
	if root.State != "queued" || root.WaitAssessment == "" {
		t.Fatal("did not recover supervision")
	}
	must(t, s.Get(worker.ID, &worker))
	if worker.State != "waiting" {
		t.Fatal("recovery authorized worker")
	}
	// Persisted fingerprint prevents a repeated wake after restart.
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	NewEngine(s).recoverStalledSupervisors()
	must(t, s.Get(root.ID, &root))
	if root.State != "waiting" {
		t.Fatal("repeated unchanged recovery")
	}
	// A real human request gives the waiter a path, without granting execution.
	decision := Decision{ID: "asked", Org: root.Org, Task: root.Task, Run: worker.ID, State: "pending", Question: "Which installations should participate?"}
	must(t, s.Put("decision", decision.Org, decision.Task, decision.State, decision.ID, decision))
	_, progressing := e.waitProgress(root)
	if !progressing {
		t.Fatal("actual question was ignored")
	}
}

func TestCoordinationKeepsDescendantAndReviewProgress(t *testing.T) {
	s, e, _, root := fixture(t)
	parent := Run{ID: "parent", Org: root.Org, Task: root.Task, Parent: root.ID, State: "waiting"}
	child := Run{ID: "grandchild", Org: root.Org, Task: root.Task, Parent: parent.ID, State: "queued"}
	for _, r := range []Run{parent, child} {
		must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	}
	_, progress := e.waitProgress(root)
	if !progress {
		t.Fatal("grandchild was ignored")
	}
	child.State = "blocked"
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	_, progress = e.waitProgress(root)
	if progress {
		t.Fatal("blocked subtree counted as progress")
	}
	// A reviewer placeholder waiting on a blocked author cannot make progress.
	review := Run{ID: "review", Org: root.Org, Task: root.Task, Parent: root.ID, ReviewOf: child.ID, State: "waiting"}
	must(t, s.Put("run", review.Org, review.Task, review.State, review.ID, review))
	_, progress = e.waitProgress(root)
	if progress {
		t.Fatal("placeholder review counted as progress")
	}
	child.State = "complete"
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	_, progress = e.waitProgress(root)
	if !progress {
		t.Fatal("runnable review was ignored")
	}
}

func TestCoordinationRecoveryPreservesHumanControls(t *testing.T) {
	for _, state := range []string{"paused", "cancelled", "ready", "needs input"} {
		t.Run(state, func(t *testing.T) {
			s, e, task, root := fixture(t)
			task.State = state
			must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
			root.State = "waiting"
			must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
			child := Run{ID: "held", Org: root.Org, Task: root.Task, Parent: root.ID, State: "blocked"}
			must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
			if state == "needs input" {
				d := Decision{ID: "pending", Org: root.Org, Task: root.Task, Run: root.ID, State: "pending"}
				must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
			}
			e.recoverStalledSupervisors()
			must(t, s.Get(root.ID, &root))
			if root.State != "waiting" {
				t.Fatal("human control bypassed")
			}
		})
	}
}

func TestCoordinationStatusPaginatesAndRetainsHumanAnswers(t *testing.T) {
	s, e, _, root := fixture(t)
	for i := 0; i < 30; i++ {
		child := Run{ID: ID(), Org: root.Org, Task: root.Task, Parent: root.ID, State: "blocked", Title: "A real blocker", Error: strings.Repeat("long evidence ", 1000)}
		must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	}
	raw, err := call(t, e, root, "adc_status", statusInput{View: "coordination"})
	must(t, err)
	if len(raw) > inlineToolBytes || !strings.Contains(raw, `"total":30`) || !strings.Contains(raw, `"next_offset":6`) {
		t.Fatal("coordination page unusable", len(raw), raw)
	}
	d := Decision{ID: "prior", Org: root.Org, Task: root.Task, Run: root.ID, State: "answered", Answer: "Ben is the backup operator", Outcome: "approve"}
	must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
	raw, err = call(t, e, root, "adc_status", statusInput{View: "decisions"})
	must(t, err)
	if !strings.Contains(raw, d.Answer) {
		t.Fatal("prior human answer hidden")
	}
	d.Task = "foreign"
	must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
	if _, err = call(t, e, root, "adc_status", statusInput{View: "decisions", ID: d.ID}); err == nil {
		t.Fatal("foreign decision disclosed")
	}
}

func TestWithdrawObsoleteDecisionDoesNotApproveOrRewriteHuman(t *testing.T) {
	s, e, _, root := fixture(t)
	d, err := e.submitDecision(root, decisionInput{Question: "Old proposal", Brief: "Approve the old scope"})
	must(t, err)
	setRunning(t, s, &root)
	_, err = call(t, e, root, "adc_withdraw_decision", map[string]string{"ID": d.ID, "Reason": "Human changed the requested scope; prepare a work proposal instead."})
	must(t, err)
	must(t, s.Get(d.ID, &d))
	if d.State != "withdrawn" || d.Outcome != "" || d.ResolvedBy != "" || pendingDecision(s, root.Task, root.ID) {
		t.Fatal("withdrawal not recorded safely", d)
	}
	for _, state := range []string{"answered", "pending"} {
		d.State = state
		d.Run = "another"
		must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
		if _, err = e.withdrawDecision(root, d.ID, "No longer needed"); err == nil {
			t.Fatal("other run request withdrawn")
		}
	}
	d.Run = root.ID
	d.State = "answered"
	d.Outcome = "approve"
	must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
	if _, err = e.withdrawDecision(root, d.ID, "No longer needed"); err == nil {
		t.Fatal("human answer rewritten")
	}
	d.State = "pending"
	d.Kind = "permission"
	must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
	if _, err = e.withdrawDecision(root, d.ID, "No longer needed"); err == nil {
		t.Fatal("permission journal bypassed")
	}
}
