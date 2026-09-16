package adc

import "strings"

// A human who types "looks good" under a pending decision has answered it.
// Structured decisions (steward, team, acceptance, action) need an outcome
// the app can act on, so short clear affirmatives approve, clear negatives
// reject, and anything with a condition or a change goes back as refinement
// notes. General decisions take the text itself as the answer.
func plainOutcome(message string, d Decision) string {
	structured := d.ProposedSteward != nil || len(d.Proposal) > 0 || d.Acceptance != nil || d.Action != nil
	if !structured {
		return ""
	}
	text := strings.ToLower(strings.TrimSpace(message))
	text = strings.NewReplacer(",", " ", ";", " ", ":", " ", "!", " ", ".", " ", "?", " ?").Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	words := strings.Fields(text)
	if len(words) == 0 || len(words) > 8 {
		return "refine"
	}
	for _, w := range words {
		switch strings.Trim(w, ".!,;:") {
		case "but", "except", "however", "instead", "unless", "although", "change", "rename", "remove", "add", "also", "not", "don't", "dont", "never":
			return "refine"
		}
	}
	for _, phrase := range []string{"yes", "yep", "yup", "yeah", "ok", "okay", "sure", "approve", "approved", "go ahead", "go for it", "do it", "ship it", "looks good", "look good", "lgtm", "sounds good", "great", "perfect", "agreed", "agree", "fine", "proceed", "make it so", "👍", "this is good", "good", "love it"} {
		if text == phrase || strings.HasPrefix(text, phrase+" ") {
			return "approve"
		}
	}
	for _, phrase := range []string{"no", "nope", "nah", "reject", "rejected", "stop", "cancel", "drop it", "scrap it", "hold", "wait", "not yet", "👎"} {
		if text == phrase || strings.HasPrefix(text, phrase+" ") {
			return "reject"
		}
	}
	return "refine"
}

// pendingRunDecision returns the decision a message to this run would answer.
func pendingRunDecision(s *Store, task, run string) (Decision, bool) {
	var found Decision
	for _, d := range taskDecisions(s, task) {
		if d.State == "pending" && d.Run == run && d.Kind != "permission" {
			found = d
		}
	}
	return found, found.ID != ""
}
