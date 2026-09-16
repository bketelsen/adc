package adc

// activationContext is the compact state a model receives at the start of every
// activation. Full run prompts, review findings, document bodies, plan step
// detail and organization-wide proposals stay reachable through adc_status
// views and adc_read_document, so history is fetched when needed rather than
// resent on every turn.
func (e *Engine) activationContext(r Run, t Assignment, models []Model, catalogs map[string][]Model, catalogErrors map[string]string) map[string]any {
	s := e.Store
	runs := taskRuns(s, r.Task)
	ctx := map[string]any{
		"assignment":       t,
		"available_models": models,
		"run":              r,
		"team":             list[Agent](s, "agent", r.Org),
		"plan":             compactPlan(e.inspectPlan(s.taskPlan(r.Task))),
		"runs":             compactRuns(runs),
		"readiness":        taskReadiness(s, r.Task),
		"resources":        taskResources(s, r.Task),
		"documents":        compactDocuments(taskDocs(s, r.Task)),
		"reviews":          compactReviews(taskReviews(s, r.Task), runs),
		"decisions":        compactDecisions(taskDecisions(s, r.Task)),
		"connections":      connectionAccess(s, r),
	}
	if r.Parent == "" {
		// Only the accountable supervisor delegates across subscriptions.
		ctx["available_models_by_provider"] = catalogs
		ctx["provider_catalog_errors"] = catalogErrors
	}
	if t.Kind == "proposal" {
		ctx["proposals"] = compactProposals(list[WorkProposal](s, "proposal", r.Org))
	}
	recent := []ToolTrace{}
	for _, trace := range taskTraces(s, r.Task) {
		if trace.Run == r.ID {
			trace.Arguments = clipped(trace.Arguments, 2000)
			trace.Result = clipped(trace.Result, 2500)
			recent = append(recent, trace)
		}
		if len(recent) >= 8 {
			break
		}
	}
	ctx["recent_tool_evidence"] = recent
	ctx["completion_evidence"] = e.completionEvidence(r)
	owner := e.ownerContext(r)
	delete(owner, "guidance")
	ctx["owner"] = owner
	coordination := e.requestContext(r)
	delete(coordination, "guidance")
	ctx["owner_coordination"] = coordination
	return ctx
}

const activationLeadIn = "Compact current state follows. Full runs, reviews, plan steps, documents and proposals are available through adc_status views and adc_read_document. Continue the outstanding work, checking external outcomes before repeating any interrupted action.\n"

// Superseded and cancelled runs are history; they stay countable and readable
// through adc_status View=run without being resent on every activation.
func compactRuns(runs []Run) map[string]any {
	out := []map[string]any{}
	retired := 0
	for _, v := range runs {
		if v.Superseded || v.State == "cancelled" {
			retired++
			continue
		}
		out = append(out, map[string]any{"id": v.ID, "agent": v.Agent, "title": v.Title, "category": v.Category, "state": v.State, "parent": v.Parent, "review_of": v.ReviewOf, "model": v.Model, "code": v.Code, "result": clipped(v.Result, 500), "error": clipped(v.Error, 350)})
	}
	return map[string]any{"current": out, "retired": retired}
}

// Only the latest review of each live target is current evidence; earlier
// rounds remain available through adc_status View=review with the review ID.
func compactReviews(reviews []Review, runs []Run) []map[string]any {
	live := map[string]bool{}
	for _, v := range runs {
		live[v.ID] = !v.Superseded && v.State != "cancelled"
	}
	latest := map[string]int{}
	for i, v := range reviews {
		if live[v.Target] {
			latest[v.Target] = i
		}
	}
	out := []map[string]any{}
	for i, v := range reviews {
		if latest[v.Target] != i {
			continue
		}
		out = append(out, map[string]any{"id": v.ID, "run": v.Run, "target": v.Target, "model": v.Model, "verdict": v.Verdict, "revision": v.Revision, "stage": v.Stage, "findings": clipped(v.Findings, 1200)})
	}
	return out
}

func compactDocuments(docs []Document) []Document {
	out := append([]Document(nil), docs...)
	for i := range out {
		out[i].Content = ""
	}
	return out
}

func compactDecisions(decisions []Decision) []map[string]any {
	out := []map[string]any{}
	for _, d := range decisions {
		if d.State != "pending" && d.State != "answered" && d.State != "resolved" {
			continue
		}
		out = append(out, map[string]any{"id": d.ID, "run": d.Run, "state": d.State, "kind": d.Kind, "brief": clipped(d.BriefText(), 500), "answer": clipped(d.Answer, 700), "outcome": d.Outcome, "resolved_by": d.ResolvedBy, "resolved_at": d.ResolvedAt})
	}
	return out
}

func compactProposals(proposals []WorkProposal) []map[string]any {
	out := []map[string]any{}
	for _, v := range proposals {
		out = append(out, map[string]any{"id": v.ID, "title": v.Title, "state": v.State, "scope": clipped(v.Scope, 1200), "evidence": clipped(v.Evidence, 600), "revision": v.Revision, "area": v.Area})
		if len(out) == 24 {
			break
		}
	}
	return out
}

// compactPlan keeps the graph and gate state of a plan; step briefs, command
// output, evidence bodies and attempt history come from adc_status View=step.
func compactPlan(p ExecutionPlan) any {
	if p.ID == "" {
		return nil
	}
	steps := []map[string]any{}
	for _, step := range p.Steps {
		evidence := map[string]bool{}
		for _, v := range step.Evidence {
			if !v.Withdrawn && v.Kind != "" {
				evidence[v.Requirement] = true
			}
		}
		requirements := []map[string]any{}
		for _, req := range step.Requirements {
			requirements = append(requirements, map[string]any{"key": req.Key, "kind": req.Kind, "satisfied": evidence[req.Key]})
		}
		checks := []map[string]any{}
		for _, check := range step.Validation {
			checks = append(checks, map[string]any{"key": check.Key, "kind": check.Kind, "state": check.State})
		}
		pending := 0
		for _, d := range step.Decisions {
			if d.State == "pending" {
				pending++
			}
		}
		steps = append(steps, map[string]any{"key": step.Key, "title": step.Title, "state": step.State, "reason": clipped(step.Reason, 400), "owner": step.Worker.Name, "reviewer": step.Verifier.Name, "run": step.Run, "review": step.Review, "depends_on": step.DependsOn, "omitted": step.Omission != nil, "requirements": requirements, "checks": checks, "waits": len(step.Waits), "pending_decisions": pending, "attempts": len(step.Attempts)})
	}
	return map[string]any{"id": p.ID, "title": p.Title, "state": p.State, "revision": p.Revision, "steps": steps}
}
