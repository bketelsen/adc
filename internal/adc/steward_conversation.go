package adc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// A steward is asked for, not filled in. The human describes what needs
// looking after; a temporary designer asks what the description leaves open,
// then proposes the agent, its charter, connections, repositories and
// routines in one decision the human approves, refines or rejects.
type RoutineSpec struct {
	Title, Brief string
	Cadence      Cadence
}
type StewardSpec struct {
	Agent, Name, Description, Model, Charter, CompletionMode string
	Tools, Repositories                                      []string
	Routines                                                 []RoutineSpec
}
type stewardProposalInput struct {
	StewardSpec
	Replaces string
}

// prepareProposedSteward validates a proposal and returns what approval would
// create. Nothing is written; approveProposedSteward produces the writes.
func (s *Store) prepareProposedSteward(org string, spec StewardSpec, account Account) (Agent, Steward, []RoutineSpec, error) {
	var agent Agent
	if spec.Agent != "" {
		var kind string
		if s.db.QueryRow("SELECT kind FROM records WHERE id=?", spec.Agent).Scan(&kind) != nil || kind != "agent" || s.Get(spec.Agent, &agent) != nil || agent.Org != org {
			return Agent{}, Steward{}, nil, fmt.Errorf("Agent must be an existing permanent agent in this organization, or empty to create one")
		}
		if _, exists := s.steward(agent.ID); exists {
			return Agent{}, Steward{}, nil, fmt.Errorf("%s is already a steward; edit its charter on the Stewards page", agent.Name)
		}
		if spec.Model != "" {
			agent.Model = spec.Model
		}
		if len(spec.Tools) > 0 {
			agent.Tools = spec.Tools
		}
		if strings.TrimSpace(spec.Description) != "" {
			agent.Description = strings.TrimSpace(spec.Description)
		}
	} else {
		name := strings.TrimSpace(spec.Name)
		if !boundedText(name, 80) || !boundedText(spec.Description, 1000) {
			return Agent{}, Steward{}, nil, fmt.Errorf("a new steward needs a short Name and a one-paragraph Description")
		}
		for _, existing := range list[Agent](s, "agent", org) {
			if strings.EqualFold(strings.TrimSpace(existing.Name), name) {
				return Agent{}, Steward{}, nil, fmt.Errorf("an agent named %s already exists; set Agent to attach the steward to it or choose another name", existing.Name)
			}
		}
		agent = Agent{ID: ID(), Org: org, Name: name, Description: strings.TrimSpace(spec.Description), Provider: providerName(account.Provider), Model: spec.Model, Category: "supervision", Authority: "draft", Tools: spec.Tools}
	}
	if err := validateAgent(s, agent); err != nil {
		return Agent{}, Steward{}, nil, err
	}
	mode := spec.CompletionMode
	if mode == "" {
		mode = "routine"
	}
	if !validCompletionMode(mode) {
		return Agent{}, Steward{}, nil, fmt.Errorf("CompletionMode is routine or reviewed")
	}
	if !boundedText(spec.Charter, charterLimit) {
		return Agent{}, Steward{}, nil, fmt.Errorf("give the steward a Charter of at most %d characters", charterLimit)
	}
	repos := []string{}
	for _, repo := range spec.Repositories {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		if len(repo) > 200 || !evidenceReference(repo) {
			return Agent{}, Steward{}, nil, fmt.Errorf("repository identities are short credential-free references such as github.com/owner/repo")
		}
		repos = append(repos, repo)
	}
	if len(spec.Routines) > 8 {
		return Agent{}, Steward{}, nil, fmt.Errorf("propose at most 8 routines; more can be proposed later as standing work")
	}
	texts := []string{spec.Charter, agent.Description}
	for _, routine := range spec.Routines {
		if !boundedText(routine.Title, 200) || !boundedText(routine.Brief, 4000) {
			return Agent{}, Steward{}, nil, fmt.Errorf("each routine needs a Title and a Brief (at most 4000 characters)")
		}
		if routine.Cadence.Frequency == "" {
			return Agent{}, Steward{}, nil, fmt.Errorf("routine %q needs a Cadence: interval, daily or weekly", routine.Title)
		}
		if err := routine.Cadence.Validate(); err != nil {
			return Agent{}, Steward{}, nil, fmt.Errorf("routine %q: %w", routine.Title, err)
		}
		texts = append(texts, routine.Title, routine.Brief)
	}
	if err := s.checkOwnershipText(org, append(texts, repos...)...); err != nil {
		return Agent{}, Steward{}, nil, err
	}
	return agent, Steward{Org: org, Agent: agent.ID, Charter: strings.TrimSpace(spec.Charter), CompletionMode: mode, Repositories: repos}, spec.Routines, nil
}

// approveProposedSteward turns an approved proposal into writes: the agent
// (new or updated), a missing category default, the steward, and one standing
// schedule per routine. Caller holds Store.mu and commits the writes.
func (e *Engine) approveProposedSteward(t Assignment, spec StewardSpec, user User) (Agent, []Write, error) {
	s := e.Store
	var account Account
	_ = s.Get(t.Account, &account)
	agent, steward, routines, err := s.prepareProposedSteward(t.Org, spec, account)
	if err != nil {
		return Agent{}, nil, err
	}
	writes := []Write{{"agent", agent.Org, agent.ReportsTo, "", agent.ID, agent}}
	key := "default:" + agent.Org + ":" + agent.Category
	var current CategoryDefault
	if s.Get(key, &current) != nil {
		writes = append(writes, Write{"default", agent.Org, "", "", key, CategoryDefault{agent.Org, agent.Category, agent.ID}})
	}
	steward.ID, steward.Created, steward.Updated, steward.UpdatedBy, steward.Revision = stewardID(agent.ID), now(), now(), "human:"+user.ID, 1
	writes = append(writes, Write{"steward", steward.Org, steward.Agent, "", steward.ID, steward})
	for _, routine := range routines {
		template := Assignment{Org: t.Org, Owner: agent.ID, Steward: agent.ID, Account: t.Account, Creator: user.ID, Authority: "observe", Title: routine.Title, Completion: selectedCompletion("routine")}
		template.Prompt = strings.TrimSpace(routine.Brief) + "\nThis is one of your routines. Record findings in your journal and raise signals for anything a human should know; do not produce a document unless the finding itself needs one."
		proposal := WorkProposal{ID: ID(), Org: t.Org, Title: routine.Title, Cadence: routine.Cadence}
		_, scheduleWrites, err := e.approveSchedule(proposal, template, "Routine of "+agent.Name+": "+routine.Title, time.Now(), agent)
		if err != nil {
			return Agent{}, nil, fmt.Errorf("routine %q: %w", routine.Title, err)
		}
		writes = append(writes, scheduleWrites...)
	}
	return agent, writes, nil
}

// startStewardConversation is the only way a steward is created. One box: what
// needs looking after. The subscription is implied when the human has one,
// and the designer model is chosen automatically.
func (w *Web) startStewardConversation(r *http.Request, p Page) error {
	s := w.Store
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if !boundedText(prompt, 6000) {
		return fmt.Errorf("describe what needs looking after")
	}
	if err := s.checkOwnershipText(p.Org.ID, prompt); err != nil {
		return err
	}
	account, err := w.impliedAccount(p, r.FormValue("account"))
	if err != nil {
		return err
	}
	model := r.FormValue("model")
	if model == "" {
		model, err = w.designerModel(p.Org.ID, account)
		if err != nil {
			return err
		}
	}
	if Family(model) == "" {
		return fmt.Errorf("choose a recognized designer model")
	}
	guide := Agent{ID: ID(), Org: p.Org.ID, Name: "Steward designer", Provider: providerName(account.Provider), Model: model, Category: "supervision", Authority: "observe", Description: "Help the human define one steward: a permanent agent that owns a domain, remembers facts, runs routines and raises signals. Interview briefly, then propose the agent, charter, connections, repositories and routines for approval. Do not create anything yourself."}
	task := Assignment{ID: ID(), Org: p.Org.ID, Owner: guide.ID, Creator: p.User.ID, Account: account.ID, Kind: "proposal", StewardCreation: true, Title: "Ask for a steward", Prompt: prompt, Authority: "observe", ConstrainTools: true}
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

// impliedAccount picks the human's subscription without asking when there is
// only one to pick.
func (w *Web) impliedAccount(p Page, chosen string) (Account, error) {
	mine := []Account{}
	for _, a := range list[Account](w.Store, "account", "") {
		if a.User == p.User.ID {
			mine = append(mine, a)
		}
	}
	if chosen != "" {
		for _, a := range mine {
			if a.ID == chosen {
				return a, nil
			}
		}
		return Account{}, fmt.Errorf("choose your own subscription")
	}
	if len(mine) == 1 {
		return mine[0], nil
	}
	if len(mine) == 0 {
		return Account{}, fmt.Errorf("connect a subscription first")
	}
	return Account{}, fmt.Errorf("choose which subscription should pay for this")
}

// designerModel: the organization's supervision default when it runs on this
// subscription's provider, otherwise the first model the subscription offers.
func (w *Web) designerModel(org string, account Account) (string, error) {
	var def CategoryDefault
	if w.Store.Get("default:"+org+":supervision", &def) == nil {
		var a Agent
		if w.Store.Get(def.Agent, &a) == nil && providerName(a.Provider) == providerName(account.Provider) && Family(a.Model) != "" {
			return a.Model, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	models, err := w.Engine.Models(ctx, account)
	if err != nil || len(models) == 0 {
		return "", fmt.Errorf("no model is available on this subscription yet; check Connections")
	}
	for _, m := range models {
		if Family(m.ID) != "" {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("this subscription offers no recognized model family")
}

func (e *Engine) stewardProposalTool(original Run) copilot.Tool {
	return copilot.DefineTool("adc_propose_steward", "Propose one steward for human approval. Either Agent (an existing permanent agent ID to become the steward) or Name, Description and Model for a new agent. Tools are connection IDs from org_connections; Repositories are identities like github.com/owner/repo; Charter says what the steward looks after and its boundaries; Routines are optional recurring checks {Title, Brief, Cadence {Frequency interval|daily|weekly, IntervalMinutes, Timezone, At, Weekday}}; CompletionMode routine (default) or reviewed. Use Replaces with the pending decision ID to revise. Approval creates everything at once.", func(p stewardProposalInput, _ copilot.ToolInvocation) (any, error) {
		s := e.Store
		s.mu.Lock()
		defer s.mu.Unlock()
		var r Run
		var task Assignment
		if s.Get(original.ID, &r) != nil || r.State != "running" || r.Superseded || s.Get(r.Task, &task) != nil || !task.StewardCreation || task.Kind != "proposal" || task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
			return nil, fmt.Errorf("active steward conversation required")
		}
		var account Account
		_ = s.Get(task.Account, &account)
		agent, _, routines, err := s.prepareProposedSteward(r.Org, p.StewardSpec, account)
		if err != nil {
			return nil, err
		}
		summary := []string{"Charter: " + strings.TrimSpace(p.Charter)}
		if len(routines) > 0 {
			lines := []string{}
			for _, routine := range routines {
				lines = append(lines, "- "+routine.Title+" ("+routine.Cadence.String()+")")
			}
			summary = append(summary, "Routines:\n"+strings.Join(lines, "\n"))
		}
		if len(p.Repositories) > 0 {
			summary = append(summary, "Repositories: "+strings.Join(p.Repositories, ", "))
		}
		verb := "Create"
		if p.Agent != "" {
			verb = "Make " + agent.Name + " a steward"
		} else {
			verb = "Create the steward " + agent.Name + " (" + agent.Model + ")"
		}
		brief := verb + ". Approval creates the agent, its charter and " + fmt.Sprint(len(routines)) + " routine(s); it grants only the listed connections."
		return e.submitDecision(r, decisionInput{Kind: "steward-proposal", Question: strings.Join(summary, "\n\n"), Brief: brief, Replaces: p.Replaces, ProposedSteward: &p.StewardSpec})
	})
}
