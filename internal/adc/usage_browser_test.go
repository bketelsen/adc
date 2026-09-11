package adc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestBrowserUsageWorkflow(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated usage browser qualification")
	}
	s, e, task, root := fixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("ui-fixture-password"), bcrypt.MinCost)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','UI fixture','fixture',?);INSERT INTO memberships VALUES('owner','org')`, hash)
	must(t, err)
	task.Title = "Fixture storage assessment"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("account", "", task.Creator, "", task.Account, Account{ID: task.Account, User: task.Creator, Name: "Fixture subscription", Secret: "must-not-render"}))
	s.Log(task.Org, task.ID, root.ID, "usage", `{"model":"gpt-5.6-sol","input":10000,"output":500,"cache_read":8000}`)
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", `{"model":"claude-opus-5","input":2000,"output":500,"cache_read":0}`, time.Now().AddDate(0, 0, -40))
	w := NewWeb(s, e, false)
	mux := http.NewServeMux()
	mux.Handle("/", w.Handler())
	mux.HandleFunc("/fixture-usage", func(rw http.ResponseWriter, r *http.Request) {
		s.Log(task.Org, task.ID, root.ID, "usage", `{"model":"gpt-5.6-sol","input":500,"output":0,"cache_read":null,"cache_write":100}`)
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/usage-check.cjs")
	must(t, err)
	output, err := exec.Command(node, script, server.URL, task.ID).CombinedOutput()
	t.Log(string(output))
	must(t, err)
}
