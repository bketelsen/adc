package adc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

func TestBrowserExecutionPlan(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated plan browser checks")
	}
	s, e, task, root := fixture(t)
	transcriptLogin(t, s)
	var account Account
	must(t, s.Get(task.Account, &account))
	account.User = task.Creator
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	_, err := e.saveExecutionPlan(root, planFixtureInput())
	must(t, err)
	mux := http.NewServeMux()
	mux.Handle("/", NewWeb(s, e, false).Handler())
	mux.HandleFunc("/fixture-plan-dispatch", func(rw http.ResponseWriter, r *http.Request) { planDispatch(e); rw.WriteHeader(204) })
	mux.HandleFunc("/fixture-plan-review", func(rw http.ResponseWriter, r *http.Request) {
		step := planStepByKey(t, s, "R1")
		completePlanWorker(t, e, step)
		reviewPlanStep(t, e, step, "pass")
		planDispatch(e)
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/execution-plan.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	if s.taskPlan(task.ID).Steps[1].Run == "" {
		t.Fatal("review failed to dispatch successor")
	}
}

func TestBrowserMilestones(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated milestone browser checks")
	}
	s, e, r, step := milestoneFixture(t, "human-evidence")
	transcriptLogin(t, s)
	var account Account
	must(t, s.Get("account", &account))
	account.User = "owner"
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	_, err := call(t, e, r, "adc_wait", map[string]any{})
	must(t, err)
	mux := http.NewServeMux()
	mux.Handle("/", NewWeb(s, e, false).Handler())
	mux.HandleFunc("/fixture-evidence", func(rw http.ResponseWriter, req *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		var worker Run
		must(t, s.Get(r.ID, &worker))
		_, err := e.submitMilestone(worker, milestonePayload("human-evidence"), "another-human")
		must(t, err)
		rw.WriteHeader(204)
	})
	mux.HandleFunc("/fixture-review", func(rw http.ResponseWriter, req *http.Request) {
		completePlanWorker(t, e, step)
		reviewPlanStep(t, e, step, "pass")
		planDispatch(e)
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/milestones.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
}

func TestBrowserWaits(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated wait browser checks")
	}
	s, e, r, step, matched, _ := externalWaitFixture(t)
	transcriptLogin(t, s)
	var account Account
	must(t, s.Get("account", &account))
	account.User = "owner"
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	_, err := e.startWait(r, "gate", false)
	must(t, err)
	_, err = call(t, e, r, "adc_wait", map[string]any{})
	must(t, err)
	dispatchWaitFixture(e, time.Now())
	mux := http.NewServeMux()
	mux.Handle("/", NewWeb(s, e, false).Handler())
	mux.HandleFunc("/fixture-observe", func(rw http.ResponseWriter, req *http.Request) {
		matched.Store(true)
		s.mu.Lock()
		waitDue(t, s, s.runWait(r.ID, "gate"))
		s.mu.Unlock()
		dispatchWaitFixture(e, time.Now())
		rw.WriteHeader(204)
	})
	mux.HandleFunc("/fixture-review", func(rw http.ResponseWriter, req *http.Request) {
		completePlanWorker(t, e, step)
		reviewPlanStep(t, e, step, "pass")
		planDispatch(e)
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/waits.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
}

func TestBrowserIntegrationEvidence(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated evidence browser checks")
	}
	s, e, r, _ := integrationFixture(t)
	repo := integrationRepo(t, e, &r)
	_, err := e.saveIntegration(r, integrationInput{Repositories: []RepositoryEvidence{repo}, Environment: []EnvironmentVersion{{Name: "compiler", Version: "fixture-v1"}}})
	must(t, err)
	integrationCheck(t, e, r, "unit", "pass")
	transcriptLogin(t, s)
	mux := http.NewServeMux()
	mux.Handle("/", NewWeb(s, e, false).Handler())
	mux.HandleFunc("/fixture-change", func(rw http.ResponseWriter, req *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		old := s.integrationEvidence(r.ID)
		_, err := e.saveIntegration(r, integrationInput{Revision: old.Revision, Repositories: []RepositoryEvidence{repo}, Environment: []EnvironmentVersion{{Name: "compiler", Version: "fixture-v2"}}})
		must(t, err)
		rw.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/integration.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
}

func TestBrowserPreflightAndGitHubConnection(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated preflight browser checks")
	}
	s, e, worker, _ := preflightPlan(t)
	v := ExecutionReadiness{ID: "preflight:" + worker.ID, Org: worker.Org, Task: worker.Task, Run: worker.ID, State: "blocked", Checks: []PreflightCheck{{Name: "Independent review model", State: "blocked", Detail: "Fixture reviewer unavailable"}}}
	must(t, s.Put("preflight", v.Org, v.Task, v.State, v.ID, v))
	transcriptLogin(t, s)
	mux := http.NewServeMux()
	mux.Handle("/", NewWeb(s, e, false).Handler())
	mux.HandleFunc("/fixture-ready", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		v.State = "ready"
		v.Checks[0].State = "pass"
		v.Checks[0].Detail = "Fixture reviewer restored"
		must(t, s.Put("preflight", v.Org, v.Task, v.State, v.ID, v))
		w.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/preflight.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	found := false
	for _, conn := range list[Connection](s, "connection", worker.Org) {
		if conn.Transport == "github" {
			found = true
			if len(conn.Args) != 1 || conn.Args[0] != "fixture/project" {
				t.Fatal("scope missing")
			}
			if conn.Headers["GitHubToken"] == "synthetic-browser-token" {
				t.Fatal("unsealed token")
			}
			token, err := s.Unseal(conn.Headers["GitHubToken"])
			must(t, err)
			if token != "synthetic-browser-token" {
				t.Fatal("token not stored")
			}
		}
	}
	if !found {
		t.Fatal("GitHub connection not saved")
	}
}

func TestBrowserSelfhostedConnection(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for self-hosted browser qualification")
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-selfhosted-key" {
			http.Error(w, "missing key", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen3.8-27B-MTP-Coding","labels":["chat","tool-calling"]}]}`))
	}))
	defer provider.Close()
	s, e, _, _ := fixture(t)
	transcriptLogin(t, s)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/selfhosted.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL, provider.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	found := false
	for _, a := range list[Account](s, "account", "") {
		if a.Provider == "selfhosted" {
			found = true
			if a.Limit != 1 || a.BaseURL != provider.URL+"/v1" || a.Secret == "synthetic-selfhosted-key" {
				t.Fatal("invalid connection state")
			}
		}
	}
	if !found {
		t.Fatal("self-hosted connection not created")
	}
}
