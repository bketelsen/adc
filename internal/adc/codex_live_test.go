package adc

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestLiveCrossProviderCompletion(t *testing.T) { qualifyLiveProviderCompletion(t, false) }
func TestLiveDirectClaudeCompletion(t *testing.T)  { qualifyLiveProviderCompletion(t, true) }
func qualifyLiveProviderCompletion(t *testing.T, directClaude bool) {
	home := os.Getenv("ADC_LIVE_CODEX_HOME")
	if home == "" {
		t.Skip("set ADC_LIVE_CODEX_HOME to an explicitly connected ADC Codex account directory; also uses signed-in Copilot")
	}
	if directClaude && os.Getenv("ADC_LIVE_CLAUDE_HOME") == "" {
		t.Skip("connect a private ADC Claude account and set ADC_LIVE_CLAUDE_HOME")
	}
	s, e, seed, seedRun := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	client, err := startCodex(ctx, home)
	must(t, err)
	e.codexClients["code-account"] = client
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused');INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	cop := Account{ID: "account", User: "owner", Provider: "copilot", Local: true, Limit: 2}
	if directClaude {
		cop.Provider, cop.Local = "claude", false
		cc, err := startClaude(ctx, os.Getenv("ADC_LIVE_CLAUDE_HOME"))
		must(t, err)
		e.codexClients[cop.ID] = cc
	}
	code := Account{ID: "code-account", User: "owner", Provider: "codex", Limit: 2}
	for _, a := range []Account{cop, code} {
		must(t, s.Put("account", "", a.User, "", a.ID, a))
	}
	reviewModel := "claude-opus-5"
	if directClaude {
		reviewModel = liveClaudeReviewModel(t, e, cop, ctx)
	}
	for _, a := range list[Agent](s, "agent", seed.Org) {
		a.Tools = nil
		if Family(a.Model) == "openai-gpt" {
			a.Provider = "codex"
			a.Model = "gpt-5.6-sol"
		} else {
			a.Provider, a.Model = cop.Provider, reviewModel
		}
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	seed.State = "ready"
	seedRun.State = "complete"
	must(t, s.Put("assignment", seed.Org, "", seed.State, seed.ID, seed))
	must(t, s.Put("run", seedRun.Org, seedRun.Task, seedRun.State, seedRun.ID, seedRun))
	task := Assignment{ID: ID(), Org: seed.Org, Owner: "boss", Account: code.ID, ExtraAccount: cop.ID, Creator: "owner", Title: "Synthetic cross-provider qualification", Prompt: "Synthetic fixture facts: Snow and Floe ship; NBC is retired. Supervisor: delegate to dev to save a short ADC document with these three facts, then arrange independent review by qa with ReviewOf targeting the completed author. Developer uses Codex; qa uses Claude through the already selected reviewer subscription. Use adc_wait for pending children, adc_finish for completed work and adc_review for the review verdict. Complete after the actual independent review passes. Use only ADC tools. No real repository, shell, web, MCP or infrastructure work and no human decision needed."}
	must(t, e.CreateAssignment(task))
	e.Start(ctx)
	defer e.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("mixed-provider workflow did not finish")
		case <-ticker.C:
			must(t, s.Get(task.ID, &task))
			if task.State == "needs input" {
				t.Fatal("routine mixed-provider work needed human continuation")
			}
			if task.State != "ready" {
				continue
			}
			if len(taskDocs(s, task.ID)) == 0 || len(taskReviews(s, task.ID)) == 0 {
				t.Fatal("mixed-provider workflow skipped review")
			}
			for _, trace := range taskTraces(s, task.ID) {
				if trace.State == "failed" {
					t.Fatalf("failed tool: %s: %s", trace.Name, trace.Result)
				}
			}
			providers := map[string]bool{}
			for _, ev := range s.Events(task.ID) {
				if ev.Kind == "usage" {
					var u usageSample
					must(t, json.Unmarshal([]byte(ev.Text), &u))
					providers[u.Provider] = true
				}
			}
			if !providers["codex"] || !providers[cop.Provider] {
				t.Fatal("missing usage for one provider")
			}
			t.Logf("Codex supervisor and author completed through independent Claude review on provider %s with both subscriptions attributed.", cop.Provider)
			return
		}
	}
}

// Select the explicit Opus 5 variant offered by this fixture's own account.
// This happens before assignment creation, never as a fallback on a live run.
func liveClaudeReviewModel(t *testing.T, e *Engine, a Account, ctx context.Context) string {
	t.Helper()
	models, err := e.claudeModels(ctx, a)
	must(t, err)
	for _, want := range []string{"claude-opus-5", "claude-opus-5[1m]"} {
		for _, m := range models {
			if m.ID == want {
				return m.ID
			}
		}
	}
	t.Fatal("qualification requires an explicitly available Opus 5 model")
	return ""
}
