package adc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GatewayTool contains discovery data only, never a server command, credential
// environment or authentication header. Fingerprint also binds server config.
type GatewayTool struct {
	ID, Org, Connection, Name, Description, Fingerprint string
	ConnectionRevision                                  string
	Schema                                              json.RawMessage
	Annotations                                         *mcp.ToolAnnotations
}

type gatewaySession struct {
	Session  gatewayClient
	Redactor Redactor
	Revision string
}

type gatewayClient interface {
	Tools(context.Context, *mcp.ListToolsParams) iter.Seq2[*mcp.Tool, error]
	CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
	Close() error
}

func connectionRevision(c Connection) string { b, _ := json.Marshal(c); return digest(string(b)) }

// Calls remain in trusted ADC. No gateway HTTP endpoint or reusable credential
// is exposed to model-controlled code. The runtime receives tool specs only.
func (s *Store) openGateway(ctx context.Context, org, id string) (*gatewaySession, error) {
	var conn Connection
	if s.Get(id, &conn) != nil || conn.Org != org {
		return nil, fmt.Errorf("connection is not available in this organization")
	}
	redactor := Redactor{}
	unseal := func(values map[string]string) (map[string]string, error) {
		out := map[string]string{}
		for key, sealed := range values {
			value, err := s.Unseal(sealed)
			if err != nil {
				return nil, fmt.Errorf("connection credentials cannot be opened")
			}
			out[key] = value
			// Boolean switches are configuration, not secret values. Redacting
			// true would also corrupt names such as truenas_system_info.
			if value != "true" && value != "false" {
				redactor.Values = append(redactor.Values, value)
			}
			if scheme, token, ok := strings.Cut(value, " "); ok && (strings.EqualFold(scheme, "Bearer") || strings.EqualFold(scheme, "Basic")) && token != "" {
				redactor.Values = append(redactor.Values, token)
			}
		}
		return out, nil
	}
	var transport mcp.Transport
	switch conn.Transport {
	case "github":
		return s.openGitHubGateway(conn)
	case "stdio":
		if !filepath.IsAbs(conn.Command) {
			return nil, fmt.Errorf("gateway stdio commands must use an administrator-configured absolute path")
		}
		env, err := unseal(conn.Env)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(conn.Command, conn.Args...)
		// MCP servers are trusted integrations, but do not inherit ADC/provider
		// credentials, host CLI configuration, agent sockets or worker state.
		private, err := os.MkdirTemp(s.Dir, "gateway-")
		if err != nil {
			return nil, err
		}
		cmd.Dir = private
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + private, "LANG=C.UTF-8"}
		keys := []string{}
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if strings.ContainsAny(key, "=\x00") || key == "" || strings.ContainsRune(env[key], 0) {
				os.RemoveAll(private)
				return nil, fmt.Errorf("invalid configured MCP environment")
			}
			cmd.Env = append(cmd.Env, key+"="+env[key])
		}
		cmd.Stderr = io.Discard
		configureGatewayProcess(cmd)
		transport = &gatewayCommandTransport{cmd: cmd, private: private}
	case "http":
		u, err := url.Parse(conn.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, fmt.Errorf("configure a valid HTTP MCP endpoint without URL credentials or fragments")
		}
		redactor.Values = append(redactor.Values, conn.URL)
		for _, values := range u.Query() {
			redactor.Values = append(redactor.Values, values...)
		}
		headers, err := unseal(conn.Headers)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Transport: gatewayHeaders{headers: headers, base: http.DefaultTransport}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("MCP redirects require explicit connection configuration")
		}}
		transport = &mcp.StreamableClientTransport{Endpoint: conn.URL, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}
	default:
		return nil, fmt.Errorf("gateway supports stdio and Streamable HTTP only")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "adc-gateway", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("MCP connection failed: %s", redactor.Text(err.Error()))
	}
	return &gatewaySession{Session: session, Redactor: redactor, Revision: connectionRevision(conn)}, nil
}

type gatewayHeaders struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t gatewayHeaders) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	response, err := t.base.RoundTrip(req)
	if err == nil {
		response.Body = &gatewayLimitedBody{ReadCloser: response.Body, remaining: 8 << 20}
	}
	return response, err
}

type gatewayLimitedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *gatewayLimitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, fmt.Errorf("MCP response exceeds 8 MiB")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

// Pair SDK process cleanup with private HOME cleanup, including failed starts.
type gatewayCommandTransport struct {
	cmd     *exec.Cmd
	private string
}

func (t *gatewayCommandTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		os.RemoveAll(t.private)
		return nil, err
	}
	stdin, err := t.cmd.StdinPipe()
	if err != nil {
		stdout.Close()
		os.RemoveAll(t.private)
		return nil, err
	}
	if err := t.cmd.Start(); err != nil {
		stdout.Close()
		stdin.Close()
		os.RemoveAll(t.private)
		return nil, err
	}
	transport := &mcp.IOTransport{Reader: &gatewayLimitedBody{ReadCloser: stdout, remaining: 8 << 20}, Writer: stdin}
	c, err := transport.Connect(ctx)
	if err != nil {
		stopGatewayProcess(t.cmd)
		_ = t.cmd.Wait()
		os.RemoveAll(t.private)
		return nil, err
	}
	return &gatewayCommandConnection{Connection: c, cmd: t.cmd, private: t.private}, nil
}

type gatewayCommandConnection struct {
	mcp.Connection
	cmd     *exec.Cmd
	private string
	once    sync.Once
	err     error
}

func (c *gatewayCommandConnection) Close() error {
	c.once.Do(func() {
		c.err = c.Connection.Close()
		done := make(chan struct{})
		go func() { _ = c.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			stopGatewayProcess(c.cmd)
			<-done
		}
		// A trusted server can still leave helpers behind; remove its process group.
		stopGatewayProcess(c.cmd)
		_ = os.RemoveAll(c.private)
	})
	return c.err
}

func gatewayCatalog(ctx context.Context, g *gatewaySession, org, connection string) ([]GatewayTool, error) {
	result := []GatewayTool{}
	seen := map[string]bool{}
	for tool, err := range g.Session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("MCP discovery failed: %s", g.Redactor.Text(err.Error()))
		}
		if tool.Name == "" || seen[tool.Name] || len(result) >= 1000 {
			return nil, fmt.Errorf("invalid or excessive MCP tool catalog")
		}
		seen[tool.Name] = true
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("invalid MCP input schema")
		}
		if len(schema) > 128<<10 {
			return nil, fmt.Errorf("MCP tool schema exceeds 128 KiB")
		}
		metadata, err := json.Marshal(tool)
		if err != nil {
			return nil, err
		}
		fingerprint := digest(g.Revision + ":" + string(metadata))
		// Titles and other metadata can echo connection secrets too.
		var annotations *mcp.ToolAnnotations
		if tool.Annotations != nil {
			b, err := json.Marshal(tool.Annotations)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(gatewayRedactedJSON(g.Redactor, b), &annotations); err != nil {
				return nil, fmt.Errorf("invalid redacted tool metadata")
			}
		}
		result = append(result, GatewayTool{ID: digest(org + ":" + connection + ":" + tool.Name), Org: org, Connection: connection, ConnectionRevision: g.Revision, Name: g.Redactor.Text(tool.Name), Description: g.Redactor.Text(tool.Description), Fingerprint: fingerprint, Schema: gatewayRedactedJSON(g.Redactor, schema), Annotations: annotations})
	}
	return result, nil
}

func (s *Store) DiscoverGateway(ctx context.Context, org, connection string) ([]GatewayTool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	g, err := s.openGateway(ctx, org, connection)
	if err != nil {
		return nil, err
	}
	defer g.Session.Close()
	tools, err := gatewayCatalog(ctx, g, org, connection)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var current Connection
	if s.Get(connection, &current) != nil || current.Org != org || connectionRevision(current) != g.Revision {
		return nil, fmt.Errorf("connection changed during discovery; refresh")
	}
	writes := []Write{}
	for _, tool := range tools {
		writes = append(writes, Write{"gateway-tool", org, connection, "discovered", tool.ID, tool})
	}
	return tools, s.Batch(writes...)
}

func validateGatewayArguments(schema json.RawMessage, arguments json.RawMessage) error {
	if len(arguments) > 2<<20 {
		return fmt.Errorf("tool arguments exceed 2 MiB")
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return fmt.Errorf("tool input schema is invalid")
	}
	resolved, err := parsed.Resolve(&jsonschema.ResolveOptions{Loader: func(*url.URL) (*jsonschema.Schema, error) {
		return nil, fmt.Errorf("external schema references are not enabled")
	}})
	if err != nil {
		return fmt.Errorf("tool input schema cannot be resolved: %w", err)
	}
	var value any
	if err := json.Unmarshal(arguments, &value); err != nil {
		return fmt.Errorf("tool arguments are invalid JSON")
	}
	if err := resolved.Validate(value); err != nil {
		return fmt.Errorf("arguments do not match tool schema: %w", err)
	}
	return nil
}

// invokeGateway requires an admission callback after fresh discovery and input
// validation. The caller records durable intent in that callback before any
// side effect can be dispatched. It must never supply model-controlled policy.
func (s *Store) invokeGateway(ctx context.Context, expected GatewayTool, arguments json.RawMessage, admit func() error) (string, error) {
	if admit == nil {
		return "", fmt.Errorf("MCP invocation requires explicit admission")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	g, err := s.openGateway(ctx, expected.Org, expected.Connection)
	if err != nil {
		return "", err
	}
	defer g.Session.Close()
	catalog, err := gatewayCatalog(ctx, g, expected.Org, expected.Connection)
	if err != nil {
		return "", err
	}
	found := false
	for _, tool := range catalog {
		if tool.ID != expected.ID {
			continue
		}
		if tool.Name != expected.Name || tool.Fingerprint != expected.Fingerprint {
			return "", fmt.Errorf("tool changed; inspect and approve its current definition")
		}
		found = true
		if err := validateGatewayArguments(tool.Schema, arguments); err != nil {
			return "", err
		}
	}
	if !found {
		return "", fmt.Errorf("tool is no longer advertised by this connection")
	}
	if err := admit(); err != nil {
		return "", err
	}
	result, err := g.Session.CallTool(ctx, &mcp.CallToolParams{Name: expected.Name, Arguments: arguments})
	if err != nil {
		return "", fmt.Errorf("MCP outcome is uncertain: %s", g.Redactor.Text(err.Error()))
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("MCP result cannot be recorded")
	}
	if len(b) > 2<<20 {
		return "", fmt.Errorf("MCP result exceeds 2 MiB; inspect outcome before retrying")
	}
	return string(gatewayRedactedJSON(g.Redactor, b)), nil
}

// Redact decoded strings so escaped secrets cannot leak and replacement text
// never changes JSON syntax, booleans, or numeric precision.
func gatewayRedactedJSON(redactor Redactor, raw []byte) json.RawMessage {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return json.RawMessage(`"[invalid JSON]"`)
	}
	var scrub func(any) any
	scrub = func(value any) any {
		switch v := value.(type) {
		case string:
			return redactor.Text(v)
		case []any:
			for i, item := range v {
				v[i] = scrub(item)
			}
			return v
		case map[string]any:
			out := map[string]any{}
			for key, item := range v {
				out[redactor.Text(key)] = scrub(item)
			}
			return out
		default:
			return value
		}
	}
	result, _ := json.Marshal(scrub(value))
	return result
}
