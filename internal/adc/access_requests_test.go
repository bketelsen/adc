package adc

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestAccessBundleCoalescesSurvivesReopenAndApprovesOnce(t *testing.T) {
	s, _, conn, calls := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	run := taskRuns(s, "task")[0]
	run.Execution = "protected"
	run.Tools = nil
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Execution = "protected"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	wants := []AccessWant{{Tool: tools[0].ID, Operation: "initial-inventory", Arguments: json.RawMessage(`{"target":"fixture-nas"}`)}}
	request, err := s.RequestAccess(run.ID, "Inspect the selected fixture NAS", wants)
	must(t, err)
	setRunning(t, s, &run)
	again, err := s.RequestAccess(run.ID, "Inspect the selected fixture NAS", wants)
	must(t, err)
	if again.ID != request.ID || again.Revision != request.Revision || len(taskDecisions(s, task.ID)) != 1 {
		t.Fatal("equivalent request produced another prompt")
	}
	child := run
	child.ID = ID()
	child.Parent = run.ID
	child.State = "running"
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	updated, err := s.RequestAccess(child.ID, "Independent fixture check", wants)
	must(t, err)
	if updated.ID != request.ID || updated.Revision != 2 || len(updated.Runs) != 2 {
		t.Fatal("requests did not coalesce")
	}
	if s.ResolveAccess("owner", "org", request.ID, 1, "operation", "") == nil {
		t.Fatal("stale human approval accepted")
	}
	if s.ResolveAccess("outsider", "org", updated.ID, 2, "operation", "") == nil {
		t.Fatal("nonmember approved access")
	}
	task.State = "needs input"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	// Reopen authoritative state, without copying grants into provider memory.
	reopened, err := Open(s.Dir)
	must(t, err)
	defer reopened.Close()
	must(t, reopened.ResolveAccess("owner", "org", updated.ID, 2, "operation", "Approved fixture observation"))
	must(t, reopened.Get(task.ID, &task))
	if task.State != "queued" {
		t.Fatal("approved assignment kept stale needs-input state")
	}
	if reopened.ResolveAccess("owner", "org", updated.ID, 2, "operation", "") == nil {
		t.Fatal("approval replay accepted")
	}
	must(t, reopened.Get(child.ID, &child))
	if child.State != "queued" || !Subset([]string{conn.ID}, child.Tools) {
		t.Fatal("approved worker did not resume with bounded connection")
	}
	child.State = "running"
	must(t, reopened.Put("run", child.Org, child.Task, child.State, child.ID, child))
	_, err = reopened.CallGateway(context.Background(), child.ID, tools[0].ID, "initial-inventory", wants[0].Arguments)
	must(t, err)
	_, err = reopened.CallGateway(context.Background(), child.ID, tools[0].ID, "initial-inventory", wants[0].Arguments)
	must(t, err)
	if calls.Load() != 1 {
		t.Fatal("approved operation was duplicated")
	}
	if _, err = reopened.CallGateway(context.Background(), child.ID, tools[0].ID, "another-operation", wants[0].Arguments); err == nil {
		t.Fatal("single-operation approval expanded")
	}
	if _, err = reopened.CallGateway(context.Background(), child.ID, tools[0].ID, "initial-inventory", json.RawMessage(`{"target":"other"}`)); err == nil {
		t.Fatal("approved target expanded")
	}
}

func TestDeclinedAccessDoesNotExpandConnections(t *testing.T) {
	s, _, conn, _ := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	run := taskRuns(s, "task")[0]
	run.Execution = "protected"
	run.Tools = nil
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Execution = "protected"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	r, err := s.RequestAccess(run.ID, "Proposed access", []AccessWant{{Tool: tools[0].ID}})
	must(t, err)
	must(t, s.ResolveAccess("owner", "org", r.ID, r.Revision, "decline", "Keep investigation local"))
	must(t, s.Get(run.ID, &run))
	must(t, s.Get(task.ID, &task))
	if len(run.Tools) != 0 || len(task.Capabilities) != 0 || run.State != "queued" {
		t.Fatal("decline expanded authority or lost continuation")
	}
}

func TestAccessApprovalIncludesAbsentOptionalArguments(t *testing.T) {
	s, _, conn, _ := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	run := taskRuns(s, "task")[0]
	run.Execution = "protected"
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Execution = "protected"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	args := json.RawMessage(`{"target":"fixture-nas"}`)
	request, err := s.RequestAccess(run.ID, "Observe exactly this target", []AccessWant{{Tool: tools[0].ID, Arguments: args}})
	must(t, err)
	must(t, s.ResolveAccess("owner", "org", request.ID, request.Revision, "assignment", ""))
	must(t, s.Get(run.ID, &run))
	setRunning(t, s, &run)
	if _, err := s.CallGateway(context.Background(), run.ID, tools[0].ID, "added-option", json.RawMessage(`{"target":"fixture-nas","force":true}`)); err == nil {
		t.Fatal("unproposed optional key allowed")
	}
	_, err = s.CallGateway(context.Background(), run.ID, tools[0].ID, "exact", args)
	must(t, err)
	if _, err := s.RequestAccess(run.ID, "Contradictory proposal", []AccessWant{{Tool: tools[0].ID, Arguments: args, Constraints: []ArgumentConstraint{{Pointer: "/target", Allowed: []json.RawMessage{json.RawMessage(`"other"`)}}}}}); err == nil {
		t.Fatal("inconsistent arguments and restrictions accepted")
	}
}

func TestPolicyRevocationDoesNotReviveOldGrants(t *testing.T) {
	s, _, conn, _ := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	tool := tools[0]
	p := ToolPolicy{Tool: tool.ID, Fingerprint: tool.Fingerprint, Mode: "allow", Class: "read"}
	must(t, s.SaveToolPolicy("owner", "org", p, 0))
	run := taskRuns(s, "task")[0]
	run.Execution = "protected"
	run.Tools = []string{conn.ID}
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Execution = "protected"
	task.Capabilities = s.initialCapabilities("org", run.Tools)
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	args := json.RawMessage(`{"target":"fixture-nas"}`)
	p.Mode = "approval"
	must(t, s.SaveToolPolicy("owner", "org", p, 1))
	p.Mode = "allow"
	must(t, s.SaveToolPolicy("owner", "org", p, 2))
	if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "revived-standing", args); err == nil {
		t.Fatal("old standing grant revived")
	}
	request, err := s.RequestAccess(run.ID, "Fresh assignment approval", []AccessWant{{Tool: tool.ID, Arguments: args}})
	must(t, err)
	must(t, s.ResolveAccess("owner", "org", request.ID, request.Revision, "assignment", ""))
	must(t, s.Get(run.ID, &run))
	setRunning(t, s, &run)
	_, err = s.CallGateway(context.Background(), run.ID, tool.ID, "fresh", args)
	must(t, err)
	p.Mode = "deny"
	must(t, s.SaveToolPolicy("owner", "org", p, 3))
	p.Mode = "approval"
	must(t, s.SaveToolPolicy("owner", "org", p, 4))
	if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "revived-assignment", args); err == nil {
		t.Fatal("denied assignment grant revived")
	}
}

func TestChangedToolApprovalPreservesInstallationPolicy(t *testing.T) {
	s, server, conn, _ := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	p := ToolPolicy{Tool: tools[0].ID, Fingerprint: tools[0].Fingerprint, Mode: "approval", Class: "read", Constraints: []ArgumentConstraint{{Pointer: "/target", Allowed: []json.RawMessage{json.RawMessage(`"fixture-nas"`)}}}}
	must(t, s.SaveToolPolicy("owner", "org", p, 0))
	mcp.AddTool(server, &mcp.Tool{Name: "inventory", Description: "Changed metadata"}, func(context.Context, *mcp.CallToolRequest, struct {
		Target string `json:"target"`
	}) (*mcp.CallToolResult, any, error) {
		t.Fatal("changed tool called without classification")
		return nil, nil, nil
	})
	tools, err = s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	run := taskRuns(s, "task")[0]
	run.Execution = "protected"
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Execution = "protected"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	request, err := s.RequestAccess(run.ID, "Changed tool access", []AccessWant{{Tool: tools[0].ID, Arguments: json.RawMessage(`{"target":"production"}`)}})
	must(t, err)
	if s.ResolveAccess("owner", "org", request.ID, request.Revision, "assignment", "") == nil {
		t.Fatal("changed tool silently reclassified")
	}
	var policy ToolPolicy
	must(t, s.Get("policy-"+p.Tool, &policy))
	if policy.Revision != 1 || policy.Class != "read" || len(policy.Constraints) != 1 || policy.Fingerprint != p.Fingerprint {
		t.Fatal("installation restrictions lost")
	}
}

func TestStandingApprovalsAreAlternativesUnderInstallationCeiling(t *testing.T) {
	s, _, conn, _ := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	tool := tools[0]
	p := ToolPolicy{Tool: tool.ID, Fingerprint: tool.Fingerprint, Mode: "approval", Class: "read", Constraints: []ArgumentConstraint{{Pointer: "/target", Allowed: []json.RawMessage{json.RawMessage(`"x"`), json.RawMessage(`"y"`)}}}}
	must(t, s.SaveToolPolicy("owner", "org", p, 0))
	run := taskRuns(s, "task")[0]
	run.Execution = "protected"
	run.Tools = []string{conn.ID}
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Execution = "protected"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	var first []CapabilityGrant
	for _, target := range []string{"x", "y"} {
		args, _ := json.Marshal(map[string]string{"target": target})
		request, err := s.RequestAccess(run.ID, "Standing observation", []AccessWant{{Tool: tool.ID, Arguments: args}})
		must(t, err)
		must(t, s.ResolveAccess("owner", "org", request.ID, request.Revision, "standing", ""))
		must(t, s.Get(run.ID, &run))
		setRunning(t, s, &run)
		if target == "x" {
			first = s.initialCapabilities("org", run.Tools)
		}
	}
	var policy ToolPolicy
	must(t, s.Get("policy-"+tool.ID, &policy))
	if len(policy.StandingRules) != 2 || len(policy.Constraints) != 1 {
		t.Fatal("standing approvals changed the installation ceiling")
	}
	if len(list[ToolPolicy](s, "tool-policy-history", "org")) < 3 {
		t.Fatal("standing history missing")
	}
	must(t, s.Get(task.ID, &task))
	task.Capabilities = s.initialCapabilities("org", run.Tools)
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	for _, target := range []string{"x", "y"} {
		args, _ := json.Marshal(map[string]string{"target": target})
		_, err = s.CallGateway(context.Background(), run.ID, tool.ID, "new-"+target, args)
		must(t, err)
	}
	task.Capabilities = first
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "old-y", json.RawMessage(`{"target":"y"}`)); err == nil {
		t.Fatal("new standing approval widened old assignment")
	}
	_, err = s.CallGateway(context.Background(), run.ID, tool.ID, "old-x", json.RawMessage(`{"target":"x"}`))
	must(t, err)
}
