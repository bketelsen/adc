package adc

import (
	"sort"
	"time"
)

type OwnershipBriefing struct {
	Briefs  []SupervisorBrief
	Areas   []Area
	Signals []OwnershipSignal
	More    int
}
type OwnershipSignal struct{ Title, State, Detail, URL string }

// Rendering is a pure read. Current record state always precedes older prose.
func (e *Engine) ownershipBriefing(org string) OwnershipBriefing {
	b := OwnershipBriefing{Areas: list[Area](e.Store, "area", org)}
	for _, o := range list[Obligation](e.Store, "obligation", org) {
		if o.State == "resolved" || o.State == "cancelled" {
			continue
		}
		state := o.State
		if due, err := time.Parse(time.RFC3339Nano, o.Due); err == nil && due.Before(time.Now()) && state == "scheduled" {
			state = "overdue"
		}
		detail := o.Note
		if state == "scheduled" {
			detail = "Next verification: " + o.Due
		}
		if state == "verifying" {
			detail = "Verification is being handled"
			var t Assignment
			if e.Store.Get(o.Task, &t) == nil && (t.State == "paused" || t.State == "needs input") {
				state = "blocked"
				detail = "Verification assignment is " + t.State
			}
		}
		b.Signals = append(b.Signals, OwnershipSignal{o.Outcome, state, detail, "/areas?org=" + org + "#area-" + o.Area})
	}
	for _, q := range list[OwnerRequest](e.Store, "owner-request", org) {
		if q.State == "blocked" {
			b.Signals = append(b.Signals, OwnershipSignal{q.Subject, "blocked", q.Response, "/coordination?org=" + org})
		}
	}
	for _, t := range list[Assignment](e.Store, "assignment", org) {
		if t.Attention != nil && t.State != "ready" && t.State != "cancelled" {
			detail := attentionSummary(t.Attention)
			if t.State == "paused" {
				detail = "Assessment stopped; retained observations need reassessment. Future approved checks remain scheduled."
			}
			b.Signals = append(b.Signals, OwnershipSignal{t.Title, t.State, detail, "/task?org=" + org + "&id=" + t.ID})
		}
	}
	sort.SliceStable(b.Signals, func(i, j int) bool {
		return attentionSignalPriority(b.Signals[i].State) < attentionSignalPriority(b.Signals[j].State)
	})
	if len(b.Signals) > 8 {
		b.More = len(b.Signals) - 8
		b.Signals = b.Signals[:8]
	}
	seen := map[string]bool{}
	briefs := list[SupervisorBrief](e.Store, "supervisor-brief", org)
	sort.SliceStable(briefs, func(i, j int) bool { return briefs[i].Created > briefs[j].Created })
	for _, v := range briefs {
		var t Assignment
		var r Run
		if e.Store.Get(v.Task, &t) != nil || t.State != "ready" || e.Store.Get(v.Run, &r) != nil || r.State != "complete" || r.Superseded || !e.hasCurrentReview(r, taskReviews(e.Store, t.ID)) {
			continue
		}
		key := t.Schedule
		if key == "" {
			key = t.ID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		b.Briefs = append(b.Briefs, v)
		if len(b.Briefs) == 3 {
			break
		}
	}
	return b
}
func attentionSignalPriority(state string) int {
	switch state {
	case "blocked", "paused", "overdue", "needs input":
		return 0
	case "queued", "verifying", "running":
		return 1
	default:
		return 2
	}
}

// Task drilldown shows provisional claims as such until their current revision passes.
type AssessmentEntry struct {
	AreaAssessment
	Reviewed bool
}
type BriefEntry struct {
	SupervisorBrief
	Reviewed bool
}

func (w *Web) populateAssessment(p *Page) {
	p.Assessments = nil
	p.AttentionBriefs = nil
	if p.Task.Attention == nil {
		return
	}
	for _, v := range list[AreaAssessment](w.Store, "area-assessment", p.Task.Org) {
		if v.Task == p.Task.ID {
			var r Run
			_ = w.Store.Get(v.Run, &r)
			p.Assessments = append(p.Assessments, AssessmentEntry{v, r.State == "complete" && !r.Superseded && w.Engine.hasCurrentReview(r, taskReviews(w.Store, p.Task.ID))})
		}
	}
	for _, v := range list[SupervisorBrief](w.Store, "supervisor-brief", p.Task.Org) {
		if v.Task == p.Task.ID {
			var r Run
			_ = w.Store.Get(v.Run, &r)
			p.AttentionBriefs = append(p.AttentionBriefs, BriefEntry{v, r.State == "complete" && !r.Superseded && w.Engine.hasCurrentReview(r, taskReviews(w.Store, p.Task.ID))})
		}
	}
}
