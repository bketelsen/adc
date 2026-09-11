package adc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexFixtureProcess(t *testing.T) {
	if os.Getenv("ADC_CODEX_FIXTURE") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	send := func(v any) { _ = encoder.Encode(v) }
	mode := os.Getenv("ADC_CODEX_FIXTURE_MODE")
	model := "gpt-5.6-sol"
	accountType := "chatgpt"
	if os.Getenv("ADC_CLAUDE_FIXTURE") == "1" {
		model, accountType = "claude-opus-5", "claude-subscription"
	}
	ready := mode != "login" && mode != "claude-login"
	loginStarted := mode == "claude-login"
	loginReads := 0
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var message map[string]json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		var method string
		_ = json.Unmarshal(message["method"], &method)
		var id any
		_ = json.Unmarshal(message["id"], &id)
		response := func(v any) { send(map[string]any{"id": id, "result": v}) }
		switch method {
		case "initialize":
			response(map[string]any{})
		case "initialized":
		case "account/read":
			if loginStarted {
				loginReads++
				if loginReads > 1 {
					ready = true
				}
			}
			if ready {
				response(map[string]any{"account": map[string]string{"type": accountType, "email": "fixture@example.test", "planType": "fixture"}})
			} else {
				response(map[string]any{"account": nil})
			}
		case "account/login/start":
			loginStarted = true
			loginReads = 0
			response(map[string]string{"loginId": "fixture-login", "verificationUrl": "https://auth.openai.com/codex/device", "userCode": "FIXT-1234"})
		case "account/logout":
			ready = false
			response(map[string]any{})
		case "model/list":
			response(map[string]any{"data": []map[string]string{{"id": model, "model": model, "displayName": "Fixture model"}}, "nextCursor": nil})
		case "thread/start":
			response(map[string]any{"thread": map[string]string{"id": "fixture-thread"}, "model": model})
		case "turn/start":
			response(map[string]any{"turn": map[string]string{"id": "fixture-turn"}})
			if os.Getenv("ADC_CODEX_FIXTURE_MODE") == "hang" {
				continue
			}
			send(map[string]any{"id": 1000, "method": "item/tool/call", "params": map[string]any{"threadId": "fixture-thread", "turnId": "fixture-turn", "callId": "status-call", "tool": "adc_status", "arguments": map[string]any{}}})
		case "turn/interrupt", "thread/unsubscribe":
			response(map[string]any{})
		case "":
			if fmt.Sprint(id) == "1000" {
				for _, input := range []int{100, 100, 150} {
					send(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "fixture-thread", "turnId": "fixture-turn", "tokenUsage": map[string]any{"total": map[string]int{"inputTokens": input, "outputTokens": 10, "cachedInputTokens": 50}}}})
				}
				send(map[string]any{"id": 1001, "method": "item/tool/call", "params": map[string]any{"threadId": "fixture-thread", "turnId": "fixture-turn", "callId": "finish-call", "tool": "adc_finish", "arguments": map[string]string{"Result": "Fixture proposal discussion completed without execution."}}})
			}
		default:
			response(map[string]any{})
		}
	}
	os.Exit(0)
}
func fakeCodex(t *testing.T) {
	t.Helper()
	binary, err := os.Executable()
	must(t, err)
	script := filepath.Join(t.TempDir(), "codex-fixture")
	must(t, os.WriteFile(script, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(binary, "'", "'\\''")+"' -test.run=^TestCodexFixtureProcess$\n"), 0700))
	t.Setenv("ADC_CODEX_CLI_PATH", script)
	t.Setenv("ADC_CODEX_FIXTURE", "1")
}
func TestCodexWireToolsAndUsageYieldToADC(t *testing.T) {
	fakeCodex(t)
	s, e, task, root := fixture(t)
	defer e.Stop()
	var account Account
	must(t, s.Get(task.Account, &account))
	account.Provider = "codex"
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	root.Provider = "codex"
	root.Account = account.ID
	root.Model = "gpt-5.6-sol"
	root.Workspace = filepath.Join(s.Dir, "workspace")
	task.Kind = "proposal"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e.execute(ctx, root, task, account)
	must(t, s.Get(root.ID, &root))
	if root.State != "complete" {
		t.Fatalf("provider outcome not persisted: %+v", root)
	}
	traces := taskTraces(s, task.ID)
	if len(traces) != 2 {
		t.Fatalf("trace count %d", len(traces))
	}
	for _, trace := range traces {
		if trace.State != "complete" {
			t.Fatal("tool trace not completed")
		}
	}
	usage, err := s.usageReport(task.Org, task.ID, 0, 0, time.Now())
	must(t, err)
	if usage.Total.Input.Value != 150 || usage.Total.Samples != 2 || usage.Total.CacheWrite.String() != "Unknown" {
		t.Fatalf("cumulative usage corrupted: %+v", usage.Total)
	}
}
func TestCodexCancellationPreservesUnfinishedWork(t *testing.T) {
	fakeCodex(t)
	t.Setenv("ADC_CODEX_FIXTURE_MODE", "hang")
	s, e, task, root := fixture(t)
	defer e.Stop()
	var account Account
	must(t, s.Get(task.Account, &account))
	account.Provider = "codex"
	root.Provider = "codex"
	root.Model = "gpt-5.6-sol"
	root.Workspace = filepath.Join(s.Dir, "workspace")
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	e.execute(ctx, root, task, account)
	must(t, s.Get(root.ID, &root))
	if root.State != "queued" || !strings.Contains(root.Prompt, "interrupted") {
		t.Fatalf("interrupted work lost: %+v", root)
	}
}
