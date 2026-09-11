package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGatewayStdioFixtureProcess(t *testing.T) {
	if os.Getenv("ADC_GATEWAY_FIXTURE") != "enabled" {
		return
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "synthetic-stdio", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "inspect_fixture", Description: "Inspect synthetic fixture only", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		if os.Getenv("ADC_HOST_SYNTHETIC_SECRET") != "" {
			return nil, nil, fmt.Errorf("host environment leaked")
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "stdio-ok " + os.Getenv("ADC_GATEWAY_TOKEN")}}}, nil, nil
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestGatewayStdioCredentialsStayInConnectionProcess(t *testing.T) {
	s, _, _, _ := fixture(t)
	t.Setenv("ADC_HOST_SYNTHETIC_SECRET", "host-private-fixture")
	binary, err := os.Executable()
	must(t, err)
	enabled, err := s.Seal("enabled")
	must(t, err)
	boolean, err := s.Seal("true")
	must(t, err)
	token, err := s.Seal("stdio-private-fixture")
	must(t, err)
	c := Connection{ID: "stdio-fixture", Org: "org", Name: "Synthetic stdio only", Transport: "stdio", Command: binary, Args: []string{"-test.run=^TestGatewayStdioFixtureProcess$"}, Env: map[string]string{"ADC_GATEWAY_FIXTURE": enabled, "ADC_GATEWAY_TOKEN": token, "ADC_BOOLEAN_SETTING": boolean}}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	tools, err := s.DiscoverGateway(context.Background(), c.Org, c.ID)
	must(t, err)
	if len(tools) != 1 {
		t.Fatal(tools)
	}
	result, err := s.invokeGateway(context.Background(), tools[0], json.RawMessage(`{}`), func() error { return nil })
	must(t, err)
	if !strings.Contains(result, "stdio-ok") || strings.Contains(result, "stdio-private-fixture") || strings.Contains(result, "host-private-fixture") {
		t.Fatal("stdio credentials or environment were exposed")
	}
	left, err := filepath.Glob(filepath.Join(s.Dir, "gateway-*"))
	must(t, err)
	if len(left) != 0 {
		t.Fatal("private connection homes not cleaned up", left)
	}
}

func gatewayFixture(t *testing.T) (*Store, *mcp.Server, Connection, *atomic.Int32) {
	t.Helper()
	s, _, _, _ := fixture(t)
	server := mcp.NewServer(&mcp.Implementation{Name: "synthetic-gateway-fixture", Version: "1"}, nil)
	calls := &atomic.Int32{}
	mcp.AddTool(server, &mcp.Tool{Name: "inventory", Description: "Synthetic inventory"}, func(_ context.Context, _ *mcp.CallToolRequest, p struct {
		Target string `json:"target"`
	}) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "inventory:" + p.Target + ":synthetic-secret"}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-secret" {
			http.Error(w, "no credentials", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	sealed, err := s.Seal("Bearer synthetic-secret")
	must(t, err)
	conn := Connection{ID: "gateway-fixture", Org: "org", Name: "Fixture only", Transport: "http", URL: httpServer.URL, Headers: map[string]string{"Authorization": sealed}}
	must(t, s.Put("connection", conn.Org, "", "", conn.ID, conn))
	return s, server, conn, calls
}

func TestGatewayFreshDiscoveryAdmissionAndSchema(t *testing.T) {
	s, server, conn, calls := gatewayFixture(t)
	ctx := context.Background()
	tools, err := s.DiscoverGateway(ctx, "org", conn.ID)
	must(t, err)
	if len(tools) != 1 || tools[0].Name != "inventory" {
		t.Fatal(tools)
	}
	if _, err := s.DiscoverGateway(ctx, "other-org", conn.ID); err == nil {
		t.Fatal("cross-organization discovery")
	}
	arguments := json.RawMessage(`{"target":"fixture-nas"}`)
	if _, err := s.invokeGateway(ctx, tools[0], arguments, nil); err == nil {
		t.Fatal("missing admission accepted")
	}
	if _, err := s.invokeGateway(ctx, tools[0], json.RawMessage(`{"target":123}`), func() error { t.Fatal("invalid input reached admission"); return nil }); err == nil {
		t.Fatal("invalid arguments accepted")
	}
	if _, err := s.invokeGateway(ctx, tools[0], arguments, func() error { return fmt.Errorf("denied") }); err == nil {
		t.Fatal("denied admission executed")
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized side effect")
	}
	result, err := s.invokeGateway(ctx, tools[0], arguments, func() error { return nil })
	must(t, err)
	if calls.Load() != 1 || !strings.Contains(result, "fixture-nas") {
		t.Fatal(result)
	}
	if strings.Contains(result, "synthetic-secret") {
		t.Fatal("credential exposed in tool output")
	}
	// Schema/description updates require renewed review, even under the same name.
	mcp.AddTool(server, &mcp.Tool{Name: "inventory", Description: "Changed behavior"}, func(context.Context, *mcp.CallToolRequest, struct {
		Target string `json:"target"`
	}) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		return &mcp.CallToolResult{}, nil, nil
	})
	if _, err := s.invokeGateway(ctx, tools[0], arguments, func() error { t.Fatal("stale definition reached admission"); return nil }); err == nil {
		t.Fatal("stale tool accepted")
	}
	if calls.Load() != 1 {
		t.Fatal("stale tool executed")
	}
	latest, err := s.DiscoverGateway(ctx, "org", conn.ID)
	must(t, err)
	if latest[0].Fingerprint == tools[0].Fingerprint {
		t.Fatal("tool change did not invalidate fingerprint")
	}
}

func TestGatewayNoRedirectCredentialForwarding(t *testing.T) {
	s, _, conn, _ := gatewayFixture(t)
	leaked := &atomic.Bool{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true); w.WriteHeader(500) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer origin.Close()
	conn.URL = origin.URL
	must(t, s.Put("connection", conn.Org, "", "", conn.ID, conn))
	if _, err := s.DiscoverGateway(context.Background(), conn.Org, conn.ID); err == nil {
		t.Fatal("redirect followed")
	}
	if leaked.Load() {
		t.Fatal("credentials forwarded to redirect endpoint")
	}
}

func TestGatewayJSONRedactionPreservesStructure(t *testing.T) {
	raw := json.RawMessage(`{"ok":true,"id":9007199254740993,"text":"secret\"value","nested":["secret\"value"]}`)
	out := gatewayRedactedJSON(Redactor{Values: []string{"secret\"value"}}, raw)
	if !json.Valid(out) || strings.Contains(string(out), "secret") || !strings.Contains(string(out), "9007199254740993") || !strings.Contains(string(out), `"ok":true`) {
		t.Fatal("redaction damaged structured result or exposed secret")
	}
}
