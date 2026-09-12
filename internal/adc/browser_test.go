package adc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBrowserWorkflow(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE to run the Playwright workflow against an isolated fixture")
	}
	s, e, task, root := fixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("ui-fixture-password"), bcrypt.MinCost)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','UI fixture','fixture',?); INSERT INTO memberships VALUES('owner','org')`, hash)
	must(t, err)
	_, err = call(t, e, root, "adc_document", map[string]any{"Title": "Website evidence", "Content": "Snow and Floe are shipping today."})
	must(t, err)
	doc := taskDocs(s, task.ID)[0]
	_, err = call(t, e, root, "adc_document", map[string]any{"ID": doc.ID, "Title": "Website evidence", "Content": "Snow and Floe ship today. NBC is retired."})
	must(t, err)
	task.State = "paused"
	root.State = "waiting"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	decision := Decision{ID: "input", Org: task.Org, Task: task.ID, Run: root.ID, Question: "Which website section should lead?", State: "pending", Kind: "scope"}
	must(t, s.Put("decision", decision.Org, decision.Task, decision.State, decision.ID, decision))
	for i := 0; i < 260; i++ {
		s.Log(task.Org, task.ID, root.ID, "message", fmt.Sprintf("Historical fixture event %03d", i))
	}
	for i := 0; i < 20; i++ {
		s.Log(task.Org, task.ID, root.ID, "tool", "Read source evidence")
		s.Log(task.Org, task.ID, root.ID, "message", "")
		s.Log(task.Org, task.ID, root.ID, "tool-result", "Read source evidence · complete")
	}
	s.Log(task.Org, task.ID, root.ID, "tool-result", "Inspect missing source · failed")
	trace := ToolTrace{ID: "fixture-trace", Org: task.Org, Task: task.ID, Run: root.ID, Name: "run_repository_checks", State: "complete", Arguments: `{"command":"go test ./..."}`, Result: "All fixture checks passed", At: now()}
	must(t, s.Put("tooltrace", trace.Org, trace.Task, trace.State, trace.ID, trace))
	w := NewWeb(s, e, false)
	mux := http.NewServeMux()
	mux.Handle("/", w.Handler())
	// This endpoint exists only on this ephemeral test server, never in ADC.
	mux.HandleFunc("/fixture-event", func(rw http.ResponseWriter, r *http.Request) {
		s.Log(task.Org, task.ID, root.ID, "tool", "Inspect updated source")
		s.Log(task.Org, task.ID, root.ID, "tool-result", "Inspect updated source · complete")
		s.Log(task.Org, task.ID, root.ID, "message", "A new evidence source is available.")
		rw.WriteHeader(204)
	})
	mux.HandleFunc("/fixture-state", func(rw http.ResponseWriter, r *http.Request) {
		updated := task
		updated.Title = "Updated fixture assignment"
		_ = s.Put("assignment", updated.Org, "", updated.State, updated.ID, updated)
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/interaction-check.cjs")
	must(t, err)
	cmd := exec.Command(node, script, server.URL, doc.ID)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	must(t, err)
	must(t, s.Get(task.ID, &task))
	if task.State != "paused" {
		t.Fatal("browser feedback resumed the paused assignment")
	}
	must(t, s.Get(decision.ID, &decision))
	if decision.State != "answered" {
		t.Fatal("human decision was not saved")
	}
}

func TestBrowserRunTranscripts(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated transcript browser checks")
	}
	s, e, task, run := fixture(t)
	transcriptLogin(t, s)
	sibling := run
	sibling.ID = "sibling"
	sibling.Title = "Concurrent fixture"
	must(t, s.Put("run", sibling.Org, sibling.Task, sibling.State, sibling.ID, sibling))
	for i := 0; i < 260; i++ {
		s.Log(task.Org, task.ID, run.ID, "message", fmt.Sprintf("Selected history %03d", i))
	}
	s.Log(task.Org, task.ID, sibling.ID, "message", "Private sibling activity")
	trace := ToolTrace{ID: "selected-trace", Org: task.Org, Task: task.ID, Run: run.ID, Name: "inspect_fixture", State: "running", Arguments: "{}", At: now()}
	must(t, s.Put("tooltrace", trace.Org, trace.Task, trace.State, trace.ID, trace))
	mux := http.NewServeMux()
	mux.Handle("/", NewWeb(s, e, false).Handler())
	mux.HandleFunc("/fixture-advance", func(rw http.ResponseWriter, r *http.Request) {
		s.Log(task.Org, task.ID, run.ID, "message", "Selected live update")
		s.Log(task.Org, task.ID, sibling.ID, "message", "Private sibling update")
		updated := trace
		updated.State = "complete"
		updated.Result = "Fixture evidence ready"
		_ = s.Put("tooltrace", updated.Org, updated.Task, updated.State, updated.ID, updated)
		finished := sibling
		finished.State = "complete"
		_ = s.Put("run", finished.Org, finished.Task, finished.State, finished.ID, finished)
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/run-transcripts.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL, run.ID).CombinedOutput()
	t.Log(string(out))
	must(t, err)
}
