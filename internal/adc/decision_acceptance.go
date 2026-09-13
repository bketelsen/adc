package adc

import (
	"fmt"
	"reflect"
	"strings"
)

// Agents name the acceptance to request. ADC supplies every revision and actor.
type decisionAcceptanceInput struct {
	Run         string `json:"run"`
	Requirement string `json:"requirement"`
}

type DecisionAcceptance struct {
	Run, Plan, Step, StepTitle, Artifact string
	PlanRevision, EvidenceRevision       int
	Requirement                          PlanRequirement
}

func sameAcceptanceInput(input *decisionAcceptanceInput, pin *DecisionAcceptance) bool {
	if input == nil || pin == nil {
		return input == nil && pin == nil
	}
	return input.Run == pin.Run && input.Requirement == pin.Requirement.Key
}

func (e *Engine) pinDecisionAcceptance(requester Run, input decisionAcceptanceInput) (*DecisionAcceptance, error) {
	var worker Run
	if e.Store.Get(input.Run, &worker) != nil || worker.Org != requester.Org || worker.Task != requester.Task || worker.Superseded || worker.State == "cancelled" {
		return nil, fmt.Errorf("acceptance must name a current worker in this assignment")
	}
	plan, step, ok := e.plannedStep(worker)
	if !ok {
		return nil, fmt.Errorf("acceptance must name a dispatched plan step")
	}
	for _, req := range step.Requirements {
		if req.Key != input.Requirement {
			continue
		}
		if req.Kind != "human-evidence" || req.Wait != nil {
			return nil, fmt.Errorf("acceptance can only record a human-evidence requirement")
		}
		old := e.Store.milestoneEvidence(worker.ID, req.Key)
		if old.ID != "" && !old.Withdrawn {
			return nil, fmt.Errorf("this requirement already has human evidence; inspect it instead of requesting acceptance again")
		}
		return &DecisionAcceptance{Run: worker.ID, Plan: plan.ID, Step: step.Key, StepTitle: step.Title, PlanRevision: plan.Revision, EvidenceRevision: old.Revision, Requirement: req, Artifact: e.artifactRevision(worker)}, nil
	}
	return nil, fmt.Errorf("acceptance requirement is not declared on that step")
}

// Called under Store.mu before any decision writes. A changed plan, artifact or
// evidence cannot inherit an approval from a stale browser card.
func (e *Engine) decisionAcceptanceWrites(d Decision, requester Run, human string) ([]Write, error) {
	if d.Acceptance == nil {
		return nil, nil
	}
	pin := d.Acceptance
	current, err := e.pinDecisionAcceptance(requester, decisionAcceptanceInput{Run: pin.Run, Requirement: pin.Requirement.Key})
	if err != nil || !reflect.DeepEqual(pin, current) {
		return nil, fmt.Errorf("the proposed acceptance changed; ask the agent to refresh this decision before approving")
	}
	var worker Run
	if err := e.Store.Get(pin.Run, &worker); err != nil {
		return nil, err
	}
	_, writes, err := e.prepareMilestone(worker, milestoneInput{
		Requirement: pin.Requirement.Key, Kind: pin.Requirement.Kind, Target: pin.Requirement.Target,
		Summary: d.Answer, Reference: "adc:decision:" + d.ID + " artifact:" + pin.Artifact,
		ObservedAt: d.ResolvedAt, Revision: pin.EvidenceRevision,
	}, human, nil)
	if err != nil {
		return nil, err
	}
	// prepareMilestone keeps a pending-decision worker waiting. This transaction
	// resolves this decision; wake its target unless another decision still needs it.
	otherPending := false
	for _, other := range taskDecisions(e.Store, d.Task) {
		if other.ID != d.ID && other.Run == worker.ID && other.State == "pending" {
			otherPending = true
		}
	}
	for i := range writes {
		if writes[i].ID != worker.ID || otherPending {
			continue
		}
		updated, ok := writes[i].Value.(Run)
		if ok && (updated.State == "waiting" || updated.State == "blocked" || updated.State == "complete") {
			updated.State, updated.Turns, updated.Attempts = "queued", 0, 0
			writes[i].State, writes[i].Value = updated.State, updated
		}
	}
	return writes, nil
}

func (d Decision) BriefText() string {
	if d.Brief != "" {
		return d.Brief
	}
	// Legacy questions have no separate brief. Show a bounded excerpt with the
	// full original request available, never invent a summary or acceptance link.
	text := strings.TrimSpace(strings.SplitN(d.Question, "\n\n", 2)[0])
	runes := []rune(text)
	if len(runes) > 600 {
		text = string(runes[:600]) + "…"
	}
	return text
}

func decisionAnswer(d Decision, outcome, notes string) (string, error) {
	notes = strings.TrimSpace(notes)
	if len(notes) > 6000 {
		return "", fmt.Errorf("keep decision notes under 6000 characters")
	}
	var prefix string
	switch outcome {
	case "approve":
		prefix = "APPROVED"
	case "reject":
		prefix = "REJECTED; do not execute this proposal"
	case "refine":
		if notes == "" {
			return "", fmt.Errorf("add notes describing how to refine the proposal")
		}
		prefix = "REFINEMENT REQUESTED; not approved. Return a revised decision brief"
	default:
		return "", fmt.Errorf("choose Approve, Reject, or Refine with notes")
	}
	answer := prefix + ": " + d.BriefText()
	if notes != "" {
		answer += "\nHuman notes: " + notes
	}
	return answer, nil
}
