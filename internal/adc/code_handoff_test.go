package adc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSupervisorCodeCompletionRequiresExactReviewedWorkerHandoff(t *testing.T) {
	s, e, task, root := fixture(t)
	dir := filepath.Join(root.Workspace, "repo")
	must(t, os.MkdirAll(dir, 0700))
	git := func(at string, args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", at}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
	}
	git(dir, "init", "-q")
	must(t, os.WriteFile(filepath.Join(dir, "file"), []byte("fixture\n"), 0600))
	git(dir, "add", "file")
	git(dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
	_, err := e.captureCode(&root, dir)
	must(t, err)
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	finish := func() error {
		_, err := call(t, e, root, "adc_finish", map[string]string{"Result": "Exact integrated candidate finalized and independently reviewed."})
		return err
	}
	if finish() == nil {
		t.Fatal("supervisor code bypassed worker and review")
	}
	clone := filepath.Join(t.TempDir(), "clone")
	git(dir, "clone", "-q", dir, clone)
	evidence, err := inspectCode(clone)
	must(t, err)
	worker := Run{ID: "finalizer", Org: root.Org, Task: root.Task, Parent: root.ID, Agent: "dev", Model: root.Model, Category: "implementation", State: "complete", Code: []CodeEvidence{evidence}, Result: "Verified exact candidate"}
	saveWorker := func() { must(t, s.Put("run", worker.Org, worker.Task, worker.State, worker.ID, worker)) }
	saveWorker()
	if finish() == nil {
		t.Fatal("unreviewed handoff accepted")
	}
	review := Review{ID: "handoff-review", Org: root.Org, Task: root.Task, Target: worker.ID, Revision: e.revision(worker), Model: "claude-opus-5", Verdict: "pass"}
	saveReview := func() { must(t, s.Put("review", review.Org, review.Task, review.Verdict, review.ID, review)) }
	saveReview()
	originalWorker := worker
	originalReview := review
	for _, scenario := range []string{"candidate", "changes", "stale", "same-family", "worker-family", "superseded", "wrong-parent", "incomplete", "changed-worker", "uncovered-artifact"} {
		t.Run(scenario, func(t *testing.T) {
			worker, review = originalWorker, originalReview
			switch scenario {
			case "candidate":
				review.Stage = "candidate"
			case "changes":
				review.Verdict = "changes"
			case "stale":
				review.Revision = "older"
			case "same-family":
				review.Model = root.Model
			case "worker-family":
				worker.Model = "claude-opus-5"
				review.Model = root.Model
				review.Revision = e.revision(worker)
			case "superseded":
				worker.Superseded = true
			case "wrong-parent":
				worker.Parent = "another-owner"
			case "incomplete":
				worker.State = "running"
			case "changed-worker":
				worker.Result = "unreviewed correction"
			case "uncovered-artifact":
				root.Code = append(root.Code, CodeEvidence{Path: dir, Commit: "uncovered"})
				must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
			}
			saveWorker()
			saveReview()
			if finish() == nil {
				t.Fatal("invalid handoff completed")
			}
			if scenario == "uncovered-artifact" {
				root.Code = root.Code[:1]
				must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
			}
		})
	}
	worker, review = originalWorker, originalReview
	saveWorker()
	saveReview()
	must(t, os.WriteFile(filepath.Join(clone, "file"), []byte("dirty worker\n"), 0600))
	if finish() == nil {
		t.Fatal("dirty reviewed worker accepted")
	}
	git(clone, "restore", "file")
	must(t, os.WriteFile(filepath.Join(dir, "file"), []byte("dirty root\n"), 0600))
	if finish() == nil {
		t.Fatal("dirty supervisor accepted")
	}
	git(dir, "restore", "file")
	// A later changes verdict takes precedence over the earlier passing verdict.
	newer := review
	newer.ID = "newer-changes"
	newer.Verdict = "changes"
	must(t, s.Put("review", newer.Org, newer.Task, newer.Verdict, newer.ID, newer))
	if finish() == nil {
		t.Fatal("old PASS hid newer changes")
	}
	newer.Verdict = "pass"
	must(t, s.Put("review", newer.Org, newer.Task, newer.Verdict, newer.ID, newer))
	must(t, finish())
	must(t, s.Get(task.ID, &task))
	if task.State != "ready" {
		t.Fatal(task.State)
	}
	var saved Run
	must(t, s.Get(root.ID, &saved))
	if len(saved.Code) != 1 || saved.Code[0].Commit != evidence.Commit {
		t.Fatal("handoff erased original provenance")
	}
}
