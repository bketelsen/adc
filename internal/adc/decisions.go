package adc

import (
	"fmt"
	"reflect"
	"strings"
)

type decisionInput struct {
	Question, Kind string
	Replaces       string  `json:"replaces,omitempty"`
	ProposedAgents []Agent `json:"proposed_agents"`
}

// Caller holds Store.mu. Replacement uses a new approval ID so an old browser
// form cannot approve a proposal that changed after the human read it.
func (e *Engine) submitDecision(r Run, p decisionInput) (Decision, error) {
	s := e.Store
	d := Decision{ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, Question: p.Question, Kind: p.Kind, Proposal: p.ProposedAgents, Replaces: p.Replaces, State: "pending"}
	writes := []Write{}
	var previous Decision
	if p.Replaces != "" {
		if s.Get(p.Replaces, &previous) != nil || previous.Org != r.Org || previous.Task != r.Task || previous.Run != r.ID || previous.State != "pending" || len(previous.Proposal) == 0 {
			return Decision{}, fmt.Errorf("replaces must identify this run's pending team proposal; resolved decisions cannot be rewritten")
		}
		if len(p.ProposedAgents) == 0 {
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
			if len(p.ProposedAgents) > 0 && (p.Question != existing.Question || !reflect.DeepEqual(p.ProposedAgents, existing.Proposal)) {
				return Decision{}, fmt.Errorf("decision %s is already pending; to revise its team proposal supply replaces=%s and the complete revised Question and proposed_agents", existing.ID, existing.ID)
			}
			r.State = "waiting"
			return existing, s.Put("run", r.Org, r.Task, r.State, r.ID, r)
		}
	}
	if strings.TrimSpace(d.Question) == "" {
		return Decision{}, fmt.Errorf("a concrete question is required")
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
