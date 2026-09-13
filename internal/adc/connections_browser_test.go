package adc

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserConnectionEdit(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for connection editor qualification")
	}
	s, w, c, _ := connectionEditFixture(t)
	transcriptLogin(t, s)
	server := httptest.NewServer(w.Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/connection-edit.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	var saved Connection
	must(t, s.Get(c.ID, &saved))
	if saved.Name != "Renamed MCP" || saved.Command != "/bin/echo" || saved.Headers["Authorization"] != c.Headers["Authorization"] || saved.Env["API_KEY"] != "" {
		t.Fatal("browser update did not preserve/remove intended values")
	}
	value, err := s.Unseal(saved.Env["NEW_KEY"])
	must(t, err)
	if value != "fixture-replacement" {
		t.Fatal("replacement not sealed/saved")
	}
}
