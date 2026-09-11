package adc

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

//go:embed claude-bridge.mjs
var claudeBridge []byte

func claudePaths() (node, sdk, cli string, err error) {
	node = os.Getenv("ADC_CLAUDE_NODE")
	if node == "" {
		node = "node"
	}
	root := os.Getenv("ADC_CLAUDE_RUNTIME_DIR")
	if root == "" {
		root = "runtime/claude"
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return
	}
	sdk = filepath.Join(root, "node_modules", "@anthropic-ai", "claude-agent-sdk", "sdk.mjs")
	cli = os.Getenv("ADC_CLAUDE_CLI_PATH")
	if cli == "" {
		arch := runtime.GOARCH
		if arch == "amd64" {
			arch = "x64"
		}
		cli = filepath.Join(root, "node_modules", "@anthropic-ai", "claude-agent-sdk-"+runtime.GOOS+"-"+arch, "claude")
	}
	for _, p := range []string{sdk, cli} {
		if _, statErr := os.Stat(p); statErr != nil {
			err = fmt.Errorf("install the pinned Claude runtime with npm ci --prefix runtime/claude, or configure ADC_CLAUDE_RUNTIME_DIR")
			return
		}
	}
	return
}
func claudeEnvironment(dir string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") || strings.HasPrefix(key, "CODEX_") || strings.HasPrefix(key, "OPENAI_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "CLAUDE_CONFIG_DIR="+dir, "CLAUDE_AGENT_SDK_CLIENT_APP=aide-de-camp/0.1")
}
func startClaude(ctx context.Context, dir string) (*codexClient, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	node, sdk, cli, err := claudePaths()
	if err != nil {
		return nil, err
	}
	// Version the embedded bridge by content to avoid changing a running process.
	bridge := filepath.Join(dir, "adc-bridge-"+digest(string(claudeBridge))[:12]+".mjs")
	if err = os.WriteFile(bridge, claudeBridge, 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command(node, bridge, sdk, cli)
	cmd.Dir, cmd.Env, cmd.Stderr = dir, claudeEnvironment(dir), io.Discard
	return startProviderRPC(ctx, cmd, "Claude")
}
func (e *Engine) claudeClient(ctx context.Context, a Account) (*codexClient, error) {
	if providerName(a.Provider) != "claude" {
		return nil, fmt.Errorf("account is not a Claude subscription")
	}
	e.clientMu.Lock()
	defer e.clientMu.Unlock()
	if c := e.codexClients[a.ID]; c != nil {
		select {
		case <-c.done:
			delete(e.codexClients, a.ID)
		default:
			return c, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, err := startClaude(ctx, filepath.Join(e.Store.Dir, "providers", "claude", a.ID))
	if err == nil {
		e.codexClients[a.ID] = c
	}
	return c, err
}
func (e *Engine) claudeModels(ctx context.Context, a Account) ([]Model, error) {
	c, err := e.claudeClient(ctx, a)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct{ ID, Model, DisplayName string }
	}
	if err = c.Call(ctx, "model/list", nil, &result); err != nil {
		return nil, err
	}
	models := []Model{}
	for _, m := range result.Data {
		if Family(m.Model) == "anthropic-claude" {
			models = append(models, Model{m.Model, m.DisplayName, "anthropic-claude"})
		}
	}
	return models, nil
}
func claudeRunConfig(t Assignment, connections map[string]copilot.MCPServerConfig) map[string]any {
	servers := map[string]any{}
	if t.Kind != "proposal" {
		for name, c := range connections {
			switch c := c.(type) {
			case copilot.MCPStdioServerConfig:
				servers[name] = map[string]any{"type": "stdio", "command": c.Command, "args": c.Args, "env": c.Env}
			case copilot.MCPHTTPServerConfig:
				servers[name] = map[string]any{"type": "http", "url": c.URL, "headers": c.Headers}
			}
		}
	}
	return map[string]any{"proposal": t.Kind == "proposal", "mcpServers": servers}
}
