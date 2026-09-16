package adc

import (
	"strings"
	"testing"
)

func TestPromptStaysSmallAndToolsMatchTheRole(t *testing.T) {
	s, e, task, root := fixture(t)
	task.Completion = selectedCompletion("routine")
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	var boss, dev, qa Agent
	must(t, s.Get("boss", &boss))
	must(t, s.Get("dev", &dev))
	must(t, s.Get("qa", &qa))
	worker := Run{ID: "prompt-worker", Org: task.Org, Task: task.ID, Parent: root.ID, Agent: dev.ID, Category: "implementation", Model: dev.Model, State: "running", Authority: "draft"}
	must(t, s.Put("run", worker.Org, worker.Task, worker.State, worker.ID, worker))
	prompt := e.systemPrompt(worker, task, dev, "")
	if len(prompt) > 3200 {
		t.Fatal("worker prompt is not compact", len(prompt))
	}
	for _, supervisorOnly := range []string{"adc_plan", "rubber-stamp", "Setup exception", "adc_workspace"} {
		if strings.Contains(prompt, supervisorOnly) {
			t.Fatal("worker prompt carries guidance for another role:", supervisorOnly)
		}
	}
	for _, kept := range []string{"adc_code", "adc_finish", "adc_blocked", "plain-language answer is final", "COMPLETION POLICY — ROUTINE"} {
		if !strings.Contains(prompt, kept) {
			t.Fatal("worker prompt lost a standing rule:", kept)
		}
	}
	supervisor := e.systemPrompt(root, task, boss, "")
	if !strings.Contains(supervisor, "adc_plan") || len(supervisor) > 4500 {
		t.Fatal("supervisor prompt missing plan guidance or too large", len(supervisor))
	}
	reviewer := Run{ID: "prompt-reviewer", Org: task.Org, Task: task.ID, Parent: root.ID, Agent: qa.ID, Category: "review", Model: qa.Model, State: "running", ReviewOf: worker.ID}
	if r := e.systemPrompt(reviewer, task, qa, ""); !strings.Contains(r, "independent reviewer of run "+worker.ID) {
		t.Fatal("reviewer prompt lacks review guidance")
	}
	task.Completion = selectedCompletion("reviewed")
	if r := e.systemPrompt(root, task, boss, ""); !strings.Contains(r, "COMPLETION POLICY — REVIEWED") {
		t.Fatal("reviewed policy not stated to the supervisor")
	}
	names := func(r Run) map[string]bool {
		out := map[string]bool{}
		for _, tool := range e.tools(r) {
			out[tool.Name] = true
		}
		return out
	}
	rootTools, workerTools := names(root), names(worker)
	if !rootTools["adc_plan"] || !rootTools["adc_reassign"] || workerTools["adc_plan"] || workerTools["adc_reassign"] || workerTools["adc_milestone"] || workerTools["adc_await"] || workerTools["adc_integration"] {
		t.Fatal("tools offered to the wrong role", rootTools, workerTools)
	}
	for _, shared := range []string{"adc_status", "adc_delegate", "adc_message", "adc_code", "adc_document", "adc_decision", "adc_wait", "adc_finish", "adc_blocked"} {
		if !workerTools[shared] || !rootTools[shared] {
			t.Fatal("shared tool missing", shared)
		}
	}
}
