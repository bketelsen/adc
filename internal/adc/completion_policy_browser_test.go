package adc

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserCompletionPolicy(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE")
	}
	s, e, task, _, a := routineFixture(t)
	_, err := s.db.Exec("INSERT INTO sessions VALUES(?, 'owner','2099-01-01T00:00:00Z')", digest("completion-fixture"))
	must(t, err)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/completion-policy.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL, a.ID, task.ID).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	must(t, s.Get(a.ID, &a))
	must(t, s.Get(task.ID, &task))
	if a.CompletionMode != "reviewed" || !routineCompletion(task) {
		t.Fatal("policy form did not preserve active work's snapshot")
	}
}
