package adc

import (
	"fmt"
	"reflect"
	"strings"
)

type decisionInput struct {
	Action         *decisionActionInput `json:"action,omitempty"`
	Question, Kind string
	Brief          string                   `json:"brief,omitempty"`
	Acceptance     *decisionAcceptanceInput `json:"acceptance,omitempty"`
	Replaces       string                   `json:"replaces,omitempty"`
	ProposedAgents []Agent                  `json:"proposed_agents"`
}

// Caller holds Store.mu. Replacement uses a new approval ID so an old browser
// form cannot approve a proposal that changed after the human read it.
func (e *Engine) submitDecision(r Run, p decisionInput) (Decision, error) {
	s := e.Store
	d := Decision{Brief: strings.TrimSpace(p.Brief), ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, Question: p.Question, Kind: p.Kind, Proposal: p.ProposedAgents, Replaces: p.Replaces, State: "pending"}
	if p.Action != nil && p.Replaces == "" {
		for _, prior := range taskDecisions(s, r.Task) {
			if prior.Run == r.ID && prior.State == "answered" && prior.Outcome == "approve" && sameActionInput(p.Action, prior.Action) {
				pin, err := e.pinDecisionAction(r, *p.Action)
				if prior.Action.State == "done" || (err == nil && pin.Artifact == prior.Action.Artifact && pin.PlanRevision == prior.Action.PlanRevision) {
					return prior, nil
				}
			}
		}
	}
	writes := []Write{}
	var previous Decision
	if p.Replaces != "" {
		if s.Get(p.Replaces, &previous) != nil || previous.Org != r.Org || previous.Task != r.Task || previous.Run != r.ID || previous.State != "pending" || previous.Kind == "permission" {
			return Decision{}, fmt.Errorf("replaces must identify this run's pending decision; resolved decisions cannot be rewritten")
		}
		if len(previous.Proposal) > 0 && len(p.ProposedAgents) == 0 {
			return Decision{}, fmt.Errorf("a revision requires the complete proposed_agents list")
		}
		d.Kind = previous.Kind
		d.Document = previous.Document
		previous.State = "superseded"
		writes = append(writes, Write{"decision", previous.Org, previous.Task, previous.State, previous.ID, previous})
	} else {
		for _, existing := range taskDecisions(s, r.Task) {
			if existing.Run != r.ID || existing.State != "pending" {
				continue
			}
			if p.Question != existing.Question || strings.TrimSpace(p.Brief) != existing.Brief || !reflect.DeepEqual(p.ProposedAgents, existing.Proposal) || !sameAcceptanceInput(p.Acceptance, existing.Acceptance) || !sameActionInput(p.Action, existing.Action) {
				return Decision{}, fmt.Errorf("decision %s is already pending; to revise it supply replaces=%s and the complete revised question, brief, acceptance (if any), and proposed_agents (for teams)", existing.ID, existing.ID)
			}
			if existing.Action != nil {
				pin, err := e.pinDecisionAction(r, existing.Action.decisionActionInput)
				if err != nil || !reflect.DeepEqual(pin, existing.Action) {
					return Decision{}, fmt.Errorf("decision action is stale; refresh using replaces=%s", existing.ID)
				}
			}
			if existing.Acceptance != nil {
				current, err := e.pinDecisionAcceptance(r, *p.Acceptance)
				if err != nil || !reflect.DeepEqual(existing.Acceptance, current) {
					detail := ""
					if err != nil {
						detail = ": " + err.Error()
					}
					return Decision{}, fmt.Errorf("decision %s has stale acceptance evidence; inspect the current plan and evidence, then refresh the request with replaces=%s and complete revised fields (do not request acceptance again if it is already recorded)%s", existing.ID, existing.ID, detail)
				}
			}
			r.State = "waiting"
			return existing, s.Put("run", r.Org, r.Task, r.State, r.ID, r)
		}
	}
	if strings.TrimSpace(d.Question) == "" {
		return Decision{}, fmt.Errorf("a concrete question is required")
	}
	if len(d.Brief) > 1000 {
		return Decision{}, fmt.Errorf("brief must be at most 1000 characters; put supporting detail in Question")
	}
	if p.Action != nil {
		if p.Acceptance != nil || len(p.ProposedAgents) > 0 || d.Kind == "permission" || d.Brief == "" {
			return Decision{}, fmt.Errorf("action needs a brief and cannot be combined with acceptance/team/access approval")
		}
		var err error
		d.Action, err = e.pinDecisionAction(r, *p.Action)
		if err != nil {
			return Decision{}, err
		}
	}
	if p.Acceptance != nil {
		if d.Brief == "" || len(d.Proposal) > 0 || d.Kind == "permission" {
			return Decision{}, fmt.Errorf("milestone acceptance needs a concise brief and cannot be combined with team or permission approval")
		}
		var err error
		d.Acceptance, err = e.pinDecisionAcceptance(r, *p.Acceptance)
		if err != nil {
			return Decision{}, err
		}
	}
	if len(d.Proposal) > 0 {
		if _, err := PrepareTeam(s, d.Org, d.Proposal); err != nil {
			return Decision{}, fmt.Errorf("team proposal needs correction: %w", err)
		}
		document := teamProposalDocument(d)
		if d.Document != "" {
			var old Document
			if err := s.Get(d.Document, &old); err != nil || old.Org != r.Org || old.Task != r.Task {
				return Decision{}, fmt.Errorf("previous proposal document is unavailable")
			}
			document.ID = old.ID
			document.Revision = old.Revision + 1
			old.ID = ID()
			writes = append(writes, Write{"revision", old.Org, document.ID, "", old.ID, old})
		}
		d.Document = document.ID
		writes = append(writes, Write{"document", document.Org, document.Task, "", document.ID, document})
	}
	r.State = "waiting"
	if err := s.Batch(append(writes, Write{"decision", d.Org, d.Task, d.State, d.ID, d}, Write{"run", r.Org, r.Task, r.State, r.ID, r})...); err != nil {
		return Decision{}, err
	}
	s.Log(r.Org, r.Task, r.ID, "decision", d.Question)
	return d, nil
}

// Withdrawal removes only an unanswered request, never human authority or its
// recorded outcome. The caller already passed the active-run guard.
func (e *Engine) withdrawDecision(r Run, id, reason string) (Decision, error) {
	var d Decision
	if strings.TrimSpace(reason) == "" || len(reason) > 1000 {
		return d, fmt.Errorf("provide a concise reason for withdrawing the obsolete request")
	}
	if e.Store.Get(id, &d) != nil || d.Org != r.Org || d.Task != r.Task || d.Run != r.ID || d.State != "pending" || d.Kind == "permission" {
		return d, fmt.Errorf("withdraw only your own pending non-permission decision; human answers and access requests cannot be withdrawn here")
	}
	d.State = "withdrawn"
	d.Answer = "Withdrawn by requesting agent: " + reason
	// No approval outcome or human attribution is recorded.
	d.Outcome = ""
	if err := e.Store.Put("decision", d.Org, d.Task, d.State, d.ID, d); err != nil {
		return d, err
	}
	e.Store.Log(r.Org, r.Task, r.ID, "decision-withdrawn", reason)
	return d, nil
}
