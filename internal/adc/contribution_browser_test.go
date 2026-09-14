package adc

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserContributions(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE")
	}
	s, e, _, _, q := contributionFixture(t)
	runtimeDir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(runtimeDir, "public"), []byte("fixture runtime"), 0400))
	runtimeID, err := ContributionRuntimeID(t.Context(), runtimeDir)
	must(t, err)
	t.Setenv("ADC_CONTRIBUTION_RUNTIME", runtimeDir)
	t.Setenv("ADC_CONTRIBUTION_RUNTIME_ID", runtimeID)
	_, err = s.db.Exec("INSERT INTO sessions VALUES(?, 'owner','2099-01-01T00:00:00Z')", digest("contribution-fixture"))
	must(t, err)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/contributions.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL, q.Area, runtimeID).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	if len(list[ContributionQueue](s, "contribution-queue", q.Org)) != 2 {
		t.Fatal("human queue policy not saved")
	}
	for _, created := range list[ContributionQueue](s, "contribution-queue", q.Org) {
		if created.ID != q.ID && created.Runtime != runtimeID {
			t.Fatal("selected runtime was not pinned")
		}
	}
}
