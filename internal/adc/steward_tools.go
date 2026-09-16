package adc

import (
	"fmt"

	copilot "github.com/github/copilot-sdk/go"
)

// stewardTools are offered only to runs whose agent is a steward and that are
// not reviewing someone else's work. They write the steward's own memory.
func (e *Engine) stewardTools(original Run) []copilot.Tool {
	s := e.Store
	if _, ok := s.steward(original.Agent); !ok || original.ReviewOf != "" {
		return nil
	}
	active := func() (Run, Steward, error) {
		var r Run
		var t Assignment
		if s.Get(original.ID, &r) != nil || r.State != "running" || r.Superseded || s.Get(r.Task, &t) != nil || t.State == "paused" || t.State == "cancelled" || t.State == "ready" {
			return r, Steward{}, fmt.Errorf("run is not active")
		}
		v, ok := s.steward(r.Agent)
		if !ok {
			return r, Steward{}, fmt.Errorf("this agent is not a steward")
		}
		return r, v, nil
	}
	return []copilot.Tool{
		copilot.DefineTool("adc_remember", "Set or replace one durable fact about your domain: a short Key (e.g. pool-layout, backup-target, disk3-replaced), its current Value, a credential-free Source and optional past RFC3339 ObservedAt. An empty Value retracts the fact. Facts are for what stays true; put events in adc_journal.", func(p struct {
			Key, Value, Source, ObservedAt string
		}, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r, v, err := active()
			if err != nil {
				return nil, err
			}
			return s.rememberFact(v, "run:"+r.ID, p.Key, p.Value, p.Source, p.ObservedAt)
		}),
		copilot.DefineTool("adc_journal", "Append one dated entry to your journal: what happened, what you found, what you changed. Text up to 2000 characters and an optional Source reference. Entries are permanent and searchable with adc_recall.", func(p struct {
			Text, Source string
		}, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r, v, err := active()
			if err != nil {
				return nil, err
			}
			return s.journalEntry(v, r.Task, r.ID, p.Text, p.Source)
		}),
		copilot.DefineTool("adc_recall", "Search your facts and journal. Query is matched case-insensitively against fact keys and values and journal text; Limit caps journal hits at 20. An empty Query lists everything recent.", func(p struct {
			Query string
			Limit int
		}, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			_, v, err := active()
			if err != nil {
				return nil, err
			}
			return s.recall(v.Agent, p.Query, p.Limit), nil
		}),
		copilot.DefineTool("adc_signal", "Tell the humans something worth knowing. Key is stable per condition (e.g. nvme-capacity) so repeats update one signal; Severity is info, warning or urgent; Message says what and why in one or two sentences; Evidence and Suggestion are optional. Clear=true resolves a condition that no longer holds. Signals appear on the home page until acknowledged.", func(p signalInput, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r, v, err := active()
			if err != nil {
				return nil, err
			}
			return s.raiseSignal(v, r.Task, r.ID, p)
		}),
	}
}
