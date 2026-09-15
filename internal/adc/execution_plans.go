package adc

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type PlanStepSpec struct {
	Preflight, ReviewPreflight                    *PreflightSpec
	Repositories                                  []string
	Checks                                        []ValidationRequirement
	Requirements                                  []PlanRequirement
	Key, Title, Agent, Reviewer, Prompt, Criteria string
	DependsOn, RequiredTools, ReviewRequiredTools []string
}
type PlanAttempt struct {
	Run, Review, Reason, Created string
	Inputs                       map[string]string
}
type PlanRelatedRun struct{ ID, Org, Agent, Title, State string }
type PlanOmission struct{ Human, Reason, At string }

func stepSatisfied(s PlanStep) bool     { return s.State == "complete" || s.Omission != nil }
func omittedRevision(s PlanStep) string { return "omitted:" + s.Key + ":" + s.Omission.At }

type PlanStep struct {
	Omission                   *PlanOmission `json:",omitempty"`
	Related                    []PlanRelatedRun
	Decisions                  []Decision
	Readiness, ReviewReadiness ExecutionReadiness
	Resources                  RunResources
	Integration                IntegrationEvidence
	Validation                 []ValidationView
	ArtifactRevision           string
	Waits                      []DurableWait
	Evidence                   []MilestoneEvidence
	Attempts                   []PlanAttempt
	PlanStepSpec
	Worker, Verifier           Agent
	Run, Review, State, Reason string
	Inputs                     map[string]string
}
type ExecutionPlan struct {
	ID, Org, Task, Supervisor, Title, Source, State, StartedBy, Created string
	Revision                                                            int
	Steps                                                               []PlanStep
}
type planInput struct {
	Revision      int
	Title, Source string
	Steps         []PlanStepSpec
	Start         bool
}

// Evidence has its own authoritative records. Views may embed large command
// output, but plan persistence and scheduler comparisons retain only graph state.
func durablePlan(p ExecutionPlan) ExecutionPlan {
	p.Steps = append([]PlanStep(nil), p.Steps...)
	for i := range p.Steps {
		step := &p.Steps[i]
		step.Related = nil
		step.Decisions = nil
		step.Readiness = ExecutionReadiness{}
		step.ReviewReadiness = ExecutionReadiness{}
		step.Resources = RunResources{}
		step.Integration = IntegrationEvidence{}
		step.Validation = nil
		step.ArtifactRevision = ""
		step.Evidence = nil
		step.Waits = nil
	}
	return p
}

var planKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,39}$`)

func (s *Store) taskPlan(task string) ExecutionPlan {
	var p ExecutionPlan
	_ = s.Get("plan:"+task, &p)
	return p
}
func (e *Engine) saveExecutionPlan(root Run, input planInput) (ExecutionPlan, error) {
	s := e.Store
	if root.Parent != "" || root.ReviewOf != "" {
		return ExecutionPlan{}, fmt.Errorf("only the assignment supervisor can define its execution plan")
	}
	var task Assignment
	if s.Get(root.Task, &task) != nil || task.Org != root.Org || task.Kind == "proposal" {
		return ExecutionPlan{}, fmt.Errorf("execution assignment required")
	}
	encoded, _ := json.Marshal(input)
	if len(encoded) > 96*1024 {
		return ExecutionPlan{}, fmt.Errorf("keep the plan under 96 KiB; link detailed source documents instead")
	}
	old := s.taskPlan(task.ID)
	if old.ID != "" && old.State != "draft" {
		return old, fmt.Errorf("this plan is already active; inspect adc_status and continue its existing runs")
	}
	if input.Revision != old.Revision {
		return old, fmt.Errorf("plan revision changed; inspect adc_status before revising")
	}
	p := ExecutionPlan{ID: "plan:" + task.ID, Org: task.Org, Task: task.ID, Supervisor: root.ID, Title: strings.TrimSpace(input.Title), Source: strings.TrimSpace(input.Source), State: "draft", Created: now(), Revision: old.Revision + 1}
	if p.Title == "" || len(p.Title) > 240 || p.Source == "" || len(p.Source) > 8000 {
		return old, fmt.Errorf("provide a title (up to 240 characters) and source references (up to 8000 characters), including pinned revisions when available")
	}
	if len(input.Steps) < 1 || len(input.Steps) > 40 {
		return old, fmt.Errorf("use 1–40 bounded steps")
	}
	keys := map[string]int{}
	for i, spec := range input.Steps {
		if !planKey.MatchString(spec.Key) {
			return old, fmt.Errorf("step keys must start with a letter and contain only letters, digits, underscore or hyphen (max 40)")
		}
		if _, exists := keys[spec.Key]; exists {
			return old, fmt.Errorf("duplicate step %s", spec.Key)
		}
		keys[spec.Key] = i
		if err := validateRequirements(spec.Requirements); err != nil {
			return old, fmt.Errorf("step %s: %w", spec.Key, err)
		}
		if err := validatePreflight(spec.Preflight); err != nil {
			return old, fmt.Errorf("step %s: %w", spec.Key, err)
		}
		if err := validatePreflight(spec.ReviewPreflight); err != nil {
			return old, fmt.Errorf("step %s review: %w", spec.Key, err)
		}
		if err := validateIntegrationSpec(spec); err != nil {
			return old, fmt.Errorf("step %s: %w", spec.Key, err)
		}
		for _, check := range spec.Checks {
			if check.Observed && root.Execution != "protected" {
				return old, fmt.Errorf("step %s requires protected execution for ADC-observed commands", spec.Key)
			}
		}
		if strings.TrimSpace(spec.Title) == "" || len(spec.Title) > 240 || strings.TrimSpace(spec.Prompt) == "" || len(spec.Prompt) > 16000 || strings.TrimSpace(spec.Criteria) == "" || len(spec.Criteria) > 8000 {
			return old, fmt.Errorf("step %s needs a bounded title, brief and completion criteria", spec.Key)
		}
		var worker, reviewer Agent
		if s.Get(spec.Agent, &worker) != nil || worker.Org != root.Org || s.Get(spec.Reviewer, &reviewer) != nil || reviewer.Org != root.Org {
			return old, fmt.Errorf("step %s requires an owner and reviewer in this organization", spec.Key)
		}
		if err := CanReview(worker.Model, reviewer.Model); err != nil {
			return old, fmt.Errorf("step %s: %w", spec.Key, err)
		}
		step := PlanStep{PlanStepSpec: spec, Worker: worker, Verifier: reviewer, State: "waiting"}
		if _, err := e.planRun(task, root, step, worker, false); err != nil {
			return old, fmt.Errorf("step %s owner: %w", spec.Key, err)
		}
		if _, err := e.planRun(task, root, step, reviewer, true); err != nil {
			return old, fmt.Errorf("step %s reviewer: %w", spec.Key, err)
		}
		p.Steps = append(p.Steps, step)
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(key string) error {
		if visiting[key] {
			return fmt.Errorf("dependency cycle at %s", key)
		}
		if visited[key] {
			return nil
		}
		i, ok := keys[key]
		if !ok {
			return fmt.Errorf("unknown prerequisite %s", key)
		}
		visiting[key] = true
		seen := map[string]bool{}
		for _, dep := range p.Steps[i].DependsOn {
			if seen[dep] {
				return fmt.Errorf("duplicate prerequisite %s", dep)
			}
			seen[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[key] = false
		visited[key] = true
		return nil
	}
	for key := range keys {
		if err := visit(key); err != nil {
			return old, err
		}
	}
	if input.Start {
		p.State = "active"
		p.StartedBy = "supervisor:" + root.ID
	}
	writes := []Write{{"execution-plan", p.Org, p.Task, p.State, p.ID, p}}
	if old.ID != "" {
		history := old
		history.ID = ID()
		writes = append(writes, Write{"execution-plan-history", p.Org, p.ID, old.State, history.ID, history})
	}
	if err := s.Batch(writes...); err != nil {
		return old, err
	}
	s.Log(p.Org, p.Task, root.ID, "plan", fmt.Sprintf("%s · revision %d · %s", p.Title, p.Revision, p.State))
	return p, nil
}

// Both planned workers and reviewers inherit the supervisor's execution mode,
// authority ceiling, connection subset and explicitly funded provider portfolio.
func (e *Engine) planRun(task Assignment, root Run, step PlanStep, a Agent, review bool) (Run, error) {
	var current Agent
	if e.Store.Get(a.ID, &current) != nil || current.Org != task.Org {
		return Run{}, fmt.Errorf("assigned agent is no longer available")
	}
	authority, err := NarrowAuthority(root.Authority, a.Authority)
	if err != nil {
		return Run{}, err
	}
	authority, err = NarrowAuthority(authority, current.Authority)
	if err != nil {
		return Run{}, err
	}
	r := Run{Preflight: step.Preflight, ID: ID(), Org: task.Org, Task: task.ID, Parent: root.ID, Agent: a.ID, Created: now(), Title: step.Key + ": " + step.Title, Prompt: step.Prompt + "\nCompletion criteria: " + step.Criteria, Category: a.Category, Provider: providerName(a.Provider), Model: a.Model, Family: Family(a.Model), Authority: authority, Execution: root.Execution, State: "queued", RequiredTools: step.RequiredTools}
	if review {
		r.Preflight = step.ReviewPreflight
		if step.Preflight != nil && step.Preflight.Models {
			p := PreflightSpec{Models: true}
			if r.Preflight != nil {
				p = *r.Preflight
				p.Models = true
			}
			r.Preflight = &p
		}
		r.RequiredTools = step.ReviewRequiredTools
	}
	for _, id := range a.Tools {
		if Subset([]string{id}, root.Tools) && Subset([]string{id}, current.Tools) {
			r.Tools = append(r.Tools, id)
		}
	}
	if err := requireConnections(e.Store, task.Org, r.RequiredTools, r.Tools); err != nil {
		return Run{}, err
	}
	if err := e.Store.bindRunAccount(task, &r); err != nil {
		return Run{}, err
	}
	r.Workspace = filepath.Join(e.Store.Dir, "workspaces", r.Org, r.Task, r.ID)
	if review {
		r.Category = "review"
		r.State = "waiting"
		r.ReviewOf = step.Run
		r.Title = "Review " + r.Title
		r.Prompt = "Independently verify the plan step against its acceptance criteria and exact saved evidence. Use adc_status to inspect the target run, documents and registered code. Use adc_review with pass or concrete changes; do not treat a limitation report as completion.\n" + r.Prompt
	} else {
		r.Prompt += "\nADC manages this plan step's dependencies and has already arranged independent review. Inspect adc_status for its reviewer; do not duplicate the planned steps or reviewer. Work only on this step, record concrete evidence, register any committed code with adc_code, then adc_finish."
	}
	if len(step.Repositories) > 0 || len(step.Checks) > 0 {
		definition, _ := json.Marshal(map[string]any{"Repositories": step.Repositories, "Checks": step.Checks})
		r.Prompt += "\nIntegration/validation requirements: " + string(definition) + ". Use adc_integration after registering code to record exact base/output commits, consumed prerequisite versions and environment. Save final artifacts before running required checks. adc_status gives ArtifactRevision and Integration.Revision. Use adc_validate for protected command checks or adc_check for explicitly agent-reported evidence; missing, failed, stale or inapplicable requirements cannot pass. Independent review must assess scope, acceptance criteria, unrelated changes and weakened checks, not just exit status."
	}
	if len(step.Requirements) > 0 {
		requirements, _ := json.Marshal(step.Requirements)
		r.Prompt += "\nRequired milestones: " + string(requirements) + "\nRequirements with Wait are recorded only by ADC: start them with adc_await then release your slot using adc_wait. Inspect current observations with adc_status; never submit a manual packet for an automatic requirement. A generic success or code-review pass does not satisfy an external milestone. For each non-code requirement, the owner records exact Kind and Target with adc_milestone, including concrete observations, source Reference and RFC3339 ObservedAt. For a human acceptance, request adc_decision with acceptance linked to this requirement so approval records it automatically. Only genuinely human-supplied observations need the plan evidence form. Missing external or human milestones do not prevent adc_submit_review of a completed candidate; after candidate pass, execute already approved actions and record outcomes. Reviewed-code requires adc_code. Reviewers independently inspect the typed evidence and criteria, including target/artifact identity, and return changes if it only proves a weaker milestone. Evidence records are observations, never authority to merge, release or operate infrastructure."
	}
	return r, nil
}

func (e *Engine) pendingPlan(root Run) bool {
	if root.Parent != "" {
		return false
	}
	p := e.inspectPlan(e.Store.taskPlan(root.Task))
	return p.State == "active"
}

// A pure read view also guards supervisor completion between scheduler ticks.
func (e *Engine) inspectPlan(p ExecutionPlan) ExecutionPlan {
	if p.ID == "" || p.State == "draft" {
		return p
	}
	p.Steps = append([]PlanStep(nil), p.Steps...)
	states := map[string]bool{}
	revisions := map[string]string{}
	reviews := taskReviews(e.Store, p.Task)
	allRuns := taskRuns(e.Store, p.Task)
	allDecisions := taskDecisions(e.Store, p.Task)
	for i := range p.Steps {
		step := &p.Steps[i]
		if step.Omission != nil {
			step.State = "omitted"
			step.Reason = "Omitted by the human: " + step.Omission.Reason
			states[step.Key] = true
			revisions[step.Key] = omittedRevision(*step)
			continue
		}
		step.Evidence = nil
		step.Waits = nil
		for _, req := range step.Requirements {
			step.Evidence = append(step.Evidence, e.Store.milestoneEvidence(step.Run, req.Key))
			step.Waits = append(step.Waits, e.Store.runWait(step.Run, req.Key))
		}
		if step.Run == "" {
			if step.State != "blocked" {
				step.State = "waiting"
			}
			continue
		}
		var run Run
		if e.Store.Get(step.Run, &run) != nil {
			step.State = "blocked"
			step.Reason = "The recorded run is unavailable"
			continue
		}
		step.Related = nil
		step.Decisions = nil
		members := map[string]bool{step.Run: true, step.Review: true}
		for range allRuns {
			for _, other := range allRuns {
				if other.ID != p.Supervisor && (members[other.Parent] || members[other.ReviewOf]) {
					members[other.ID] = true
				}
			}
		}
		for _, other := range allRuns {
			if members[other.ID] && other.ID != step.Run && other.ID != step.Review && !other.Superseded {
				step.Related = append(step.Related, PlanRelatedRun{other.ID, other.Org, other.Agent, other.Title, other.State})
			}
		}
		for _, d := range allDecisions {
			if d.State == "pending" && (members[d.Run] || (d.Action != nil && members[d.Action.Run]) || (d.Acceptance != nil && members[d.Acceptance.Run])) {
				step.Decisions = append(step.Decisions, d)
			}
		}
		step.Readiness = e.Store.runReadiness(run.ID)
		step.ReviewReadiness = e.Store.runReadiness(step.Review)
		step.Resources = e.Store.runResources(run.ID)
		step.Integration = e.Store.integrationEvidence(run.ID)
		step.Validation = e.validationViews(run)
		step.ArtifactRevision = e.artifactRevision(run)
		step.State = run.State
		step.Reason = run.Error
		if run.CandidateRevision != "" {
			step.State = "review"
			step.Reason = "Independent candidate review before delivery; external milestones remain pending"
		}
		if run.State == "complete" {
			step.State = "review"
			step.Reason = "Waiting for independent review of the current evidence"
			var reviewer Run
			if e.Store.Get(step.Review, &reviewer) != nil {
				step.State = "blocked"
				step.Reason = "The planned reviewer is unavailable"
			} else if reviewer.State == "blocked" || reviewer.State == "cancelled" {
				step.State = "blocked"
				step.Reason = "Independent review is " + reviewer.State + ": " + reviewer.Error
			}
			if e.hasPlannedReview(*step, run, reviews) && e.milestoneMissing(run) == "" && !pendingDecision(e.Store, p.Task, run.ID) {
				step.State = "complete"
				step.Reason = ""
				states[step.Key] = true
				revisions[step.Key] = run.ID + ":" + e.revision(run)
			}
		}
		if missing := e.milestoneMissing(run); missing != "" && run.State == "complete" {
			step.State = "evidence"
			step.Reason = missing
		}
		if e.waitingHumanMilestone(run) && run.State == "waiting" {
			step.Reason = "Waiting for human milestone evidence in this plan"
		}
		if run.State == "cancelled" {
			step.State = "blocked"
			step.Reason = "The step was cancelled; its outcome is not satisfied"
		}
	}
	// Propagate invalid prerequisites regardless of the submitted graph's order.
	for range p.Steps {
		for i := range p.Steps {
			step := &p.Steps[i]
			if step.Omission != nil {
				continue
			}
			for _, dep := range step.DependsOn {
				stale := step.Run != "" && step.Inputs[dep] != revisions[dep]
				if !states[dep] || stale {
					states[step.Key] = false
					if step.Run == "" {
						step.State = "waiting"
						step.Reason = "Waiting for reviewed completion of " + dep
					} else {
						step.State = "blocked"
						step.Reason = "Prerequisite " + dep + " is no longer the reviewed evidence used by this step"
					}
					break
				}
			}
		}
	}
	complete := true
	for _, step := range p.Steps {
		if !stepSatisfied(step) {
			complete = false
		}
	}
	p.State = "active"
	if complete {
		p.State = "complete"
	}
	return p
}

// Called under Store.mu. A run pair and its plan mapping commit together. The
// ordinary run scheduler applies capacity limits, review pinning and recovery.
func (e *Engine) dispatchPlans() {
	e.expireOperationRequests()
	s := e.Store
	for _, stored := range list[ExecutionPlan](s, "execution-plan", "") {
		if stored.State == "draft" {
			continue
		}
		var task Assignment
		var root Run
		if s.Get(stored.Task, &task) != nil || task.Org != stored.Org || task.State == "paused" || task.State == "cancelled" || task.State == "ready" || s.Get(stored.Supervisor, &root) != nil {
			continue
		}
		if root.State == "blocked" || root.State == "cancelled" || root.State == "complete" {
			continue
		}
		// Reuse the same reviewer for final outcome evidence after candidate delivery.
		for _, step := range stored.Steps {
			if step.Omission != nil {
				continue
			}
			var worker, reviewer Run
			if s.Get(step.Run, &worker) == nil && s.Get(step.Review, &reviewer) == nil && worker.State == "complete" && reviewer.State == "complete" && reviewer.ReviewStage == "candidate" {
				reviewer.State = "waiting"
				reviewer.ReviewStage = ""
				reviewer.Turns = 0
				reviewer.Prompt += "\nFinal outcome review: the owner completed delivery. Verify every required milestone and actual external outcome now; the earlier candidate pass did not satisfy these gates."
				_ = s.Put("run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer)
			}
		}
		p := e.inspectPlan(stored)
		for i, step := range p.Steps {
			if step.Run != "" && step.State == "blocked" && strings.HasPrefix(step.Reason, "Prerequisite ") {
				ready := true
				for _, dep := range step.DependsOn {
					for _, upstream := range p.Steps {
						if upstream.Key == dep && !stepSatisfied(upstream) {
							ready = false
						}
					}
				}
				if ready {
					e.retirePlanAttempt(&p, i)
				}
			}
		}
		verifiedCode := map[string]error{}
		ready := map[string]bool{}
		for _, step := range p.Steps {
			ready[step.Key] = stepSatisfied(step)
		}
		for i := range p.Steps {
			step := &p.Steps[i]
			if step.Run != "" || step.Omission != nil {
				continue
			}
			eligible := true
			for _, dep := range step.DependsOn {
				if !ready[dep] {
					eligible = false
				}
			}
			if !eligible {
				continue
			}
			step.Inputs = map[string]string{}
			evidence := ""
			if len(step.Attempts) > 0 {
				previous := step.Attempts[len(step.Attempts)-1]
				evidence += fmt.Sprintf("\nEarlier attempt %s was superseded by changed prerequisite evidence. Inspect that run and its artifacts, reuse valid work, and reconcile any external outcomes before repeating actions. Its history is preserved.", previous.Run)
			}
			for _, dep := range step.DependsOn {
				for _, upstream := range p.Steps {
					if upstream.Key == dep {
						if upstream.Omission != nil {
							step.Inputs[dep] = omittedRevision(upstream)
							evidence += fmt.Sprintf("\nFormer prerequisite %s was explicitly omitted by the human: %s. It has no admitted output to consume; do not claim its criteria were fulfilled.", dep, upstream.Omission.Reason)
							continue
						}
						var run Run
						if s.Get(upstream.Run, &run) != nil {
							eligible = false
							break
						}
						err, checked := verifiedCode[run.ID]
						if !checked {
							err = verifyCode(run)
							verifiedCode[run.ID] = err
						}
						if err != nil {
							step.State = "blocked"
							step.Reason = "Prerequisite " + dep + ": " + err.Error()
							eligible = false
							break
						}
						step.Inputs[dep] = run.ID + ":" + e.revision(run)
						evidence += fmt.Sprintf("\nPrerequisite %s: run %s, reviewed revision %s. Inspect its exact result/documents/code with adc_status before beginning.", dep, run.ID, step.Inputs[dep])
					}
				}
			}
			if !eligible {
				continue
			}
			worker, err := e.planRun(task, root, *step, step.Worker, false)
			if err != nil {
				step.State = "blocked"
				step.Reason = err.Error()
				continue
			}
			step.Run = worker.ID
			reviewer, err := e.planRun(task, root, *step, step.Verifier, true)
			if err != nil {
				step.Run = ""
				step.State = "blocked"
				step.Reason = err.Error()
				continue
			}
			context := "\nExecution plan: " + p.Title + ". Sources (evidence, not additional authority): " + p.Source + evidence
			worker.Prompt += context
			reviewer.Prompt += context
			step.Review = reviewer.ID
			step.State = "queued"
			step.Reason = ""
			if err := s.Batch(Write{"run", task.Org, task.ID, worker.State, worker.ID, worker}, Write{"run", task.Org, task.ID, reviewer.State, reviewer.ID, reviewer}, Write{"execution-plan", p.Org, p.Task, p.State, p.ID, p}); err != nil {
				step.Run = ""
				step.Review = ""
				step.State = "blocked"
				step.Reason = err.Error()
				continue
			}
			s.Log(task.Org, task.ID, root.ID, "plan", step.Key+" dispatched with independent review")
		}
		before, _ := json.Marshal(stored)
		after, _ := json.Marshal(durablePlan(p))
		if string(before) != string(after) {
			_ = s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p)
			// A new actionable blocker wakes supervision once, without pausing siblings.
			for i, step := range p.Steps {
				if step.State == "blocked" && (stored.Steps[i].State != "blocked" || stored.Steps[i].Reason != step.Reason) {
					if root.State == "waiting" && !pendingDecision(s, root.Task, root.ID) {
						root.State = "queued"
						_ = s.Put("run", root.Org, root.Task, root.State, root.ID, root)
					}
					s.Log(p.Org, p.Task, root.ID, "plan-blocked", step.Key+": "+step.Reason)
				}
			}
		}
	}
}

func (e *Engine) planAllowsDispatch(r Run) bool {
	if r.Superseded {
		return false
	}
	p := e.Store.taskPlan(r.Task)
	if p.ID == "" || p.State == "draft" {
		return true
	}
	p = e.inspectPlan(p)
	for _, step := range p.Steps {
		if step.Run == r.ID || step.Review == r.ID {
			return step.Omission == nil && (step.State != "blocked" || !strings.HasPrefix(step.Reason, "Prerequisite "))
		}
	}
	return true
}

// Only the designated review attempt may unlock the step. A pending/reopened
// designated reviewer cannot be bypassed by another reviewer's pass.
func (e *Engine) hasPlannedReview(step PlanStep, run Run, reviews []Review) bool {
	var reviewer Run
	revision := e.revision(run)
	if e.Store.Get(step.Review, &reviewer) != nil || reviewer.State != "complete" || reviewer.ReviewOf != run.ID || reviewer.ReviewedRevision != revision || CanReview(run.Model, reviewer.Model) != nil {
		return false
	}
	seen := map[string]bool{}
	passed := false
	for _, review := range reviews {
		if review.Stage == "candidate" || review.Target != run.ID || review.Revision != revision || seen[review.Run] {
			continue
		}
		seen[review.Run] = true
		if review.Verdict == "changes" {
			return false
		}
		if review.Run == step.Review && review.Verdict == "pass" && CanReview(run.Model, review.Model) == nil {
			passed = true
		}
	}
	return passed
}

// Retire only quiescent attempts. Running provider work and its descendants
// must yield before a replacement is created; external effects are reconciled
// by the new worker, never blindly replayed by the scheduler.
func (e *Engine) retirePlanAttempt(p *ExecutionPlan, index int) bool {
	step := p.Steps[index]
	if step.Run == "" {
		return false
	}
	obsolete := map[string]bool{step.Run: true}
	if step.Review != "" {
		obsolete[step.Review] = true
	}
	runs := taskRuns(e.Store, p.Task)
	for range runs {
		for _, r := range runs {
			if obsolete[r.Parent] || obsolete[r.ReviewOf] {
				obsolete[r.ID] = true
			}
		}
	}
	for _, r := range runs {
		if obsolete[r.ID] {
			e.mu.Lock()
			_, busy := e.active[r.ID]
			e.mu.Unlock()
			if busy || r.State == "running" {
				return false
			}
		}
	}
	// Do not discard outstanding human questions or coalesced access requests.
	for _, decision := range taskDecisions(e.Store, p.Task) {
		if obsolete[decision.Run] && decision.State == "pending" {
			return false
		}
	}
	for _, request := range list[AccessRequest](e.Store, "access-request", p.Org) {
		if request.Task == p.Task && request.State == "pending" {
			for _, run := range request.Runs {
				if obsolete[run] {
					return false
				}
			}
		}
	}
	writes := []Write{}
	for _, r := range runs {
		if obsolete[r.ID] {
			r.State = "cancelled"
			r.Superseded = true
			r.Error = "Superseded after prerequisite evidence changed; history retained"
			writes = append(writes, Write{"run", r.Org, r.Task, r.State, r.ID, r})
		}
	}
	fresh := step
	fresh.Attempts = append(append([]PlanAttempt(nil), step.Attempts...), PlanAttempt{Run: step.Run, Review: step.Review, Inputs: step.Inputs, Reason: step.Reason, Created: now()})
	fresh.Run = ""
	fresh.Review = ""
	fresh.Inputs = nil
	fresh.State = "waiting"
	fresh.Reason = "Preparing a fresh attempt against reviewed prerequisite evidence"
	p.Steps[index] = fresh
	if err := e.Store.Batch(append(writes, Write{"execution-plan", p.Org, p.Task, p.State, p.ID, *p})...); err != nil {
		p.Steps[index] = step
		return false
	}
	e.Store.Log(p.Org, p.Task, p.Supervisor, "plan", step.Key+" superseded an earlier attempt after prerequisite evidence changed")
	return true
}
