package adc

import (
	"context"
	copilot "github.com/github/copilot-sdk/go"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeClaude(t *testing.T) {
	t.Helper()
	fakeCodex(t)
	t.Setenv("ADC_CLAUDE_FIXTURE", "1")
	t.Setenv("ADC_CLAUDE_NODE", os.Getenv("ADC_CODEX_CLI_PATH"))
	t.Setenv("ADC_CLAUDE_CLI_PATH", os.Getenv("ADC_CODEX_CLI_PATH"))
	dir := t.TempDir()
	sdkDir := filepath.Join(dir, "node_modules", "@anthropic-ai", "claude-agent-sdk")
	must(t, os.MkdirAll(sdkDir, 0700))
	must(t, os.WriteFile(filepath.Join(sdkDir, "sdk.mjs"), []byte("// fixture"), 0600))
	t.Setenv("ADC_CLAUDE_RUNTIME_DIR", dir)
}
func TestClaudeAccountIsolationAndGrants(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "do-not-inherit")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "do-not-inherit")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", "/do-not-import")
	env := strings.Join(claudeEnvironment("/private/adc/claude"), "\n")
	if strings.Contains(env, "do-not-") || strings.Contains(env, "USE_BEDROCK") || !strings.Contains(env, "CLAUDE_CONFIG_DIR=/private/adc/claude") {
		t.Fatal("host identity or billing override inherited")
	}
	cfg := claudeRunConfig(Assignment{Kind: "proposal"}, map[string]copilot.MCPServerConfig{"storage": copilot.MCPStdioServerConfig{Command: "not-granted"}})
	if len(cfg["mcpServers"].(map[string]any)) != 0 || cfg["proposal"] != true {
		t.Fatal("proposal can access operational MCP")
	}
	s, e, task, root := fixture(t)
	defer e.Stop()
	a := Account{ID: "claude-account", Provider: "claude", User: task.Creator, Limit: 1}
	must(t, s.Put("account", "", a.User, "", a.ID, a))
	var qa Agent
	must(t, s.Get("qa", &qa))
	qa.Provider = "claude"
	must(t, s.Put("agent", qa.Org, "", "", qa.ID, qa))
	if _, err := call(t, e, root, "adc_delegate", map[string]any{"Agent": "qa", "Title": "Inspect", "Prompt": "Inspect fixture"}); err == nil {
		t.Fatal("used unapproved Claude subscription")
	}
	task.ExtraAccount = a.ID
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	_, err := call(t, e, root, "adc_delegate", map[string]any{"Agent": "qa", "Title": "Inspect", "Prompt": "Inspect fixture"})
	must(t, err)
	for _, r := range taskRuns(s, task.ID) {
		if r.Agent == "qa" && (r.Provider != "claude" || r.Account != a.ID) {
			t.Fatal("wrong funding")
		}
	}
	w := NewWeb(s, e, false)
	values := url.Values{"name": {"Claude"}, "provider": {"claude"}, "limit": {"2"}, "token": {"do-not-import"}}
	req := httptest.NewRequest("POST", "/accounts?org=org", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	if w.saveAccount(req, User{ID: task.Creator}) == nil {
		t.Fatal("imported credential instead of native login")
	}
	req.Form.Del("token")
	must(t, w.saveAccount(req, User{ID: task.Creator}))
	if !strings.HasPrefix(req.Form.Get("return"), "/claude-account?") {
		t.Fatal("missing connection setup")
	}
	if w.claudeAccountPage(httptest.NewRequest("GET", "/claude-account?id="+a.ID, nil), &Page{User: User{ID: "other"}}) == nil {
		t.Fatal("exposed another human's account")
	}
}
func TestClaudeWireCompletionAndCancellation(t *testing.T) {
	for _, mode := range []string{"complete", "hang"} {
		t.Run(mode, func(t *testing.T) {
			fakeClaude(t)
			t.Setenv("ADC_CODEX_FIXTURE_MODE", mode)
			s, e, task, r := fixture(t)
			defer e.Stop()
			a := Account{ID: task.Account, User: task.Creator, Provider: "claude", Limit: 1}
			must(t, s.Put("account", "", a.User, "", a.ID, a))
			r.Provider, r.Model, r.Account = "claude", "claude-opus-5", a.ID
			task.Kind = "proposal"
			must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
			must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			timeout := 10 * time.Second
			if mode == "hang" {
				timeout = 300 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			e.execute(ctx, r, task, a)
			must(t, s.Get(r.ID, &r))
			if mode == "hang" {
				if r.State != "queued" || !strings.Contains(r.Prompt, "interrupted") {
					t.Fatal("lost unfinished work")
				}
				return
			}
			if r.State != "complete" {
				t.Fatalf("outcome not durable: %s %s", r.State, r.Error)
			}
			usage, err := s.usageReport(task.Org, task.ID, 0, 0, time.Now())
			must(t, err)
			if usage.Total.Input.Value != 150 || usage.Total.Samples != 2 {
				t.Fatal("usage replay counted twice")
			}
			for _, ev := range s.Events(task.ID) {
				if ev.Kind == "usage" && !strings.Contains(ev.Text, `"provider":"claude"`) {
					t.Fatal("wrong usage provider")
				}
			}
		})
	}
}
func TestClaudeRuntimeWithoutCredentials(t *testing.T) {
	if os.Getenv("ADC_CLAUDE_PROTOCOL_TEST") != "1" {
		t.Skip("set ADC_CLAUDE_PROTOCOL_TEST with the pinned runtime for a no-login/no-model protocol check")
	}
	t.Setenv("ANTHROPIC_API_KEY", "must-not-be-used")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := startClaude(ctx, t.TempDir())
	must(t, err)
	defer c.Close()
	var status codexAccountStatus
	must(t, c.Call(ctx, "account/read", nil, &status))
	if status.Account != nil {
		t.Fatal("inherited host authentication")
	}
	if c.Call(ctx, "model/list", nil, nil) == nil {
		t.Fatal("offered unauthenticated execution")
	}
	t.Log("Official SDK bridge and native CLI booted in private state; host credentials were not inherited. No model turn sent.")
	node, sdk, cli, err := claudePaths()
	must(t, err)
	dir := t.TempDir()
	script, err := filepath.Abs("testdata/claude-sdk-check.mjs")
	must(t, err)
	cmd := exec.CommandContext(ctx, node, script, sdk, cli, dir)
	cmd.Env = claudeEnvironment(dir)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	must(t, err)
}
