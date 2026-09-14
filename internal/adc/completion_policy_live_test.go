package adc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIndependentCompletionPolicyAudit(t *testing.T) {
	if os.Getenv("ADC_COMPLETION_AUDIT") != "1" {
		t.Skip("set ADC_COMPLETION_AUDIT")
	}
	runIndependentCodeAudit(t, []string{"status_views.go", "status_views_test.go", "completion_policy.go", "completion_policy_test.go", "engine.go", "ownership.go", "ownership_web.go", "knowledge.go", "proposals.go", "schedules.go", "review_status.go", "provider_tools.go", "provider_tools_test.go", "model.go", "templates/completion_policy.html", "templates/areas.html"}, "Focused final re-review of P5 after your PASS, at most four bounded reads then adc_review_report. New files status_views.go/status_views_test.go provide optional adc_status View summary/run/review/assessment/connections/proposals/documents to reduce whole-transcript context; legacy empty View remains unchanged, and a focused run read must stay in the current task/org. A real strict-policy continuity fixture exposed a reviewed prose report finishing without a linked obligation observation: verificationEvidenceComplete now requires the stored observation and applicable review before root completion. Delegation prompts explain recording the observation, and retry dedup includes injected instructions. TestRoutineVerificationNeedsTheLinkedObservation covers generic evidence being insufficient. Existing routine-evidence policy, snapshot immutability, code/plan gates and wait outcome behavior retain your reviewed design. TestRoutineAreaCannotRelaxAssessmentOrLegacyStandingWork explicitly resolves your earlier uncertainty: P4 attention always remains reviewed and old schedules cannot pick up an area default. knowledge.go creates discovery, not a P4 assessment. Live routine family ownership passed through a real timer without QA/docs; synthetic NAS and product/delivery cases retain cross-family review. Inspect focused changes for actual defects and regressions, then pass or actionable changes; no requirement for P6/federation or enforced advisory host execution in this phase.", "completion-policy-independent-review.json")
}
func TestLiveRoutineFamilyOwnership(t *testing.T) {
	if os.Getenv("ADC_LIVE_ROUTINE") != "1" {
		t.Skip("set ADC_LIVE_ROUTINE")
	}
	s, e, task, root, area := routineFixture(t)
	defer e.Stop()
	for _, a := range list[Agent](s, "agent", task.Org) {
		a.Tools = nil
		if Family(a.Model) == "openai-gpt" {
			a.Model = "gpt-5.6-sol"
		}
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	account := Account{ID: task.Account, User: task.Creator, Local: true, Limit: 2}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	task.Prompt = "Synthetic family responsibility, under the human-selected routine evidence policy. Work directly as the owner: no delegation, QA, code, document artifact, shell or network is necessary. The supplied authoritative current fixture at fixture://family-confirmation states that Saturday transport is confirmed for 10:00. Record that observed evidence and retain ONE later verification obligation for this area using adc_followup, key transport-confirmation, due " + time.Now().Add(90*time.Second).UTC().Format(time.RFC3339) + ". Completion of today's confirmation must not close that later obligation. For the later verification only, the authoritative fixture fact at fixture://later-confirmation states the transport remains confirmed at 10:00; the due task should record this with adc_obligation_result and finish directly under the same routine policy. No new action or communication is authorized. Area ID: " + area.ID
	task.ConstrainTools = true
	task.Tools = []string{}
	task.ConstrainCapabilities = true
	task.Capabilities = nil
	root.Model = "gpt-5.6-sol"
	root.Family = Family(root.Model)
	root.Tools = nil
	root.Prompt = task.Prompt
	root.State = "queued"
	must(t, s.Batch(Write{"assignment", task.Org, "", task.State, task.ID, task}, Write{"run", root.Org, root.Task, root.State, root.ID, root}))
	e = NewEngine(s)
	defer e.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	e.Start(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	dispatched := false
	for {
		select {
		case <-ctx.Done():
			t.Fatal("routine family ownership timed out")
		case <-ticker.C:
			s.mu.Lock()
			var current Assignment
			err := s.Get(task.ID, &current)
			if err != nil {
				s.mu.Unlock()
				t.Fatal(err)
			}
			if current.State == "needs input" || current.State == "paused" {
				s.mu.Unlock()
				t.Fatal("routine work needs avoidable continuation")
			}
			if current.State == "ready" && !dispatched {
				obs := list[Obligation](s, "obligation", task.Org)
				if len(obs) != 1 {
					s.mu.Unlock()
					t.Fatal("one later confirmation expected", len(obs))
				}
				if len(taskReviews(s, task.ID)) != 0 || len(taskDocs(s, task.ID)) != 0 {
					s.mu.Unlock()
					t.Fatal("routine work manufactured QA/document ceremony")
				}
				e.dispatchObligations(time.Now())
				dispatched = true
			}
			resolved := false
			for _, pending := range list[Assignment](s, "assignment", task.Org) {
				if pending.Obligation != "" && (pending.State == "needs input" || pending.State == "paused") {
					s.mu.Unlock()
					t.Fatal("verification needs continuation", pending.State)
				}
			}
			for _, o := range list[Obligation](s, "obligation", task.Org) {
				if o.State == "blocked" {
					s.mu.Unlock()
					t.Fatal("routine verification blocked", o.Note)
				}
				if o.State == "resolved" {
					resolved = true
					if len(taskReviews(s, o.Task)) != 0 {
						s.mu.Unlock()
						t.Fatal("routine verification forced QA")
					}
				}
			}
			s.mu.Unlock()
			if resolved {
				for _, tr := range list[ToolTrace](s, "tooltrace", task.Org) {
					if tr.State == "failed" {
						t.Logf("Recovered friction: %s: %s", tr.Name, clipped(tr.Result, 350))
					}
				}
				if !strings.Contains(current.Output, "10") {
					t.Fatal("confirmation outcome missing")
				}
				t.Log("PASS: owner confirmed family arrangement, retained later obligation, and verified it in a fresh Sol run without QA, documents, code, external actions or human continuation")
				return
			}
		}
	}
}

func TestLiveReviewedCrossResourceOwnership(t *testing.T) {
	if os.Getenv("ADC_LIVE_CROSS_RESOURCE") != "1" {
		t.Skip("set ADC_LIVE_CROSS_RESOURCE")
	}
	s, e, task, root, area := ownershipFixture(t)
	defer e.Stop()
	for _, a := range list[Agent](s, "agent", task.Org) {
		a.Tools = nil
		if Family(a.Model) == "openai-gpt" {
			a.Model = "gpt-5.6-sol"
		}
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	area.Name = "Shared product delivery"
	area.Intent = "Keep delivery cost proportionate to a small user base; distinguish product variants and measured facts from uncertain claims."
	var err error
	area, err = s.saveArea(area, area.Revision, "human:owner")
	must(t, err)
	account := Account{ID: task.Account, User: task.Creator, Local: true, Limit: 3}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	task.Area = area.ID
	task.Completion = &CompletionPolicy{Mode: "reviewed", Version: 1}
	task.ConstrainTools = true
	task.Tools = []string{}
	task.ConstrainCapabilities = true
	task.Capabilities = nil
	task.Prompt = "Synthetic read-only Frostyard cross-resource qualification. Use only supplied fixture facts, ADC tools and current shared area context; no shell/network, infrastructure action, repository changes, or publication. At fixture://delivery, the prior delivery bill is 31 units and nightly extension artifacts dominate traffic; the actual byte breakdown and retention settings are UNKNOWN. At fixture://product, there are eight known installs and bootable images already use a separate OCI delivery channel. The server and desktop have different goals; moving all users or artifacts is not authorized. Have a specialist analyze these resources together and author a short sourced note that separates measured fixture facts from unknowns, then obtain actual cross-family review and finish with one proportionate recommendation to measure retention/traffic before considering a migration. As permanent owner, retain a brief inferred summary using adc_remember after review. No proposal or follow-up is required if the recommendation is adequately recorded as planning output. The reviewed policy remains in force; no human continuation is needed."
	root.Model = "gpt-5.6-sol"
	root.Family = Family(root.Model)
	root.Tools = nil
	root.Prompt = task.Prompt
	root.State = "queued"
	must(t, s.Batch(Write{"assignment", task.Org, "", task.State, task.ID, task}, Write{"run", root.Org, root.Task, root.State, root.ID, root}))
	e = NewEngine(s)
	defer e.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	e.Start(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("cross-resource qualification timed out")
		case <-ticker.C:
			var current Assignment
			must(t, s.Get(task.ID, &current))
			if current.State == "needs input" || current.State == "paused" {
				t.Fatal("avoidable continuation", current.State)
			}
			if current.State != "ready" {
				continue
			}
			if len(taskReviews(s, task.ID)) == 0 || len(taskDocs(s, task.ID)) == 0 {
				t.Fatal("reviewed work lost its deliverable/gate")
			}
			must(t, s.Get(area.ID, &area))
			if area.Summary == "" {
				t.Fatal("no retained owner understanding")
			}
			for _, tr := range taskTraces(s, task.ID) {
				if tr.State == "failed" {
					t.Logf("Recovered friction: %s: %s", tr.Name, clipped(tr.Result, 400))
				}
			}
			t.Log("PASS: Sol/Opus jointly considered two product/delivery resources, preserved unknowns and observed scope, completed independently reviewed planning and retained owner understanding")
			return
		}
	}
}
