package adc

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIndependentAttentionAudit(t *testing.T) {
	if os.Getenv("ADC_ATTENTION_AUDIT") != "1" {
		t.Skip("set ADC_ATTENTION_AUDIT")
	}
	runIndependentCodeAudit(t, []string{"attention.go", "attention_test.go", "attention_web.go", "engine.go", "schedules.go", "proposals.go", "ownership.go", "knowledge.go", "review_handoff.go", "review_status.go", "web.go", "model.go", "templates/attention.html", "templates/schedules.html"}, "Focused re-review of P4 corrections following your PASS and low-severity notes; at most four bounded source reads then adc_review_report. New: attentionQueueExpired pauses never-started occurrences after twice their approved execution window; resume cannot reset it. attentionStageRemaining rejects adc_delegate into a spent stage before persisting a child; active last turns still finish. Prior observations explicitly sort newest first. A live Sol/Opus fixture found scan workers could finish ordinary reports without retaining area observations; adc_delegate now appends the concrete save instruction and adc_finish calls attentionWorkerComplete for scan owners. Current observations and independently reviewed briefs can be inspected from task UI. Assessment context now includes bounded prior proposal decision notes; renamed same-scope/same-evidence suggestions dedupe. Proposal edit form exposes numeric budgets; the accepted snapshot is still immutable. Tests cover never-started recovery, stage rejection, missing stored scans, stale reviews, replacement snapshot/coalescing, proposal bounds and read-only rendering. Review only concrete correction or regression bugs, not deferred P5/public participation/federation. Advisory native tools remain advisory by approved product design. Finish with pass or actionable changes.", "attention-independent-review.json")
}
func TestLiveBoundedAttention(t *testing.T) {
	if os.Getenv("ADC_LIVE_ATTENTION") != "1" {
		t.Skip("set ADC_LIVE_ATTENTION")
	}
	s, e, task, root, nas := attentionFixture(t)
	defer e.Stop()
	for _, a := range list[Agent](s, "agent", task.Org) {
		a.Tools = nil
		if Family(a.Model) == "openai-gpt" {
			a.Model = "gpt-5.6-sol"
		}
		if a.ID == "dev" {
			a.Name = "NAS owner"
			a.Category = "research"
		}
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	host := Agent{ID: "hosting", Org: task.Org, Name: "Hosting owner", Description: "Own container recoverability", Category: "research", Model: "gpt-5.6-sol", Authority: "observe"}
	must(t, s.Put("agent", host.Org, "", "", host.ID, host))
	area, err := s.saveArea(Area{Org: task.Org, Owner: host.ID, Name: "Hosting", Intent: "Protect container recoverability; no speculative maintenance."}, 0, "human:owner")
	must(t, err)
	account := Account{ID: task.Account, User: task.Creator, Local: true, Limit: 3}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	task.Attention = &AttentionPolicy{Areas: []string{nas.ID, area.ID}, Scan: 4, Investigation: 10, Review: 6, MaxProposals: 1, Minutes: 10, Concurrent: 3}
	task.ConstrainTools = true
	task.Tools = []string{}
	task.ConstrainCapabilities = true
	task.Capabilities = nil
	task.Prompt = "Synthetic bounded assessment. These supplied fixture facts are the authoritative evidence; do not use shell, network or other external tools. Evidence fixture://nas says the backup export is absent for container volume B; evidence fixture://hosting says volume B is required to restore its container. These describe ONE shared recoverability gap. Scan both areas through their permanent owners, then cluster and propose exactly one bounded verification of restore coverage. No real infrastructure mutation, no new team and no human decision is needed during this fixture. Have one owner also author the supervisor briefing after seeing both observations; use adc_message if useful. Independent cross-family review is required for area reports and briefing. Record facts rather than perform repairs; finishing this cycle does not depend on acceptance of the proposal. Keep the interaction concise, use ADC tools and finish when reviewed."
	writes, err := e.assignmentWrites(task)
	must(t, err)
	// Replace only the untouched fixture root so it starts with actual model/tools.
	_, err = s.db.Exec("DELETE FROM records WHERE id=? AND kind='run'", root.ID)
	must(t, err)
	must(t, s.Batch(writes...))
	e = NewEngine(s)
	defer e.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	e.Start(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("bounded assessment timed out")
		case <-ticker.C:
			var current Assignment
			must(t, s.Get(task.ID, &current))
			if current.State == "needs input" || current.State == "paused" {
				for _, trace := range taskTraces(s, current.ID) {
					if trace.State == "failed" {
						t.Logf("Failed tool %s: %s; args %s", trace.Name, clipped(trace.Result, 600), clipped(trace.Arguments, 500))
					}
				}
				t.Fatalf("assessment stopped: %s", current.State)
			}
			if current.State != "ready" {
				continue
			}
			must(t, e.attentionComplete(current))
			count := 0
			for _, p := range list[WorkProposal](s, "proposal", current.Org) {
				if p.Task == current.ID {
					count++
				}
			}
			if count != 1 {
				t.Fatal("expected one cross-area proposal", count)
			}
			for _, trace := range taskTraces(s, current.ID) {
				if trace.State == "failed" {
					t.Logf("Recovered friction: %s: %s", trace.Name, clipped(trace.Result, 500))
				}
			}
			t.Log("PASS: Sol owners combined NAS/hosting evidence into one proposal, independently reviewed by Opus, completed without a human continuation or infrastructure action")
			return
		}
	}
}
