package adc

import (
	"strings"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
)

func TestToolEvidenceRedactsCredentialsAndCorrelatesOutcomes(t *testing.T) {
	s, e, _, r := fixture(t)
	redact := Redactor{Values: []string{"configured-private-value"}}
	e.toolStarted(r, "session", redact, &copilot.ToolExecutionStartData{ToolCallID: "call", ToolName: "fixture_inventory", Arguments: map[string]any{"target": "fixture-nas", "Authorization": "Bearer never-show-this", "options": map[string]any{"password": "another-private-value"}, "command": "read configured-private-value"}})
	traces := taskTraces(s, r.Task)
	if len(traces) != 1 {
		t.Fatal("tool start not saved")
	}
	for _, secret := range []string{"never-show-this", "another-private-value", "configured-private-value"} {
		if strings.Contains(traces[0].Arguments, secret) {
			t.Fatal("credential leaked in arguments")
		}
	}
	e.toolCompleted(r, "session", redact, &copilot.ToolExecutionCompleteData{ToolCallID: "call", Success: true, Result: &copilot.ToolExecutionCompleteResult{Content: "fixture-nas healthy; token=must-not-survive configured-private-value"}})
	traces = taskTraces(s, r.Task)
	if traces[0].State != "complete" || !strings.Contains(traces[0].Result, "healthy") {
		t.Fatal("outcome not linked")
	}
	if strings.Contains(traces[0].Result, "must-not-survive") || strings.Contains(traces[0].Result, "configured-private-value") {
		t.Fatal("credential leaked in result")
	}
	s.interruptTraces(r.ID, "session")
	must(t, s.Get(traces[0].ID, &traces[0]))
	if traces[0].State != "complete" {
		t.Fatal("finished tool incorrectly marked interrupted")
	}
	e.toolStarted(r, "session", redact, &copilot.ToolExecutionStartData{ToolCallID: "pending", ToolName: "fixture_action"})
	s.interruptTraces(r.ID, "session")
	var pending ToolTrace
	must(t, s.Get(traceID("session", "pending"), &pending))
	if pending.State != "interrupted" {
		t.Fatal("uncertain external outcome not recorded")
	}
}
func TestRedactionRecognizesTokenAndPrivateKeyFormats(t *testing.T) {
	r := Redactor{}
	token := "ghp_" + strings.Repeat("a", 32)
	result := r.Text("credential " + token + "\n-----BEGIN PRIVATE KEY-----\nprivate-material\n-----END PRIVATE KEY-----")
	if strings.Contains(result, token) || strings.Contains(result, "private-material") {
		t.Fatal("recognizable credential leaked")
	}
}
