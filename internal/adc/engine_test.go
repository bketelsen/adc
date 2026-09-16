package adc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
)

func fixture(t *testing.T) (*Store, *Engine, Assignment, Run) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	e := NewEngine(s)
	org := Organization{ID: "org", Name: "Fixture organization"}
	must(t, s.Put("organization", org.ID, "", "", org.ID, org))
	a := Account{ID: "account", User: "owner", Limit: 2}
	must(t, s.Put("account", "", a.User, "", a.ID, a))
	boss := Agent{ID: "boss", Org: "org", Name: "Supervisor", Description: "Own the outcome", Category: "supervision", Model: "gpt-6-astra", Authority: "draft", Tools: []string{"storage"}}
	dev := Agent{ID: "dev", Org: "org", Name: "Developer", Description: "Implementation", Category: "implementation", Model: "gpt-6-astra", Authority: "approved-action", Tools: []string{"storage"}}
	qa := Agent{ID: "qa", Org: "org", Name: "Reviewer", Description: "Independent verification", Category: "review", Model: "claude-opus-5", Authority: "observe"}
	for _, a := range []Agent{boss, dev, qa} {
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	// The shared fixture keeps the reviewed contract so the review, handoff and
	// plan gates stay exercised; routine behavior has its own fixtures.
	task := Assignment{ID: "task", Org: "org", Owner: "boss", Creator: "owner", Account: "account", Title: "Fixture", Prompt: "Finish a fixture", Completion: &CompletionPolicy{Mode: "reviewed", Version: 1}}
	must(t, e.CreateAssignment(task))
	must(t, s.Get(task.ID, &task))
	r := taskRuns(s, task.ID)[0]
	r.State = "running"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	return s, e, task, r
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func call(t *testing.T, e *Engine, r Run, name string, args any) (string, error) {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var p any
	must(t, json.Unmarshal(b, &p))
	for _, tool := range e.tools(r) {
		if tool.Name == name {
			result, err := tool.Handler(copilot.ToolInvocation{Arguments: p})
			return result.TextResultForLLM, err
		}
	}
	t.Fatalf("missing tool %s", name)
	return "", nil
}
func setRunning(t *testing.T, s *Store, r *Run) {
	t.Helper()
	must(t, s.Get(r.ID, r))
	r.State = "running"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, *r))
}
func TestDelegationCannotExpandAuthorityOrTools(t *testing.T) {
	s, e, _, root := fixture(t)
	_, err := call(t, e, root, "adc_delegate", map[string]any{"Agent": "dev", "Title": "Work", "Prompt": "Implement", "Tools": []string{"other"}})
	if err == nil {
		t.Fatal("additional MCP access accepted")
	}
	_, err = call(t, e, root, "adc_delegate", map[string]any{"Agent": "dev", "Title": "Work", "Prompt": "Implement"})
	must(t, err)
	runs := taskRuns(s, "task")
	if len(runs) != 2 {
		t.Fatal(runs)
	}
	for _, r := range runs {
		if r.Parent != "" && r.Authority != "draft" {
			t.Fatal("authority expanded", r.Authority)
		}
	}
}
func TestDifferentProviderIsNotEnoughForReview(t *testing.T) {
	if CanReview("gpt-6-astra", "gpt-5.6-sol") == nil {
		t.Fatal("same family accepted")
	}
	if CanReview("auto", "claude-opus-5") == nil {
		t.Fatal("unknown automatic family accepted")
	}
	must(t, CanReview("gpt-6-astra", "claude-opus-5"))
}
func TestReviewFindingsAutomaticallyQueueCorrection(t *testing.T) {
	s, e, _, root := fixture(t)
	_, err := call(t, e, root, "adc_delegate", map[string]any{"Agent": "dev", "Title": "Implementation", "Prompt": "Implement fixture"})
	must(t, err)
	var dev Run
	for _, r := range taskRuns(s, "task") {
		if r.Agent == "dev" {
			dev = r
		}
	}
	setRunning(t, s, &dev)
	_, err = call(t, e, dev, "adc_finish", map[string]any{"Result": "Initial output"})
	must(t, err)
	_, err = call(t, e, root, "adc_delegate", map[string]any{"Agent": "qa", "Title": "Review", "Prompt": "Verify output", "ReviewOf": dev.ID})
	must(t, err)
	var qa Run
	for _, r := range taskRuns(s, "task") {
		if r.Agent == "qa" {
			qa = r
		}
	}
	setRunning(t, s, &qa)
	_, err = call(t, e, qa, "adc_review", map[string]any{"Verdict": "changes", "Findings": "Correct the unsupported shipping claim."})
	must(t, err)
	must(t, s.Get(dev.ID, &dev))
	must(t, s.Get(qa.ID, &qa))
	if dev.State != "queued" || qa.State != "waiting" {
		t.Fatalf("correction was not scheduled: dev=%s qa=%s", dev.State, qa.State)
	}
	if !strings.Contains(dev.Prompt, "unsupported shipping") {
		t.Fatal("findings lost")
	}
}
func TestCompletionRejectsMissingAndStaleReview(t *testing.T) {
	s, e, _, root := fixture(t)
	_, err := call(t, e, root, "adc_finish", map[string]any{"Result": "Done"})
	if err == nil {
		t.Fatal("unreviewed assignment completed")
	}
	dev := Run{ID: "devrun", Org: root.Org, Task: root.Task, Parent: root.ID, Category: "implementation", Model: "gpt-6-astra", State: "complete", Result: "version 1"}
	must(t, s.Put("run", dev.Org, dev.Task, dev.State, dev.ID, dev))
	review := Review{ID: "review", Org: root.Org, Task: root.Task, Target: dev.ID, Revision: e.revision(dev), Model: "claude-opus-5", Verdict: "pass"}
	must(t, s.Put("review", review.Org, review.Task, review.Verdict, review.ID, review))
	dev.Result = "version 2"
	must(t, s.Put("run", dev.Org, dev.Task, dev.State, dev.ID, dev))
	_, err = call(t, e, root, "adc_finish", map[string]any{"Result": "Done"})
	if err == nil {
		t.Fatal("stale review accepted")
	}
	review.Revision = e.revision(dev)
	must(t, s.Put("review", review.Org, review.Task, review.Verdict, review.ID, review))
	_, err = call(t, e, root, "adc_finish", map[string]any{"Result": "Ready for review"})
	must(t, err)
}
func TestDuplicateDelegationReturnsExistingRun(t *testing.T) {
	s, e, _, root := fixture(t)
	args := map[string]any{"Agent": "dev", "Title": "Work", "Prompt": "Same bounded task"}
	for i := 0; i < 2; i++ {
		_, err := call(t, e, root, "adc_delegate", args)
		must(t, err)
	}
	if len(taskRuns(s, "task")) != 2 {
		t.Fatal("duplicate worker created")
	}
}
func TestCredentialAndDocumentPersistence(t *testing.T) {
	s, e, _, root := fixture(t)
	secret, err := s.Seal("private-token")
	must(t, err)
	if strings.Contains(secret, "private-token") {
		t.Fatal("plaintext credential")
	}
	plain, err := s.Unseal(secret)
	must(t, err)
	if plain != "private-token" {
		t.Fatal("credential changed")
	}
	_, err = call(t, e, root, "adc_document", map[string]any{"Title": "Evidence", "Content": "original", "Source": "https://example.org/source"})
	must(t, err)
	doc := taskDocs(s, "task")[0]
	_, err = call(t, e, root, "adc_document", map[string]any{"ID": doc.ID, "Title": "Evidence", "Content": "revised"})
	must(t, err)
	must(t, s.Get(doc.ID, &doc))
	if doc.Revision != 2 {
		t.Fatal("revision not advanced")
	}
	rs := list[Document](s, "revision", "org")
	if len(rs) != 1 || rs[0].Content != "original" {
		t.Fatal("history lost")
	}
}
func TestRestartRequeuesInterruptedWork(t *testing.T) {
	s, e, _, root := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.Start(ctx)
	e.Stop()
	must(t, s.Get(root.ID, &root))
	if root.State != "queued" || !strings.Contains(root.Prompt, "Inspect existing work") {
		t.Fatal("interrupted work not reconciled")
	}
}
func TestAllTemplatesRenderActualStates(t *testing.T) {
	s, e, task, root := fixture(t)
	w := NewWeb(s, e, false)
	for _, view := range []string{"auth", "work", "team", "connections", "settings", "library", "task", "stewards"} {
		t.Run(view, func(t *testing.T) {
			p := Page{View: view, User: User{ID: "owner", Name: "Test"}, Org: Organization{ID: "org", Name: "Fixture"}, Task: task, Runs: []Run{root}, Agents: list[Agent](s, "agent", "org")}
			var out bytes.Buffer
			must(t, w.templates.ExecuteTemplate(&out, "page", p))
			if !strings.Contains(out.String(), "Aide de Camp") {
				t.Fatal("missing app")
			}
		})
	}
}
func TestAuthAndCrossOriginProtection(t *testing.T) {
	s, e, _, _ := fixture(t)
	w := NewWeb(s, e, false)
	req := httptest.NewRequest(http.MethodPost, "http://adc.test/setup", strings.NewReader(url.Values{"username": {"test"}, "name": {"Test"}, "organization": {"Org"}, "password": {"long-fixture-password"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://evil.test")
	res := httptest.NewRecorder()
	w.Handler().ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal("cross-origin setup allowed", res.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	res = httptest.NewRecorder()
	w.Handler().ServeHTTP(res, req)
	if !strings.Contains(res.Body.String(), "Create workspace") {
		t.Fatal("setup unavailable")
	}
}
