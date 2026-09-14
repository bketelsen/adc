package adc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIndependentOwnerCoordinationAudit(t *testing.T) {
	if os.Getenv("ADC_OWNER_COORDINATION_AUDIT") != "1" {
		t.Skip("set ADC_OWNER_COORDINATION_AUDIT")
	}
	runIndependentCodeAudit(t, []string{"owner_requests.go", "owner_requests_test.go", "owner_requests_web.go", "engine.go", "store.go", "model.go", "messages.go", "knowledge.go", "ownership.go", "templates/coordination.html", "web.go"}, "Focused permanent-owner correspondence re-review, at most four bounded source reads then adc_review_report. Fixes for your previous findings: receivingRun now accepts only already running/queued runs, and neither delivery nor receipts ever changes waiting to queued (new wait-gate regression). A blocked response clears its receiving claim while retaining responder evidence; a later authorized owner run can answer with new evidence, without automatic redelivery (new regression). Identical response retries are no-ops. Lead transfer rejects the recipient as lead. Candidate searches are cached once per permanent owner per scheduler pass, including absent owners, eliminating per-request full run scans; a cached selected run is reloaded before appending to avoid lost messages. TestOwnerRoutingCachePreservesAllMessages covers multiple messages to the same receiver. Same-org boundaries, receiving funding/grants, source cancellation, shared creation/attempt limits and attributed-response semantics are unchanged. Review the fixes for actual defects or pass. Background attention is not funded/enabled by this slice; caller-provided messages are not human authority.", "owner-coordination-independent-review.json")
}

func TestLiveOwnerCoordination(t *testing.T) {
	if os.Getenv("ADC_LIVE_OWNER_COORDINATION") != "1" {
		t.Skip("set ADC_LIVE_OWNER_COORDINATION")
	}
	s, e, task, r, _ := ownershipFixture(t)
	defer e.Stop()
	for _, agent := range list[Agent](s, "agent", task.Org) {
		agent.Tools = nil
		if Family(agent.Model) == "openai-gpt" {
			agent.Model = "gpt-5.6-sol"
		}
		must(t, s.Put("agent", agent.Org, "", "", agent.ID, agent))
	}
	q := makeOwnerRequest(t, e, r)
	completeSource(t, s, task, r)
	// A previous attempt found no evidence. A later authorized context must
	// recover this same request without a human cancelling/recreating it.
	previous := recipientFixture(t, s, e)
	e.dispatchOwnerRequests()
	must(t, s.Get(q.ID, &q))
	_, err := e.respondOwnerRequest(previous, q.ID, "blocked", "No supplied backup evidence was available in this earlier context.", "fixture://earlier-gap", q.Revision)
	must(t, err)
	previous.State = "complete"
	must(t, s.Put("run", previous.Org, previous.Task, previous.State, previous.ID, previous))
	var previousTask Assignment
	must(t, s.Get(previous.Task, &previousTask))
	previousTask.State = "ready"
	must(t, s.Put("assignment", previousTask.Org, "", previousTask.State, previousTask.ID, previousTask))
	account := Account{ID: task.Account, User: task.Creator, Local: true, Limit: 2}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	receiving := Assignment{ID: "receiving-work", Org: task.Org, Owner: "dev", Creator: task.Creator, Account: task.Account, Authority: "observe", Prompt: "Synthetic authorized NAS evidence review. Supplied authoritative fixture facts: volume A has a successful observed backup at fixture://backup-A; volume B has no verification evidence. Use only these supplied facts and ADC tools, no shell, network or external tools. Handle the existing permanent-owner request in adc_owner_inbox; do not create new requests. As the permanent owner/root, delegate a bounded evidence assessment and independent cross-family review, then personally record the substantive response with adc_owner_respond and finish. Your worker should not answer the request; the permanent owner/root should reconcile the earlier blocker after reviewing this newly supplied evidence. No real infrastructure action or follow-up is needed.", Title: "Handle known NAS evidence"}
	must(t, e.CreateAssignment(receiving))
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
			t.Fatal("owner correspondence timed out")
		case <-ticker.C:
			var current Assignment
			must(t, s.Get(receiving.ID, &current))
			must(t, s.Get(q.ID, &q))
			if current.State == "needs input" || current.State == "paused" {
				t.Fatal("avoidable human continuation")
			}
			if current.State != "ready" {
				continue
			}
			if q.State != "answered" || !strings.Contains(strings.ToLower(q.Response), "volume b") || len(taskReviews(s, current.ID)) == 0 {
				t.Fatal("request not substantively answered with independently reviewed assessment", q.State, q.Response)
			}
			must(t, s.Get(task.ID, &task))
			if task.State != "ready" {
				t.Fatal("source resurrected")
			}
			for _, trace := range taskTraces(s, current.ID) {
				if trace.State == "failed" {
					t.Logf("Recovered friction: %s: %s", trace.Name, clipped(trace.Result, 400))
				}
			}
			t.Log("PASS: real owner recovered a retained earlier blocker and answered after source completion, with Sol/Opus evidence review and no human continuation")
			return
		}
	}
}
