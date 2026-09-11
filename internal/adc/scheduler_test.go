package adc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func controlScheduler(t *testing.T, e *Engine) <-chan Run {
	t.Helper()
	started := make(chan Run, 20)
	e.runActivation = func(ctx context.Context, r Run, _ Assignment, _ Account) { started <- r; <-ctx.Done() }
	t.Cleanup(e.Stop)
	return started
}
func awaitRun(t *testing.T, ch <-chan Run) Run {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not start expected run")
		return Run{}
	}
}
func TestReviewerPriorityAndConcurrencyLimit(t *testing.T) {
	s, e, _, root := fixture(t)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	account := Account{ID: "account", User: "owner", Limit: 1}
	must(t, s.Put("account", "", "owner", "", account.ID, account))
	dev := Run{ID: "a-dev", Org: root.Org, Task: root.Task, Parent: root.ID, Category: "implementation", State: "queued", Created: "2026-01-01T00:00:00Z"}
	qa := Run{ID: "z-review", Org: root.Org, Task: root.Task, Parent: root.ID, Category: "review", Model: "claude-opus-5", State: "queued", Created: "2026-01-02T00:00:00Z"}
	for _, r := range []Run{dev, qa} {
		must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	}
	started := controlScheduler(t, e)
	e.tick(context.Background())
	if r := awaitRun(t, started); r.ID != qa.ID {
		t.Fatal("review was starved by leaf work", r.ID)
	}
	e.tick(context.Background())
	select {
	case r := <-started:
		t.Fatal("concurrency exceeded", r.ID)
	default:
	}
	must(t, s.Get(dev.ID, &dev))
	if dev.State != "queued" {
		t.Fatal("waiting worker state changed")
	}
}
func TestUnavailableReviewerReleasesSlotWithoutChangingFamily(t *testing.T) {
	s, e, _, root := fixture(t)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	account := Account{ID: "account", User: "owner", Limit: 1}
	must(t, s.Put("account", "", "owner", "", account.ID, account))
	qa := Run{ID: "qa-run", Org: root.Org, Task: root.Task, Parent: root.ID, Category: "review", Model: "claude-opus-5", State: "running"}
	must(t, s.Put("run", qa.Org, qa.Task, qa.State, qa.ID, qa))
	e.handleFailure(context.Background(), qa, errors.New("configured model claude-opus-5 unavailable; no substitution permitted"))
	must(t, s.Get(qa.ID, &qa))
	if qa.State != "queued" || qa.Model != "claude-opus-5" || qa.NextAt <= now() {
		t.Fatal("missing reviewer did not wait intact")
	}
	dev := Run{ID: "unaffected", Org: root.Org, Task: root.Task, Parent: root.ID, Category: "implementation", State: "queued"}
	must(t, s.Put("run", dev.Org, dev.Task, dev.State, dev.ID, dev))
	started := controlScheduler(t, e)
	e.tick(context.Background())
	if r := awaitRun(t, started); r.ID != dev.ID {
		t.Fatal("unaffected work did not continue")
	}
	if len(taskDecisions(s, root.Task)) != 0 {
		t.Fatal("temporary model unavailability unnecessarily requested human input")
	}
}
func TestOldQueuedWorkPrecedesNewWorkInSameCategory(t *testing.T) {
	s, e, _, root := fixture(t)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	account := Account{ID: "account", User: "owner", Limit: 1}
	must(t, s.Put("account", "", "owner", "", account.ID, account))
	old := Run{ID: "old", Org: root.Org, Task: root.Task, Parent: root.ID, Category: "implementation", State: "queued", Created: "2026-01-01T00:00:00Z"}
	newer := old
	newer.ID = "new"
	newer.Created = "2026-02-01T00:00:00Z"
	for _, r := range []Run{old, newer} {
		must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	}
	started := controlScheduler(t, e)
	e.tick(context.Background())
	if r := awaitRun(t, started); r.ID != old.ID {
		t.Fatal("new work overtook waiting work")
	}
}
func TestPauseRejectsLateOutcomeAndResumesWithoutFailureBackoff(t *testing.T) {
	s, e, task, root := fixture(t)
	task.State = "paused"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := call(t, e, root, "adc_document", map[string]any{"Title": "Late write", "Content": "should not be saved"}); err == nil {
		t.Fatal("paused run accepted a late action")
	}
	if len(taskDocs(s, task.ID)) != 0 {
		t.Fatal("late action persisted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.handleFailure(ctx, root, context.Canceled)
	must(t, s.Get(root.ID, &root))
	if root.State != "queued" || root.NextAt != "" || root.Attempts != 0 || !strings.Contains(root.Prompt, "Inspect existing work") {
		t.Fatal("pause did not preserve immediate, reconciled resumption")
	}
	must(t, s.Get(task.ID, &task))
	if task.State != "paused" {
		t.Fatal("failure resumed paused assignment")
	}
}
func TestSupervisorWakesOnlyOnceAfterPersistedChildrenComplete(t *testing.T) {
	s, e, _, root := fixture(t)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	child := Run{ID: "child", Org: root.Org, Task: root.Task, Parent: root.ID, State: "complete"}
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	started := controlScheduler(t, e)
	e.tick(context.Background())
	e.tick(context.Background())
	if r := awaitRun(t, started); r.ID != root.ID {
		t.Fatal("wrong run woke")
	}
	select {
	case <-started:
		t.Fatal("duplicate supervisor activation")
	default:
	}
}
