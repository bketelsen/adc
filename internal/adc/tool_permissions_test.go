package adc

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestGatewayGrantReplayRevocationAndNarrowing(t *testing.T) {
	s, _, conn, count := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	tool := tools[0]
	policy := ToolPolicy{Tool: tool.ID, Fingerprint: tool.Fingerprint, Mode: "allow", Class: "read", Constraints: []ArgumentConstraint{{Pointer: "/target", Allowed: []json.RawMessage{json.RawMessage(`"fixture-nas"`)}}}}
	if err := s.SaveToolPolicy("outsider", "org", policy, 0); err == nil {
		t.Fatal("nonmember changed policy")
	}
	must(t, s.SaveToolPolicy("owner", "org", policy, 0))
	if err := s.SaveToolPolicy("owner", "org", policy, 0); err == nil {
		t.Fatal("stale policy update accepted")
	}
	run := taskRuns(s, "task")[0]
	run.Tools = []string{conn.ID}
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	args := json.RawMessage(`{"target":"fixture-nas"}`)
	if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "inventory", args); err == nil {
		t.Fatal("standing grant retroactively applied to existing task")
	}
	task.Capabilities = s.initialCapabilities("org", run.Tools)
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "wrong-target", json.RawMessage(`{"target":"other-nas"}`)); err == nil {
		t.Fatal("target constraint escaped")
	}
	result, err := s.CallGateway(context.Background(), run.ID, tool.ID, "inventory", args)
	must(t, err)
	again, err := s.CallGateway(context.Background(), run.ID, tool.ID, "inventory", args)
	must(t, err)
	if again != result || count.Load() != 1 {
		t.Fatal("completed operation was repeated")
	}
	if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "inventory", json.RawMessage(`{"target":"fixture-nas","extra":true}`)); err == nil {
		t.Fatal("operation arguments changed")
	}
	child := run
	child.ID = ID()
	child.Parent = run.ID
	child.Tools = nil
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	if _, err := s.CallGateway(context.Background(), child.ID, tool.ID, "child", args); err == nil {
		t.Fatal("child recovered narrowed-away connection")
	}
	policy.Mode = "deny"
	must(t, s.SaveToolPolicy("owner", "org", policy, 1))
	if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "revoked", args); err == nil {
		t.Fatal("revoked policy used")
	}
	if count.Load() != 1 {
		t.Fatal("unexpected server operation")
	}
}

func TestArgumentConstraintsAreExactAndRequirePresence(t *testing.T) {
	_, value, err := canonicalArguments(json.RawMessage(`{"x/y":{"~id":9007199254740993},"targets":["one","two"]}`))
	must(t, err)
	if !constraintsMatch(value, []ArgumentConstraint{{Pointer: "/x~1y/~0id", Allowed: []json.RawMessage{json.RawMessage(`9007199254740993`)}}}) {
		t.Fatal("exact large identifier lost")
	}
	if constraintsMatch(value, []ArgumentConstraint{{Pointer: "/x~1y/~0id", Allowed: []json.RawMessage{json.RawMessage(`9007199254740992`)}}}) {
		t.Fatal("rounded identifier accepted")
	}
	if constraintsMatch(value, []ArgumentConstraint{{Pointer: "/missing", Allowed: []json.RawMessage{json.RawMessage(`null`)}}}) {
		t.Fatal("missing target accepted as null")
	}
	if !constraintsMatch(value, []ArgumentConstraint{{Pointer: "/targets/1", Allowed: []json.RawMessage{json.RawMessage(`"two"`)}}}) {
		t.Fatal("array target failed")
	}
}

func TestBulkPoliciesAreAtomicAndGrantCeilingsPersist(t *testing.T) {
	s, _, conn, calls := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	p := ToolPolicy{Tool: tools[0].ID, Fingerprint: tools[0].Fingerprint, Mode: "allow", Class: "read", Constraints: []ArgumentConstraint{{Pointer: "/target", Allowed: []json.RawMessage{json.RawMessage(`"fixture-nas"`)}}}}
	must(t, s.SaveToolPolicy("owner", "org", p, 0))
	run := taskRuns(s, "task")[0]
	run.Tools = []string{conn.ID}
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Capabilities = s.initialCapabilities("org", run.Tools)
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	p.Constraints = nil
	if s.SaveToolPolicies("owner", "org", []PolicyChange{{Policy: p, Expected: 1}, {Policy: ToolPolicy{Tool: "missing"}, Expected: 0}}) == nil {
		t.Fatal("invalid bulk save accepted")
	}
	var unchanged ToolPolicy
	must(t, s.Get("policy-"+p.Tool, &unchanged))
	if unchanged.Revision != 1 {
		t.Fatal("bulk save partially committed")
	}
	must(t, s.SaveToolPolicy("owner", "org", p, 1))
	if _, err := s.CallGateway(context.Background(), run.ID, p.Tool, "outside-old-ceiling", json.RawMessage(`{"target":"other"}`)); err == nil {
		t.Fatal("policy widening widened existing work")
	}
	_, err = s.CallGateway(context.Background(), run.ID, p.Tool, "within-old-ceiling", json.RawMessage(`{"target":"fixture-nas"}`))
	must(t, err)
	p.Constraints = []ArgumentConstraint{{Pointer: "/target", Allowed: []json.RawMessage{json.RawMessage(`"other"`)}}}
	must(t, s.SaveToolPolicy("owner", "org", p, 2))
	if _, err := s.CallGateway(context.Background(), run.ID, p.Tool, "tightened", json.RawMessage(`{"target":"fixture-nas"}`)); err == nil {
		t.Fatal("tightened policy ignored")
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected external dispatch")
	}
}

// A second service operation uses the same declarative gateway; this test does
// not publish anything or assert that arbitrary API tools are draft-only.
func TestGatewayPublicationCanBeScopedWithoutServiceAdapter(t *testing.T) {
	s, server, conn, _ := gatewayFixture(t)
	published := 0
	mcp.AddTool(server, &mcp.Tool{Name: "repository_action", Description: "Synthetic authenticated publication"}, func(_ context.Context, _ *mcp.CallToolRequest, args struct {
		Owner, Repository, Action string
		Draft                     bool
	}) (*mcp.CallToolResult, any, error) {
		published++
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "synthetic-draft-created"}}}, nil, nil
	})
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	var tool GatewayTool
	for _, candidate := range tools {
		if candidate.Name == "repository_action" {
			tool = candidate
		}
	}
	policy := ToolPolicy{Tool: tool.ID, Fingerprint: tool.Fingerprint, Mode: "allow", Class: "change", Constraints: []ArgumentConstraint{
		{Pointer: "/Owner", Allowed: []json.RawMessage{json.RawMessage(`"fixture-org"`)}},
		{Pointer: "/Repository", Allowed: []json.RawMessage{json.RawMessage(`"fixture-repo"`)}},
		{Pointer: "/Action", Allowed: []json.RawMessage{json.RawMessage(`"create_pull_request"`)}},
		{Pointer: "/Draft", Allowed: []json.RawMessage{json.RawMessage(`true`)}},
	}}
	must(t, s.SaveToolPolicy("owner", "org", policy, 0))
	run := taskRuns(s, "task")[0]
	run.Tools = []string{conn.ID}
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Capabilities = s.initialCapabilities("org", run.Tools)
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	args := map[string]any{"Owner": "fixture-org", "Repository": "fixture-repo", "Action": "create_pull_request", "Draft": true}
	for key, bad := range map[string]any{"Owner": "another-org", "Repository": "another-repo", "Action": "merge", "Draft": false} {
		original := args[key]
		args[key] = bad
		raw, _ := json.Marshal(args)
		if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "negative-"+key, raw); err == nil {
			t.Fatal("publication scope expanded", key)
		}
		args[key] = original
	}
	raw, _ := json.Marshal(args)
	_, err = s.CallGateway(context.Background(), run.ID, tool.ID, "draft-fixture", raw)
	must(t, err)
	_, err = s.CallGateway(context.Background(), run.ID, tool.ID, "draft-fixture", raw)
	must(t, err)
	if published != 1 {
		t.Fatal("authorized publication fixture did not execute exactly once")
	}
}
