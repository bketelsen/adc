package adc

import "fmt"

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
	r.Prompt += "\nADC detected that the review target changed during the previous activation. No verdict from that activation was recorded. Inspect the completed target and its exact current documents/code in this fresh activation before submitting a new verdict."
	if err := e.Store.Put("run", r.Org, r.Task, r.State, r.ID, r); err != nil {
		return "", err
	}
	message := fmt.Sprintf("Review target changed; no verdict recorded. %s will automatically resume with a fresh revision pin after the author completes.", r.Title)
	e.Store.Log(r.Org, r.Task, r.ID, "review-restart", message)
	return message + " End this turn.", nil
}
