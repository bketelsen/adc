package adc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type githubActorKey struct{}
type githubActor struct {
	Run, Operation    string
	Arguments         json.RawMessage
	MutationAttempted *bool
}
type githubFixtureConfig struct {
	API    string
	Remote func(string, string) string
}
type githubGateway struct {
	store      *Store
	connection Connection
	token, api string
	client     *http.Client
}
type githubArguments struct {
	Owner, Repository, Ref, SourceRun, Commit, Title, Body string `json:",omitempty"`
	Number, Index                                          int    `json:",omitempty"`
}

var githubName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var githubRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]{0,199}$`)

func validGitHubRepository(owner, repo string) bool {
	return githubName.MatchString(owner) && githubName.MatchString(repo) && !strings.Contains(owner, ".")
}
func (g *githubGateway) allowed(owner, repo string) bool {
	if !validGitHubRepository(owner, repo) {
		return false
	}
	for _, rule := range g.connection.Args {
		if strings.EqualFold(rule, owner+"/"+repo) || strings.EqualFold(rule, owner+"/*") {
			return true
		}
	}
	return false
}
func (s *Store) openGitHubGateway(conn Connection) (*gatewaySession, error) {
	if len(conn.Args) == 0 || len(conn.Args) > 100 {
		return nil, fmt.Errorf("configure 1–100 allowed owner/repository or owner/* scopes")
	}
	token, err := s.Unseal(conn.Headers["GitHubToken"])
	if err != nil || token == "" {
		return nil, fmt.Errorf("GitHub connection needs its own sealed access token")
	}
	g := &githubGateway{store: s, connection: conn, token: token, api: "https://api.github.com", client: &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("GitHub redirects are not authorized") }}}
	if s.githubFixture != nil {
		g.api = s.githubFixture.API
	}
	redactor := Redactor{Values: []string{token, base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))}}
	return &gatewaySession{Session: g, Revision: connectionRevision(conn), Redactor: redactor}, nil
}
func (*githubGateway) Close() error { return nil }
func (*githubGateway) Tools(context.Context, *mcp.ListToolsParams) iter.Seq2[*mcp.Tool, error] {
	return func(yield func(*mcp.Tool, error) bool) {
		for _, spec := range []struct {
			name, description string
			required          []string
			read              bool
		}{
			{"github_repository", "Read repository metadata and token access. Scope is restricted by the connection's repository allowlist.", nil, true},
			{"github_pull_request", "Read one pull request including merged/draft/head/base state.", []string{"Number"}, true},
			{"github_release", "Read a release at an exact tag Ref.", []string{"Ref"}, true},
			{"github_checks", "Read check runs at an exact commit Ref.", []string{"Ref"}, true},
			{"github_fetch", "Fetch an authenticated repository Ref into this protected run as a credential-free Git bundle. Returns the isolated Path and exact Commit. Replaying returns the original import location and does not recreate deleted files. GitHub credentials never enter the worker.", []string{"Ref"}, true},
			{"github_draft_pr", "Publish one exact independently reviewed source Commit as an ADC-owned branch and draft PR. Requires publication authorization and gateway approval. SourceRun/Index select registered code; Ref is the target base branch. Title/Body describe the reviewed change. Reconcile the same operation after interruption; never invent another key to repeat publication. No merge, release or deployment.", []string{"SourceRun", "Commit", "Ref", "Title", "Body"}, false},
		} {
			properties := map[string]any{"Owner": map[string]any{"type": "string"}, "Repository": map[string]any{"type": "string"}}
			for _, key := range spec.required {
				typ := "string"
				if key == "Number" {
					typ = "integer"
				}
				properties[key] = map[string]any{"type": typ}
			}
			if spec.name == "github_draft_pr" {
				properties["Index"] = map[string]any{"type": "integer", "minimum": 0}
			}
			tool := &mcp.Tool{Name: spec.name, Description: spec.description, InputSchema: map[string]any{"type": "object", "properties": properties, "required": append([]string{"Owner", "Repository"}, spec.required...), "additionalProperties": false}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: spec.read}}
			if !yield(tool, nil) {
				return
			}
		}
	}
}
func (g *githubGateway) request(ctx context.Context, method, path string, body any) (json.RawMessage, int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.api+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("GitHub request failed: %w", err)
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if len(b) > 2<<20 {
		return nil, response.StatusCode, fmt.Errorf("GitHub result exceeds 2 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, fmt.Errorf("GitHub %s returned HTTP %d", method, response.StatusCode)
	}
	if !json.Valid(b) {
		return nil, response.StatusCode, fmt.Errorf("GitHub returned non-JSON data")
	}
	return b, response.StatusCode, nil
}
func (g *githubGateway) CallTool(ctx context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(p.Arguments)
	if err != nil {
		return nil, err
	}
	var args githubArguments
	if err = json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if !g.allowed(args.Owner, args.Repository) {
		return nil, fmt.Errorf("repository is outside this GitHub connection's allowed scope")
	}
	if args.Ref != "" && (!githubRef.MatchString(args.Ref) || strings.Contains(args.Ref, "..")) {
		return nil, fmt.Errorf("use a bounded exact branch, tag or commit reference")
	}
	base := "/repos/" + args.Owner + "/" + args.Repository
	var data any
	switch p.Name {
	case "github_repository":
		data, _, err = g.request(ctx, "GET", base, nil)
	case "github_pull_request":
		if args.Number < 1 {
			return nil, fmt.Errorf("positive pull request number required")
		}
		data, _, err = g.request(ctx, "GET", fmt.Sprintf("%s/pulls/%d", base, args.Number), nil)
	case "github_release":
		data, _, err = g.request(ctx, "GET", base+"/releases/tags/"+githubRefPath(args.Ref), nil)
	case "github_checks":
		data, err = g.checks(ctx, base, args.Ref)
	case "github_fetch":
		data, err = g.fetch(ctx, args)
	case "github_draft_pr":
		data, err = g.deliver(ctx, args)
	default:
		return nil, fmt.Errorf("unsupported built-in GitHub tool")
	}
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{StructuredContent: map[string]any{"repository": args.Owner + "/" + args.Repository, "observed_at": now(), "data": data}}, nil
}

// Never present a partial CI observation as the complete set of checks.
func (g *githubGateway) checks(ctx context.Context, base, ref string) (any, error) {
	var checks []json.RawMessage
	for page := 1; page <= 10; page++ {
		raw, _, err := g.request(ctx, "GET", fmt.Sprintf("%s/commits/%s/check-runs?per_page=100&page=%d", base, githubRefPath(ref), page), nil)
		if err != nil {
			return nil, err
		}
		var response struct {
			Total  int               `json:"total_count"`
			Checks []json.RawMessage `json:"check_runs"`
		}
		if json.Unmarshal(raw, &response) != nil || response.Total < 0 || response.Total > 1000 || response.Checks == nil {
			return nil, fmt.Errorf("invalid or oversized GitHub check observation (maximum 1000 checks)")
		}
		checks = append(checks, response.Checks...)
		if len(checks) == response.Total {
			return map[string]any{"total_count": len(checks), "check_runs": checks}, nil
		}
		if len(response.Checks) != 100 || len(checks) > response.Total {
			break
		}
	}
	return nil, fmt.Errorf("GitHub checks changed during pagination or exceed the observation limit; obtain a fresh observation")
}

func (g *githubGateway) caller(ctx context.Context) (Run, error) {
	actor, _ := ctx.Value(githubActorKey{}).(githubActor)
	var r Run
	var task Assignment
	if actor.Run == "" || g.store.Get(actor.Run, &r) != nil || r.Org != g.connection.Org || r.Execution != "protected" || r.State != "running" || r.Superseded || !Subset([]string{g.connection.ID}, r.Tools) || g.store.Get(r.Task, &task) != nil || task.Org != r.Org || task.State == "paused" || task.State == "cancelled" {
		return r, fmt.Errorf("built-in Git operations require this active protected caller and connection grant")
	}
	return r, nil
}

func githubRefPath(ref string) string {
	parts := strings.Split(ref, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
