package adc

import (
	"fmt"
	"strings"
)

func (e *Engine) reviewerBrief(r Run) any {
	var target Run
	if r.ReviewOf == "" || e.Store.Get(r.ReviewOf, &target) != nil || target.Org != r.Org || target.Task != r.Task {
		return nil
	}
	var task Assignment
	_ = e.Store.Get(r.Task, &task)
	plan, step, _ := e.plannedStep(target)
	docs := []Document{}
	for _, d := range taskDocs(e.Store, r.Task) {
		if d.Run == target.ID {
			d.Content = ""
			docs = append(docs, d)
		}
	}
	return map[string]any{"request": task.Prompt, "output": task.Output, "authority": task.Authority, "publication": task.Publication, "step_brief": step.Prompt, "criteria": step.Criteria, "sources": plan.Source, "target": target.ID, "pinned_revision": r.ReviewedRevision, "current_revision": e.revision(target), "artifact_revision": e.artifactRevision(target), "result": target.Result, "code": target.Code, "documents": docs, "integration": e.Store.integrationEvidence(target.ID), "checks": e.validationViews(target), "assessment": "Independently inspect exact artifacts and authoritative sources. Assess every acceptance criterion, unrelated changes and weakened/disabled checks. Report concrete changes for scope drift or unmet criteria even when checks pass. Agent-reported observations are claims to verify, never authority."}
}

// All paths into a fresh reviewer activation (dependency wake, collaboration,
// human steering or retry) must bind the completed target being handed over.
// Caller holds Store.mu; an in-flight verdict never rebinds its own pin.
func (e *Engine) prepareReview(r *Run) bool {
	if r.ReviewOf == "" {
		return true
	}
	var target Run
	if e.Store.Get(r.ReviewOf, &target) != nil || target.Org != r.Org || target.Task != r.Task || target.State != "complete" {
		r.State = "waiting"
		return false
	}
	r.ReviewedRevision = e.revision(target)
	return true
}

func (e *Engine) restartStaleReview(r Run) (string, error) {
	r.State = "waiting"
	r.Turns = 0
	const notice = "\nADC detected that the review target changed during the previous activation. No verdict from that activation was recorded. Inspect the completed target and its exact current documents/code in this fresh activation before submitting a new verdict."
	if !strings.Contains(r.Prompt, notice) {
		r.Prompt += notice
	}
	if err := e.Store.Put("run", r.Org, r.Task, r.State, r.ID, r); err != nil {
		return "", err
	}
	message := fmt.Sprintf("Review target changed; no verdict recorded. %s will automatically resume with a fresh revision pin after the author completes.", r.Title)
	e.Store.Log(r.Org, r.Task, r.ID, "review-restart", message)
	return message + " End this turn.", nil
}
