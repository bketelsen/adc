package adc

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserOwnership(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE")
	}
	s, e, _, r, a := ownershipFixture(t)
	_, err := s.db.Exec("INSERT INTO sessions VALUES(?, 'owner','2099-01-01T00:00:00Z')", digest("transcript-fixture"))
	must(t, err)
	a.Summary = "Observed Monday collection, awaiting human confirmation."
	a.Source = "fixture://initial"
	a, err = s.saveArea(a, a.Revision, "run:"+r.ID)
	must(t, err)
	o := createFollowup(t, e, r, followupArgs(a))
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/ownership.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	must(t, s.Get(o.ID, &o))
	if o.State != "cancelled" {
		t.Fatal("browser did not cancel obligation")
	}
	must(t, s.Get(a.ID, &a))
	notes := s.areaKnowledge(a).Notes
	found := false
	for _, n := range notes {
		if n.Text == "Tuesday is correct. Please retain that correction." && n.Selection == "Observed Monday collection, awaiting human confirmation." {
			found = true
		}
	}
	if !found {
		t.Fatal("selected correction not retained")
	}
	if a.Intent != "Keep the family responsibility current." {
		t.Fatal("browser intent correction not saved")
	}
}
