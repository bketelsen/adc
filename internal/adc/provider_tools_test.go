package adc

import (
	"strings"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
)

func TestProviderErrorsExplainRecoveryAndOnlySuccessfulOutcomesYield(t *testing.T) {
	s, e, _, root := fixture(t)
	yielded := ""
	var wait copilot.Tool
	for _, tool := range e.providerTools(root, Redactor{}, func(id string) { yielded = id }) {
		if tool.Name == "adc_wait" {
			wait = tool
		}
	}
	result, err := wait.Handler(copilot.ToolInvocation{ToolCallID: "failed", Arguments: map[string]any{}})
	must(t, err)
	if result.ResultType != "success" || !strings.Contains(result.TextResultForLLM, "No delegated work is pending") || yielded != "" {
		t.Fatal("already-completed handoff treated as an error or yielded without a state transition")
	}
	child := Run{ID: "child", Org: root.Org, Task: root.Task, Parent: root.ID, State: "queued"}
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	result, err = wait.Handler(copilot.ToolInvocation{ToolCallID: "outcome", Arguments: map[string]any{}})
	must(t, err)
	must(t, s.Get(root.ID, &root))
	if result.ResultType != "success" || yielded != "outcome" || root.State != "waiting" {
		t.Fatal("durable handoff did not signal turn completion")
	}
}

func TestCollaborationPersistsWithoutEscalatingAuthority(t *testing.T) {
	s, e, _, root := fixture(t)
	child := Run{ID: "child", Org: root.Org, Task: root.Task, Parent: root.ID, Title: "Research", State: "running", Authority: "observe", Model: "gpt-5.6-sol"}
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	_, err := call(t, e, root, "adc_message", map[string]any{"Run": child.ID, "Message": "The site has a supported-product deadline; inspect the ADR."})
	must(t, err)
	must(t, s.Get(child.ID, &child))
	if !child.UpdatesPending || child.Steering || !strings.Contains(child.Prompt, "not a human instruction") || child.Authority != "observe" || child.Model != "gpt-5.6-sol" || len(child.Tools) != 0 {
		t.Fatal("message lost context or changed receiving authority")
	}
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	_, err = call(t, e, child, "adc_message", map[string]any{"Run": root.ID, "Message": "Evidence is ready for your next turn."})
	must(t, err)
	must(t, s.Get(root.ID, &root))
	if root.State != "queued" {
		t.Fatal("waiting recipient was not woken")
	}
	for _, state := range []string{"cancelled", "blocked"} {
		root.State = state
		must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
		if _, err = call(t, e, child, "adc_message", map[string]any{"Run": root.ID, "Message": "Wake"}); err == nil {
			t.Fatal("message silently revived " + state + " work")
		}
	}
	root.State = "running"
	root.Task = "other-task"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	if _, err = call(t, e, child, "adc_message", map[string]any{"Run": root.ID, "Message": "Cross assignment"}); err == nil {
		t.Fatal("message crossed assignment boundary")
	}
}
