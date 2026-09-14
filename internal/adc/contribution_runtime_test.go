package adc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContributionRuntimePinAndSnapshot(t *testing.T) {
	source := t.TempDir()
	path := filepath.Join(source, "compiler")
	must(t, os.WriteFile(path, []byte("public-runtime"), 0700))
	id, err := ContributionRuntimeID(t.Context(), source)
	must(t, err)
	t.Setenv("ADC_CONTRIBUTION_RUNTIME", source)
	t.Setenv("ADC_CONTRIBUTION_RUNTIME_ID", id)
	snapshot, cleanup, err := snapshotContributionRuntime(t.Context(), id)
	must(t, err)
	defer cleanup()
	must(t, os.WriteFile(path, []byte("changed-runtime"), 0700))
	content, err := os.ReadFile(filepath.Join(snapshot, "compiler"))
	must(t, err)
	if string(content) != "public-runtime" {
		t.Fatal("snapshot changed with source")
	}
	if _, _, err := snapshotContributionRuntime(t.Context(), id); err == nil {
		t.Fatal("changed runtime admitted")
	}
	// Default queues never acquire configured runtimes.
	if path, cleanup, err := snapshotContributionRuntime(t.Context(), ""); path != "" || err != nil {
		t.Fatal(path, err)
	} else {
		cleanup()
	}
	if _, _, err := snapshotContributionRuntime(t.Context(), "unapproved"); err == nil {
		t.Fatal("unpinned runtime admitted")
	}
}

func TestContributionRuntimeRejectsSymlinkAndCancellation(t *testing.T) {
	source := t.TempDir()
	must(t, os.Symlink("/etc/passwd", filepath.Join(source, "private")))
	if _, err := ContributionRuntimeID(t.Context(), source); err == nil {
		t.Fatal("symlink admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ContributionRuntimeID(ctx, t.TempDir()); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestContributionRuntimeWithdrawalPreventsSpending(t *testing.T) {
	s, _, _, p, q := contributionFixture(t)
	source := t.TempDir()
	must(t, os.WriteFile(filepath.Join(source, "public"), []byte("public"), 0400))
	id, err := ContributionRuntimeID(t.Context(), source)
	must(t, err)
	t.Setenv("ADC_CONTRIBUTION_RUNTIME", source)
	t.Setenv("ADC_CONTRIBUTION_RUNTIME_ID", id)
	q.Runtime, p.Public.Runtime = id, id
	must(t, s.Put("contribution-queue", q.Org, q.Area, q.State, q.ID, q))
	must(t, s.Put("contribution-packet", p.Org, p.Task, p.State, p.ID, p))
	claim, err := s.claimContribution(p.ID, time.Now())
	must(t, err)
	t.Setenv("ADC_CONTRIBUTION_RUNTIME_ID", "withdrawn")
	body := "def add(a,b): return a+b\n"
	_, err = s.submitContribution(p.ID, claim["Receipt"].(string), contributionSubmission{Revision: p.Public.Revision, Summary: "candidate", Files: map[string]*string{"calc.py": &body}}, time.Now())
	if err == nil {
		t.Fatal("withdrawn runtime spent review budget")
	}
	must(t, s.Get(q.ID, &q))
	if q.Spent != 0 {
		t.Fatal("review budget spent")
	}
}
