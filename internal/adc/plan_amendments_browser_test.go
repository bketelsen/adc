package adc

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserHumanPlanAmendment(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE")
	}
	s, e, r, _ := milestoneFixture(t, "human-evidence")
	r.State = "blocked"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	transcriptLogin(t, s)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/plan-amendment.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	if s.taskPlan(r.Task).Steps[0].Omission == nil || len(s.taskPlan(r.Task).Steps[0].Requirements) != 0 || s.milestoneEvidence(r.ID, "gate").ID != "" {
		t.Fatal("browser change did not retire the requirement honestly")
	}
}
