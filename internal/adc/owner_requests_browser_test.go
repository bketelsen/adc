package adc

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserOwnerCoordination(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE")
	}
	s, e, _, r, _ := ownershipFixture(t)
	q := makeOwnerRequest(t, e, r)
	_, err := s.db.Exec("INSERT INTO sessions VALUES(?, 'owner','2099-01-01T00:00:00Z')", digest("coordination-fixture"))
	must(t, err)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/owner-coordination.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	must(t, s.Get(q.ID, &q))
	if q.State != "cancelled" {
		t.Fatal("cancellation not persisted")
	}
}
