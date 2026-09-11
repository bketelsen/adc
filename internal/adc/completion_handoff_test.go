package adc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkerSchedulesReviewBeforeCompletionWithoutCircularDependency(t *testing.T) {
	s, e, _, root := fixture(t)
	dev := Run{ID: "author", Org: root.Org, Task: root.Task, Parent: root.ID, Agent: "dev", Model: "gpt-5.6-sol", Category: "implementation", State: "running", Authority: "draft"}
	must(t, s.Put("run", dev.Org, dev.Task, dev.State, dev.ID, dev))
	args := map[string]any{"Agent": "qa", "ReviewOf": dev.ID, "Title": "Review output", "Prompt": "Inspect the completed output"}
	result, err := call(t, e, dev, "adc_delegate", args)
	must(t, err)
	var qa Run
	must(t, json.Unmarshal([]byte(result), &qa))
	if qa.Parent != root.ID || qa.State != "waiting" || qa.Authority != "observe" || qa.Model != "claude-opus-5" {
		t.Fatal("review lost supervisor accountability, hold, or authority limits")
	}
	args["Prompt"] = "Check exact evidence after completion"
	again, err := call(t, e, dev, "adc_delegate", args)
	must(t, err)
	var same Run
	must(t, json.Unmarshal([]byte(again), &same))
	if same.ID != qa.ID {
		t.Fatal("retry created competing review")
	}
	_, err = call(t, e, dev, "adc_finish", map[string]any{"Result": "Completed fixture evidence"})
	must(t, err)
	must(t, s.Get(dev.ID, &dev))
	if dev.State != "complete" {
		t.Fatal("review prevented author completion")
	}
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	started := controlScheduler(t, e)
	e.tick(context.Background())
	active := awaitRun(t, started)
	if active.ID != qa.ID || active.ReviewedRevision != e.revision(dev) {
		t.Fatal("review failed to start against completed artifact")
	}
}

func TestLateCollaborationDoesNotReopenCompletedWork(t *testing.T) {
	s, e, _, root := fixture(t)
	dev := Run{ID: "author", Org: root.Org, Task: root.Task, Parent: root.ID, State: "running"}
	must(t, s.Put("run", dev.Org, dev.Task, dev.State, dev.ID, dev))
	_, err := call(t, e, root, "adc_message", map[string]any{"Run": dev.ID, "Message": "Please finish the existing work"})
	must(t, err)
	_, err = call(t, e, dev, "adc_finish", map[string]any{"Result": "Done"})
	must(t, err)
	must(t, s.Get(dev.ID, &dev))
	if e.resumeForUpdates(&dev) || dev.State != "complete" || dev.UpdatesPending {
		t.Fatal("late note reopened completed work")
	}
	must(t, s.Put("run", dev.Org, dev.Task, dev.State, dev.ID, dev))
	result, err := call(t, e, root, "adc_message", map[string]any{"Run": dev.ID, "Message": "Please finish"})
	must(t, err)
	if !strings.Contains(result, "already completed") {
		t.Fatal("late sender did not receive completed status")
	}
	must(t, s.Get(dev.ID, &dev))
	if dev.State != "complete" {
		t.Fatal("late message revived completed run")
	}
	dev.Steering = true
	if !e.resumeForUpdates(&dev) || dev.State != "queued" {
		t.Fatal("real human steering was discarded")
	}
}

func TestStatusIdentifiesPublicationReviewWithoutWaivingIt(t *testing.T) {
	s, e, _, root := fixture(t)
	publication := Run{ID: "publisher", Org: root.Org, Task: root.Task, Parent: root.ID, State: "complete", Model: "gpt-5.6-sol", Category: "implementation", Title: "Publish draft PR", Result: "Draft PR URL verified"}
	must(t, s.Put("run", publication.Org, publication.Task, publication.State, publication.ID, publication))
	needs := e.reviewNeeds(root.Task)
	if len(needs) != 1 || needs[0]["run"] != publication.ID {
		t.Fatal("publication review hidden from supervisor")
	}
	if _, err := call(t, e, root, "adc_finish", map[string]any{"Result": "Finished"}); err == nil {
		t.Fatal("publication review gate bypassed")
	}
	v := Review{ID: "pass", Org: root.Org, Task: root.Task, Target: publication.ID, Model: "claude-opus-5", Revision: e.revision(publication), Verdict: "pass"}
	must(t, s.Put("review", v.Org, v.Task, v.Verdict, v.ID, v))
	if len(e.reviewNeeds(root.Task)) != 0 {
		t.Fatal("current reviewed publication still marked missing")
	}
	publication.Result = "Different PR head"
	must(t, s.Put("run", publication.Org, publication.Task, publication.State, publication.ID, publication))
	if len(e.reviewNeeds(root.Task)) != 1 {
		t.Fatal("changed publication incorrectly retained review")
	}
}
