package adc

import (
	"fmt"
	"strings"
)

// Human scope reduction, reached only through the membership/CSRF checked UI.
// It retires a requirement rather than inventing evidence that it was met.
// Existing independent review and all other requirements remain in force.
func (e *Engine) removePlanRequirement(plan ExecutionPlan, key, requirement, reason, human string) error {
	s := e.Store
	plan = durablePlan(plan)
	if human == "" || strings.TrimSpace(reason) == "" || len(reason) > 1500 {
		return fmt.Errorf("identify the human and give a short reason for removing this requirement")
	}
	current := s.taskPlan(plan.Task)
	if current.ID != plan.ID || current.Org != plan.Org || current.Revision != plan.Revision || current.State != "active" {
		return fmt.Errorf("active plan changed; reload before changing its scope")
	}
	var task Assignment
	var root Run
	if s.Get(plan.Task, &task) != nil || task.State == "paused" || task.State == "cancelled" || task.State == "ready" || s.Get(plan.Supervisor, &root) != nil {
		return fmt.Errorf("active assignment required")
	}
	for i := range plan.Steps {
		step := &plan.Steps[i]
		if step.Key != key {
			continue
		}
		index := -1
		for j, req := range step.Requirements {
			if req.Key == requirement {
				index = j
			}
		}
		if index < 0 {
			return fmt.Errorf("requirement is no longer present")
		}
		// This initial amendment UI covers human acceptance/information only.
		// Code checks, reviews and observed operational outcomes cannot be removed.
		req := step.Requirements[index]
		if req.Kind != "human-evidence" || req.Wait != nil {
			return fmt.Errorf("only human information or acceptance requirements can be removed here")
		}
		var worker, reviewer Run
		if step.Run == "" || s.Get(step.Run, &worker) != nil || s.Get(step.Review, &reviewer) != nil {
			return fmt.Errorf("current worker and reviewer required")
		}
		if worker.Superseded || reviewer.Superseded || worker.State == "cancelled" || reviewer.State == "cancelled" {
			return fmt.Errorf("recover the current step attempt before amending it")
		}
		if worker.State == "running" || worker.State == "queued" || worker.State == "complete" || reviewer.State == "running" || reviewer.State == "queued" || worker.CandidateRevision != "" {
			return fmt.Errorf("the step must be idle and unfinished before its requirements change")
		}
		before := durablePlan(plan)
		before.ID = ID()
		// Copy the backing array; preserve the old plan in history.
		step.Requirements = append(append([]PlanRequirement{}, step.Requirements[:index]...), step.Requirements[index+1:]...)
		note := fmt.Sprintf("Human scope amendment by %s: requirement %s (%s) is removed. Reason: %s. Its associated acceptance/information is no longer required; do not ask for it again or claim it was fulfilled. Assess the remaining scope and preserve all other requirements and independent review. This grants no new publication or operational authority.", human, req.Key, req.Target, strings.TrimSpace(reason))
		step.Prompt += "\n" + note
		step.Criteria += "\n" + note
		worker.Prompt += "\n" + note
		reviewer.Prompt += "\n" + note
		if reviewer.State == "complete" {
			reviewer.State = "waiting"
			reviewer.Turns = 0
			reviewer.Error = ""
			reviewer.NextAt = ""
		}
		worker.Error = ""
		worker.NextAt = ""
		worker.Turns = 0
		worker.State = "queued"
		writes := []Write{{"plan-history", before.Org, before.Task, before.State, before.ID, before}}
		// Retire only unanswered requests explicitly bound to the removed gate.
		// Unrelated questions and all resolved human answers are retained.
		for _, d := range taskDecisions(s, plan.Task) {
			if d.State == "pending" && d.Acceptance != nil && d.Acceptance.Run == worker.ID && d.Acceptance.Requirement.Key == req.Key {
				d.State = "superseded"
				writes = append(writes, Write{"decision", d.Org, d.Task, d.State, d.ID, d})
			} else if d.State == "pending" && (d.Run == worker.ID || (d.Action != nil && d.Action.Run == worker.ID)) {
				worker.State = "waiting"
			}
		}
		root.Prompt += "\n" + note
		if root.State == "running" {
			root.UpdatesPending = true
		} else if root.State != "complete" && root.State != "cancelled" && root.State != "blocked" && !pendingDecision(s, root.Task, root.ID) {
			root.State = "queued"
			root.Turns = 0
		}
		plan.Revision++
		writes = append(writes, Write{"execution-plan", plan.Org, plan.Task, plan.State, plan.ID, durablePlan(plan)}, Write{"run", worker.Org, worker.Task, worker.State, worker.ID, worker}, Write{"run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer}, Write{"run", root.Org, root.Task, root.State, root.ID, root})
		if err := s.Batch(writes...); err != nil {
			return err
		}
		s.Log(plan.Org, plan.Task, root.ID, "human", note)
		return nil
	}
	return fmt.Errorf("unknown step")
}

// Omitting scope admits no artifact and asserts no success. Downstream work can
// start under the remaining plan; already-started dependents require a separate
// considered revision and are deliberately refused by this first control.
func (e *Engine) omitPlanStep(plan ExecutionPlan, key, reason, human string) error {
	s := e.Store
	reason = strings.TrimSpace(reason)
	plan = durablePlan(plan)
	current := s.taskPlan(plan.Task)
	if human == "" || strings.TrimSpace(reason) == "" || len(reason) > 1500 || current.ID != plan.ID || current.Org != plan.Org || current.Revision != plan.Revision || current.State != "active" {
		return fmt.Errorf("current active plan, human and a short omission reason required")
	}
	var task Assignment
	var root Run
	if s.Get(plan.Task, &task) != nil || task.State == "paused" || task.State == "cancelled" || task.State == "ready" || s.Get(plan.Supervisor, &root) != nil {
		return fmt.Errorf("active assignment required")
	}
	selected := -1
	for i, step := range plan.Steps {
		if step.Key == key {
			selected = i
		}
	}
	if selected < 0 || plan.Steps[selected].Omission != nil {
		return fmt.Errorf("current unomitted step required")
	}
	descendants := map[string]bool{key: true}
	for range plan.Steps {
		for _, step := range plan.Steps {
			for _, dep := range step.DependsOn {
				if descendants[dep] {
					descendants[step.Key] = true
				}
			}
		}
	}
	for _, step := range plan.Steps {
		if step.Key != key && descendants[step.Key] && step.Run != "" {
			return fmt.Errorf("dependent %s has already started; revise the affected work together instead", step.Key)
		}
	}
	step := &plan.Steps[selected]
	members := map[string]bool{}
	if step.Run != "" {
		members[step.Run] = true
	}
	if step.Review != "" {
		members[step.Review] = true
	}
	runs := taskRuns(s, plan.Task)
	for range runs {
		for _, r := range runs {
			if members[r.Parent] || members[r.ReviewOf] {
				members[r.ID] = true
			}
		}
	}
	for _, r := range runs {
		e.mu.Lock()
		_, busy := e.active[r.ID]
		e.mu.Unlock()
		if members[r.ID] && (r.State == "running" || busy) {
			return fmt.Errorf("wait for this step's active workers to yield before omitting it")
		}
	}
	for _, request := range list[AccessRequest](s, "access-request", plan.Org) {
		if request.Task == plan.Task && request.State == "pending" {
			for _, id := range request.Runs {
				if members[id] {
					return fmt.Errorf("resolve the pending access request before omitting this step")
				}
			}
		}
	}
	before := durablePlan(plan)
	before.ID = ID()
	step.Omission = &PlanOmission{Human: human, Reason: strings.TrimSpace(reason), At: now()}
	step.State = "omitted"
	step.Reason = "Omitted by the human: " + reason
	writes := []Write{{"plan-history", before.Org, before.Task, before.State, before.ID, before}}
	for _, r := range runs {
		if !members[r.ID] {
			continue
		}
		// Keep completed evidence historical; only unfinished execution is cancelled.
		if r.State != "complete" && r.State != "cancelled" {
			r.State = "cancelled"
		}
		r.Superseded = true
		writes = append(writes, Write{"run", r.Org, r.Task, r.State, r.ID, r})
	}
	rootPending := false
	for _, d := range taskDecisions(s, plan.Task) {
		if d.State == "pending" && (members[d.Run] || (d.Acceptance != nil && members[d.Acceptance.Run]) || (d.Action != nil && members[d.Action.Run])) {
			d.State = "superseded"
			writes = append(writes, Write{"decision", d.Org, d.Task, d.State, d.ID, d})
		} else if d.State == "pending" && d.Run == root.ID {
			rootPending = true
		}
	}
	note := fmt.Sprintf("Human %s omitted plan step %s: %s. This is a scope change, not completed work or admitted output. Do not execute or review the omitted step. Continue remaining authorized work; no new publication or operational authority.", human, key, reason)
	root.Prompt += "\n" + note
	if root.State == "running" {
		root.UpdatesPending = true
	} else if root.State != "complete" && root.State != "cancelled" && root.State != "blocked" && !rootPending {
		root.State = "queued"
		root.Turns = 0
	}
	plan.Revision++
	writes = append(writes, Write{"execution-plan", plan.Org, plan.Task, plan.State, plan.ID, plan}, Write{"run", root.Org, root.Task, root.State, root.ID, root})
	if err := s.Batch(writes...); err != nil {
		return err
	}
	s.Log(plan.Org, plan.Task, root.ID, "human", note)
	return nil
}
