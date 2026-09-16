package adc

import "fmt"

type statusInput struct {
	View   string `json:"View,omitempty"`
	ID     string `json:"ID,omitempty"`
	Offset int    `json:"Offset,omitempty"`
}

func (e *Engine) statusView(r Run, p statusInput) (any, error) {
	s := e.Store
	var task Assignment
	_ = s.Get(r.Task, &task)
	switch p.View {
	case "stewards":
		items := []map[string]any{}
		for _, v := range s.stewards(r.Org) {
			var a Agent
			_ = s.Get(v.Agent, &a)
			open := 0
			for _, sig := range s.stewardSignals(v.Agent) {
				if sig.State != "resolved" {
					open++
				}
			}
			items = append(items, map[string]any{"agent": v.Agent, "name": a.Name, "charter": clipped(v.Charter, 600), "completion_mode": v.CompletionMode, "facts": len(s.facts(v.Agent)), "open_signals": open, "repositories": v.Repositories})
		}
		start, end := statusPage(p.Offset, len(items))
		return map[string]any{"items": items[start:end], "total": len(items), "next_offset": end}, nil
	case "coordination":
		return e.coordinationView(r, p.Offset), nil
	case "step":
		for _, step := range e.inspectPlan(s.taskPlan(r.Task)).Steps {
			if step.Key == p.ID {
				return step, nil
			}
		}
		return nil, fmt.Errorf("unknown step key in this assignment")
	case "decisions":
		decisions := taskDecisions(s, r.Task)
		if p.ID != "" {
			for _, d := range decisions {
				if d.ID == p.ID {
					return d, nil
				}
			}
			return nil, fmt.Errorf("decision unavailable in this assignment")
		}
		start, end := statusPage(p.Offset, len(decisions))
		items := []map[string]any{}
		for _, d := range decisions[start:end] {
			items = append(items, map[string]any{"id": d.ID, "run": d.Run, "state": d.State, "brief": clipped(d.BriefText(), 500), "answer": clipped(d.Answer, 700), "outcome": d.Outcome, "resolved_at": d.ResolvedAt})
		}
		return map[string]any{"items": items, "total": len(decisions), "next_offset": end, "guidance": "Use Offset=next_offset for further pages, or ID for the exact decision. Resolved answers remain authoritative within their scope; do not repeat unchanged approvals."}, nil
	case "run":
		var target Run
		if s.Get(p.ID, &target) != nil || target.Org != r.Org || target.Task != r.Task {
			return nil, fmt.Errorf("run unavailable in this assignment")
		}
		return target, nil
	case "review":
		if p.ID != "" {
			for _, v := range taskReviews(s, r.Task) {
				if v.ID == p.ID {
					return v, nil
				}
			}
			return nil, fmt.Errorf("review unavailable in this assignment")
		}
		return e.reviewerBrief(r), nil
	case "connections":
		return connectionAccess(s, r), nil
	case "proposals":
		proposals := list[WorkProposal](s, "proposal", r.Org)
		out := []map[string]any{}
		for _, v := range proposals {
			out = append(out, map[string]any{"id": v.ID, "title": v.Title, "state": v.State, "scope": clipped(v.Scope, 1200), "evidence": clipped(v.Evidence, 600), "revision": v.Revision, "steward": v.Steward})
			if len(out) == 24 {
				break
			}
		}
		return map[string]any{"items": out, "total": len(proposals), "guidance": "Use the proposal UI for the full conversation and retained decisions."}, nil
	case "documents":
		return documentCatalog(s, r.Org), nil
	case "summary":
		runs := []map[string]any{}
		for _, v := range taskRuns(s, r.Task) {
			runs = append(runs, map[string]any{"id": v.ID, "agent": v.Agent, "title": v.Title, "state": v.State, "review_of": v.ReviewOf, "model": v.Model, "result": clipped(v.Result, 500), "error": clipped(v.Error, 350)})
		}
		decisions := []map[string]any{}
		for _, d := range taskDecisions(s, r.Task) {
			if d.State == "pending" {
				decisions = append(decisions, map[string]any{"id": d.ID, "brief": d.BriefText(), "state": d.State})
			}
		}
		return map[string]any{"task": task.ID, "state": task.State, "completion_policy": completionPolicy(task), "runs": runs, "review_needed": e.reviewNeeds(r.Task), "pending_decisions": decisions, "completion_evidence": e.completionEvidence(r), "guidance": "For exact run results use View run with ID; use review, connections, proposals or documents for focused evidence. Omit View for the legacy full snapshot."}, nil
	default:
		return nil, fmt.Errorf("View must be stewards, coordination, step (with ID), decisions (optional ID/Offset), summary, run (with ID), review (optional ID for one exact review), connections, proposals or documents; omit for the full snapshot")
	}
}
