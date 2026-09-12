package adc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Requirements are frozen with the plan; evidence belongs to a particular attempt.
// These are reviewed observations, not grants or built-in external-service checks.
type PlanRequirement struct {
	Key, Kind, Target, Criteria string
	Wait                        *WaitSpec
}
type MilestoneEvidence struct {
	ID, Org, Task, Run, Requirement, Kind, Target  string
	Summary, Reference, ObservedAt, Actor, Created string
	Revision                                       int
	Withdrawn                                      bool
}
type milestoneInput struct {
	Requirement, Kind, Target, Summary, Reference, ObservedAt string
	Revision                                                  int
	Withdraw                                                  bool
}

func validateRequirements(requirements []PlanRequirement) error {
	if len(requirements) > 12 {
		return fmt.Errorf("use at most 12 milestone requirements per step")
	}
	seen := map[string]bool{}
	for _, r := range requirements {
		if !planKey.MatchString(r.Key) || seen[r.Key] {
			return fmt.Errorf("milestone keys must be unique valid step-style keys")
		}
		seen[r.Key] = true
		if err := validateWait(r); err != nil {
			return err
		}
		switch r.Kind {
		case "reviewed-code", "merged-pr", "published-release", "verified-canary", "human-evidence", "elapsed-time":
		default:
			return fmt.Errorf("unknown milestone kind %q", r.Kind)
		}
		if strings.TrimSpace(r.Target) == "" || len(r.Target) > 2000 || strings.TrimSpace(r.Criteria) == "" || len(r.Criteria) > 8000 {
			return fmt.Errorf("milestone %s needs a bounded exact target and evidence criteria", r.Key)
		}
	}
	return nil
}
func (s *Store) milestoneEvidence(run, key string) MilestoneEvidence {
	var v MilestoneEvidence
	_ = s.Get("milestone:"+run+":"+key, &v)
	return v
}
func (e *Engine) plannedStep(r Run) (ExecutionPlan, PlanStep, bool) {
	p := e.Store.taskPlan(r.Task)
	if p.ID != "" && p.Org == r.Org && p.State != "draft" && !r.Superseded {
		for _, step := range p.Steps {
			if step.Run == r.ID {
				return p, step, true
			}
		}
	}
	return p, PlanStep{}, false
}
func (e *Engine) milestoneMissing(r Run) string {
	if missing := e.integrationMissing(r); missing != "" {
		return missing
	}
	_, step, ok := e.plannedStep(r)
	if !ok {
		return ""
	}
	for _, req := range step.Requirements {
		if req.Wait != nil && e.Store.runWait(r.ID, req.Key).State != "satisfied" {
			return "Milestone " + req.Key + " requires its durable wait to be satisfied; use adc_await then adc_wait"
		}
		if req.Kind == "reviewed-code" {
			if len(r.Code) == 0 {
				return "Milestone " + req.Key + " requires registered committed code"
			}
			continue
		}
		v := e.Store.milestoneEvidence(r.ID, req.Key)
		if v.ID == "" || v.Withdrawn || v.Kind != req.Kind || v.Target != req.Target {
			return "Milestone " + req.Key + " requires " + req.Kind + " evidence for " + req.Target
		}
	}
	return ""
}
func (e *Engine) waitingHumanMilestone(r Run) bool {
	_, step, ok := e.plannedStep(r)
	if !ok {
		return false
	}
	for _, req := range step.Requirements {
		if req.Kind == "human-evidence" {
			v := e.Store.milestoneEvidence(r.ID, req.Key)
			if v.ID == "" || v.Withdrawn {
				return true
			}
		}
	}
	return false
}
func (e *Engine) milestoneRevision(r Run) []byte {
	_, step, ok := e.plannedStep(r)
	if !ok || len(step.Requirements) == 0 {
		return nil
	}
	evidence := []MilestoneEvidence{}
	for _, req := range step.Requirements {
		evidence = append(evidence, e.Store.milestoneEvidence(r.ID, req.Key))
	}
	b, _ := json.Marshal(evidence)
	return b
}

// Caller holds Store.mu. human is supplied only by authenticated web routing,
// never by model arguments. Evidence changes and lifecycle changes commit together.
func (e *Engine) submitMilestone(r Run, input milestoneInput, human string) (MilestoneEvidence, error) {
	return e.recordMilestone(r, input, human, nil)
}
func (e *Engine) recordMilestone(r Run, input milestoneInput, human string, observation *DurableWait) (MilestoneEvidence, error) {
	s := e.Store
	p, step, ok := e.plannedStep(r)
	if !ok {
		return MilestoneEvidence{}, fmt.Errorf("only the current planned worker can record its milestone evidence")
	}
	var task Assignment
	if s.Get(r.Task, &task) != nil || task.Org != r.Org || task.State == "paused" || task.State == "cancelled" || r.State == "cancelled" {
		return MilestoneEvidence{}, fmt.Errorf("resume the assignment and current step before changing evidence")
	}
	var req PlanRequirement
	for _, candidate := range step.Requirements {
		if candidate.Key == input.Requirement {
			req = candidate
		}
	}
	if req.Key == "" || req.Kind != input.Kind || req.Target != input.Target {
		return MilestoneEvidence{}, fmt.Errorf("requirement, kind and target must exactly match the active plan")
	}
	if req.Wait != nil && observation == nil {
		return MilestoneEvidence{}, fmt.Errorf("this requirement is recorded only by its durable observer")
	}
	if req.Kind == "reviewed-code" {
		return MilestoneEvidence{}, fmt.Errorf("use adc_code to register committed code; independent review satisfies this requirement")
	}
	if (req.Kind == "human-evidence") != (human != "") {
		return MilestoneEvidence{}, fmt.Errorf("human-evidence must be supplied by an organization member in the plan; other observations belong to its worker")
	}
	old := s.milestoneEvidence(r.ID, req.Key)
	if old.Revision != input.Revision {
		return old, fmt.Errorf("milestone evidence changed; inspect latest evidence before replacing")
	}
	if !input.Withdraw {
		if strings.TrimSpace(input.Summary) == "" || len(input.Summary) > 8000 || strings.TrimSpace(input.Reference) == "" || len(input.Reference) > 2000 {
			return old, fmt.Errorf("provide a concrete observation and its source reference (up to 8000 and 2000 characters)")
		}
		observed, err := time.Parse(time.RFC3339, input.ObservedAt)
		if err != nil || observed.After(time.Now().Add(time.Minute)) {
			return old, fmt.Errorf("ObservedAt must be an RFC3339 observation time, not a future promise")
		}
	} else if old.ID == "" {
		return old, fmt.Errorf("no evidence to withdraw")
	}
	if input.Withdraw {
		input.Summary = old.Summary
		input.Reference = old.Reference
		input.ObservedAt = old.ObservedAt
	}
	actor := "run:" + r.ID
	if human != "" {
		actor = "human:" + human
	}
	if observation != nil {
		actor = "observer:" + observation.ID
	}
	v := MilestoneEvidence{ID: "milestone:" + r.ID + ":" + req.Key, Org: r.Org, Task: r.Task, Run: r.ID, Requirement: req.Key, Kind: req.Kind, Target: req.Target, Summary: input.Summary, Reference: input.Reference, ObservedAt: input.ObservedAt, Actor: actor, Created: now(), Revision: old.Revision + 1, Withdrawn: input.Withdraw}
	writes := []Write{{"milestone-evidence", r.Org, r.Task, "", v.ID, v}}
	if old.ID != "" {
		history := old
		history.ID = ID()
		writes = append(writes, Write{"milestone-evidence-history", r.Org, r.Task, "", history.ID, history})
	}
	var reviewer Run
	if s.Get(step.Review, &reviewer) == nil && reviewer.State == "complete" {
		reviewer.State = "waiting"
		reviewer.Turns = 0
		writes = append(writes, Write{"run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer})
	}
	if observation != nil {
		writes = append(writes, Write{"durable-wait", observation.Org, observation.Task, observation.State, observation.ID, *observation})
	}
	if human != "" || observation != nil {
		// Wake a yielded worker; an active worker receives steering without concurrent execution.
		r.Prompt += "\nMilestone evidence changed. Inspect the plan in adc_status, check its current evidence against the criteria, and finish only when all requirements are met. Evidence is not additional execution authority."
		if r.State == "running" {
			r.UpdatesPending = true
		} else if (r.State == "waiting" || r.State == "complete") && !pendingDecision(s, r.Task, r.ID) {
			r.State = "queued"
			r.Turns = 0
			r.Attempts = 0
		}
		writes = append(writes, Write{"run", r.Org, r.Task, r.State, r.ID, r})
		var root Run
		if s.Get(p.Supervisor, &root) == nil && root.State == "complete" {
			root.State = "waiting"
			writes = append(writes, Write{"run", root.Org, root.Task, root.State, root.ID, root})
		}
		if task.State == "ready" {
			task.State = "queued"
			task.Output = ""
			writes = append(writes, Write{"assignment", task.Org, "", task.State, task.ID, task})
		}
		p.State = "active"
		writes = append(writes, Write{"execution-plan", p.Org, p.Task, p.State, p.ID, p})
	}
	if err := s.Batch(writes...); err != nil {
		return old, err
	}
	s.Log(r.Org, r.Task, r.ID, "milestone", fmt.Sprintf("%s evidence revision %d recorded by %s (withdrawn: %t)", req.Key, v.Revision, actor, v.Withdrawn))
	return v, nil
}

// Milestones joins frozen requirements to their current evidence for the UI.
type PlanMilestoneView struct {
	PlanRequirement
	Evidence MilestoneEvidence
	Wait     DurableWait
}

func (s PlanStep) Milestones() []PlanMilestoneView {
	views := []PlanMilestoneView{}
	for i, r := range s.Requirements {
		v := PlanMilestoneView{PlanRequirement: r}
		if i < len(s.Waits) {
			v.Wait = s.Waits[i]
		}
		if i < len(s.Evidence) {
			v.Evidence = s.Evidence[i]
		}
		views = append(views, v)
	}
	return views
}
