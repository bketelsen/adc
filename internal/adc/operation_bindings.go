package adc

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Model-supplied description of one proposed action. ADC supplies all identity
// and revision fields; prose cannot manufacture a binding or an approval.
type OperationProposal struct {
	Action, Target, Environment, Effect, Validation, Rollback string
}
type OperationBinding struct {
	OperationProposal
	Plan, Step, Run, ArtifactRevision, EvidenceRevision string
	PlanRevision                                        int
	PlanEvidence                                        string
}

func operationBundleOwner(entries []AccessEntry) string {
	for _, entry := range entries {
		if entry.Binding != nil {
			return entry.Binding.Run
		}
	}
	return ""
}
func mixedOperationBundle(entries []AccessEntry) bool {
	bound, unbound := false, false
	for _, entry := range entries {
		bound = bound || entry.Binding != nil
		unbound = unbound || entry.Binding == nil
	}
	return bound && unbound
}

// Completed-plan finalization is still an exact human-approved operation. Never
// re-enable broad grants merely because the last step finished.
func (e *Engine) operationOwner(run Run) (ExecutionPlan, string, string, bool) {
	p := e.Store.taskPlan(run.Task)
	if run.ReviewOf != "" {
		return p, "", "", false
	}
	if p.ID != "" && p.Org == run.Org && p.State == "complete" && p.Supervisor == run.ID && run.Parent == "" {
		view := e.inspectPlan(p)
		if view.State != "complete" {
			return p, "", "", false
		}
		pins := map[string]string{}
		for _, step := range p.Steps {
			var source Run
			if e.Store.Get(step.Run, &source) != nil {
				return p, "", "", false
			}
			pins[step.Key] = source.ID + ":" + e.dependencyRevision(source)
		}
		b, _ := json.Marshal(pins)
		return p, "supervisor", digest(string(b)), true
	}
	p, step, ok := e.plannedStep(run)
	return p, step.Key, "", ok && step.Run == run.ID && p.State == "active"
}

func (s *Store) bindOperation(run Run, want AccessWant, policy ToolPolicy) (*OperationBinding, error) {
	plan := s.taskPlan(run.Task)
	required := plan.ID != "" && plan.State != "draft" && (policy.Human == "" || policy.Class != "read")
	if want.Context == nil && !required {
		return nil, nil
	}
	if want.Context == nil || want.Operation == "" || len(want.Arguments) == 0 {
		return nil, fmt.Errorf("planned operations require exact Arguments, a stable Operation and Context with Action, Target, Environment, Effect, Validation and Rollback")
	}
	for _, value := range []string{want.Context.Action, want.Context.Target, want.Context.Environment, want.Context.Effect, want.Context.Validation, want.Context.Rollback} {
		if strings.TrimSpace(value) == "" || len(value) > 4000 {
			return nil, fmt.Errorf("each operation context field must be concrete and at most 4000 characters")
		}
	}
	e := &Engine{Store: s}
	p, step, planEvidence, ok := e.operationOwner(run)
	if !ok {
		return nil, fmt.Errorf("the current planned step worker must request this operation; delegation cannot borrow another step's authority")
	}
	b := &OperationBinding{OperationProposal: *want.Context, Plan: p.ID, PlanRevision: p.Revision, Step: step, Run: run.ID, ArtifactRevision: e.artifactRevision(run), EvidenceRevision: e.revision(run), PlanEvidence: planEvidence}
	if err := s.validateOperationBinding(run, *b); err != nil {
		return nil, err
	}
	return b, nil
}

// Caller holds Store.mu. Validate both at human approval and gateway admission.
// Pending permission decisions are expected here; they never authorize a call
// until ResolveAccess has persisted the matching grant.
func (s *Store) validateOperationBinding(run Run, b OperationBinding) error {
	e := &Engine{Store: s}
	p, key, planEvidence, ok := e.operationOwner(run)
	if !ok || p.ID != b.Plan || p.Revision != b.PlanRevision || key != b.Step || planEvidence != b.PlanEvidence || run.ID != b.Run || run.Superseded || run.State == "cancelled" || e.artifactRevision(run) != b.ArtifactRevision || e.revision(run) != b.EvidenceRevision || !e.planAllowsDispatch(run) {
		return fmt.Errorf("operation proposal is stale or belongs to another step/attempt; inspect current evidence and request a fresh exact operation approval")
	}
	if err := verifyCode(run); err != nil {
		return err
	}
	_, step, _ := e.plannedStep(run)
	for _, upstream := range p.Steps {
		if planEvidence == "" && !containsString(step.DependsOn, upstream.Key) {
			continue
		}
		var source Run
		if s.Get(upstream.Run, &source) != nil || (planEvidence == "" && step.Inputs[upstream.Key] != source.ID+":"+e.dependencyRevision(source)) {
			return fmt.Errorf("approved prerequisite evidence changed")
		}
		if err := verifyCode(source); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) operationGrantMatches(run Run, policy ToolPolicy, grant CapabilityGrant) bool {
	plan := s.taskPlan(run.Task)
	if grant.Binding == nil {
		// Built-in draft delivery already requires explicit task publication, exact
		// reviewed source and current prerequisites at every remote mutation. An
		// existing matching capability need not be approved again just for a plan.
		var task Assignment
		var tool GatewayTool
		var connection Connection
		if grant.Scope == "assignment" && s.Get(run.Task, &task) == nil && task.Publication && s.Get(policy.Tool, &tool) == nil && tool.Name == "github_draft_pr" && s.Get(tool.Connection, &connection) == nil && connection.Transport == "github" {
			return true
		}

		// Neither a broader standing grant nor a child/root handoff can bypass
		// artifact-bound approval for operations in a protected execution plan.
		return plan.ID == "" || plan.State == "draft" || policy.Class == "read"
	}
	return grant.Scope == "operation" && s.validateOperationBinding(run, *grant.Binding) == nil
}

// Caller holds Store.mu. Expiration is not a denial or an approval: it retires a
// now-invalid proposal and returns its owner to ordinary correction/reproposal.
// Separate owners have separate bound bundles, so one stale branch cannot hold
// another branch's otherwise reviewable request.
func (e *Engine) expireOperationRequests() {
	s := e.Store
	for _, request := range list[AccessRequest](s, "access-request", "") {
		if request.State != "pending" || operationBundleOwner(request.Entries) == "" {
			continue
		}
		var task Assignment
		if s.Get(request.Task, &task) != nil || task.State == "paused" {
			continue
		}
		stale := false
		for _, entry := range request.Entries {
			if entry.Binding == nil {
				continue
			}
			var owner Run
			if s.Get(entry.Binding.Run, &owner) != nil || owner.Org != request.Org || owner.Task != request.Task || s.validateOperationBinding(owner, *entry.Binding) != nil {
				stale = true
				break
			}
		}
		if !stale {
			continue
		}
		history := request
		history.ID = ID()
		request.State = "stale"
		request.Resolved = now()
		request.Revision++
		var decision Decision
		writes := []Write{{"access-request-history", request.Org, request.ID, history.State, history.ID, history}, {"access-request", request.Org, request.Task, request.State, request.ID, request}}
		if s.Get(request.Decision, &decision) == nil && decision.State == "pending" {
			decision.State = "superseded"
			decision.Answer = "Operation evidence changed; no access was granted."
			writes = append(writes, Write{"decision", request.Org, request.Task, decision.State, decision.ID, decision})
		}
		otherTaskDecision := false
		for _, other := range taskDecisions(s, task.ID) {
			if other.ID != decision.ID && other.State == "pending" {
				otherTaskDecision = true
			}
		}
		if task.State == "needs input" && !otherTaskDecision {
			task.State = "queued"
			writes = append(writes, Write{"assignment", task.Org, "", task.State, task.ID, task})
		}
		for _, id := range request.Runs {
			var owner Run
			if s.Get(id, &owner) != nil || owner.Superseded || owner.State != "waiting" || task.State == "cancelled" {
				continue
			}
			pending := false
			for _, other := range taskDecisions(s, task.ID) {
				if other.ID != decision.ID && other.Run == id && other.State == "pending" {
					pending = true
				}
			}
			if pending {
				continue
			}
			owner.State, owner.Turns = "queued", 0
			const notice = "\nAn operational proposal expired because its evidence changed. Inspect current artifacts and prerequisites; propose the exact operation again if still needed. No access was granted by expiration."
			if !strings.Contains(owner.Prompt, notice) {
				owner.Prompt += notice
			}
			writes = append(writes, Write{"run", owner.Org, owner.Task, owner.State, owner.ID, owner})
		}
		if s.Batch(writes...) == nil {
			s.Log(request.Org, request.Task, "", "permission-stale", "Operational proposal expired after its evidence changed; unaffected requests remain available.")
		}
	}
}
