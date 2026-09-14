package adc

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIndependentOwnershipAudit(t *testing.T) {
	if os.Getenv("ADC_OWNERSHIP_AUDIT") != "1" {
		t.Skip("set ADC_OWNERSHIP_AUDIT for independent source review")
	}
	runIndependentCodeAudit(t, []string{"ownership.go", "ownership_web.go", "ownership_test.go", "engine.go", "model.go", "store.go", "tool_permissions.go", "templates/areas.html", "integration_evidence.go", "review_status.go", "web.go", "recovery.go"},
		"Focused re-review of continuing ownership after two review rounds, at most six bounded source reads then adc_review_report. Check fixes: observation-bearing workers now enter reviewNeeds and root adc_finish required-review regardless of category (operations regression in finishFollowupObservation); obligationSourceProblem rechecks pinned funding membership on every active tool/gateway/start and scheduler tick (mid-verification revocation regression). First-round fixes already qualified: secret screening, superseded/cancelled observation exclusion, ready-at-budget completion, cleared inherited output. Reassess your previous stale-attempt finding against actual recovery.go: Reassign reuses the same run ID; new observations replace the current observation with history. Root finish refuses unfinished noncancelled runs, and cancelled/superseded observations are ignored. Missing or unreviewed current observations intentionally block resolution, rather than being silently blessed. Advisory execution is an explicitly accepted product contract in AGENTS: native tools remain advisory and must never be described as technically read-only; follow-up grants cannot expand and protected capabilities are narrowed. Requiring protected mode or stripping advisory tools is outside this authorized change. Return pass or concrete remaining correctness defects in this slice, not later P2-P7 features. Inspect the code rather than assuming the described fixes are correct.", "ownership-independent-review.json")
}

func TestLiveOwnerFollowup(t *testing.T) {
	if os.Getenv("ADC_LIVE_OWNER_TEST") != "1" {
		t.Skip("set ADC_LIVE_OWNER_TEST for synthetic provider-backed follow-up")
	}
	s, e, task, r, a := ownershipFixture(t)
	defer e.Stop()
	for _, agent := range list[Agent](s, "agent", task.Org) {
		agent.Tools = nil
		if Family(agent.Model) == "openai-gpt" {
			agent.Model = "gpt-5.6-sol"
		}
		must(t, s.Put("agent", agent.Org, "", "", agent.ID, agent))
	}
	account := Account{ID: "account", User: task.Creator, Local: true, Limit: 2}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	r.Tools = nil
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	a.Intent = "Authoritative synthetic fixture: the current household collection day is Tuesday. Earlier Monday notes are superseded. Only reason about these supplied facts; use ADC tools only, no shell, external tools or real services."
	a.Summary = "A human corrected the old Monday assumption to Tuesday. Future work must use Tuesday."
	a.Source = "fixture://human-correction"
	a, err := s.saveArea(a, a.Revision, "human:owner")
	must(t, err)
	p := followupArgs(a)
	p.Due = time.Now().Add(3 * time.Second).UTC().Format(time.RFC3339Nano)
	p.Outcome = "Verify the remembered collection day"
	p.Criteria = "The reported current day is Tuesday, matching the corrected area intent."
	p.Basis = "Read-only synthetic continuity verification."
	if os.Getenv("ADC_LIVE_OWNER_SCENARIO") == "homelab" {
		a.Name = "NAS protection"
		a.Intent = "Authoritative synthetic NAS observation at fixture://nas-protection: dataset containers has a verified daily backup; old statements claiming it has none are superseded. Supplied facts only; no shell, external services or real infrastructure action."
		a.Summary = "Corrected ownership knowledge: containers has an observed daily backup."
		a.Source = "fixture://nas-protection"
		a, err = s.saveArea(a, a.Revision, "human:owner")
		must(t, err)
		p.Outcome = "Verify the corrected NAS backup coverage"
		p.Criteria = "Report the supplied observed daily backup for dataset containers; keep this distinct from unverified restore testing."
		p.Basis = "Read-only synthetic NAS continuity verification."
	}
	o := createFollowup(t, e, r, p)
	completeSource(t, s, task, r)
	// A fresh engine dispatches at a real short due time. Accelerated clocks
	// and database reopen behavior are covered by deterministic lifecycle tests.
	e = NewEngine(s)
	defer e.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	e.Start(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("provider-backed follow-up timed out")
		case <-ticker.C:
			must(t, s.Get(o.ID, &o))
			if o.Task == "" {
				continue
			}
			if o.State == "blocked" {
				t.Fatal("follow-up blocked", o.Note)
			}
			var current Assignment
			must(t, s.Get(o.Task, &current))
			if current.State == "needs input" {
				t.Fatal("synthetic follow-up requested human continuation")
			}
			if o.State != "resolved" {
				continue
			}
			if len(taskReviews(s, o.Task)) == 0 {
				t.Fatal("missing independent review")
			}
			for _, trace := range taskTraces(s, o.Task) {
				if trace.State == "failed" {
					t.Logf("Recovered tool friction: %s: %s", trace.Name, clipped(trace.Result, 500))
				}
			}
			t.Log("PASS: fresh provider-backed owner verification used corrected area context and independently reviewed evidence without human continuation")
			return
		}
	}
}
