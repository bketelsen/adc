package adc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCodeEvidenceRejectsDirtyOrChangedCommit(t *testing.T) {
	s, e, _, root := fixture(t)
	dir := filepath.Join(root.Workspace, "repository")
	must(t, os.MkdirAll(dir, 0700))
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %s: %v", out, err)
		}
	}
	git("init", "--quiet")
	git("config", "user.name", "ADC fixture")
	git("config", "user.email", "fixture@example.invalid")
	file := filepath.Join(dir, "content.md")
	must(t, os.WriteFile(file, []byte("Shipping facts"), 0600))
	if _, err := e.captureCode(&root, dir); err == nil {
		t.Fatal("uncommitted code accepted")
	}
	git("add", "content.md")
	git("commit", "--quiet", "-m", "Fixture")
	evidence, err := e.captureCode(&root, dir)
	must(t, err)
	if evidence.Commit == "" {
		t.Fatal("commit missing")
	}
	must(t, verifyCode(root))
	before := e.revision(root)
	must(t, os.WriteFile(file, []byte("Changed facts"), 0600))
	if verifyCode(root) == nil {
		t.Fatal("dirty reviewed tree accepted")
	}
	git("add", "content.md")
	git("commit", "--quiet", "-m", "Second fixture")
	if verifyCode(root) == nil {
		t.Fatal("changed commit accepted")
	}
	_, err = e.captureCode(&root, dir)
	must(t, err)
	if e.revision(root) == before {
		t.Fatal("code revision did not change")
	}
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	other := root
	other.Workspace = t.TempDir()
	if _, err = e.captureCode(&other, dir); err == nil {
		t.Fatal("another worker's repository registered as own work")
	}
}
