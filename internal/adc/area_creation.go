package adc

import (
	"fmt"
	copilot "github.com/github/copilot-sdk/go"
	"net/http"
	"strings"
)

type AreaSpec struct{ Name, Owner, Intent, CompletionMode string }
type areaProposalInput struct {
	AreaSpec
	Replaces string
}

func (s *Store) prepareProposedArea(org string, p AreaSpec, human string) (Area, []Write, error) {
	var kind string
	if s.db.QueryRow("SELECT kind FROM records WHERE id=?", p.Owner).Scan(&kind) != nil || kind != "agent" {
		return Area{}, nil, fmt.Errorf("choose an existing permanent owner from this organization's team")
	}
	for _, a := range list[Area](s, "area", org) {
		if strings.EqualFold(strings.TrimSpace(a.Name), strings.TrimSpace(p.Name)) {
			return Area{}, nil, fmt.Errorf("an area with this name already exists; edit it or choose a distinct responsibility")
		}
	}
	return s.prepareArea(Area{Org: org, Name: strings.TrimSpace(p.Name), Owner: p.Owner, Intent: strings.TrimSpace(p.Intent), CompletionMode: p.CompletionMode}, 0, human)
}

func (w *Web) startAreaConversation(r *http.Request, p Page) error {
	s := w.Store
	if !boundedText(r.FormValue("prompt"), 6000) || Family(r.FormValue("model")) == "" {
		return fmt.Errorf("describe the responsibility and choose an explicit designer model")
	}
	if len(list[Agent](s, "agent", p.Org.ID)) == 0 {
		return fmt.Errorf("create a permanent team member first so the area can have an owner")
	}
	var account Account
	if s.Get(r.FormValue("account"), &account) != nil || account.User != p.User.ID {
		return fmt.Errorf("choose your own subscription")
	}
	if err := s.checkOwnershipText(p.Org.ID, r.FormValue("prompt")); err != nil {
		return err
	}
	guide := Agent{ID: ID(), Org: p.Org.ID, Name: "Area designer", Provider: providerName(account.Provider), Model: r.FormValue("model"), Category: "supervision", Authority: "observe", Description: "Help the human define an area of responsibility conversationally. Recommend an existing permanent owner, clear intent and boundaries, and proportionate completion expectations. Ask focused questions when needed, then propose the area for human approval."}
	task := Assignment{ID: ID(), Org: p.Org.ID, Owner: guide.ID, Creator: p.User.ID, Account: account.ID, Kind: "proposal", AreaCreation: true, Title: "Define an area of responsibility", Prompt: r.FormValue("prompt"), Authority: "observe", ConstrainTools: true}
	writes, err := w.Engine.assignmentWrites(task, guide)
	if err != nil {
		return err
	}
	writes = append(writes, Write{"guide", guide.Org, "", "", guide.ID, guide})
	if err := s.Batch(writes...); err != nil {
		return err
	}
	r.Form.Set("return", "/task?org="+task.Org+"&id="+task.ID)
	return nil
}

func (e *Engine) areaProposalTool(original Run) copilot.Tool {
	return copilot.DefineTool("adc_propose_area", "Propose one area for human approval in this area-creation conversation. Supply Name, an existing permanent Owner agent ID, Intent covering desired outcomes and boundaries, and CompletionMode reviewed (default) or routine. Creation grants no tools and schedules no work. Use Replaces with the pending decision ID to revise the complete proposal; after reject/refine create a new proposal. Human approval creates the area; do not claim it exists before approval.", func(p areaProposalInput, _ copilot.ToolInvocation) (any, error) {
		s := e.Store
		s.mu.Lock()
		defer s.mu.Unlock()
		var r Run
		var task Assignment
		if s.Get(original.ID, &r) != nil || r.State != "running" || r.Superseded || s.Get(r.Task, &task) != nil || !task.AreaCreation || task.Kind != "proposal" || task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
			return nil, fmt.Errorf("active area-creation conversation required")
		}
		if p.CompletionMode == "" {
			p.CompletionMode = "reviewed"
		}
		brief := "Create the area “" + p.Name + "” with the proposed owner and boundaries. This creates context only; it grants no access and starts no scheduled work."
		return e.submitDecision(r, decisionInput{Kind: "area-proposal", Question: p.Intent, Brief: brief, Replaces: p.Replaces, ProposedArea: &p.AreaSpec})
	})
}
