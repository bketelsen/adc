package adc

import (
	"context"
	"strings"
	"testing"
)

func TestMessageToReviewerAfterAuthorCompletionRefreshesPin(t *testing.T) {
	s, e, _, root := fixture(t)
	author := Run{ID: "author", Org: root.Org, Task: root.Task, Parent: root.ID, Model: "gpt-5.6-sol", State: "complete", Result: "revision 2"}
	qa := Run{ID: "reviewer", Org: root.Org, Task: root.Task, Parent: root.ID, Model: "claude-opus-5", Category: "review", State: "waiting", ReviewOf: author.ID, ReviewedRevision: e.revision(author)}
	author.Result = "revision 3"
	must(t, s.Put("run", author.Org, author.Task, author.State, author.ID, author))
	must(t, s.Put("run", qa.Org, qa.Task, qa.State, qa.ID, qa))
	_, err := call(t, e, root, "adc_message", map[string]any{"Run": qa.ID, "Message": "The corrected author run is complete; inspect it again."})
	must(t, err)
	must(t, s.Get(qa.ID, &qa))
	if qa.State != "queued" {
		t.Fatal("message did not take direct queued path")
	}
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	started := controlScheduler(t, e)
	e.tick(context.Background())
	qa = awaitRun(t, started)
	if qa.ReviewedRevision != e.revision(author) {
		t.Fatal("direct queued path retained stale review pin")
	}
}

func TestCollaborationWakeWaitsForAuthorAndRefreshesReviewPin(t *testing.T) {
	s, e, _, root := fixture(t)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	author := Run{ID: "author", Org: root.Org, Task: root.Task, Parent: root.ID, Model: "gpt-5.6-sol", State: "complete", Result: "revision 2"}
	must(t, s.Put("run", author.Org, author.Task, author.State, author.ID, author))
	qa := Run{ID: "reviewer", Org: root.Org, Task: root.Task, Parent: root.ID, Model: "claude-opus-5", Category: "review", State: "running", ReviewOf: author.ID, ReviewedRevision: e.revision(author)}
	must(t, s.Put("run", qa.Org, qa.Task, qa.State, qa.ID, qa))
	_, err := call(t, e, qa, "adc_review", map[string]any{"Verdict": "changes", "Findings": "Correct the lifecycle date"})
	must(t, err)
	must(t, s.Get(author.ID, &author))
	author.State = "running"
	author.Result = "revision 3"
	must(t, s.Put("run", author.Org, author.Task, author.State, author.ID, author))
	_, err = call(t, e, author, "adc_message", map[string]any{"Run": qa.ID, "Message": "Revision 3 is saved; re-review after I complete."})
	must(t, err)
	started := controlScheduler(t, e)
	e.tick(context.Background())
	must(t, s.Get(qa.ID, &qa))
	if qa.State != "waiting" {
		t.Fatal("message started reviewer before author completed")
	}
	select {
	case r := <-started:
		t.Fatal("premature review activation", r.ID)
	default:
	}
	author.State = "complete"
	must(t, s.Put("run", author.Org, author.Task, author.State, author.ID, author))
	e.tick(context.Background())
	qa = awaitRun(t, started)
	if qa.ReviewedRevision != e.revision(author) {
		t.Fatal("collaboration bypassed revision binding")
	}
	_, err = call(t, e, qa, "adc_review", map[string]any{"Verdict": "pass", "Findings": "Independently checked the revision 3 date"})
	must(t, err)
	reviews := taskReviews(s, root.Task)
	if len(reviews) != 2 {
		t.Fatal("correction history lost")
	}
	for _, review := range reviews {
		if review.Verdict == "pass" && review.Revision != e.revision(author) {
			t.Fatal("pass stored against stale revision")
		}
	}
}

func TestTargetChangeDuringReviewRestartsWithoutRecordingVerdict(t *testing.T) {
	s, e, _, root := fixture(t)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	author := Run{ID: "author", Org: root.Org, Task: root.Task, Parent: root.ID, Model: "gpt-5.6-sol", State: "complete", Result: "old"}
	must(t, s.Put("run", author.Org, author.Task, author.State, author.ID, author))
	qa := Run{ID: "reviewer", Org: root.Org, Task: root.Task, Parent: root.ID, Model: "claude-opus-5", Category: "review", State: "running", ReviewOf: author.ID, ReviewedRevision: e.revision(author)}
	must(t, s.Put("run", qa.Org, qa.Task, qa.State, qa.ID, qa))
	old := qa.ReviewedRevision
	author.Result = "new"
	must(t, s.Put("run", author.Org, author.Task, author.State, author.ID, author))
	result, err := call(t, e, qa, "adc_review", map[string]any{"Verdict": "pass", "Findings": "I inspected the old target"})
	must(t, err)
	must(t, s.Get(qa.ID, &qa))
	if !strings.Contains(result, "no verdict recorded") || len(taskReviews(s, root.Task)) != 0 || qa.State != "waiting" || qa.ReviewedRevision != old {
		t.Fatal("stale verdict was blessed or recovery was not scheduled")
	}
	started := controlScheduler(t, e)
	e.tick(context.Background())
	qa = awaitRun(t, started)
	if qa.ReviewedRevision != e.revision(author) || !strings.Contains(qa.Prompt, "Inspect the completed target") {
		t.Fatal("fresh review did not receive current target and reinspection instruction")
	}
}
