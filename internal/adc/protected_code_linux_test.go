//go:build linux

package adc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectedCodeReviewUsesIndependentPinnedCopy(t *testing.T) {
	executorFixture(t)
	s, e, _, author := fixture(t)
	author.Execution = "protected"
	x, err := runExecutor(author)
	must(t, err)
	r, err := x.Execute(context.Background(), workspaceCommand{Command: "git init -q repo && cd repo && printf 'original\\n' > file.txt && git add file.txt && git -c user.name=Fixture -c user.email=fixture@example.test commit -qm fixture"})
	must(t, err)
	if r.ExitCode != 0 {
		t.Fatal(r)
	}
	code, err := e.captureCode(&author, "/workspace/repo")
	must(t, err)
	must(t, s.Put("run", author.Org, author.Task, author.State, author.ID, author))
	if !strings.HasPrefix(code.Path, author.Workspace) {
		t.Fatal("evidence path escaped")
	}
	if _, err := e.captureCode(&author, "/workspace/../../etc"); err == nil {
		t.Fatal("outside path accepted")
	}
	reviewer := author
	reviewer.ID = ID()
	reviewer.Workspace = filepath.Join(t.TempDir(), "reviewer")
	reviewer.Code = nil
	snapshot, err := e.checkoutProtectedCode(context.Background(), reviewer, author.ID, 0)
	must(t, err)
	other, err := runExecutor(reviewer)
	must(t, err)
	r, err = other.Execute(context.Background(), workspaceCommand{Command: "printf 'reviewer-only\\n' > " + shellQuote(snapshot["Path"]+"/file.txt")})
	must(t, err)
	if r.ExitCode != 0 {
		t.Fatal(r)
	}
	b, err := os.ReadFile(filepath.Join(author.Workspace, "repo", "file.txt"))
	must(t, err)
	if string(b) != "original\n" {
		t.Fatal("reviewer modified author checkout")
	}
	must(t, verifyCode(author))
}
