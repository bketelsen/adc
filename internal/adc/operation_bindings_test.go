package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func operationFixture(t *testing.T) (*Store, *Engine, Run, GatewayTool, *atomic.Int32) {
	t.Helper()
	s, _, conn, calls := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	tool := tools[0]
	must(t, s.SaveToolPolicy("owner", "org", ToolPolicy{Tool: tool.ID, Fingerprint: tool.Fingerprint, Mode: "allow", Class: "change"}, 0))
	var task Assignment
	must(t, s.Get("task", &task))
	task.Execution = "protected"
	task.Capabilities = s.initialCapabilities("org", []string{conn.ID})
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	root := taskRuns(s, "task")[0]
	root.Execution = "protected"
	root.Tools = []string{conn.ID}
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	for _, a := range list[Agent](s, "agent", "org") {
		a.Tools = []string{conn.ID}
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	e := NewEngine(s)
	p := planFixtureInput()
	p.Start = true
	p.Steps = p.Steps[:2]
	p.Steps[1].DependsOn = nil
	_, err = e.saveExecutionPlan(root, p)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	var r Run
	must(t, s.Get(step.Run, &r))
	r.State = "running"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	return s, e, r, tool, calls
}

func TestOperationMixedReadBundleHasExplicitNarrowApproval(t *testing.T) {
	s, _, r, tool, _ := operationFixture(t)
	read := tool
	read.ID = "fixture-read"
	read.Name = "read-fixture"
	read.Fingerprint = "fixture-read-fingerprint"
	read.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
	must(t, s.Put("gateway-tool", r.Org, read.Connection, "discovered", read.ID, read))
	must(t, s.SaveToolPolicy("owner", r.Org, ToolPolicy{Tool: read.ID, Fingerprint: read.Fingerprint, Mode: "approval", Class: "read"}, 0))
	request, err := s.RequestAccess(r.ID, "Read and one canary", []AccessWant{operationWant(tool), {Tool: read.ID}})
	must(t, err)
	if !mixedOperationBundle(request.Entries) {
		t.Fatal("mixed bundle not identified")
	}
	if s.ResolveAccess("owner", r.Org, request.ID, request.Revision, "operation", "") == nil {
		t.Fatal("operation-only scope silently gained reusable read access")
	}
	must(t, s.ResolveAccess("owner", r.Org, request.ID, request.Revision, "mixed", "Exact operation plus listed read access"))
	setRunning(t, s, &r)
	var task Assignment
	must(t, s.Get(r.Task, &task))
	readGrant, mutationGrant := false, false
	for _, g := range task.Capabilities {
		if g.Approval != request.ID {
			continue
		}
		if g.Tool == read.ID {
			readGrant = g.Scope == "assignment" && g.Binding == nil
		}
		if g.Tool == tool.ID {
			mutationGrant = g.Scope == "operation" && g.Binding != nil
		}
	}
	if !readGrant || !mutationGrant {
		t.Fatal("mixed grant scope incorrect")
	}
}

func TestOperationMixedClassificationChangeIsVisible(t *testing.T) {
	s, e, r, tool, _ := operationFixture(t)
	read := tool
	read.ID = "fixture-read"
	read.Name = "read-fixture"
	read.Fingerprint = "fixture-read-fingerprint"
	must(t, s.Put("gateway-tool", r.Org, read.Connection, "discovered", read.ID, read))
	must(t, s.SaveToolPolicy("owner", r.Org, ToolPolicy{Tool: read.ID, Fingerprint: read.Fingerprint, Mode: "approval", Class: "read"}, 0))
	_, err := s.RequestAccess(r.ID, "mixed scope fixture", []AccessWant{operationWant(tool), {Tool: read.ID, Operation: "one-read", Arguments: json.RawMessage(`{"target":"fixture-canary"}`)}})
	must(t, err)
	view := NewWeb(s, e, false).permissionPage(r.Org, "").Requests[0]
	if !view.MixedAllowed || !view.OneOperation {
		t.Fatal("exact mixed bundle lacks both choices")
	}
	must(t, s.SaveToolPolicy("owner", r.Org, ToolPolicy{Tool: read.ID, Fingerprint: read.Fingerprint, Mode: "approval", Class: "change"}, 1))
	view = NewWeb(s, e, false).permissionPage(r.Org, "").Requests[0]
	if view.MixedAllowed || view.OneOperation {
		t.Fatal("changed read classification still offers invalid approval")
	}
}

func TestOperationStaleBranchDoesNotTrapAnotherRequest(t *testing.T) {
	s, e, a, tool, _ := operationFixture(t)
	want := operationWant(tool)
	first, err := s.RequestAccess(a.ID, "A canary", []AccessWant{want})
	must(t, err)
	var b Run
	must(t, s.Get(planStepByKey(t, s, "R2").Run, &b))
	setRunning(t, s, &b)
	want.Operation = "branch-b-canary"
	second, err := s.RequestAccess(b.ID, "B canary", []AccessWant{want})
	must(t, err)
	if first.ID == second.ID {
		t.Fatal("different operation owners coalesced")
	}
	d := Document{ID: "changed-A", Org: a.Org, Task: a.Task, Run: a.ID, Content: "changed after request", Revision: 1}
	must(t, s.Put("document", d.Org, d.Task, "", d.ID, d))
	planDispatch(e)
	var expired AccessRequest
	must(t, s.Get(first.ID, &expired))
	if expired.State != "stale" {
		t.Fatal("stale request not expired")
	}
	var decision Decision
	must(t, s.Get(first.Decision, &decision))
	if decision.State == "pending" {
		t.Fatal("expired approval still blocks attempt recovery")
	}
	must(t, s.Get(a.ID, &a))
	if a.State != "queued" {
		t.Fatal("owner not resumed to re-propose")
	}
	must(t, s.ResolveAccess("owner", b.Org, second.ID, second.Revision, "operation", "B remains approved"))
	if s.ResolveAccess("owner", a.Org, first.ID, first.Revision, "operation", "") == nil {
		t.Fatal("expired proposal approved")
	}
}

func TestOperationCompletedPlanFinalizationStaysBound(t *testing.T) {
	s, e, r, tool, calls := operationFixture(t)
	for _, key := range []string{"R1", "R2"} {
		step := planStepByKey(t, s, key)
		completePlanWorker(t, e, step)
		reviewPlanStep(t, e, step, "pass")
	}
	planDispatch(e)
	p := s.taskPlan(r.Task)
	if p.State != "complete" {
		t.Fatal("fixture not complete")
	}
	var root Run
	must(t, s.Get(p.Supervisor, &root))
	setRunning(t, s, &root)
	want := operationWant(tool)
	if _, err := s.CallGateway(context.Background(), root.ID, tool.ID, want.Operation, want.Arguments); err == nil {
		t.Fatal("completed plan re-enabled unbound mutation")
	}
	request, err := s.RequestAccess(root.ID, "Finalize the reviewed fixture", []AccessWant{want})
	must(t, err)
	if request.Entries[0].Binding.PlanEvidence == "" {
		t.Fatal("finalization lacks graph evidence pin")
	}
	must(t, s.ResolveAccess("owner", root.Org, request.ID, request.Revision, "operation", "Exact finalization"))
	setRunning(t, s, &root)
	_, err = s.CallGateway(context.Background(), root.ID, tool.ID, want.Operation, want.Arguments)
	must(t, err)
	if calls.Load() != 1 {
		t.Fatal("finalization did not execute")
	}
	// The completed plan's artifact evidence is part of that finalization grant.
	d := Document{ID: "changed-final-plan", Org: r.Org, Task: r.Task, Run: r.ID, Content: "new artifact", Revision: 1}
	must(t, s.Put("document", d.Org, d.Task, "", d.ID, d))
	if _, err := s.CallGateway(context.Background(), root.ID, tool.ID, want.Operation, want.Arguments); err == nil {
		t.Fatal("finalization retained changed plan evidence")
	}
}

func TestOperationExpirationWithoutPendingDecisionPreservesRetryBudget(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			s, e, r, tool, _ := operationFixture(t)
			request, err := s.RequestAccess(r.ID, "stale decision fixture", []AccessWant{operationWant(tool)})
			must(t, err)
			if missing {
				_, err = s.db.Exec(`DELETE FROM records WHERE id=?`, request.Decision)
				must(t, err)
			} else {
				var d Decision
				must(t, s.Get(request.Decision, &d))
				d.State = "answered"
				d.Answer = "Retain this historical answer"
				must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
			}
			must(t, s.Get(r.ID, &r))
			r.Attempts = 2
			must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			doc := Document{ID: "changed", Org: r.Org, Task: r.Task, Run: r.ID, Content: "new artifact", Revision: 1}
			must(t, s.Put("document", doc.Org, doc.Task, "", doc.ID, doc))
			planDispatch(e)
			var retired AccessRequest
			must(t, s.Get(request.ID, &retired))
			must(t, s.Get(r.ID, &r))
			if retired.State != "stale" || r.State != "queued" || r.Attempts != 2 {
				t.Fatal("stale request stuck or retry budget refunded", retired.State, r.State, r.Attempts)
			}
			if !missing {
				var d Decision
				must(t, s.Get(request.Decision, &d))
				if d.Answer != "Retain this historical answer" {
					t.Fatal("expiration changed an answered decision")
				}
			}
		})
	}
}
func operationWant(tool GatewayTool) AccessWant {
	return AccessWant{Tool: tool.ID, Operation: "single-canary", Arguments: json.RawMessage(`{"target":"fixture-canary"}`), Context: &OperationProposal{Action: "Publish one fixture canary", Target: "fixture-canary", Environment: "synthetic-only", Effect: "One publication", Validation: "Inspect fixture result", Rollback: "Stop and retain evidence if validation fails"}}
}

func TestOperationApprovalBoundToStepArtifactsAndSingleAction(t *testing.T) {
	s, _, r, tool, calls := operationFixture(t)
	want := operationWant(tool)
	if _, err := s.CallGateway(context.Background(), r.ID, tool.ID, want.Operation, want.Arguments); err == nil {
		t.Fatal("standing grant bypassed plan operation approval")
	}
	bad := want
	bad.Context = nil
	if _, err := s.RequestAccess(r.ID, "one fixture operation", []AccessWant{bad}); err == nil {
		t.Fatal("unbound planned mutation requested")
	}
	request, err := s.RequestAccess(r.ID, "one fixture operation", []AccessWant{want})
	must(t, err)
	if request.Entries[0].Binding == nil || request.Entries[0].Binding.Run != r.ID {
		t.Fatal("missing ADC-supplied binding")
	}
	if s.ResolveAccess("owner", r.Org, request.ID, request.Revision, "assignment", "") == nil {
		t.Fatal("bound approval widened to assignment")
	}
	if s.ResolveAccess("owner", r.Org, request.ID, request.Revision, "standing", "") == nil {
		t.Fatal("bound approval widened to standing")
	}
	if s.ResolveAccess("outsider", r.Org, request.ID, request.Revision, "operation", "") == nil {
		t.Fatal("nonmember approved")
	}
	must(t, s.ResolveAccess("owner", r.Org, request.ID, request.Revision, "operation", "Only one synthetic canary"))
	setRunning(t, s, &r)
	_, err = s.CallGateway(context.Background(), r.ID, tool.ID, want.Operation, want.Arguments)
	must(t, err)
	_, err = s.CallGateway(context.Background(), r.ID, tool.ID, want.Operation, want.Arguments)
	must(t, err)
	if calls.Load() != 1 {
		t.Fatal("single operation repeated")
	}
	for _, attempt := range []struct {
		op   string
		args json.RawMessage
	}{{"second-canary", want.Arguments}, {"credential-cutover", json.RawMessage(`{"target":"producer-credentials"}`)}, {want.Operation, json.RawMessage(`{"target":"another-canary"}`)}} {
		if _, err := s.CallGateway(context.Background(), r.ID, tool.ID, attempt.op, attempt.args); err == nil {
			t.Fatal("one approval widened", attempt)
		}
	}
	// Neither siblings nor delegated children can borrow the exact grant.
	var sibling Run
	must(t, s.Get(planStepByKey(t, s, "R2").Run, &sibling))
	setRunning(t, s, &sibling)
	child := r
	child.ID = ID()
	child.Parent = r.ID
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	for _, other := range []Run{sibling, child} {
		if _, err := s.CallGateway(context.Background(), other.ID, tool.ID, want.Operation, want.Arguments); err == nil {
			t.Fatal("another run borrowed approval")
		}
	}
	if _, err := s.RequestAccess(child.ID, "borrow another step", []AccessWant{want}); err == nil {
		t.Fatal("child manufactured a step binding")
	}
}

func TestOperationChangedEvidenceRequiresFreshApprovalAndCanRecover(t *testing.T) {
	s, e, r, tool, calls := operationFixture(t)
	want := operationWant(tool)
	d := Document{ID: "operation-plan", Org: r.Org, Task: r.Task, Run: r.ID, Title: "Fixture operation", Content: "Revision one", Revision: 1}
	must(t, s.Put("document", d.Org, d.Task, "", d.ID, d))
	request, err := s.RequestAccess(r.ID, "fixture operation", []AccessWant{want})
	must(t, err)
	d.Content = "Revision two"
	d.Revision++
	must(t, s.Put("document", d.Org, d.Task, "", d.ID, d))
	if s.ResolveAccess("owner", r.Org, request.ID, request.Revision, "operation", "") == nil {
		t.Fatal("stale pending bundle approved")
	}
	setRunning(t, s, &r)
	newRequest, err := s.RequestAccess(r.ID, "Updated fixture operation", []AccessWant{want})
	must(t, err)
	if newRequest.ID != request.ID || len(newRequest.Entries) != 1 || newRequest.Revision != request.Revision+1 {
		t.Fatal("fresh proposal trapped behind stale entry", newRequest)
	}
	if newRequest.Entries[0].Binding.EvidenceRevision != e.revision(r) {
		t.Fatal("binding not refreshed")
	}
	// Resolve after a store restart, proving no provider-memory grant dependency.
	reopened, err := Open(s.Dir)
	must(t, err)
	defer reopened.Close()
	must(t, reopened.ResolveAccess("owner", r.Org, newRequest.ID, newRequest.Revision, "operation", "approved current version"))
	setRunning(t, reopened, &r)
	d.Content = "Revision three after approval"
	d.Revision++
	must(t, reopened.Put("document", d.Org, d.Task, "", d.ID, d))
	if _, err := reopened.CallGateway(context.Background(), r.ID, tool.ID, want.Operation, want.Arguments); err == nil {
		t.Fatal("artifact changed after approval but operation executed")
	}
	if calls.Load() != 0 {
		t.Fatal("stale operation reached integration")
	}
	if len(list[AccessRequest](reopened, "access-request-history", r.Org)) != 1 {
		t.Fatal("old proposal evidence lost")
	}
	history := list[AccessRequest](reopened, "access-request-history", r.Org)[0]
	if history.Entries[0].Binding.EvidenceRevision != request.Entries[0].Binding.EvidenceRevision || history.Entries[0].Binding.EvidenceRevision == newRequest.Entries[0].Binding.EvidenceRevision {
		t.Fatal("replacement overwrote historical binding through a shared slice")
	}
}

func TestOperationRejectsChangedRegisteredCodeAndConstraintWidening(t *testing.T) {
	s, e, r, tool, calls := operationFixture(t)
	repo := integrationRepo(t, e, &r)
	want := operationWant(tool)
	request, err := s.RequestAccess(r.ID, "only this committed fixture", []AccessWant{want})
	must(t, err)
	if s.EditAccess("owner", r.Org, request.ID, request.Revision, [][]ArgumentConstraint{{}}, "remove exact target") == nil {
		t.Fatal("bound request lost exact arguments through editing")
	}
	must(t, s.ResolveAccess("owner", r.Org, request.ID, request.Revision, "operation", "approved"))
	setRunning(t, s, &r)
	// No adc_code refresh: the saved pin alone is insufficient if the tree changed.
	must(t, os.WriteFile(filepath.Join(repo.Path, "file"), []byte("unreviewed change"), 0600))
	if _, err := s.CallGateway(context.Background(), r.ID, tool.ID, want.Operation, want.Arguments); err == nil {
		t.Fatal("unrecorded working tree change escaped approval binding")
	}
	if calls.Load() != 0 {
		t.Fatal("changed code reached external tool")
	}
}
