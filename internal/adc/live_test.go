package adc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in subscription-backed qualification, separate from the default test suite.
// It uses synthetic content in a private temporary store, never a real repository.
func TestLiveSupervisorCompletion(t *testing.T) {
	if os.Getenv("ADC_LIVE_TEST") != "1" {
		t.Skip("set ADC_LIVE_TEST=1 to use the connected Copilot subscription")
	}
	s, e, task, root := fixture(t)
	account := Account{ID: "account", User: "owner", Name: "Live fixture account", Local: true, Limit: 2}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	task.Prompt = "This is a synthetic ADC qualification fixture, not real infrastructure. Authoritative facts: Snow and Floe are shipping; NBC is retired. Create a short website copy document reflecting those facts. Supervisor: delegate implementation to agent dev using adc_delegate, then adc_wait; the worker will schedule its independent review before finishing, so inspect adc_status and wait for that existing reviewer instead of creating a duplicate. When review passes, adc_finish with a concise summary. Do not use shell, external tools, or GitHub. All work must use ADC tools. Worker: use adc_message to send the supervisor a brief evidence update, then save copy using adc_document, schedule reviewer qa using adc_delegate with ReviewOf set to your own run ID while you are still running, then immediately adc_finish; do not wait on your own reviewer. Reviewer: compare the document to these facts and use adc_review. No human decision is needed for this fixture."
	root.Prompt = task.Prompt
	root.State = "queued"
	root.Tools = nil
	root.Model = "gpt-5.6-sol"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	for _, a := range list[Agent](s, "agent", task.Org) {
		a.Tools = nil
		if Family(a.Model) == "openai-gpt" {
			a.Model = "gpt-5.6-sol"
		}
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	e.Start(ctx)
	defer e.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, r := range taskRuns(s, task.ID) {
				t.Logf("%s: %s %s", r.Title, r.State, r.Error)
			}
			t.Fatal("live assignment did not finish before timeout")
		case <-ticker.C:
			must(t, s.Get(task.ID, &task))
			if task.State == "ready" {
				t.Log(task.Output)
				if len(taskDocs(s, task.ID)) == 0 || len(taskReviews(s, task.ID)) == 0 {
					t.Fatal("missing evidence")
				}
				messaged := false
				for _, trace := range taskTraces(s, task.ID) {
					if trace.Name == "write_agent" || trace.Name == "read_agent" {
						t.Fatal("provider-native agent tool was exposed")
					}
					if trace.Name == "adc_message" && trace.State == "complete" {
						messaged = true
					}
					if strings.HasPrefix(trace.Name, "adc_") && trace.State == "failed" {
						t.Fatalf("ADC tool failed during lifecycle: %s: %s", trace.Name, trace.Result)
					}
				}
				if !messaged {
					t.Fatal("collaboration message was not delivered")
				}
				return
			}
			for _, d := range taskDecisions(s, task.ID) {
				if d.State == "pending" {
					t.Fatalf("fixture unexpectedly requested human intervention: %s", d.Question)
				}
			}
		}
	}
}

// Confirm that the provider can see and yield through the blocked outcome tool;
// no real infrastructure or credentials are involved in this isolated fixture.
func TestLiveMissingAccessReportsBlocked(t *testing.T) {
	if os.Getenv("ADC_LIVE_BLOCKED_TEST") != "1" {
		t.Skip("set ADC_LIVE_BLOCKED_TEST=1 to use the connected Copilot subscription")
	}
	s, e, task, root := fixture(t)
	account := Account{ID: "account", User: "owner", Name: "Blocked fixture account", Local: true, Limit: 1}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	task.Prompt = "Synthetic ADC qualification: obtain a current inventory from the storage MCP connection. No storage connection is configured or granted in this fixture, so current observations cannot be obtained. Do not use shell, web, files or alternative clients. Handle this unmet prerequisite with the appropriate durable outcome. An explanation of missing access is not the requested inventory."
	root.Prompt = task.Prompt
	root.State = "queued"
	root.Tools = nil
	root.Model = "gpt-5.6-sol"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e.Start(ctx)
	defer e.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("missing prerequisite did not produce a durable blocker")
		case <-ticker.C:
			must(t, s.Get(task.ID, &task))
			must(t, s.Get(root.ID, &root))
			if task.State == "ready" {
				t.Fatal("missing inventory was declared ready")
			}
			if task.State == "needs input" {
				if root.State != "blocked" || len(taskRuns(s, task.ID)) != 1 || len(taskReviews(s, task.ID)) != 0 {
					t.Fatal("missing prerequisite produced a review committee")
				}
				found := false
				for _, trace := range taskTraces(s, task.ID) {
					if trace.Name == "adc_blocked" && trace.State == "complete" {
						found = true
					}
				}
				if !found {
					continue
				} // Provider outcome evidence arrives just after persistence.
				t.Log("Unmet prerequisite recorded with adc_blocked; no reviewers, no ready state.")
				return
			}
		}
	}
}
