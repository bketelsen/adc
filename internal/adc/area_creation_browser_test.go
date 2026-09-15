package adc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserAreaCreationConversation(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE")
	}
	s, e, _, _ := fixture(t)
	transcriptLogin(t, s)
	var account Account
	must(t, s.Get("account", &account))
	account.User = "owner"
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	handler := NewWeb(s, e, false).Handler()
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fixture-area-proposal" {
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, task := range list[Assignment](s, "assignment", "org") {
				if task.AreaCreation {
					run := taskRuns(s, task.ID)[0]
					run.State = "running"
					must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
					name := "Fixture backups"
					if len(taskDecisions(s, task.ID)) > 0 {
						name = "Fixture recovery"
					}
					_, err := e.submitDecision(run, decisionInput{Kind: "area-proposal", Brief: "Create the proposed recovery responsibility.", Question: "Fixture scope: understand restore coverage; no operations or scheduled work.", ProposedArea: &AreaSpec{Name: name, Owner: "dev", Intent: "Understand restore coverage; propose bounded checks.", CompletionMode: "reviewed"}})
					if err != nil {
						http.Error(rw, err.Error(), 500)
						return
					}
					rw.WriteHeader(204)
					return
				}
			}
			http.Error(rw, "missing fixture conversation", 500)
			return
		}
		handler.ServeHTTP(rw, r)
	}))
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/area-creation.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	areas := list[Area](s, "area", "org")
	if len(areas) != 1 || areas[0].Name != "Fixture recovery" || areas[0].UpdatedBy != "human:owner" {
		t.Fatal("browser did not create the approved revision")
	}
}
