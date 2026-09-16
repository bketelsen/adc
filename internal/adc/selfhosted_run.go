package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	// Some compatible servers return opaque reasoning needed for their next turn.
	// Keep it in the ephemeral protocol conversation; do not expose it in activity.
	ReasoningContent string `json:"reasoning_content,omitempty"`
}
type chatCompletion struct {
	ID, Model string
	Choices   []struct {
		Message      chatMessage
		FinishReason string `json:"finish_reason"`
	}
	Usage *struct {
		Input   *int64 `json:"prompt_tokens"`
		Output  *int64 `json:"completion_tokens"`
		Details struct {
			Cached *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
}

// Like the CLI-backed adapters, each activation begins from ADC's authoritative
// evidence. Tool effects, traces, documents and outcomes are durable; provider
// conversation is ephemeral. HTTP inference retries never replay tool handlers.
func (e *Engine) executeSelfhosted(ctx context.Context, r Run, t Assignment, a Account, system string, evidence []byte, redact Redactor) error {
	session := "selfhosted-" + ID()
	e.Store.mu.Lock()
	var current Run
	if e.Store.Get(r.ID, &current) != nil || current.State != "running" || current.Superseded {
		e.Store.mu.Unlock()
		return fmt.Errorf("run is no longer active")
	}
	current.Session = session
	err := e.Store.Put("run", r.Org, r.Task, current.State, current.ID, current)
	e.Store.mu.Unlock()
	if err != nil {
		return err
	}
	e.Store.Log(r.Org, r.Task, r.ID, "started", r.Title+" · "+r.Model+" · Self-hosted")
	defer e.Store.interruptTraces(r.ID, session)
	outcome := false
	tools := e.providerTools(r, redact, func(string) { outcome = true }, ctx)
	byName := map[string]copilot.Tool{}
	specs := []map[string]any{}
	for _, tool := range tools {
		byName[tool.Name] = tool
		specs = append(specs, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters}})
	}
	messages := []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: activationLeadIn + "Use ADC outcome tools; do not stop at a promise to act.\n" + string(evidence)}}
	seenCalls := map[string]bool{}
	for round := 0; round < 24; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.Store.mu.Lock()
		var latest Run
		var task Assignment
		valid := e.Store.Get(r.ID, &latest) == nil && latest.State == "running" && !latest.Superseded && e.Store.Get(r.Task, &task) == nil && task.State != "paused" && task.State != "cancelled"
		updates := latest.Steering || latest.UpdatesPending
		e.Store.mu.Unlock()
		if !valid {
			return nil
		}
		if updates {
			return nil
		} // completeActivation delivers persisted steering next.
		payload := map[string]any{"model": r.Model, "messages": messages, "tools": specs, "stream": false, "max_tokens": 8192}
		encoded, _ := json.Marshal(payload)
		if len(encoded) > 4<<20 {
			return fmt.Errorf("self-hosted conversation exceeds 4 MiB; save progress in ADC documents and continue from a bounded evidence packet")
		}
		raw, err := e.Store.selfhostedRequest(ctx, a, "/chat/completions", payload)
		if err != nil {
			return err
		}
		var response chatCompletion
		if json.Unmarshal(raw, &response) != nil || len(response.Choices) != 1 {
			return fmt.Errorf("self-hosted provider returned an invalid single-choice completion")
		}
		if response.Usage != nil {
			u := response.Usage
			sample := usageSample{Model: response.Model, Input: u.Input, Output: u.Output, CacheRead: u.Details.Cached, Account: a.ID, Human: a.User, Agent: r.Agent, Provider: "selfhosted", Session: session, EventID: strconv.Itoa(round)}
			data, _ := json.Marshal(sample)
			e.Store.Log(r.Org, r.Task, r.ID, "usage", string(data))
		}
		if response.Model != r.Model {
			return fmt.Errorf("self-hosted requested %q but provider reported %q; no tools executed and no substitution permitted", r.Model, clipped(response.Model, 200))
		}
		choice := response.Choices[0]
		message := choice.Message
		if message.Role != "assistant" {
			return fmt.Errorf("self-hosted completion did not contain an assistant message")
		}
		// Never execute partial/truncated tool arguments, even if they happen to parse.
		if choice.FinishReason != "stop" && choice.FinishReason != "tool_calls" {
			return fmt.Errorf("self-hosted completion ended with %q; no tool effects were dispatched", choice.FinishReason)
		}
		if len(message.ToolCalls) > 16 {
			return fmt.Errorf("self-hosted completion exceeds 16 tool calls")
		}
		for _, call := range message.ToolCalls {
			if call.ID == "" {
				return fmt.Errorf("self-hosted provider returned a missing tool_call id")
			}
			if len(call.ID) > 200 {
				return fmt.Errorf("self-hosted provider returned a tool_call id longer than 200 characters")
			}
			if seenCalls[call.ID] {
				return fmt.Errorf("self-hosted provider repeated tool_call id %q; no calls in this response were dispatched", call.ID)
			}
			if call.Type != "function" || len(call.Function.Arguments) > 1<<20 {
				return fmt.Errorf("self-hosted provider returned an unsupported or oversized function call")
			}
			seenCalls[call.ID] = true
		}
		if text := chatText(message.Content); strings.TrimSpace(text) != "" {
			e.Store.Log(r.Org, r.Task, r.ID, "message", clipped(redact.Text(text), 32000))
		}
		messages = append(messages, message)
		if len(message.ToolCalls) == 0 {
			return nil
		}
		for _, call := range message.ToolCalls {
			if err := ctx.Err(); err != nil {
				return err
			}
			// A steering/cancel boundary can arrive after inference. Do not dispatch a
			// now-obsolete batch; the next activation sees current persisted context.
			e.Store.mu.Lock()
			var live Run
			var liveTask Assignment
			eligible := e.Store.Get(r.ID, &live) == nil && live.State == "running" && !live.Superseded && !live.Steering && !live.UpdatesPending && e.Store.Get(r.Task, &liveTask) == nil && liveTask.State != "paused" && liveTask.State != "cancelled"
			e.Store.mu.Unlock()
			if !eligible {
				return nil
			}
			var arguments map[string]any
			decoder := json.NewDecoder(strings.NewReader(call.Function.Arguments))
			decoder.UseNumber()
			decodeErr := decoder.Decode(&arguments)
			text := ""
			success := false
			e.codexTrace(r, session, call.ID, call.Function.Name, json.RawMessage(call.Function.Arguments), "", false, false, redact)
			if decodeErr != nil || arguments == nil || !json.Valid([]byte(call.Function.Arguments)) {
				text = "Tool arguments must be a JSON object. Correct the call."
			} else if tool, ok := byName[call.Function.Name]; !ok {
				text = "This tool is not available. Use the supplied ADC tools."
			} else {
				result, err := tool.Handler(copilot.ToolInvocation{ToolCallID: call.ID, Arguments: arguments})
				if err != nil {
					text = redact.Text(err.Error())
				} else {
					text = redact.Text(result.TextResultForLLM)
					success = result.ResultType != "failure" && result.Error == ""
				}
			}
			text = clipped(text, 128*1024)
			e.codexTrace(r, session, call.ID, call.Function.Name, arguments, text, true, success, redact)
			messages = append(messages, chatMessage{Role: "tool", ToolCallID: call.ID, Content: text})
			if outcome {
				return nil
			}
		}
	}
	return fmt.Errorf("self-hosted activation reached 24 model requests without an ADC outcome")
}

func chatText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	var text strings.Builder
	if parts, ok := content.([]any); ok {
		for _, part := range parts {
			if p, ok := part.(map[string]any); ok && (p["type"] == "text" || p["type"] == "output_text") {
				if value, ok := p["text"].(string); ok {
					text.WriteString(value)
				}
			}
		}
	}
	return text.String()
}
