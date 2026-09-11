package adc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBrowserProposalWorkflow(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated browser qualification")
	}
	s, e, _, _, p := proposalFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("ui-fixture-password"), bcrypt.MinCost)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','UI fixture','fixture',?); INSERT INTO memberships VALUES('owner','org')`, hash)
	must(t, err)
	w := NewWeb(s, e, false)
	mux := http.NewServeMux()
	mux.Handle("/", w.Handler())
	mux.HandleFunc("/fixture-proposal-reply", func(rw http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, task := range list[Assignment](s, "assignment", p.Org) {
			if task.Kind == "proposal" {
				run := taskRuns(s, task.ID)[0]
				run.State = "running"
				_ = s.Put("run", run.Org, run.Task, run.State, run.ID, run)
				if _, err := e.finishProposalConversation(run, task, "Fixture reply: keep the inspection read-only and report unknowns."); err != nil {
					http.Error(rw, err.Error(), 500)
					return
				}
				rw.WriteHeader(204)
				return
			}
		}
		http.Error(rw, "missing fixture discussion", 500)
	})
	mux.HandleFunc("/fixture-proposal-add", func(rw http.ResponseWriter, r *http.Request) {
		extra := p
		extra.ID = "extra-fixture-proposal"
		extra.Title = "Fixture deferred proposal"
		if err := s.Put("proposal", extra.Org, extra.Task, extra.State, extra.ID, extra); err != nil {
			http.Error(rw, err.Error(), 500)
			return
		}
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/proposals-check.cjs")
	must(t, err)
	cmd := exec.Command(node, script, server.URL, p.ID)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	must(t, err)
	must(t, s.Get(p.ID, &p))
	if p.State != "accepted" {
		t.Fatal("browser acceptance not persisted")
	}
	var task Assignment
	must(t, s.Get(p.AcceptedTask, &task))
	if task.Authority != "observe" || task.Creator != "owner" {
		t.Fatal("browser acceptance changed selected authority/account")
	}
}
