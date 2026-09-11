package adc

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBrowserClaudeConnectionWorkflow(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated Claude browser qualification")
	}
	fakeClaude(t)
	t.Setenv("ADC_CODEX_FIXTURE_MODE", "claude-login")
	s, e, task, _ := fixture(t)
	defer e.Stop()
	var a Account
	must(t, s.Get(task.Account, &a))
	a.Provider = "claude"
	must(t, s.Put("account", "", a.User, "", a.ID, a))
	hash, err := bcrypt.GenerateFromPassword([]byte("ui-fixture-password"), bcrypt.MinCost)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','UI fixture','fixture',?);INSERT INTO memberships VALUES('owner','org')`, hash)
	must(t, err)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/claude-check.cjs")
	must(t, err)
	output, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(output))
	must(t, err)
	var dev Agent
	must(t, s.Get("dev", &dev))
	if dev.Provider != "claude" || dev.Model != "claude-opus-5" {
		t.Fatal("role provider selection not saved")
	}
}
