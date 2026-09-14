package adc

import "fmt"

type statusInput struct {
	View string `json:"View,omitempty"`
	ID   string `json:"ID,omitempty"`
}

func (e *Engine) statusView(r Run, p statusInput) (any, error) {
	s := e.Store
	var task Assignment
	_ = s.Get(r.Task, &task)
	switch p.View {
	case "run":
		var target Run
		if s.Get(p.ID, &target) != nil || target.Org != r.Org || target.Task != r.Task {
			return nil, fmt.Errorf("run unavailable in this assignment")
		}
		return target, nil
	case "review":
		return e.reviewerBrief(r), nil
	case "assessment":
		return e.assessmentContext(task), nil
	case "connections":
		return connectionAccess(s, r), nil
	case "proposals":
		proposals := list[WorkProposal](s, "proposal", r.Org)
		out := []map[string]any{}
		for _, v := range proposals {
			out = append(out, map[string]any{"id": v.ID, "title": v.Title, "state": v.State, "scope": clipped(v.Scope, 1200), "evidence": clipped(v.Evidence, 600), "revision": v.Revision, "area": v.Area})
			if len(out) == 24 {
				break
			}
		}
		return map[string]any{"items": out, "total": len(proposals), "guidance": "Use the proposal UI for the full conversation and retained decisions; assessment view supplies recent decision notes."}, nil
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
		return map[string]any{"task": task.ID, "state": task.State, "completion_policy": completionPolicy(task), "runs": runs, "review_needed": e.reviewNeeds(r.Task), "pending_decisions": decisions, "assessment_available": task.Attention != nil, "completion_evidence": e.completionEvidence(r), "observation": e.obligationObservation(r), "guidance": "For exact run results use View run with ID; use review, assessment, connections, proposals or documents for focused evidence. Omit View for the legacy full snapshot."}, nil
	default:
		return nil, fmt.Errorf("View must be summary, run (with ID), review, assessment, connections, proposals or documents; omit for the full snapshot")
	}
}
