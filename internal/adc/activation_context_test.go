package adc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActivationContextStaysCompactAndPointsAtFocusedViews(t *testing.T) {
	s, e, task, root := fixture(t)
	child := Run{ID: "child-ctx", Org: task.Org, Task: task.ID, Parent: root.ID, Agent: "dev", Category: "implementation", State: "complete", Title: "Worker", Prompt: strings.Repeat("historical worker prompt ", 2000), Result: strings.Repeat("observed result ", 400), Code: []CodeEvidence{{Path: "/tmp/repo", Commit: "abc123"}}}
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	doc := Document{ID: "doc-ctx", Org: task.Org, Task: task.ID, Run: child.ID, Title: "Design", Content: strings.Repeat("document body text ", 1200), Revision: 1}
	must(t, s.Put("document", doc.Org, doc.Task, "", doc.ID, doc))
	review := Review{ID: "review-ctx", Org: task.Org, Task: task.ID, Run: "qa-run", Target: child.ID, Model: "claude-opus-5", Verdict: "changes", Findings: strings.Repeat("finding detail ", 800)}
	must(t, s.Put("review", review.Org, review.Task, "", review.ID, review))
	for _, id := range []string{"prop-a", "prop-b"} {
		p := WorkProposal{ID: id, Org: task.Org, Title: id, State: "pending", Scope: strings.Repeat("proposal scope ", 500), Evidence: "proposal evidence"}
		must(t, s.Put("proposal", p.Org, "", p.State, p.ID, p))
	}
	b, err := json.Marshal(e.activationContext(root, task, []Model{{ID: "gpt-6-astra", Family: "openai-gpt"}}, map[string][]Model{"copilot": {{ID: "gpt-6-astra"}}}, map[string]string{}))
	must(t, err)
	raw := string(b)
	if len(raw) > 24000 {
		t.Fatal("activation context is not compact", len(raw))
	}
	for _, leaked := range []string{"historical worker prompt", "document body text", "proposal scope", "document_catalog", "\"guidance\""} {
		if strings.Contains(raw, leaked) {
			t.Fatal("activation context carried history that belongs to focused views:", leaked)
		}
	}
	for _, kept := range []string{"\"abc123\"", "\"review-ctx\"", "\"doc-ctx\"", "finding detail", "available_models_by_provider"} {
		if !strings.Contains(raw, kept) {
			t.Fatal("activation context lost current evidence:", kept)
		}
	}
	// The exact records remain one focused call away.
	exact, err := call(t, e, root, "adc_status", statusInput{View: "review", ID: review.ID})
	must(t, err)
	if strings.Count(exact, "finding detail") < 800 {
		t.Fatal("focused review read lost the full findings")
	}
	if _, err = call(t, e, root, "adc_status", statusInput{View: "review", ID: "missing"}); err == nil {
		t.Fatal("unknown review ID must be refused")
	}
	// Workers do not delegate across subscriptions and do not receive the catalogs.
	worker, err := json.Marshal(e.activationContext(child, task, nil, map[string][]Model{"copilot": {{ID: "gpt-6-astra"}}}, nil))
	must(t, err)
	if strings.Contains(string(worker), "available_models_by_provider") {
		t.Fatal("worker context carried supervisor-only catalogs")
	}
	// Proposal conversations still receive the compact proposal list.
	task.Kind = "proposal"
	proposal, err := json.Marshal(e.activationContext(root, task, nil, nil, nil))
	must(t, err)
	if !strings.Contains(string(proposal), "\"prop-a\"") || strings.Contains(string(proposal), strings.Repeat("proposal scope ", 100)) {
		t.Fatal("proposal conversation context is wrong")
	}
}
