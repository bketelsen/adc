package adc

import (
	"encoding/json"
	"fmt"

	copilot "github.com/github/copilot-sdk/go"
)

// Policies are small named contracts, not arbitrary workflow expressions.
// New work defaults to routine: the accountable owner finishes on registered
// code, saved documents or observed evidence, and the human reviews the
// result. Reviewed is the opt-in contract with mandatory cross-family review.
// Nil on historical work always means reviewed; a stored record never changes
// meaning because the default moved.
type CompletionPolicy struct {
	Mode    string
	Version int
}
type CompletionEvidence struct {
	ID, Org, Task, Run, Summary, Reference, Created string
	Revision                                        int
}

func completionPolicy(t Assignment) CompletionPolicy {
	if t.Completion == nil {
		return CompletionPolicy{Mode: "reviewed", Version: 1}
	}
	return *t.Completion
}

// selectedCompletion turns an explicit form choice into a policy; an empty
// choice leaves the area default (or routine) to snapshotCompletion.
func selectedCompletion(mode string) *CompletionPolicy {
	if mode == "" {
		return nil
	}
	return &CompletionPolicy{Mode: mode, Version: 1}
}
func validCompletionMode(mode string) bool {
	return mode == "" || mode == "reviewed" || mode == "routine"
}
func routineCompletion(t Assignment) bool {
	return completionPolicy(t).Mode == "routine"
}
func (s *Store) snapshotCompletion(t *Assignment) error {
	if t.Completion == nil {
		p := CompletionPolicy{Mode: "routine", Version: 1}
		if t.Steward != "" && t.Schedule == "" {
			v, ok := s.steward(t.Steward)
			if !ok || v.Org != t.Org {
				return fmt.Errorf("choose a steward in this organization")
			}
			if v.CompletionMode != "" {
				p.Mode = v.CompletionMode
			}
		}
		t.Completion = &p
	}
	if !validCompletionMode(t.Completion.Mode) || t.Completion.Mode == "" || t.Completion.Version != 1 {
		return fmt.Errorf("unsupported completion policy")
	}
	return nil
}
func (e *Engine) requiresIndependentReview(t Assignment, r Run) bool {
	if routineCompletion(t) {
		return false
	}
	if len(r.Code) > 0 || e.completionEvidence(r).ID != "" || r.Category == "implementation" {
		return true
	}
	for _, d := range taskDocs(e.Store, t.ID) {
		if d.Run == r.ID {
			return true
		}
	}
	return false
}
func (e *Engine) completionEvidence(r Run) CompletionEvidence {
	var v CompletionEvidence
	_ = e.Store.Get("completion-evidence:"+r.ID, &v)
	return v
}
func (e *Engine) completionEvidenceRevision(r Run) []byte {
	v := e.completionEvidence(r)
	if v.ID == "" {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
func (e *Engine) recordCompletionEvidence(r Run, summary, reference string, revision int) (CompletionEvidence, error) {
	var task Assignment
	_ = e.Store.Get(r.Task, &task)
	if r.Parent == "" && !routineCompletion(task) {
		return CompletionEvidence{}, fmt.Errorf("reviewed work requires a substantive specialist to author evidence for independent review")
	}
	if r.ReviewOf != "" {
		return CompletionEvidence{}, fmt.Errorf("reviewers record their verdict using adc_review")
	}
	if !boundedText(summary, 3000) || !evidenceReference(reference) {
		return CompletionEvidence{}, fmt.Errorf("supply observed evidence Summary (at most 3000 bytes) and one short credential-free Reference, with no URL query or fragment")
	}
	if err := e.Store.checkOwnershipText(r.Org, summary, reference); err != nil {
		return CompletionEvidence{}, err
	}
	old := e.completionEvidence(r)
	if old.Revision != revision {
		return old, fmt.Errorf("evidence changed; inspect revision %d", old.Revision)
	}
	v := CompletionEvidence{ID: "completion-evidence:" + r.ID, Org: r.Org, Task: r.Task, Run: r.ID, Summary: summary, Reference: reference, Created: now(), Revision: revision + 1}
	writes := []Write{{"completion-evidence", r.Org, r.Task, "", v.ID, v}}
	if old.ID != "" {
		writes = append(writes, Write{"completion-evidence-history", old.Org, old.ID, "", ID(), old})
	}
	return v, e.Store.Batch(writes...)
}

// Routine completion needs something concrete on record: registered code, a
// saved document, or observed evidence. A bare completion message is not it.
func (e *Engine) routineEvidenceComplete(t Assignment) bool {
	authored := map[string]bool{}
	for _, d := range taskDocs(e.Store, t.ID) {
		authored[d.Run] = true
	}
	for _, r := range taskRuns(e.Store, t.ID) {
		// The active supervisor may provide its own evidence before finishing.
		if r.Superseded || r.ReviewOf != "" || r.State == "cancelled" || (r.State != "complete" && (r.Parent != "" || r.State != "running")) {
			continue
		}
		if len(r.Code) > 0 || authored[r.ID] || e.completionEvidence(r).ID != "" {
			return true
		}
	}
	return false
}

// Draft PR delivery is the one place routine work still needs an independent
// cross-family review: the exact commit ADC publishes must have been checked
// by a different model family. See github_delivery.go.
func (e *Engine) publicationReviewNeeded(t Assignment, r Run) bool {
	return t.Publication && len(r.Code) > 0 && r.ReviewOf == "" && !r.Superseded && r.State != "cancelled"
}
func completionInstructions(t Assignment) string {
	if routineCompletion(t) {
		text := "\nCOMPLETION POLICY — ROUTINE: Do the work and finish on concrete evidence: registered code (adc_code), a saved document (adc_document) or observed verification (adc_evidence). No independent model review is required for code, documents or plan steps; the human reviews the result. Do small work yourself; delegate when parallelism or a different specialty helps. Optional expert review is available with adc_delegate ReviewOf. A promise, external claim or inability report is not evidence."
		if t.Publication {
			text += " Draft PR publication is the exception: the exact commit needs one passing cross-family review before ADC delivers it, so arrange that review for the run that registered the code."
		}
		return text
	}
	if t.Kind == "proposal" {
		return ""
	}
	return "\nCOMPLETION POLICY — REVIEWED: A human selected mandatory independent review for this work. Every run that registers code, saves a deliverable document or performs implementation needs a passing review from a different model family at its final revision. The accountable supervisor does not finish its own code or documents: delegate finalization to a specialist worker and obtain that worker's independent review. Execution-plan steps name a designated reviewer. After review findings, correct and resubmit autonomously."
}
func (e *Engine) completionTools(original Run) []copilot.Tool {
	var task Assignment
	_ = e.Store.Get(original.Task, &task)
	if original.ReviewOf != "" || (original.Parent == "" && !routineCompletion(task)) {
		return nil
	}
	return []copilot.Tool{copilot.DefineTool("adc_evidence", "Record concise observed evidence of an achieved outcome: Summary (up to 3000 bytes), one credential-free Reference (no URL query or fragment) and Revision (0 initially). This is evidence, not new authority or acceptance of an external claim.", func(p struct {
		Summary, Reference string
		Revision           int
	}, _ copilot.ToolInvocation) (any, error) {
		s := e.Store
		s.mu.Lock()
		defer s.mu.Unlock()
		var r Run
		var t Assignment
		if s.Get(original.ID, &r) != nil || r.State != "running" || r.Superseded || s.Get(r.Task, &t) != nil || t.State == "paused" || t.State == "cancelled" || t.State == "ready" {
			return nil, fmt.Errorf("run is not active")
		}
		return e.recordCompletionEvidence(r, p.Summary, p.Reference, p.Revision)
	})}
}
