package adc

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveScheduledOccurrenceGetsIndependentReview(t *testing.T) {
	if os.Getenv("ADC_LIVE_SCHEDULE_TEST") != "1" {
		t.Skip("set ADC_LIVE_SCHEDULE_TEST for synthetic Copilot qualification")
	}
	s, e, seed, seedRun := fixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	account := Account{ID: "account", User: "owner", Name: "Schedule fixture", Local: true, Limit: 2}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	for _, agent := range list[Agent](s, "agent", seed.Org) {
		agent.Tools = nil
		if Family(agent.Model) == "openai-gpt" {
			agent.Model = "gpt-5.6-sol"
		}
		must(t, s.Put("agent", agent.Org, "", "", agent.ID, agent))
	}
	seed.State = "ready"
	seedRun.State = "complete"
	must(t, s.Put("assignment", seed.Org, "", seed.State, seed.ID, seed))
	must(t, s.Put("run", seedRun.Org, seedRun.Task, seedRun.State, seedRun.ID, seedRun))
	proposal := WorkProposal{ID: "approved-fixture", Org: seed.Org, Title: "Scheduled fixture report", Cadence: Cadence{Frequency: "interval", IntervalMinutes: 15}}
	template := Assignment{Org: seed.Org, Creator: "owner", Account: "account", Owner: "boss", Authority: "draft", Title: proposal.Title, Proposal: proposal.ID, Prompt: "Synthetic scheduled-work qualification. This occurrence must deliver a short report stating these authoritative fixture facts: Snow and Floe are shipping; NBC is retired. Supervisor: delegate to dev to author an ADC document, then obtain independent review from qa with ReviewOf pointing at the completed author. Use adc_wait for pending children. Reviewer: check the report against these facts and use adc_review. Supervisor: finish after actual cross-family review passes. Use only ADC tools. No external tools, shell, real repositories or infrastructure. No human decision is needed."}
	at := time.Now()
	schedule, writes, err := e.approveSchedule(proposal, template, "Synthetic report and independent review only", at)
	must(t, err)
	must(t, s.Batch(writes...))
	// Simulate the due instant in this isolated fixture, without waiting 15 minutes.
	e.dispatchSchedules(at.Add(15 * time.Minute))
	must(t, s.Get(schedule.ID, &schedule))
	if schedule.LastTask == "" {
		t.Fatal("occurrence not queued")
	}
	var task Assignment
	must(t, s.Get(schedule.LastTask, &task))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	e.Start(ctx)
	defer e.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("scheduled occurrence did not finish")
		case <-ticker.C:
			must(t, s.Get(task.ID, &task))
			if task.State == "needs input" {
				t.Fatal("routine scheduled fixture requested a human continuation")
			}
			if task.State != "ready" {
				continue
			}
			if len(taskDocs(s, task.ID)) == 0 || len(taskReviews(s, task.ID)) == 0 {
				t.Fatal("scheduled work bypassed independent review")
			}
			for _, trace := range taskTraces(s, task.ID) {
				if trace.State == "failed" {
					t.Fatalf("scheduled tool failure: %s: %s", trace.Name, trace.Result)
				}
			}
			must(t, s.Get(schedule.ID, &schedule))
			if schedule.Runs != 1 {
				t.Fatal("qualification queued multiple occurrences")
			}
			usage, usageErr := s.usageReport(task.Org, task.ID, 0, 0, time.Now())
			must(t, usageErr)
			if usage.Total.Input.Reports == 0 || usage.Total.Output.Reports == 0 || len(usage.Models) < 2 || len(usage.Accounts) != 1 || usage.Accounts[0].ID != "account" {
				t.Fatalf("real usage was not captured across the reviewed workflow: %+v", usage)
			}
			t.Logf("Captured %d real usage reports across %d models under the approved account.", usage.Total.Samples, len(usage.Models))
			t.Log("Due occurrence used the approved account/scope and completed through Sol execution and independent Claude review.")
			return
		}
	}
}
