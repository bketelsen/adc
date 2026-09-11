package adc

import "strings"

type activityEvent struct {
	Event
	RunTitle string
}

type activityItem struct {
	ID       int64
	Event    *activityEvent
	Tools    []activityEvent
	Failures int
}

// Keep meaningful conversation in order, folding tool bursts around it.
// Empty provider messages and usage records must not split those bursts.
func activityItems(events []Event, runs []Run) []activityItem {
	names := map[string]string{}
	for _, r := range runs {
		names[r.ID] = r.Title
	}
	var items []activityItem
	for _, e := range events {
		if e.Kind == "usage" || strings.TrimSpace(e.Text) == "" {
			continue
		}
		view := activityEvent{Event: e, RunTitle: names[e.Run]}
		if e.Kind != "tool" && e.Kind != "tool-result" {
			items = append(items, activityItem{ID: e.ID, Event: &view})
			continue
		}
		if len(items) == 0 || items[len(items)-1].Event != nil {
			items = append(items, activityItem{ID: e.ID})
		}
		group := &items[len(items)-1]
		group.Tools = append(group.Tools, view)
		if e.Kind == "tool-result" && strings.HasSuffix(e.Text, " · failed") {
			group.Failures++
		}
	}
	return items
}
