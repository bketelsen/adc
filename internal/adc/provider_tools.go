package adc

import (
	"context"
	copilot "github.com/github/copilot-sdk/go"
)

func excludedProviderTools() []string {
	// Native agent IDs are unrelated to ADC roles and durable runs.
	return []string{"task", "ask_user", "read_agent", "write_agent", "list_agents"}
}

func (e *Engine) providerTools(r Run, redact Redactor, outcome func(string), contexts ...context.Context) []copilot.Tool {
	tools := e.tools(r)
	var task Assignment
	_ = e.Store.Get(r.Task, &task)
	if r.Execution == "protected" && task.Kind != "proposal" {
		ctx := context.Background()
		if len(contexts) > 0 {
			ctx = contexts[0]
		}
		tools = append(tools, e.protectedTools(ctx, r)...)
	}
	for i := range tools {
		handler, name := tools[i].Handler, tools[i].Name
		tools[i].Handler = func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
			result, err := handler(inv)
			if err != nil {
				// A Go handler error is replaced with a generic failure by the CLI.
				// Return a structured failure so the model can act on the reason.
				message := redact.Text(err.Error())
				return copilot.ToolResult{ResultType: "failure", TextResultForLLM: message, Error: message}, nil
			}
			switch name {
			case "adc_wait", "adc_blocked", "adc_finish", "adc_review", "adc_decision", "adc_request_access":
				outcome(inv.ToolCallID)
			}
			return result, nil
		}
	}
	return tools
}
