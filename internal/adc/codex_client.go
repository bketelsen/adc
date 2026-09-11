package adc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type codexEnvelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int
		Message string
	} `json:"error,omitempty"`
}
type codexClient struct {
	label           string
	cmd             *exec.Cmd
	in              io.WriteCloser
	writeMu, syncMu sync.Mutex
	seq             atomic.Int64
	pending         map[string]chan codexEnvelope
	threads         map[string]chan codexEnvelope
	done            chan struct{}
	exited          chan struct{}
	stopOnce        sync.Once
	closeOnce       sync.Once
}
type codexLogin struct {
	LoginID string `json:"loginId"`
	URL     string `json:"verificationUrl"`
	Code    string `json:"userCode"`
}
type codexAccountStatus struct {
	Account            *struct{ Type, Email, PlanType string } `json:"account"`
	RequiresOpenaiAuth bool                                    `json:"requiresOpenaiAuth"`
}

func codexEnvironment(home string) []string {
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if strings.HasPrefix(key, "CODEX_") || strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "CHATGPT_") {
			continue
		}
		env = append(env, v)
	}
	return append(env, "CODEX_HOME="+home)
}
func startCodex(ctx context.Context, home string) (*codexClient, error) {
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	binary := os.Getenv("ADC_CODEX_CLI_PATH")
	if binary == "" {
		binary = "codex"
	}
	// This private home is dedicated to one ADC subscription. It never imports
	// the desktop/CLI login or host configuration. Credentials are Codex-managed.
	cmd := exec.Command(binary, "app-server", "--stdio", "-c", `cli_auth_credentials_store="file"`, "-c", `forced_login_method="chatgpt"`, "-c", `features.apps=false`, "-c", `features.plugins=false`, "-c", `features.multi_agent=false`, "-c", `features.multi_agent_v2=false`, "-c", `features.hooks=false`, "-c", `features.memories=false`, "-c", `features.skip_host_skill_discovery=true`)
	cmd.Env = codexEnvironment(home)
	cmd.Dir = home
	cmd.Stderr = io.Discard
	return startProviderRPC(ctx, cmd, "Codex")
}

// Both app-server and the official Claude SDK bridge use this private RPC pipe.
func startProviderRPC(ctx context.Context, cmd *exec.Cmd, label string) (*codexClient, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c := &codexClient{label: label, cmd: cmd, in: in, pending: map[string]chan codexEnvelope{}, threads: map[string]chan codexEnvelope{}, done: make(chan struct{}), exited: make(chan struct{})}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s runtime: %w", label, err)
	}
	go c.read(out)
	go func() { _ = cmd.Wait(); close(c.exited); c.closeOnce.Do(func() { close(c.done) }) }()
	if err = c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "aide_de_camp", "title": "Aide de Camp", "version": "0.1"}, "capabilities": map[string]bool{"experimentalApi": true}}, nil); err != nil {
		c.Close()
		return nil, err
	}
	if err = c.send(map[string]any{"method": "initialized"}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *codexClient) send(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return json.NewEncoder(c.in).Encode(v)
}
func (c *codexClient) Call(ctx context.Context, method string, params, out any) error {
	id := c.seq.Add(1)
	key := fmt.Sprint(id)
	ch := make(chan codexEnvelope, 1)
	c.syncMu.Lock()
	c.pending[key] = ch
	c.syncMu.Unlock()
	defer func() { c.syncMu.Lock(); delete(c.pending, key); c.syncMu.Unlock() }()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("%s runtime disconnected", c.label)
	case msg := <-ch:
		if msg.Error != nil {
			return fmt.Errorf("%s %s failed (%d): %s", c.label, method, msg.Error.Code, (Redactor{}).Text(msg.Error.Message))
		}
		if out != nil {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	}
}
func (c *codexClient) reply(id json.RawMessage, result any) error {
	return c.send(map[string]any{"id": id, "result": result})
}
func (c *codexClient) read(out io.Reader) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var msg codexEnvelope
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.Method == "" {
			c.syncMu.Lock()
			ch := c.pending[string(msg.ID)]
			c.syncMu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
				}
			}
			continue
		}
		if len(msg.ID) == 0 {
			switch msg.Method {
			case "item/started", "item/completed", "thread/tokenUsage/updated", "turn/completed":
			default:
				continue
			}
		}
		var params struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		c.syncMu.Lock()
		ch := c.threads[params.ThreadID]
		c.syncMu.Unlock()
		if ch != nil {
			select {
			case ch <- msg:
			case <-c.done:
				return
			}
			continue
		}
		if len(msg.ID) > 0 {
			_ = c.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "No active ADC handler for this request; use ADC decisions for human input"}})
		}
	}
	c.Close()
}
func (c *codexClient) subscribe(id string) chan codexEnvelope {
	ch := make(chan codexEnvelope, 256)
	c.syncMu.Lock()
	c.threads[id] = ch
	c.syncMu.Unlock()
	return ch
}
func (c *codexClient) unsubscribe(id string) {
	c.syncMu.Lock()
	delete(c.threads, id)
	c.syncMu.Unlock()
}
func (c *codexClient) Close() {
	c.stopOnce.Do(func() {
		_ = c.in.Close()
		if c.label == "Claude" {
			// Give the official SDK bridge time to close its native sessions.
			select {
			case <-c.exited:
			case <-time.After(2 * time.Second):
			}
		}
		c.closeOnce.Do(func() { close(c.done) })
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
}
func (e *Engine) codexClient(ctx context.Context, a Account) (*codexClient, error) {
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
	c, err := startCodex(ctx, filepath.Join(e.Store.Dir, "providers", "codex", a.ID))
	if err == nil {
		e.codexClients[a.ID] = c
	}
	return c, err
}
func (e *Engine) codexModels(ctx context.Context, a Account) ([]Model, error) {
	c, err := e.codexClient(ctx, a)
	if err != nil {
		return nil, err
	}
	var status codexAccountStatus
	if err = c.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &status); err != nil {
		return nil, err
	}
	if status.Account == nil || status.Account.Type != "chatgpt" {
		return nil, fmt.Errorf("sign in to this Codex subscription from Connections")
	}
	models := []Model{}
	cursor := ""
	for {
		var page struct {
			Data       []struct{ ID, Model, DisplayName string }
			NextCursor *string
		}
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err = c.Call(ctx, "model/list", params, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Data {
			id := m.Model
			if id == "" {
				id = m.ID
			}
			if Family(id) != "" {
				models = append(models, Model{id, m.DisplayName, Family(id)})
			}
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		if *page.NextCursor == cursor {
			return nil, fmt.Errorf("Codex model catalog repeated a cursor")
		}
		cursor = *page.NextCursor
	}
	return models, nil
}
