package adc

import (
	"encoding/json"
	"fmt"

	copilot "github.com/github/copilot-sdk/go"
)

// Policies are small named contracts, not arbitrary workflow expressions.
// Nil on historical work always means reviewed; defaults cannot weaken it.
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
func validCompletionMode(mode string) bool {
	return mode == "" || mode == "reviewed" || mode == "routine"
}
func routineCompletion(t Assignment) bool {
	return completionPolicy(t).Mode == "routine" && t.Attention == nil
}
func (s *Store) snapshotCompletion(t *Assignment) error {
	if t.Completion == nil {
		p := CompletionPolicy{Mode: "reviewed", Version: 1}
		if t.Area != "" && t.Obligation == "" && t.Schedule == "" {
			var a Area
			if s.Get(t.Area, &a) != nil || a.Org != t.Org {
				return fmt.Errorf("select an area in this organization")
			}
			if a.CompletionMode != "" {
				p.Mode = a.CompletionMode
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
	// A routine responsibility never weakens registered code or planned work.
	if len(r.Code) > 0 {
		return true
	}
	if routineCompletion(t) {
		_, step, ok := e.plannedStep(r)
		return ok && step.Key != ""
	}
	if e.completionEvidence(r).ID != "" || r.Category == "implementation" || e.obligationObservation(r).ID != "" || e.hasOwnerDeliverable(r) {
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
func (e *Engine) routineEvidenceComplete(t Assignment) bool {
	for _, r := range taskRuns(e.Store, t.ID) {
		// The active supervisor may provide its own evidence before finishing.
		if r.Superseded || r.ReviewOf != "" || r.State == "cancelled" || (r.State != "complete" && (r.Parent != "" || r.State != "running")) {
			continue
		}
		if e.completionEvidence(r).ID != "" || e.obligationObservation(r).ID != "" {
			return true
		}
	}
	return false
}
func completionInstructions(t Assignment) string {
	if !routineCompletion(t) {
		return ""
	}
	return "\nCOMPLETION POLICY — ROUTINE EVIDENCE: This responsibility was explicitly configured by a human for evidence-based completion. Achieve the requested outcome, record actual observed verification using adc_evidence (or adc_obligation_result for a linked verification), then finish. The accountable owner can perform and verify simple work directly. No separate QA document or mandatory independent review is needed for routine observations, internal notes or acknowledgments. Registered code artifacts and execution-plan steps still require their independent cross-family gates. Optional expert review remains available for uncertainty. A proposed future action, external claim or inability report is not observed success. Keep remaining obligations open; evidence is not new authority. This policy overrides generic instructions requiring a separate reviewer for every ordinary document or task."
}
func (e *Engine) completionTools(original Run) []copilot.Tool {
	var task Assignment
	_ = e.Store.Get(original.Task, &task)
	if original.ReviewOf != "" || (original.Parent == "" && !routineCompletion(task)) {
		return nil
	}
	return []copilot.Tool{copilot.DefineTool("adc_evidence", "Record concise observed evidence of an achieved outcome. Supply Summary (up to 3000 bytes), one credential-free Reference (no URL query/fragment), and Revision (0 initially). This is evidence, not new authority, human intent, or acceptance of an external claim. Routine responsibilities require this or an obligation observation before completion; code retains independent review.", func(p struct {
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
		if reason := s.obligationRunProblem(t); reason != "" {
			return nil, fmt.Errorf("%s", reason)
		}
		return e.recordCompletionEvidence(r, p.Summary, p.Reference, p.Revision)
	})}
}

// A verification activity can report success or failure, but its concrete
// observation must exist before completion; a reviewed prose report alone is
// not the linked obligation's result.
func (e *Engine) verificationEvidenceComplete(t Assignment) bool {
	if t.Obligation == "" {
		return true
	}
	for _, r := range taskRuns(e.Store, t.ID) {
		if r.Superseded || r.ReviewOf != "" || r.State == "cancelled" || (r.State != "complete" && (r.Parent != "" || r.State != "running")) {
			continue
		}
		v := e.obligationObservation(r)
		if v.ID != "" && v.Obligation == t.Obligation && (!e.requiresIndependentReview(t, r) || e.hasCurrentReview(r, taskReviews(e.Store, t.ID))) {
			return true
		}
	}
	return false
}

const verificationInstructions = "\nIf this handoff provides the verification observation, record adc_obligation_result with actual observed facts and a short source reference BEFORE finishing; an ordinary message or document does not persist the linked obligation result. Review this stored observation under the saved completion policy."
