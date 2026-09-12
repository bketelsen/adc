package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func selfhostedFixture(t *testing.T, handler http.HandlerFunc) (*Store, *Engine, Assignment, Run, Account) {
	t.Helper()
	s, e, task, run := fixture(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	key, err := s.Seal("synthetic-model-api-key")
	must(t, err)
	a := Account{ID: "local-model", User: task.Creator, Name: "Fixture server", Provider: "selfhosted", BaseURL: server.URL + "/v1", Secret: key, Limit: 1}
	must(t, s.Put("account", "", a.User, "", a.ID, a))
	task.Account = a.ID
	task.Execution = "protected"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	run.Account = a.ID
	run.Provider = a.Provider
	run.Model = "Qwen3.8-27B-MTP-Coding"
	run.Family = Family(run.Model)
	run.Execution = "protected"
	run.Tools = nil
	supervisor := run
	supervisor.State = "waiting"
	must(t, s.Put("run", supervisor.Org, supervisor.Task, supervisor.State, supervisor.ID, supervisor))
	run.ID = ID()
	run.Parent = supervisor.ID
	run.Agent = "dev"
	run.Category = "implementation"
	run.Workspace += "-local-worker"
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	return s, e, task, run, a
}
func chatFixtureResponse(w http.ResponseWriter, model, id, name, args, reason string) {
	message := map[string]any{"role": "assistant", "content": ""}
	if name != "" {
		message["tool_calls"] = []any{map[string]any{"id": id, "type": "function", "function": map[string]string{"name": name, "arguments": args}}}
	}
	json.NewEncoder(w).Encode(map[string]any{"id": "fixture-response", "model": model, "choices": []any{map[string]any{"message": message, "finish_reason": reason}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "prompt_tokens_details": map[string]any{"cached_tokens": 12}}})
}
func TestSelfhostedToolLoopDurableOutcomesUsageAndNoKeyInPrompt(t *testing.T) {
	var calls atomic.Int32
	s, e, task, run, a := selfhostedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-model-api-key" {
			t.Error("missing API auth")
		}
		var payload struct {
			Model    string
			Messages []chatMessage
			Tools    []map[string]any
			Stream   bool
		}
		must(t, json.NewDecoder(r.Body).Decode(&payload))
		encoded, _ := json.Marshal(payload)
		if strings.Contains(string(encoded), "synthetic-model-api-key") {
			t.Error("API key entered model context")
		}
		if len(payload.Tools) == 0 || payload.Stream {
			t.Error("tool definitions absent or unsupported stream")
		}
		if calls.Add(1) == 1 {
			chatFixtureResponse(w, payload.Model, "status", "adc_status", "{}", "tool_calls")
		} else {
			last := payload.Messages[len(payload.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "status" {
				t.Error("tool response correlation lost")
			}
			chatFixtureResponse(w, payload.Model, "finish", "adc_finish", `{"Result":"Synthetic local model task completed"}`, "tool_calls")
		}
	})
	must(t, e.executeSelfhosted(context.Background(), run, task, a, "Synthetic fixture system", []byte(`{"fixture":true}`), Redactor{Values: []string{"synthetic-model-api-key"}}))
	var saved Run
	must(t, s.Get(run.ID, &saved))
	if saved.State != "complete" || calls.Load() != 2 {
		t.Fatal("outcome not durable", saved.State, calls.Load())
	}
	reopened, err := Open(s.Dir)
	must(t, err)
	defer reopened.Close()
	must(t, reopened.Get(run.ID, &saved))
	if saved.Result != "Synthetic local model task completed" {
		t.Fatal("result lost after reopen")
	}
	report, err := s.usageReport(run.Org, task.ID, 0, 0, time.Now())
	must(t, err)
	if report.Total.Input.Value != 200 || report.Total.Output.Value != 40 || report.Total.CacheRead.Value != 24 {
		t.Fatal("usage attribution incorrect", report.Total)
	}
	if len(taskTraces(s, task.ID)) != 2 {
		t.Fatal("missing live tool transcripts")
	}
}
func TestSelfhostedRejectsMismatchedModelAndTruncatedTools(t *testing.T) {
	for _, mode := range []string{"model", "truncated", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			s, e, task, run, a := selfhostedFixture(t, func(w http.ResponseWriter, r *http.Request) {
				model := "Qwen3.8-27B-MTP-Coding"
				reason := "tool_calls"
				if mode == "model" {
					model = "gpt-5.6-sol"
				}
				if mode == "truncated" {
					reason = "length"
				}
				if mode == "duplicate" {
					call := map[string]any{"id": "same", "type": "function", "function": map[string]string{"name": "adc_finish", "arguments": `{"Result":"must not execute"}`}}
					json.NewEncoder(w).Encode(map[string]any{"model": model, "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{call, call}}, "finish_reason": reason}}})
					return
				}
				chatFixtureResponse(w, model, "bad", "adc_finish", `{"Result":"must not execute"}`, reason)
			})
			if e.executeSelfhosted(context.Background(), run, task, a, "fixture", nil, Redactor{}) == nil {
				t.Fatal("unsafe completion accepted")
			}
			var current Run
			must(t, s.Get(run.ID, &current))
			if current.State != "running" || len(taskTraces(s, task.ID)) != 0 {
				t.Fatal("rejected response dispatched effects")
			}
		})
	}
}
func TestSelfhostedCancellationDoesNotReplayInferenceOrDispatchTools(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	s, e, task, run, a := selfhostedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		chatFixtureResponse(w, "Qwen3.8-27B-MTP-Coding", "late", "adc_finish", `{"Result":"too late"}`, "tool_calls")
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- e.executeSelfhosted(ctx, run, task, a, "fixture", nil, Redactor{}) }()
	<-entered
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled request succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider ignored cancellation")
	}
	close(release)
	if calls.Load() != 1 || len(taskTraces(s, task.ID)) != 0 {
		t.Fatal("cancelled inference replayed or executed tool")
	}
}
func TestSelfhostedConnectionOwnershipSealingAndActiveEdit(t *testing.T) {
	s, e, _, run := fixture(t)
	w := NewWeb(s, e, false)
	form := url.Values{"name": {"Private endpoint"}, "provider": {"selfhosted"}, "base_url": {"http://127.0.0.1:13305"}, "token": {"private-optional-key"}, "limit": {"1"}}
	req := formRequest("/accounts?org=org", form)
	must(t, w.saveAccount(req, User{ID: "owner"}))
	var a Account
	for _, candidate := range list[Account](s, "account", "") {
		if candidate.Provider == "selfhosted" {
			a = candidate
		}
	}
	if a.ID == "" || a.Secret == "private-optional-key" || a.BaseURL != "http://127.0.0.1:13305/v1" {
		t.Fatal("connection not normalized/sealed", a.ID, a.BaseURL)
	}
	form.Set("id", a.ID)
	form.Set("token", "")
	req = formRequest("/accounts?org=org", form)
	if w.saveAccount(req, User{ID: "other"}) == nil {
		t.Fatal("cross-human edit permitted")
	}
	must(t, w.saveAccount(req, User{ID: "owner"}))
	must(t, s.Get(a.ID, &a))
	key, err := s.Unseal(a.Secret)
	must(t, err)
	if key != "private-optional-key" {
		t.Fatal("blank token erased saved key")
	}
	run.Account = a.ID
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	e.active[run.ID] = func() {}
	req.Form.Set("base_url", "http://other-server/v1")
	if w.saveAccount(req, User{ID: "owner"}) == nil {
		t.Fatal("active endpoint changed")
	}
	delete(e.active, run.ID)
	req.Form.Set("clear_token", "on")
	must(t, w.saveAccount(req, User{ID: "owner"}))
	must(t, s.Get(a.ID, &a))
	if a.Secret != "" {
		t.Fatal("explicit clear failed")
	}
	for _, value := range []string{"file:///tmp/api", "http://name:key@host/v1", "http://host/v1?key=secret", "http://host/v1#fragment"} {
		if _, err := selfhostedBase(value); err == nil {
			t.Fatal("invalid base accepted", value)
		}
	}
}
func TestSelfhostedCatalogFamiliesAndRedirectBoundary(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer other.Close()
	s, e, _, _, a := selfhostedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "Qwen3.8-27B-MTP-Coding", "labels": []string{"chat", "tool-calling"}}, map[string]any{"id": "Qwen-embedding", "labels": []string{"embeddings"}}, map[string]any{"id": "unknown-alias"}, map[string]any{"id": "openai/gpt-oss-20b"}, map[string]any{"id": "Qwen-chat-only", "labels": []string{"chat"}}, map[string]any{"id": "Qwen-custom-label", "labels": []string{"vendor-custom"}}}})
			return
		}
		http.Redirect(w, r, other.URL, 302)
	})
	models, err := e.Models(context.Background(), a)
	must(t, err)
	if len(models) != 4 {
		t.Fatal("catalog filtering failed", models)
	}
	if CanReview(models[0].ID, "qwen/another/Qwen3-32B") == nil {
		t.Fatal("same family review accepted")
	}
	must(t, CanReview(models[0].ID, "claude-opus-5"))
	if _, err := s.selfhostedRequest(context.Background(), a, "/redirect", nil); err == nil || redirected.Load() != 0 {
		t.Fatal("credential redirect followed")
	}
}
func TestSelfhostedRejectsAdvisoryExecutionWithoutChangingOtherProviders(t *testing.T) {
	if validateSelfhostedExecution(Assignment{}, "selfhosted") == nil {
		t.Fatal("unsupported tool boundary silently accepted")
	}
	for _, provider := range []string{"copilot", "codex", "claude"} {
		must(t, validateSelfhostedExecution(Assignment{}, provider))
	}
	must(t, validateSelfhostedExecution(Assignment{Kind: "proposal"}, "selfhosted"))
	if err := validateProvider("selfhosted"); err != nil {
		t.Fatal(fmt.Sprint(err))
	}
}

func TestSelfhostedProviderErrorsRedactCredential(t *testing.T) {
	s, _, _, _, a := selfhostedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "rate limit for synthetic-model-api-key"}})
	})
	_, err := s.selfhostedRequest(context.Background(), a, "/chat/completions", map[string]string{"model": "fixture"})
	if err == nil || strings.Contains(err.Error(), "synthetic-model-api-key") || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limit") {
		t.Fatal("provider error lost useful detail or leaked key", err)
	}
}
func TestSelfhostedMalformedToolArgumentsCanBeCorrected(t *testing.T) {
	var calls atomic.Int32
	s, e, task, run, a := selfhostedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Model    string
			Messages []chatMessage
		}
		must(t, json.NewDecoder(r.Body).Decode(&p))
		if calls.Add(1) == 1 {
			chatFixtureResponse(w, p.Model, "bad-json", "adc_finish", `{"Result":"wrong"} {"extra":"invalid"}`, "tool_calls")
			return
		}
		if len(p.Messages) < 4 || p.Messages[len(p.Messages)-1].ToolCallID != "bad-json" {
			t.Error("malformed call did not return a correlated error")
		}
		chatFixtureResponse(w, p.Model, "corrected", "adc_finish", `{"Result":"Corrected fixture result"}`, "tool_calls")
	})
	must(t, e.executeSelfhosted(context.Background(), run, task, a, "fixture", nil, Redactor{}))
	var current Run
	must(t, s.Get(run.ID, &current))
	if current.Result != "Corrected fixture result" || calls.Load() != 2 {
		t.Fatal("malformed arguments executed or correction failed")
	}
}

func TestSelfhostedContentPartsAreVisibleButReasoningIsNot(t *testing.T) {
	s, e, task, run, a := selfhostedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		message := map[string]any{"role": "assistant", "content": []any{map[string]string{"type": "text", "text": "Fixture content "}, map[string]string{"type": "output_text", "text": "is visible."}}, "reasoning_content": "unexposed fixture reasoning", "tool_calls": []any{map[string]any{"id": "finish", "type": "function", "function": map[string]string{"name": "adc_finish", "arguments": `{"Result":"Fixture finished"}`}}}}
		json.NewEncoder(w).Encode(map[string]any{"model": "Qwen3.8-27B-MTP-Coding", "choices": []any{map[string]any{"message": message, "finish_reason": "tool_calls"}}})
	})
	must(t, e.executeSelfhosted(context.Background(), run, task, a, "fixture", nil, Redactor{}))
	found := false
	for _, event := range s.Events(task.ID) {
		if event.Kind == "message" && event.Text == "Fixture content is visible." {
			found = true
		}
		if strings.Contains(event.Text, "unexposed fixture reasoning") {
			t.Fatal("opaque reasoning exposed in activity")
		}
	}
	if !found {
		t.Fatal("text content parts lost from transcript")
	}
}
func TestSelfhostedOptionalKeyLeavesErrorTextIntact(t *testing.T) {
	s, _, _, _, a := selfhostedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("unexpected authorization header")
		}
		w.WriteHeader(503)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "model warming up"}})
	})
	a.Secret = ""
	_, err := s.selfhostedRequest(context.Background(), a, "/models", nil)
	if err == nil || !strings.Contains(err.Error(), "model warming up") || strings.Contains(err.Error(), "[redacted]") {
		t.Fatal("empty configured secret damaged error text", err)
	}
}
func TestDocumentCreationExplainsServerAssignedID(t *testing.T) {
	s, e, _, r := fixture(t)
	_, err := call(t, e, r, "adc_document", map[string]any{"ID": "invented", "Title": "Fixture", "Content": "body", "Source": "fixture"})
	if err == nil || !strings.Contains(err.Error(), "omit ID") {
		t.Fatal("unhelpful creation error", err)
	}
	_, err = call(t, e, r, "adc_document", map[string]any{"Title": "Fixture", "Content": "body", "Source": "fixture"})
	must(t, err)
	docs := taskDocs(s, r.Task)
	if len(docs) != 1 || docs[0].ID == "invented" {
		t.Fatal("ADC did not allocate document identity")
	}
}
