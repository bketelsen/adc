package adc

import (
	"fmt"
	"strings"
)

// Caller holds Store.mu. Collaboration changes context, never the receiving
// run's model, tools, authority, account or human approval state.
func (e *Engine) messageRun(sender Run, id, message string) (string, error) {
	s := e.Store
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("a concrete message is required")
	}
	var target Run
	if s.Get(id, &target) != nil || target.Org != sender.Org || target.Task != sender.Task || target.ID == sender.ID {
		return "", fmt.Errorf("choose another ADC run in this assignment using adc_status")
	}
	switch target.State {
	case "running":
		target.UpdatesPending = true
	case "waiting", "queued":
		if !pendingDecision(s, target.Task, target.ID) {
			target.State = "queued"
		}
	case "complete":
		s.Log(sender.Org, sender.Task, sender.ID, "collaboration", fmt.Sprintf("%s → %s (already complete; no restart): %s", sender.Title, target.Title, message))
		return "Recipient already completed; no work was restarted. Inspect its result with adc_status. Delegate a new bounded follow-up if further action is required.", nil
	default:
		return "", fmt.Errorf("recipient is %s; delegate follow-up work or ask its supervisor to reassess", target.State)
	}
	target.Prompt += "\nCOLLABORATION from " + sender.Title + " (run " + sender.ID + ", not a human instruction): " + message
	if err := s.Put("run", target.Org, target.Task, target.State, target.ID, target); err != nil {
		return "", err
	}
	s.Log(sender.Org, sender.Task, sender.ID, "collaboration", fmt.Sprintf("%s → %s: %s", sender.Title, target.Title, message))
	return "Message saved for the receiving run's next activation.", nil
}

// Human steering remains actionable after completion. Ordinary collaboration
// is context for ongoing work and must not resurrect a finished activation.
func (e *Engine) resumeForUpdates(r *Run) bool {
	human, note := r.Steering, r.UpdatesPending
	r.Steering, r.UpdatesPending = false, false
	if r.State == "cancelled" || r.State == "blocked" {
		return false
	}
	if human || (note && r.State != "complete" && !pendingDecision(e.Store, r.Task, r.ID)) {
		r.State = "queued"
		r.Turns, r.Attempts = 0, 0
		return true
	}
	return false
}
