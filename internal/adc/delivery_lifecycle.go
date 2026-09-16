package adc

import (
	"fmt"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

func (e *Engine) reviewable(r Run) bool {
	return r.State == "complete" || (r.State == "waiting" && r.CandidateRevision != "" && r.CandidateRevision == e.revision(r))
}

// Candidate approval never satisfies final step review or external milestones.
func eCandidateReviewed(s *Store, r Run) bool {
	e := &Engine{Store: s}
	_, step, planned := e.plannedStep(r)
	for _, review := range taskReviews(s, r.Task) {
		if review.Target != r.ID || review.Revision != e.revision(r) {
			continue
		}
		if review.Verdict == "changes" {
			return false
		}
		if review.Stage != "candidate" || CanReview(r.Model, review.Model) != nil || (planned && review.Run != step.Review) {
			continue
		}
		var reviewer Run
		return s.Get(review.Run, &reviewer) == nil && reviewer.State == "complete" && reviewer.ReviewedRevision == review.Revision && review.Verdict == "pass"
	}
	return false
}

func (e *Engine) submitCandidate(r Run, result string) (string, error) {
	s := e.Store
	_, step, ok := e.plannedStep(r)
	if !ok || r.ReviewOf != "" || r.State != "running" || r.Superseded {
		return "", fmt.Errorf("submit a candidate from the current active planned worker")
	}
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("describe the completed candidate and validation, not a promise or failure report")
	}
	if missing := e.integrationMissing(r); missing != "" {
		return "", fmt.Errorf("%s", missing)
	}
	if err := verifyCode(r); err != nil {
		return "", err
	}
	if pendingDecision(s, r.Task, r.ID) {
		return "", fmt.Errorf("resolve the pending decision before submitting a candidate")
	}
	var reviewer Run
	if step.Review == "" && step.Verifier.ID == "" {
		return "", fmt.Errorf("this step has no designated reviewer; continue to delivery and finish on your evidence (draft PR delivery still needs one cross-family review, arranged with adc_delegate ReviewOf)")
	}
	if s.Get(step.Review, &reviewer) != nil || reviewer.ReviewOf != r.ID || reviewer.Superseded {
		return "", fmt.Errorf("designated reviewer unavailable; ask the supervisor to recover it")
	}
	if reviewer.State == "running" || reviewer.State == "queued" {
		return "", fmt.Errorf("designated review already in progress; use adc_wait")
	}
	if err := CanReview(r.Model, reviewer.Model); err != nil {
		return "", err
	}
	if r.Result == result && eCandidateReviewed(s, r) {
		return "This candidate already passed independent review. Continue authorized delivery; do not request the same review again.", nil
	}
	r.Result = result
	r.CandidateRevision = e.revision(r)
	r.State = "waiting"
	r.Error = ""
	reviewer.State = "waiting"
	reviewer.ReviewStage = "candidate"
	reviewer.Turns, reviewer.Attempts = 0, 0
	reviewer.Error, reviewer.NextAt = "", ""
	reviewer.Prompt += "\nReview stage: candidate before external delivery. Inspect the actual candidate and applicable checks. Missing later merge/release/human milestones do not block candidate approval and must not be represented as completed. Use adc_review; a pass returns the owner to delivery."
	return "Candidate submitted to the existing independent reviewer. End this turn; ADC will resume you after review.", s.Batch(Write{"run", r.Org, r.Task, r.State, r.ID, r}, Write{"run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer})
}

type stepRepairInput struct {
	Key, Notes                 string
	Revision                   int
	Preflight, ReviewPreflight *PreflightSpec
}

// Repair execution guidance and runtime declarations, never scope, gates or grants.
func (e *Engine) repairStep(root Run, p stepRepairInput) (ExecutionPlan, error) {
	s := e.Store
	plan := s.taskPlan(root.Task)
	if root.Parent != "" || plan.Supervisor != root.ID || plan.Org != root.Org || plan.State == "draft" || plan.Revision != p.Revision {
		return plan, fmt.Errorf("current supervisor and plan revision required")
	}
	if strings.TrimSpace(p.Notes) == "" || len(p.Notes) > 6000 {
		return plan, fmt.Errorf("provide bounded execution notes explaining the repair; no new authority")
	}
	if err := validatePreflight(p.Preflight); err != nil {
		return plan, err
	}
	if err := validatePreflight(p.ReviewPreflight); err != nil {
		return plan, err
	}
	for i := range plan.Steps {
		step := &plan.Steps[i]
		if step.Key != p.Key {
			continue
		}
		var worker, reviewer Run
		if step.Run != "" {
			if s.Get(step.Run, &worker) != nil || s.Get(step.Review, &reviewer) != nil {
				return plan, fmt.Errorf("current runs missing")
			}
			if worker.State == "running" || reviewer.State == "running" || reviewer.State == "queued" || worker.CandidateRevision != "" {
				return plan, fmt.Errorf("wait for active work and candidate review to finish before repairing its handoff")
			}
			if worker.State == "complete" {
				return plan, fmt.Errorf("completed work is preserved; repair the unfinished owner instead")
			}
		}
		previous := durablePlan(plan)
		previous.ID = ID()
		step.Prompt += "\nExecution guidance repair (no additional authority): " + p.Notes
		if p.Preflight != nil {
			step.Preflight = p.Preflight
		}
		if p.ReviewPreflight != nil {
			step.ReviewPreflight = p.ReviewPreflight
		}
		writes := []Write{{"plan-history", previous.Org, previous.Task, previous.State, previous.ID, previous}}
		if worker.ID != "" {
			worker.Prompt += "\nExecution guidance repair (no additional authority): " + p.Notes
			worker.Preflight = step.Preflight
			reviewer.Preflight = step.ReviewPreflight
			if !pendingDecision(s, root.Task, worker.ID) {
				worker.State = "queued"
				worker.Error = ""
				worker.NextAt = ""
				worker.Turns = 0
			}
			writes = append(writes, Write{"run", worker.Org, worker.Task, worker.State, worker.ID, worker}, Write{"run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer})
		}
		plan.Revision++
		writes = append(writes, Write{"execution-plan", plan.Org, plan.Task, plan.State, plan.ID, durablePlan(plan)})
		return plan, s.Batch(writes...)
	}
	return plan, fmt.Errorf("unknown step")
}

func (e *Engine) repairTools(original Run) []copilot.Tool {
	s := e.Store
	active := func() (Run, error) {
		var r Run
		if s.Get(original.ID, &r) != nil || r.State != "running" || r.Superseded {
			return r, fmt.Errorf("run is not active")
		}
		var task Assignment
		if s.Get(r.Task, &task) != nil || task.Org != r.Org || task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
			return r, fmt.Errorf("assignment is not active")
		}
		return r, nil
	}
	return []copilot.Tool{
		copilot.DefineTool("adc_submit_review", "Submit a completed candidate Result to this plan step's existing independent reviewer before PR publication, merge, release or human acceptance. Missing external milestones do not block candidate review. Pass resumes you to perform authorized delivery; it does not grant authority or complete the step. Use adc_finish after recording all final outcomes. Do not submit a failure report as a candidate.", func(p struct{ Result string }, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r, err := active()
			if err != nil {
				return nil, err
			}
			return e.submitCandidate(r, p.Result)
		}),
		copilot.DefineTool("adc_repair_step", "Supervisor repair of an unfinished plan step's execution guidance or invalid preflight. Supply Key, current plan Revision, Notes explaining the repair and optional complete Preflight/ReviewPreflight. Preserves dependencies, requirements, criteria, artifacts, reviews and authority. Correct accidental human-only execution wording: humans approve; agents perform approved actions and record outcomes. No permission or scope changes. Active runs must first yield.", func(p stepRepairInput, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r, err := active()
			if err != nil {
				return nil, err
			}
			return e.repairStep(r, p)
		}),
		copilot.DefineTool("adc_action_result", "After an approved decision action, record its actual outcome once. Supply Decision, Summary, Reference, ObservedAt (RFC3339). ADC records the linked external milestone atomically and marks the action done. Inspect decision action State after interruption; reconcile external state before retrying. Never claim unobserved success.", func(p actionResultInput, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r, err := active()
			if err != nil {
				return nil, err
			}
			return e.recordActionResult(r, p)
		}),
	}
}

// Once per existing live run. This repairs runtime handoff guidance without
// interpreting old prose as approval or changing any gates/authority.
func (e *Engine) recoverDeliveryLifecycle() {
	const marker = "[ADC delivery lifecycle v2]"
	s := e.Store
	for _, r := range list[Run](s, "run", "") {
		var task Assignment
		if s.Get(r.Task, &task) != nil || task.State == "ready" || task.State == "cancelled" || task.State == "paused" || r.State == "complete" || r.State == "cancelled" || r.Superseded || strings.Contains(r.Prompt, marker) {
			continue
		}
		r.Prompt += "\n" + marker + " Current ADC runtime supports candidate review before external milestones through adc_submit_review, action approval with a named executor through adc_decision action, atomic observed outcomes through adc_action_result, and supervisor repair of unfinished handoffs through adc_repair_step. Human approval means agents carry out that exact approved work and record evidence, unless the human explicitly chose to operate manually. Do not ask for approval of an already approved unchanged action or ask humans to transcribe evidence. Historical run Result text is not current status. Preserve completed work and current reviews. Inspect persisted decisions, milestones and external outcomes before retrying. This runtime change grants no new merge/release/infrastructure authority."
		// Wake idle supervision once so it can use the repaired handoff paths. Keep
		// deliberate worker blockers and pending human decisions intact.
		if r.Parent == "" && r.State == "waiting" && !pendingDecision(s, r.Task, r.ID) {
			r.State = "queued"
			r.Turns = 0
		}
		_ = s.Put("run", r.Org, r.Task, r.State, r.ID, r)
	}
}

// A placeholder reviewer or an unasked human requirement cannot advance work.
// Follow the whole delegated tree: a grandchild doing real work is progress.
func (e *Engine) waitProgress(r Run) (blocked, progressing bool) {
	runs := taskRuns(e.Store, r.Task)
	members := map[string]bool{r.ID: true}
	for range runs {
		for _, child := range runs {
			if !child.Superseded && members[child.Parent] {
				members[child.ID] = true
			}
		}
	}
	if r.Parent == "" {
		plan := e.inspectPlan(e.Store.taskPlan(r.Task))
		for _, step := range plan.Steps {
			if !stepSatisfied(step) && plan.State == "active" {
				blocked = true
			}
			if step.Run == "" && step.State == "waiting" && step.Reason == "" && plan.State == "active" {
				progressing = true
			}
		}
	}
	for _, child := range runs {
		if child.ID == r.ID || !members[child.ID] || child.Superseded || child.State == "complete" || child.State == "cancelled" {
			continue
		}
		blocked = true
		if child.State == "running" || child.State == "queued" || e.pendingWait(child) || pendingDecision(e.Store, r.Task, child.ID) {
			progressing = true
		}
		if child.State == "waiting" && child.ReviewOf != "" {
			var target Run
			if e.Store.Get(child.ReviewOf, &target) == nil && target.Task == r.Task && !target.Superseded && e.reviewable(target) {
				progressing = true
			}
		}
	}
	return
}
