package adc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

type AgentRunView struct {
	Agent Agent
	Runs  []Run
}

// Filter before paginating: a busy sibling must not crowd out this run's history.
func (s *Store) runEvents(org, task, run string, before int64) ([]Event, int64) {
	rows, err := s.db.Query(`SELECT id,org,task,run,kind,text,at FROM (SELECT * FROM events WHERE org=? AND task=? AND run=? AND kind!='usage' AND trim(text)!='' AND (?=0 OR id<?) ORDER BY id DESC LIMIT 250) ORDER BY id`, org, task, run, before, before)
	if err != nil {
		return nil, 0
	}
	events := []Event{}
	for rows.Next() {
		var event Event
		if rows.Scan(&event.ID, &event.Org, &event.Task, &event.Run, &event.Kind, &event.Text, &event.At) == nil {
			events = append(events, event)
		}
	}
	rows.Close()
	var more int
	if len(events) > 0 && s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE org=? AND task=? AND run=? AND kind!='usage' AND trim(text)!='' AND id<?)`, org, task, run, events[0].ID).Scan(&more) == nil && more > 0 {
		return events, events[0].ID
	}
	return events, 0
}

func (w *Web) populateRun(p *Page, id string) bool {
	if w.Store.Get(id, &p.Run) != nil || p.Run.Org != p.Org.ID {
		return false
	}
	if w.Store.Get(p.Run.Task, &p.Task) != nil || p.Task.Org != p.Org.ID {
		return false
	}
	p.Runs = []Run{p.Run}
	p.Agents = append(list[Agent](w.Store, "agent", p.Org.ID), list[Agent](w.Store, "guide", p.Org.ID)...)
	return true
}
func (w *Web) populateTranscript(p *Page) {
	p.Events, p.Older = w.Store.runEvents(p.Org.ID, p.Task.ID, p.Run.ID, p.Before)
	p.Traces = w.Store.runTraces(p.Org.ID, p.Task.ID, p.Run.ID)
}
func (s *Store) runTraces(org, task, run string) []ToolTrace {
	rows, err := s.db.Query(`SELECT data FROM records WHERE kind='tooltrace' AND org=? AND parent=? AND json_extract(data,'$.Run')=? ORDER BY updated DESC,id DESC LIMIT 100`, org, task, run)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var traces []ToolTrace
	for rows.Next() {
		var data []byte
		var trace ToolTrace
		if rows.Scan(&data) == nil && json.Unmarshal(data, &trace) == nil && trace.Org == org && trace.Task == task && trace.Run == run {
			traces = append(traces, trace)
		}
	}
	return traces
}
func (w *Web) populateActiveRuns(p *Page) {
	p.ActiveRuns = map[string][]Run{}
	tasks := map[string]Assignment{}
	for _, task := range list[Assignment](w.Store, "assignment", p.Org.ID) {
		tasks[task.ID] = task
	}
	for _, run := range list[Run](w.Store, "run", p.Org.ID) {
		task, ok := tasks[run.Task]
		if !ok || task.State == "ready" || task.State == "cancelled" || task.State == "paused" {
			continue
		}
		switch run.State {
		case "running", "queued", "waiting", "blocked":
			p.ActiveRuns[run.Agent] = append(p.ActiveRuns[run.Agent], run)
		}
	}
	for _, runs := range p.ActiveRuns {
		sort.SliceStable(runs, func(i, j int) bool {
			if (runs[i].State == "running") != (runs[j].State == "running") {
				return runs[i].State == "running"
			}
			return runs[i].Created < runs[j].Created
		})
	}
}

// These streams read persisted evidence only; opening a transcript never starts
// a provider session or changes the run. Recheck membership/session each tick.
func (w *Web) liveTranscript(rw http.ResponseWriter, r *http.Request, p Page) {
	w.streamRunViews(rw, r, p, func(p *Page) []string {
		if !w.populateRun(p, p.Run.ID) {
			return nil
		}
		w.populateTranscript(p)
		return []string{"run-header", "timeline", "toollist"}
	})
}
func (w *Web) liveTeamRuns(rw http.ResponseWriter, r *http.Request, p Page) {
	sse := datastar.NewSSE(rw, r)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	last := map[string]string{}
	for {
		user, _ := w.user(r)
		if user.ID != p.User.ID || !w.member(user.ID, p.Org.ID) {
			return
		}
		w.populateActiveRuns(&p)
		for _, agent := range p.Agents {
			var b bytes.Buffer
			if w.templates.ExecuteTemplate(&b, "agent-active-runs", AgentRunView{Agent: agent, Runs: p.ActiveRuns[agent.ID]}) != nil {
				return
			}
			value := b.String()
			fingerprint := digest(value)
			if last[agent.ID] != fingerprint {
				if sse.PatchElements(value) != nil {
					return
				}
				last[agent.ID] = fingerprint
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}
func (w *Web) streamRunViews(rw http.ResponseWriter, r *http.Request, p Page, populate func(*Page) []string) {
	sse := datastar.NewSSE(rw, r)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	last := map[string]string{}
	for {
		user, _ := w.user(r)
		if user.ID != p.User.ID || !w.member(user.ID, p.Org.ID) {
			return
		}
		names := populate(&p)
		if len(names) == 0 {
			return
		}
		for _, name := range names {
			var b bytes.Buffer
			if w.templates.ExecuteTemplate(&b, name, p) != nil {
				return
			}
			value := b.String()
			fingerprint := digest(value)
			if last[name] != fingerprint {
				if sse.PatchElements(value) != nil {
					return
				}
				last[name] = fingerprint
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}
