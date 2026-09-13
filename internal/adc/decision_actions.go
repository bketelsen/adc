package adc

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

type decisionActionInput struct {
	Run, Action, Target, Reference, Validation, Rollback, Requirement string
}
type DecisionAction struct {
	RequirementDefinition *PlanRequirement
	OutcomeArtifact       string
	decisionActionInput
	Artifact, Plan                               string
	PlanRevision                                 int
	State, Summary, OutcomeReference, ObservedAt string
}

func (e *Engine) pinDecisionAction(requester Run, p decisionActionInput) (*DecisionAction, error) {
	s := e.Store
	var task Assignment
	if s.Get(requester.Task, &task) != nil || task.Org != requester.Org || task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
		return nil, fmt.Errorf("resume the active assignment before proposing or approving an action")
	}
	var r Run
	if s.Get(p.Run, &r) != nil || r.Org != requester.Org || r.Task != requester.Task || r.Superseded || r.State == "cancelled" || r.ReviewOf != "" || (r.ID != requester.ID && r.Parent != requester.ID) {
		return nil, fmt.Errorf("action executor must be this worker or a directly delegated current worker")
	}
	if strings.TrimSpace(p.Action) == "" || strings.TrimSpace(p.Target) == "" || strings.TrimSpace(p.Validation) == "" || strings.TrimSpace(p.Rollback) == "" {
		return nil, fmt.Errorf("action requires concrete Action, Target, Validation and Rollback (including irreversibility if applicable)")
	}
	if len(p.Action)+len(p.Target)+len(p.Validation)+len(p.Rollback)+len(p.Reference) > 8000 {
		return nil, fmt.Errorf("keep the action bounded")
	}
	plan, step, _ := e.plannedStep(r)
	var definition *PlanRequirement
	if p.Requirement != "" {
		found := false
		for _, req := range step.Requirements {
			if req.Key == p.Requirement && req.Kind != "human-evidence" && req.Kind != "reviewed-code" && req.Wait == nil {
				found = true
				copy := req
				definition = &copy
			}
		}
		if !found {
			return nil, fmt.Errorf("action requirement must be this executor's manually observed external milestone")
		}
	}
	return &DecisionAction{RequirementDefinition: definition, decisionActionInput: p, Artifact: e.artifactRevision(r), Plan: plan.ID, PlanRevision: plan.Revision, State: "proposed"}, nil
}

func sameActionInput(input *decisionActionInput, action *DecisionAction) bool {
	if input == nil || action == nil {
		return input == nil && action == nil
	}
	return reflect.DeepEqual(*input, action.decisionActionInput)
}
func (e *Engine) approveDecisionAction(d *Decision, requester Run) ([]Write, error) {
	if d.Action == nil {
		return nil, nil
	}
	current, err := e.pinDecisionAction(requester, d.Action.decisionActionInput)
	if err != nil || !reflect.DeepEqual(current, d.Action) {
		return nil, fmt.Errorf("action or artifacts changed; ask the agent to refresh the concrete request")
	}
	var worker Run
	if err := e.Store.Get(d.Action.Run, &worker); err != nil {
		return nil, err
	}
	d.Action.State = "approved"
	worker.Prompt += fmt.Sprintf("\nHuman approved decision %s: perform %s on %s. Validate: %s. Rollback: %s. Approval means you execute, not the human. First reconcile any existing external outcome; after success call adc_action_result for this decision. This authorizes only the stated action, and does not change tool grants.", d.ID, d.Action.Action, d.Action.Target, d.Action.Validation, d.Action.Rollback)
	if worker.State == "running" {
		worker.UpdatesPending = true
	} else if !otherPendingDecision(e.Store, worker.Task, worker.ID, d.ID) {
		worker.State = "queued"
		worker.Error = ""
		worker.NextAt = ""
		worker.Turns = 0
		worker.Attempts = 0
	}
	return []Write{{"run", worker.Org, worker.Task, worker.State, worker.ID, worker}}, nil
}
func otherPendingDecision(s *Store, task, run, except string) bool {
	for _, d := range taskDecisions(s, task) {
		if d.ID != except && d.Run == run && d.State == "pending" {
			return true
		}
	}
	return false
}

type actionResultInput struct{ Decision, Summary, Reference, ObservedAt string }

func (e *Engine) recordActionResult(r Run, p actionResultInput) (Decision, error) {
	s := e.Store
	var d Decision
	if s.Get(p.Decision, &d) != nil || d.Org != r.Org || d.Task != r.Task || d.Action == nil || d.Action.Run != r.ID || d.Outcome != "approve" || d.State != "answered" {
		return d, fmt.Errorf("approved action for this executor required")
	}
	if d.Action.State == "done" {
		return d, nil
	} // Reconcile the persisted result; never publish twice.
	if d.Action.State != "approved" {
		return d, fmt.Errorf("action is not approved")
	}
	if strings.TrimSpace(p.Summary) == "" || strings.TrimSpace(p.Reference) == "" {
		return d, fmt.Errorf("concrete observed summary and external reference required")
	}
	at, err := time.Parse(time.RFC3339, p.ObservedAt)
	if err != nil || at.After(time.Now().Add(5*time.Minute)) {
		return d, fmt.Errorf("valid observation time required")
	}
	writes := []Write{}
	if d.Action.Requirement != "" {
		_, step, ok := e.plannedStep(r)
		if !ok {
			return d, fmt.Errorf("current plan step missing")
		}
		found := false
		for _, req := range step.Requirements {
			if req.Key != d.Action.Requirement {
				continue
			}
			found = true
			if d.Action.RequirementDefinition == nil || !reflect.DeepEqual(req, *d.Action.RequirementDefinition) {
				return d, fmt.Errorf("linked requirement changed; preserve the external observation and repair its evidence mapping, not its historical approval")
			}
			old := s.milestoneEvidence(r.ID, req.Key)
			_, prepared, err := e.prepareMilestone(r, milestoneInput{Requirement: req.Key, Kind: req.Kind, Target: req.Target, Summary: p.Summary, Reference: p.Reference + "; approved decision " + d.ID, ObservedAt: p.ObservedAt, Revision: old.Revision}, "", nil)
			if err != nil {
				return d, err
			}
			writes = append(writes, prepared...)
		}
		if !found {
			return d, fmt.Errorf("linked requirement missing")
		}
	}
	// Recording an observed fact grants no authority. Artifacts can legitimately
	// change after execution; retain both pins for final independent verification.
	d.Action.OutcomeArtifact = e.artifactRevision(r)
	d.Action.State = "done"
	d.Action.Summary = p.Summary
	d.Action.OutcomeReference = p.Reference
	d.Action.ObservedAt = p.ObservedAt
	return d, s.Batch(append(writes, Write{"decision", d.Org, d.Task, d.State, d.ID, d})...)
}
