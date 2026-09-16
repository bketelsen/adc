package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

func codexRunConfig(t Assignment, conns map[string]copilot.MCPServerConfig) map[string]any {
	servers := map[string]any{}
	for name, c := range conns {
		switch c := c.(type) {
		case copilot.MCPStdioServerConfig:
			servers[name] = map[string]any{"command": c.Command, "args": c.Args, "env": c.Env, "required": true}
		case copilot.MCPHTTPServerConfig:
			servers[name] = map[string]any{"url": c.URL, "http_headers": c.Headers, "required": true}
		}
	}
	config := map[string]any{"mcp_servers": servers, "web_search": "live", "features.multi_agent": false, "features.multi_agent_v2": false, "features.apps": false, "features.plugins": false, "features.browser_use": false, "features.computer_use": false, "features.image_generation": false, "features.goals": false, "features.sleep_tool": false}
	if t.Kind == "proposal" {
		config["web_search"] = "disabled"
		config["features.shell_tool"] = false
		config["features.view_image"] = false
		config["mcp_servers"] = map[string]any{}
	}
	return config
}
func codexToolSpecs(tools []copilot.Tool) []map[string]any {
	specs := []map[string]any{}
	for _, tool := range tools {
		specs = append(specs, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "inputSchema": tool.Parameters})
	}
	return specs
}
func (e *Engine) codexTrace(r Run, session, id, name string, args any, result string, complete, success bool, redact Redactor) {
	key := traceID(session, id)
	var trace ToolTrace
	if complete {
		if e.Store.Get(key, &trace) != nil {
			trace = ToolTrace{ID: key, Org: r.Org, Task: r.Task, Run: r.ID, Session: session, Call: id, Name: name, At: now()}
		}
		trace.State = "complete"
		if !success {
			trace.State = "failed"
		}
		trace.Result = clipped(redact.Text(result), 16384)
	} else {
		trace = ToolTrace{ID: key, Org: r.Org, Task: r.Task, Run: r.ID, Session: session, Call: id, Name: name, At: now(), State: "running", Arguments: clipped(redact.JSON(args), 8192)}
	}
	_ = e.Store.Put("tooltrace", r.Org, r.Task, trace.State, key, trace)
	if complete {
		e.Store.Log(r.Org, r.Task, r.ID, "tool-result", trace.Name+" · "+trace.State)
	} else {
		e.Store.Log(r.Org, r.Task, r.ID, "tool", name)
	}
}

type codexTokens struct {
	Input      *int64 `json:"inputTokens"`
	Output     *int64 `json:"outputTokens"`
	CacheRead  *int64 `json:"cachedInputTokens"`
	CacheWrite *int64 `json:"cacheWriteInputTokens"`
}

func codexUsageDelta(current, prior codexTokens) (codexTokens, bool) {
	out := codexTokens{}
	changed := false
	dst := []**int64{&out.Input, &out.Output, &out.CacheRead, &out.CacheWrite}
	cur := []*int64{current.Input, current.Output, current.CacheRead, current.CacheWrite}
	old := []*int64{prior.Input, prior.Output, prior.CacheRead, prior.CacheWrite}
	for i, p := range cur {
		if p == nil {
			continue
		}
		before := int64(0)
		if old[i] != nil {
			before = *old[i]
		}
		if *p < before || *p < 0 {
			return codexTokens{}, false
		}
		value := *p - before
		*dst[i] = &value
		if value > 0 || old[i] == nil {
			changed = true
		}
	}
	return out, changed
}
func (e *Engine) executeRPCProvider(ctx context.Context, r Run, t Assignment, a Account, agent Agent, system string, evidence []byte, conns map[string]copilot.MCPServerConfig, redact Redactor) error {
	var c *codexClient
	var err error
	config := codexRunConfig(t, conns)
	if providerName(a.Provider) == "claude" {
		c, err = e.claudeClient(ctx, a)
		config = claudeRunConfig(t, conns)
	} else {
		c, err = e.codexClient(ctx, a)
	}
	if err != nil {
		return err
	}
	outcome := ""
	tools := e.providerTools(r, redact, func(id string) { outcome = id }, ctx)
	toolMap := map[string]copilot.Tool{}
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}
	sandbox := "danger-full-access"
	if t.Kind == "proposal" || r.Execution == "protected" {
		sandbox = "read-only"
	}
	var start struct {
		Thread struct{ ID string }
		Model  string
	}
	startParams := map[string]any{"model": r.Model, "allowProviderModelFallback": false, "cwd": r.Workspace, "approvalPolicy": "never", "sandbox": sandbox, "ephemeral": true, "developerInstructions": system, "dynamicTools": codexToolSpecs(tools), "config": config}
	if r.Execution == "protected" {
		applyProtectedRPCConfig(startParams, config, providerName(a.Provider))
	}
	err = c.Call(ctx, "thread/start", startParams, &start)
	if err != nil {
		return err
	}
	if start.Thread.ID == "" {
		return fmt.Errorf("%s did not return a thread ID", c.label)
	}
	id := start.Thread.ID
	events := c.subscribe(id)
	defer c.unsubscribe(id)
	defer e.Store.interruptTraces(r.ID, id)
	defer func() {
		end, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.Call(end, "thread/unsubscribe", map[string]string{"threadId": id}, nil)
	}()
	if start.Model != "" && start.Model != r.Model {
		return fmt.Errorf("%s returned a different model; no substitution permitted", c.label)
	}
	e.Store.mu.Lock()
	var current Run
	_ = e.Store.Get(r.ID, &current)
	current.Session = id
	_ = e.Store.Put("run", r.Org, r.Task, current.State, r.ID, current)
	e.Store.mu.Unlock()
	e.Store.Log(r.Org, r.Task, r.ID, "started", r.Title+" · "+r.Model+" · "+c.label)
	var turn struct{ Turn struct{ ID string } }
	params := map[string]any{"threadId": id, "input": []map[string]string{{"type": "text", "text": activationLeadIn + string(evidence)}}}
	if agent.Effort != "" {
		params["effort"] = agent.Effort
	}
	if err = c.Call(ctx, "turn/start", params, &turn); err != nil {
		return err
	}
	defer func() {
		end, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.Call(end, "turn/interrupt", map[string]string{"threadId": id, "turnId": turn.Turn.ID}, nil)
	}()
	prior := map[string]codexTokens{}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return fmt.Errorf("%s runtime disconnected during the run", c.label)
		case msg := <-events:
			switch msg.Method {
			case "item/tool/call":
				var call struct {
					CallID    string `json:"callId"`
					Tool      string
					Arguments any
				}
				if err = json.Unmarshal(msg.Params, &call); err != nil {
					return err
				}
				tool, ok := toolMap[call.Tool]
				if !ok {
					_ = c.reply(msg.ID, map[string]any{"success": false, "contentItems": []map[string]string{{"type": "inputText", "text": "Tool is not granted to this ADC run"}}})
					continue
				}
				e.codexTrace(r, id, call.CallID, call.Tool, call.Arguments, "", false, false, redact)
				result, toolErr := tool.Handler(copilot.ToolInvocation{SessionID: id, ToolCallID: call.CallID, ToolName: call.Tool, Arguments: call.Arguments, TraceContext: ctx})
				success := toolErr == nil && result.ResultType != "failure"
				text := result.TextResultForLLM
				if toolErr != nil {
					text = redact.Text(toolErr.Error())
				}
				e.codexTrace(r, id, call.CallID, call.Tool, nil, text, true, success, redact)
				if err = c.reply(msg.ID, map[string]any{"success": success, "contentItems": []map[string]string{{"type": "inputText", "text": text}}}); err != nil {
					return err
				}
				if outcome == call.CallID && success {
					return nil
				}
			case "item/started", "item/completed":
				var params struct {
					Item struct {
						ID, Type, Text, Status string
						ExitCode               *int            `json:"exitCode"`
						Error                  json.RawMessage `json:"error"`
						Server, Tool           string
					}
				}
				if json.Unmarshal(msg.Params, &params) != nil {
					continue
				}
				item := params.Item
				if msg.Method == "item/completed" && item.Type == "agentMessage" {
					e.Store.Log(r.Org, r.Task, r.ID, "message", redact.Text(item.Text))
				}
				switch item.Type {
				case "commandExecution", "fileChange", "mcpToolCall", "webSearch", "nativeTool":
					complete := msg.Method == "item/completed"
					name := item.Type
					if item.Type == "nativeTool" {
						name = item.Tool
					}
					if item.Type == "mcpToolCall" {
						name = item.Server + "." + item.Tool
					}
					success := item.Status != "failed" && item.Status != "declined" && (item.ExitCode == nil || *item.ExitCode == 0) && (len(item.Error) == 0 || string(item.Error) == "null")
					e.codexTrace(r, id, item.ID, name, json.RawMessage(msg.Params), redact.JSON(json.RawMessage(msg.Params)), complete, success, redact)
				}
			case "thread/tokenUsage/updated":
				var p struct {
					Model      string                      `json:"model"`
					TokenUsage struct{ Total codexTokens } `json:"tokenUsage"`
				}
				if json.Unmarshal(msg.Params, &p) != nil {
					continue
				}
				if p.Model == "" {
					p.Model = r.Model
				}
				delta, changed := codexUsageDelta(p.TokenUsage.Total, prior[p.Model])
				if !changed {
					continue
				}
				prior[p.Model] = p.TokenUsage.Total
				total, _ := json.Marshal(p.TokenUsage.Total)
				sample := usageSample{Model: p.Model, Input: delta.Input, Output: delta.Output, CacheRead: delta.CacheRead, CacheWrite: delta.CacheWrite, Account: a.ID, Human: a.User, Agent: r.Agent, Provider: providerName(a.Provider), Session: id, EventID: digest(p.Model + string(total))}
				data, _ := json.Marshal(sample)
				e.Store.Log(r.Org, r.Task, r.ID, "usage", string(data))
			case "turn/completed":
				var p struct {
					Turn struct {
						Status string
						Error  *struct{ Message string }
					}
				}
				_ = json.Unmarshal(msg.Params, &p)
				if p.Turn.Error != nil {
					return fmt.Errorf("%s turn failed: %s", c.label, redact.Text(p.Turn.Error.Message))
				}
				if p.Turn.Status == "failed" {
					return fmt.Errorf("%s turn failed", c.label)
				}
				return nil
			default:
				if len(msg.ID) > 0 {
					_ = c.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "Use adc_decision for human decisions. This native request is not supported by ADC."}})
				}
			}
		}
	}
}
