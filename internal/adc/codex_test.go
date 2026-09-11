package adc

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

func TestProviderFundingRequiresExplicitOwnPortfolio(t *testing.T) {
	s, e, task, root := fixture(t)
	code := Account{ID: "codex-account", User: task.Creator, Provider: "codex", Limit: 1}
	must(t, s.Put("account", "", code.User, "", code.ID, code))
	var dev Agent
	must(t, s.Get("dev", &dev))
	dev.Provider = "codex"
	must(t, s.Put("agent", dev.Org, "", "", dev.ID, dev))
	args := map[string]any{"Agent": "dev", "Title": "Codex work", "Prompt": "Inspect fixture"}
	if _, err := call(t, e, root, "adc_delegate", args); err == nil {
		t.Fatal("used an unapproved subscription")
	}
	task.ExtraAccount = code.ID
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	_, err := call(t, e, root, "adc_delegate", args)
	must(t, err)
	var worker Run
	for _, r := range taskRuns(s, task.ID) {
		if r.Agent == "dev" {
			worker = r
		}
	}
	if worker.Provider != "codex" || worker.Account != code.ID {
		t.Fatalf("wrong worker funding: %+v", worker)
	}
	code.User = "another-human"
	must(t, s.Put("account", "", code.User, "", code.ID, code))
	if _, err = s.runAccount(task, worker); err == nil {
		t.Fatal("cross-human subscription accepted")
	}
	if err = CanReview("gpt-5.6-sol", "gpt-6-astra"); err == nil {
		t.Fatal("provider distinction bypassed family independence")
	}
}
func TestMixedProviderConcurrencyUsesEachSubscription(t *testing.T) {
	s, e, task, root := fixture(t)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	for _, a := range []Account{{ID: "account", User: task.Creator, Provider: "copilot", Limit: 1}, {ID: "codex-account", User: task.Creator, Provider: "codex", Limit: 1}} {
		must(t, s.Put("account", "", a.User, "", a.ID, a))
	}
	task.ExtraAccount = "codex-account"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	for _, r := range []Run{
		{ID: "copilot-worker", Provider: "copilot", Account: "account"},
		{ID: "codex-worker-a", Provider: "codex", Account: "codex-account"},
		{ID: "codex-worker-b", Provider: "codex", Account: "codex-account"},
	} {
		r.Org, r.Task, r.Parent, r.State, r.Category = task.Org, task.ID, root.ID, "queued", "implementation"
		must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	}
	started := controlScheduler(t, e)
	e.tick(context.Background())
	first, second := awaitRun(t, started), awaitRun(t, started)
	if first.Account == second.Account {
		t.Fatal("one subscription exceeded its limit")
	}
	e.tick(context.Background())
	select {
	case r := <-started:
		t.Fatal("active supplemental worker did not consume its subscription slot", r.ID)
	default:
	}
	var queued Run
	must(t, s.Get("codex-worker-b", &queued))
	if queued.State != "queued" {
		t.Fatal("second Codex worker should wait for its own subscription")
	}
}

func TestCodexAccountCannotImportHostCredentials(t *testing.T) {
	s, e, _, _ := fixture(t)
	w := NewWeb(s, e, false)
	user := User{ID: "owner"}
	values := url.Values{"name": {"Codex"}, "provider": {"codex"}, "limit": {"2"}, "local": {"on"}}
	req := httptest.NewRequest("POST", "/accounts?org=org", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	if err := w.saveAccount(req, user); err == nil {
		t.Fatal("accepted host login import")
	}
	req.Form.Del("local")
	must(t, w.saveAccount(req, user))
	var account Account
	for _, a := range list[Account](s, "account", "") {
		if a.Provider == "codex" {
			account = a
		}
	}
	if account.ID == "" || account.Secret != "" || account.Local {
		t.Fatal("wrong pending Codex account")
	}
	req.Form.Set("id", account.ID)
	if err := w.saveAccount(req, User{ID: "other"}); err == nil {
		t.Fatal("edited another human's account")
	}
}
func TestCodexUsageIsCumulativeAndNullAware(t *testing.T) {
	a, b, z := int64(100), int64(20), int64(0)
	first := codexTokens{Input: &a, Output: &b, CacheRead: &z}
	delta, ok := codexUsageDelta(first, codexTokens{})
	if !ok || *delta.Input != 100 || delta.CacheWrite != nil {
		t.Fatal("initial snapshot corrupted")
	}
	if _, ok = codexUsageDelta(first, first); ok {
		t.Fatal("counted duplicate snapshot")
	}
	next := int64(130)
	second := first
	second.Input = &next
	delta, ok = codexUsageDelta(second, first)
	if !ok || *delta.Input != 30 || *delta.Output != 0 {
		t.Fatal("cumulative snapshot was counted as per-call usage")
	}
	if _, ok = codexUsageDelta(first, second); ok {
		t.Fatal("out-of-order snapshot accepted")
	}
}
func TestCodexProposalConfigKeepsGrantBoundary(t *testing.T) {
	_, e, _, root := fixture(t)
	cfg := codexRunConfig(Assignment{Kind: "proposal"}, map[string]copilot.MCPServerConfig{"secret": copilot.MCPStdioServerConfig{Command: "do-not-start"}})
	if len(cfg["mcp_servers"].(map[string]any)) != 0 || cfg["features.shell_tool"] != false || cfg["web_search"] != "disabled" {
		t.Fatal("proposal can execute tools")
	}
	defs := codexToolSpecs(e.tools(root))
	if len(defs) == 0 || defs[0]["type"] != "function" || defs[0]["inputSchema"] == nil {
		t.Fatal("dynamic tool declaration missing")
	}
}
func TestCodexProtocolWithoutCredentials(t *testing.T) {
	if os.Getenv("ADC_CODEX_PROTOCOL_TEST") != "1" {
		t.Skip("set ADC_CODEX_PROTOCOL_TEST for the installed app-server protocol check; no login or model turn")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	home := filepath.Join(t.TempDir(), "codex")
	c, err := startCodex(ctx, home)
	must(t, err)
	defer c.Close()
	var status codexAccountStatus
	must(t, c.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &status))
	if status.Account != nil {
		t.Fatal("private account unexpectedly inherited host credentials")
	}
	var start struct {
		Thread struct{ ID string }
		Model  string
	}
	params := map[string]any{"model": "gpt-5.6-sol", "cwd": home, "ephemeral": true, "sandbox": "read-only", "approvalPolicy": "never", "allowProviderModelFallback": false, "dynamicTools": []map[string]any{{"type": "function", "name": "adc_fixture", "description": "Protocol fixture; never invoked", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}, "config": codexRunConfig(Assignment{Kind: "proposal"}, nil)}
	must(t, c.Call(ctx, "thread/start", params, &start))
	if start.Thread.ID == "" {
		t.Fatal("missing protocol thread")
	}
	t.Log("Isolated account/read and experimental dynamic-tool thread/start succeeded without signing in or executing a model turn.")
}
