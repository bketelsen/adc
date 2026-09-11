package adc

import (
	"fmt"
	"strings"
)

// Caller holds Store.mu. A blocker is an unmet prerequisite, not a deliverable
// awaiting review. Existing artifacts remain available for eventual recovery.
func (e *Engine) blockWork(r Run, reason, needed string) (string, error) {
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(needed) == "" {
		return "", fmt.Errorf("describe the observed blocker in Reason and the prerequisite or decision in Needed")
	}
	evidence := reason + "\nNeeded to continue: " + needed
	r.Result = evidence
	writes := e.escalationWrites(&r, evidence)
	if err := e.Store.Batch(append(writes, Write{"run", r.Org, r.Task, r.State, r.ID, r})...); err != nil {
		return "", err
	}
	e.Store.Log(r.Org, r.Task, r.ID, "blocked", evidence)
	e.refreshTaskStates()
	return "Blocked outcome recorded; this is not completion and requires no review of the failure report. Responsibility returned to the supervisor, or a human decision if this is the supervisor. End your turn.", nil
}
