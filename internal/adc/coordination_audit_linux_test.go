//go:build linux

package adc

import (
	"os"
	"testing"
)

func TestIndependentCoordinationRecoveryAudit(t *testing.T) {
	if os.Getenv("ADC_COORDINATION_AUDIT") != "1" {
		t.Skip("set ADC_COORDINATION_AUDIT")
	}
	files := []string{"coordination.go", "coordination_test.go", "status_views.go", "plan_amendments.go", "plan_amendments_test.go", "execution_plans_web.go", "decisions.go", "delivery_lifecycle.go", "engine.go", "model.go", "execution_plans.go", "milestones.go", "templates/execution_plan.html"}
	prompt := `Independently review this bounded ADC recovery change. Live supervisors went to sleep forever because an unasked human-evidence gate counted as progress. waitProgress now examines descendants and runnable reviews, actual pending decisions and timers. Scheduler calls recoverStalledSupervisors after dispatchPlans/dispatchWaits, before its ordinary waiting loop: wakes only idle roots on changed persisted blocker fingerprints; no held worker or completed result is resumed by recovery. adc_wait rejects a fully stalled child tree. New paginated coordination/decisions and exact step status views support recovery. adc_withdraw_decision calls withdrawDecision after active-run guard, never grants approval. Human-only /plan-action remove-requirement is under existing authenticated membership+CSRF router; removes a human-evidence requirement with reason/history, resumes its idle worker, keeps independent review and all other gates. This does not let models mutate plan scope. Existing root prompt tells models to use current state, prior human answers, concise questions and perform approved actions themselves. Look for concrete lifecycle/authorization/regression defects, particularly repeated wake loops, unresolved review dependencies, human hold preservation, stale scope change, lost history or fake evidence. No broad architecture audit. Read the supplied primary sources, at most two targeted helper reads, then adc_review_report PASS or concrete blocking changes.`
	for _, file := range []string{"coordination.go", "status_views.go", "plan_amendments.go", "execution_plans_web.go", "decisions.go", "delivery_lifecycle.go", "coordination_test.go", "plan_amendments_test.go"} {
		b, err := os.ReadFile(file)
		must(t, err)
		prompt += "\nFILE " + file + "\n" + string(b) + "\nEND FILE\n"
	}
	runIndependentCodeAudit(t, files, prompt, "coordination-independent-review.json")
}

func TestIndependentPlanOmissionAudit(t *testing.T) {
	if os.Getenv("ADC_OMISSION_AUDIT") != "1" {
		t.Skip("set ADC_OMISSION_AUDIT")
	}
	files := []string{"plan_amendments.go", "plan_amendments_test.go", "execution_plans.go", "execution_plans_web.go", "integration_evidence.go", "review_status.go", "plan_graph.go", "engine.go", "templates/execution_plan.html", "templates/plan_graph.html"}
	prompt := `Final focused re-review after PASS. Applied your busy-run check, superseded-ancestor traversal, pending-access-request refusal and post-supersession root-question accounting. One additional change: dispatchPlans no longer freezes the entire ALREADY ACTIVE, approved graph merely because the root has a pending question. Root itself still waits for its answer; paused/cancelled/blocked root and task guards remain, per-worker decisions/prerequisite gates remain, and start-plan still refuses pending supervisor decisions. This keeps other already-authorized branches running while a blocked worker needs a decision; no question grants authority. TestSupervisorQuestionDoesNotFreezeOtherAuthorizedPlanSteps covers it. Review this scheduling delta and the fixes, do not repeat the full prior audit. Review a human-controlled plan omission lifecycle change. Removing the sole information requirement still left a no-work step manufacturing repository baselines to pass generic integration gates. A human can now omit the whole step (separate from removing one requirement), retaining history and admitting NO output or evidence. Explicit PlanStep.Omission drives separate 'omitted' state and graph count; dependencies accept its omission pin, never consume its former code. Final assignment review skips superseded history but all remaining substantive work needs ordinary independent review. ONLY existing membership+CSRF checked /plan-action omit-step invokes this; no agent tool or plan input accepts an Omission. Reject stale/paused/cancelled/ready task, active member run, already-started transitive dependents. Cancel only unfinished member runs; preserve completed historical states but mark member runs superseded. Supersede pending member-bound decisions; never rewrite answered ones or grant external action authority. Review concrete scope/security/lifecycle bugs, especially dependents, omission persistence, related reviewers and final completion. At most two focused helper reads, then adc_review_report with PASS or actionable blocking changes. Do not redesign generalized graph editing.`
	for _, file := range []string{"plan_amendments.go", "plan_amendments_test.go", "execution_plans.go", "execution_plans_web.go", "review_status.go"} {
		b, err := os.ReadFile(file)
		must(t, err)
		prompt += "\nFILE " + file + "\n" + string(b) + "\nEND FILE\n"
	}
	runIndependentCodeAudit(t, files, prompt, "plan-omission-independent-review.json")
}
