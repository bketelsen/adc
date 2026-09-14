package adc

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserAttention(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE")
	}
	s, e, task, root, a := attentionFixture(t)
	worker := assessmentWorker(t, e, root, "dev", "scan")
	_, err := s.saveAssessment(worker, a.ID, "No restore proof for volume B. Other sampled volumes are unchanged.", "fixture://restore-evidence", "unknown", 0)
	must(t, err)
	_, err = s.saveSupervisorBrief(worker, "One shared recoverability gap covers storage and hosting. **Restore evidence for volume B is still owed.** Existing work covers it; no new proposal needed.", "fixture://restore-evidence", 0)
	must(t, err)
	passAttentionRun(t, e, worker)
	task.State = "ready"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	o := Obligation{ID: "blocked-obligation", Org: task.Org, Area: a.ID, Owner: a.Owner, Outcome: "Verify container restore", State: "blocked", Note: "No restore evidence available"}
	must(t, s.Put("obligation", o.Org, "", o.State, o.ID, o))
	_, err = s.db.Exec("INSERT INTO sessions VALUES(?, 'owner','2099-01-01T00:00:00Z')", digest("attention-fixture"))
	must(t, err)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/attention.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
}
