package adc

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	datastar "github.com/starfederation/datastar-go/datastar"
)

// StewardView is one steward rendered on the Stewards page.
type StewardView struct {
	Steward     Steward
	Agent       Agent
	Facts       []Fact
	Journal     []JournalEntry
	Signals     []Signal
	Routines    []StandingSchedule
	Tasks       []Assignment
	Connections []Connection
}

func (w *Web) stewardViews(org string) []StewardView {
	s := w.Store
	views := []StewardView{}
	schedules := list[StandingSchedule](s, "schedule", org)
	tasks := list[Assignment](s, "assignment", org)
	for _, v := range s.stewards(org) {
		view := StewardView{Steward: v, Facts: s.facts(v.Agent), Journal: s.journal(v.Agent, 15), Signals: s.stewardSignals(v.Agent)}
		_ = s.Get(v.Agent, &view.Agent)
		for _, id := range view.Agent.Tools {
			var c Connection
			if s.Get(id, &c) == nil {
				view.Connections = append(view.Connections, c)
			}
		}
		for _, schedule := range schedules {
			if schedule.Template.Owner == v.Agent {
				view.Routines = append(view.Routines, schedule)
			}
		}
		for _, t := range tasks {
			if (t.Steward == v.Agent || t.Owner == v.Agent) && t.Kind != "proposal" && len(view.Tasks) < 6 {
				view.Tasks = append(view.Tasks, t)
			}
		}
		views = append(views, view)
	}
	return views
}

// Human actions on a steward. Caller holds Store.mu.
func (w *Web) stewardAction(r *http.Request, p Page) error {
	s := w.Store
	v, ok := s.steward(r.FormValue("agent"))
	if !ok || v.Org != p.Org.ID {
		return fmt.Errorf("steward unavailable")
	}
	rev, err := strconv.Atoi(r.FormValue("revision"))
	if err != nil {
		return fmt.Errorf("steward revision required")
	}
	switch r.FormValue("action") {
	case "charter":
		if rev != v.Revision {
			return fmt.Errorf("steward changed; reload before editing")
		}
		v.Charter = r.FormValue("charter")
		v.Repositories = strings.Split(r.FormValue("repositories"), "\n")
		if _, present := r.Form["completion_mode"]; present {
			v.CompletionMode = r.FormValue("completion_mode")
		}
		_, err = s.saveSteward(v, rev, "human:"+p.User.ID)
		return err
	case "learn":
		if rev != v.Revision {
			return fmt.Errorf("steward changed; reload before starting discovery")
		}
		t := Assignment{ID: ID(), Creator: p.User.ID, Account: r.FormValue("account"), ExtraAccount: r.FormValue("extra_account"), Prompt: r.FormValue("brief"), Execution: r.FormValue("execution")}
		writes, err := w.Engine.discoveryWrites(v, t)
		if err != nil {
			return err
		}
		if err = s.Batch(writes...); err != nil {
			return err
		}
		r.Form.Set("return", "/task?org="+v.Org+"&id="+t.ID)
		return nil
	case "journal":
		if !boundedText(r.FormValue("text"), journalTextLimit) {
			return fmt.Errorf("write a note first")
		}
		_, err = s.journalEntry(v, "", "", p.User.Name+": "+strings.TrimSpace(r.FormValue("text")), "")
		return err
	}
	return fmt.Errorf("unknown steward action")
}

// discoveryWrites starts an ordinary read-only assignment asking a steward to
// learn its domain and record what it finds as facts and a journal entry.
func (e *Engine) discoveryWrites(v Steward, t Assignment) ([]Write, error) {
	for _, old := range list[Assignment](e.Store, "assignment", v.Org) {
		if old.Steward == v.Agent && strings.HasPrefix(old.Title, "Learn: ") && old.State != "ready" && old.State != "cancelled" {
			return nil, fmt.Errorf("a discovery is already running: /task?org=%s&id=%s", v.Org, old.ID)
		}
	}
	if len(t.Prompt) > 6000 {
		return nil, fmt.Errorf("keep the discovery brief within 6000 characters")
	}
	if err := e.Store.checkOwnershipText(v.Org, t.Prompt); err != nil {
		return nil, err
	}
	var owner Agent
	if e.Store.Get(v.Agent, &owner) != nil {
		return nil, fmt.Errorf("steward agent unavailable")
	}
	t.Org, t.Owner, t.Steward, t.Authority, t.Publication = v.Org, v.Agent, v.Agent, "observe", false
	t.Title = "Learn: " + owner.Name
	t.ConstrainTools, t.ConstrainCapabilities = true, true
	t.Tools = append([]string{}, owner.Tools...)
	for _, grant := range e.Store.initialCapabilities(v.Org, t.Tools) {
		if grant.Class == "read" && grant.Operation == "" && grant.Binding == nil {
			t.Capabilities = append(t.Capabilities, grant)
		}
	}
	t.Completion = selectedCompletion("routine")
	brief := strings.TrimSpace(t.Prompt)
	t.Prompt = "Learn your domain. Inspect your connections and repositories read-only, guided by your charter. Record what stays true as facts with adc_remember (with sources and observation times), write a journal entry summarising what you found and what remains unknown, and raise a signal for anything a human should act on. Do not change infrastructure, repositories or public documents; propose standing checks only with adc_propose_work."
	if brief != "" {
		t.Prompt += "\nHuman brief: " + brief
	}
	return e.assignmentWrites(t)
}

func (w *Web) liveStewards(rw http.ResponseWriter, r *http.Request, p Page) {
	sse := datastar.NewSSE(rw, r)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	last := ""
	for {
		if !w.member(p.User.ID, p.Org.ID) {
			return
		}
		p.Stewards = w.stewardViews(p.Org.ID)
		var b bytes.Buffer
		if w.templates.ExecuteTemplate(&b, "steward-list", p) != nil {
			return
		}
		current := b.String()
		hash := digest(current)
		if hash != last {
			if sse.PatchElements(current) != nil {
				return
			}
			last = hash
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}
