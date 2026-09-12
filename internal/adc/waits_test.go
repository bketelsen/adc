package adc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func timerFixture(t *testing.T) (*Store, *Engine, Run, PlanStep) {
	s, e, r, step := milestoneFixture(t, "published-release")
	p := s.taskPlan(r.Task)
	p.Steps[0].Requirements[0].Kind = "elapsed-time"
	p.Steps[0].Requirements[0].Wait = &WaitSpec{StableSeconds: 10, TimeoutSeconds: 60}
	must(t, s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p))
	step = p.Steps[0]
	return s, e, r, step
}
func externalWaitFixture(t *testing.T) (*Store, *Engine, Run, PlanStep, *atomic.Bool, *atomic.Int32) {
	t.Helper()
	s, e, r, step := milestoneFixture(t, "published-release")
	matched := &atomic.Bool{}
	calls := &atomic.Int32{}
	server := mcp.NewServer(&mcp.Implementation{Name: "durable-wait-fixture", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "observe", Description: "Read synthetic release", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, _ *mcp.CallToolRequest, args struct {
		Target string `json:"target"`
	}) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		return &mcp.CallToolResult{}, map[string]any{"target": args.Target, "published": matched.Load(), "version": "v1"}, nil
	})
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(httpServer.Close)
	conn := Connection{ID: "observer", Org: r.Org, Name: "Fixture observer", Transport: "http", URL: httpServer.URL}
	must(t, s.Put("connection", conn.Org, "", "", conn.ID, conn))
	catalog, err := s.DiscoverGateway(context.Background(), r.Org, conn.ID)
	must(t, err)
	for _, id := range []string{r.Agent, s.taskPlan(r.Task).Supervisor} {
		var a Agent
		if s.Get(id, &a) == nil {
			a.Tools = append(a.Tools, conn.ID)
			must(t, s.Put("agent", a.Org, "", "", a.ID, a))
		}
	}
	r.Tools = append(r.Tools, conn.ID)
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	policy := ToolPolicy{ID: "policy-" + catalog[0].ID, Org: r.Org, Tool: catalog[0].ID, Connection: conn.ID, Fingerprint: catalog[0].Fingerprint, Mode: "allow", Class: "read", Revision: 1}
	must(t, s.Put("tool-policy", r.Org, conn.ID, "", policy.ID, policy))
	p := s.taskPlan(r.Task)
	p.Steps[0].Requirements[0].Wait = &WaitSpec{Tool: catalog[0].ID, Arguments: `{"target":"fixture:release/v1"}`, Match: []WaitMatch{{Pointer: "/target", ExpectedJSON: `"fixture:release/v1"`}, {Pointer: "/published", ExpectedJSON: `true`}}, VersionPointer: "/version", StableSeconds: 0, PollSeconds: 30, TimeoutSeconds: 120}
	must(t, s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p))
	step = p.Steps[0]
	return s, e, r, step, matched, calls
}
func dispatchWaitFixture(e *Engine, at time.Time) {
	e.Store.mu.Lock()
	e.dispatchWaits(context.Background(), at)
	e.Store.mu.Unlock()
	e.wg.Wait()
}
func waitDue(t *testing.T, s *Store, w DurableWait) DurableWait {
	w.NextAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	w.Lease = ""
	must(t, s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w))
	return w
}
func TestDurableTimerReviewAndRecovery(t *testing.T) {
	s, e, r, step := timerFixture(t)
	if _, err := e.submitMilestone(r, milestonePayload("elapsed-time"), ""); err == nil {
		t.Fatal("manual packet bypassed timer")
	}
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	_, err = call(t, e, r, "adc_wait", map[string]any{})
	must(t, err)
	dispatchWaitFixture(e, time.Now())
	if s.runWait(r.ID, "gate").State != "pending" {
		t.Fatal("timer completed early")
	}
	// Recover the persisted wait with a new engine. No model activation is needed.
	e = NewEngine(s)
	w.Created = time.Now().Add(-11 * time.Second).UTC().Format(time.RFC3339Nano)
	w = waitDue(t, s, w)
	dispatchWaitFixture(e, time.Now())
	dispatchWaitFixture(e, time.Now())
	w = s.runWait(r.ID, "gate")
	if w.State != "satisfied" || w.Checks != 1 {
		t.Fatal("timer did not reconcile once", w)
	}
	if s.milestoneEvidence(r.ID, "gate").Revision != 1 {
		t.Fatal("duplicate evidence after repeated reconciliation")
	}
	must(t, s.Get(r.ID, &r))
	if r.State != "queued" {
		t.Fatal("timer failed to wake its owner")
	}
	completePlanWorker(t, e, step)
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run != "" {
		t.Fatal("timer bypassed independent review")
	}
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run == "" {
		t.Fatal("reviewed timer failed to unlock dependent")
	}
}
func TestDurableExternalWaitUsesRealMCPAndFreshResult(t *testing.T) {
	s, e, r, step, matched, calls := externalWaitFixture(t)
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	_, err = call(t, e, r, "adc_wait", map[string]any{})
	must(t, err)
	dispatchWaitFixture(e, time.Now())
	w = s.runWait(r.ID, "gate")
	if w.State != "pending" || calls.Load() != 1 || !strings.Contains(w.Reason, "not yet met") {
		t.Fatal("false external condition accepted", w, calls.Load())
	}
	dispatchWaitFixture(e, time.Now())
	if calls.Load() != 1 {
		t.Fatal("ignored polling cadence")
	}
	matched.Store(true)
	e = NewEngine(s)
	waitDue(t, s, w)
	dispatchWaitFixture(e, time.Now())
	w = s.runWait(r.ID, "gate")
	if w.State != "satisfied" || calls.Load() != 2 || !strings.HasPrefix(s.milestoneEvidence(r.ID, "gate").Actor, "observer:") {
		t.Fatal("MCP observation not recorded", w)
	}
	completePlanWorker(t, e, step)
	reviewPlanStep(t, e, step, "pass")
	planDispatch(e)
	if planStepByKey(t, s, "R2").Run == "" {
		t.Fatal("external result not usable after independent review")
	}
	dispatchWaitFixture(e, time.Now())
	if calls.Load() != 2 {
		t.Fatal("satisfied wait kept polling")
	}
}
func TestDurableWaitRequiresTimeAndVersionAndRealCondition(t *testing.T) {
	s, e, r, _, _, _ := externalWaitFixture(t)
	p := s.taskPlan(r.Task)
	p.Steps[0].Requirements[0].Wait.StableSeconds = 48 * 3600
	p.Steps[0].Requirements[0].Wait.TimeoutSeconds = 72 * 3600
	must(t, s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p))
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	good := `{"structuredContent":{"target":"fixture:release/v1","published":true,"version":"v1"}}`
	e.applyWaitResult(w, good, nil, time.Now())
	w = s.runWait(r.ID, "gate")
	if w.State != "pending" || w.MatchedSince == "" {
		t.Fatal("first match should start interval", w)
	}
	w.MatchedSince = time.Now().Add(-49 * time.Hour).UTC().Format(time.RFC3339Nano)
	must(t, s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w))
	e.applyWaitResult(w, strings.ReplaceAll(good, `true`, `false`), nil, time.Now())
	w = s.runWait(r.ID, "gate")
	if w.State != "pending" || w.MatchedSince != "" {
		t.Fatal("elapsed time substituted for real publication")
	}
	e.applyWaitResult(w, good, nil, time.Now())
	w = s.runWait(r.ID, "gate")
	w.MatchedSince = time.Now().Add(-49 * time.Hour).UTC().Format(time.RFC3339Nano)
	must(t, s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w))
	e.applyWaitResult(w, strings.ReplaceAll(good, `v1"}`, `v2"}`), nil, time.Now())
	w = s.runWait(r.ID, "gate")
	since, _ := time.Parse(time.RFC3339Nano, w.MatchedSince)
	if w.State != "pending" || time.Since(since) > time.Minute {
		t.Fatal("version change retained old observation window", w)
	}
	w.MatchedSince = time.Now().Add(-49 * time.Hour).UTC().Format(time.RFC3339Nano)
	must(t, s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w))
	e.applyWaitResult(w, strings.ReplaceAll(good, `v1"}`, `v2"}`), nil, time.Now())
	if s.runWait(r.ID, "gate").State != "satisfied" {
		t.Fatal("fresh same-version observation after interval not accepted")
	}
}
func TestDurableWaitRevocationDeadlineAndRetry(t *testing.T) {
	s, e, r, _, _, calls := externalWaitFixture(t)
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	var policy ToolPolicy
	must(t, s.Get("policy-"+w.Tool.ID, &policy))
	policy.Class = "change"
	must(t, s.Put("tool-policy", policy.Org, policy.Connection, "", policy.ID, policy))
	dispatchWaitFixture(e, time.Now())
	if calls.Load() != 0 || s.runWait(r.ID, "gate").State != "failed" {
		t.Fatal("revoked read classification reached MCP")
	}
	policy.Class = "read"
	must(t, s.Put("tool-policy", policy.Org, policy.Connection, "", policy.ID, policy))
	w, err = e.startWait(r, "gate", true)
	must(t, err)
	if w.Generation != 2 || len(list[DurableWait](s, "wait-history", r.Org)) != 1 {
		t.Fatal("retry lost prior wait")
	}
	w.Deadline = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	must(t, s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w))
	dispatchWaitFixture(e, time.Now())
	if calls.Load() != 0 || s.runWait(r.ID, "gate").State != "failed" {
		t.Fatal("expired wait performed a poll")
	}
}
func TestDurableWaitLeasesPauseAndLateResults(t *testing.T) {
	s, e, r, _, _, calls := externalWaitFixture(t)
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	w.Lease = time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	must(t, s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w))
	dispatchWaitFixture(NewEngine(s), time.Now())
	if calls.Load() != 0 {
		t.Fatal("recovery ignored persisted lease")
	}
	var task Assignment
	must(t, s.Get(r.Task, &task))
	task.State = "paused"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	w = waitDue(t, s, w)
	dispatchWaitFixture(e, time.Now())
	if calls.Load() != 0 {
		t.Fatal("paused task polled")
	}
	e.applyWaitResult(w, `{"structuredContent":{"target":"fixture:release/v1","published":true,"version":"v1"}}`, nil, time.Now())
	if s.runWait(r.ID, "gate").State != "pending" {
		t.Fatal("late result completed paused task")
	}
	task.State = "queued"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Get(r.ID, &r))
	r.Superseded = true
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	dispatchWaitFixture(e, time.Now())
	if s.runWait(r.ID, "gate").State != "cancelled" || calls.Load() != 0 {
		t.Fatal("superseded wait remained active")
	}
}
func TestDurableWaitProtectedNeedsReusableGrant(t *testing.T) {
	s, e, r, _, _, _ := externalWaitFixture(t)
	r.Execution = "protected"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	p := s.taskPlan(r.Task)
	tool := p.Steps[0].Requirements[0].Wait.Tool
	var gt GatewayTool
	must(t, s.Get(tool, &gt))
	var task Assignment
	must(t, s.Get(r.Task, &task))
	grant := CapabilityGrant{Tool: tool, Fingerprint: gt.Fingerprint, Class: "read", Scope: "operation", Operation: "single", Approval: "human"}
	task.Capabilities = []CapabilityGrant{grant}
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := e.startWait(r, "gate", false); err == nil {
		t.Fatal("one-shot permission reused for background polling")
	}
	grant.Scope = "assignment"
	task.Capabilities = []CapabilityGrant{grant}
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	_, err := e.startWait(r, "gate", false)
	must(t, err)
}
func TestWaitValidationAndMCPError(t *testing.T) {
	req := PlanRequirement{Kind: "elapsed-time", Wait: &WaitSpec{StableSeconds: 10, TimeoutSeconds: 20}}
	must(t, validateWait(req))
	req.Wait.TimeoutSeconds = 10
	if validateWait(req) == nil {
		t.Fatal("impossible deadline accepted")
	}
	spec := WaitSpec{Match: []WaitMatch{{Pointer: "/ok", ExpectedJSON: "true"}}, VersionPointer: "/v"}
	for _, raw := range []string{`{"isError":true,"structuredContent":{"ok":true,"v":"1"}}`, `{"structuredContent":{"ok":true}}`, `{"structuredContent":{"ok":false,"v":"1"}}`, `{"structuredContent":{"ok":true,"v":{}}}`} {
		if _, err := waitObservation(spec, raw, time.Now().Add(-time.Hour), time.Now()); err == nil {
			t.Fatal("bad observation accepted", raw)
		}
	}
	version, err := waitObservation(spec, `{"content":[{"type":"text","text":"{\"ok\":true,\"v\":\"1\"}"}]}`, time.Now().Add(-time.Hour), time.Now())
	must(t, err)
	if version != `"1"` {
		t.Fatal(fmt.Sprint(version))
	}
}

func TestWaitEventWindowAndRetryIdentity(t *testing.T) {
	at := time.Now().UTC()
	start := at.Add(-time.Hour)
	spec := WaitSpec{VersionPointer: "/version", EventTimePointer: "/published_at", Match: []WaitMatch{{Pointer: "/published", ExpectedJSON: "true"}}}
	sample := func(event time.Time) string {
		return fmt.Sprintf(`{"structuredContent":{"published":true,"version":"v1","published_at":%q}}`, event.Format(time.RFC3339Nano))
	}
	for _, event := range []time.Time{start.Add(-time.Second), at.Add(time.Second)} {
		if _, err := waitObservation(spec, sample(event), start, at); err == nil {
			t.Fatal("out-of-window event satisfied gate")
		}
	}
	if _, err := waitObservation(spec, sample(start.Add(time.Second)), start, at); err != nil {
		t.Fatal(err)
	}
	s, e, r, _, _, _ := externalWaitFixture(t)
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	e.failWait(w, r, "fixture interruption")
	fresh, err := e.startWait(r, "gate", true)
	must(t, err)
	if fresh.EventAfter != w.EventAfter || fresh.Generation == w.Generation {
		t.Fatal("retry lost the original event window")
	}
	e.applyWaitResult(w, `{"structuredContent":{"published":true,"version":"v1","target":"fixture:release/v1"}}`, nil, time.Now())
	if s.runWait(r.ID, "gate").State != "pending" || s.milestoneEvidence(r.ID, "gate").ID != "" {
		t.Fatal("old generation completed fresh wait")
	}
}
func TestWaitPreservesOutstandingHumanDecision(t *testing.T) {
	s, e, r, _ := timerFixture(t)
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	r.State = "waiting"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	d := Decision{ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, State: "pending", Question: "Fixture decision"}
	must(t, s.Put("decision", d.Org, d.Task, d.State, d.ID, d))
	w.Created = time.Now().Add(-11 * time.Second).UTC().Format(time.RFC3339Nano)
	w = waitDue(t, s, w)
	dispatchWaitFixture(e, time.Now())
	must(t, s.Get(r.ID, &r))
	if r.State != "waiting" || !pendingDecision(s, r.Task, r.ID) {
		t.Fatal("observation bypassed pending decision")
	}
}
func TestWaitDatabaseReopenAndDuplicateClaims(t *testing.T) {
	s, e, r, _, _, calls := externalWaitFixture(t)
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	dir := s.Dir
	must(t, s.Close())
	restored, err := Open(dir)
	must(t, err)
	defer restored.Close()
	e = NewEngine(restored)
	var dispatchers sync.WaitGroup
	for range 8 {
		dispatchers.Add(1)
		go func() {
			defer dispatchers.Done()
			restored.mu.Lock()
			e.dispatchWaits(context.Background(), time.Now())
			restored.mu.Unlock()
		}()
	}
	dispatchers.Wait()
	e.wg.Wait()
	got := restored.runWait(r.ID, "gate")
	if got.ID != w.ID || got.Checks != 1 || calls.Load() != 1 {
		t.Fatal("database recovery duplicated or lost observation", got, calls.Load())
	}
}

func TestTimerRecoveryAfterItsDeadline(t *testing.T) {
	s, e, r, _ := timerFixture(t)
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	_, err = call(t, e, r, "adc_wait", map[string]any{})
	must(t, err)
	w.Created = time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano)
	w.Deadline = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	w = waitDue(t, s, w)
	dir := s.Dir
	must(t, s.Close())
	restored, err := Open(dir)
	must(t, err)
	defer restored.Close()
	fresh := NewEngine(restored)
	dispatchWaitFixture(fresh, time.Now())
	got := restored.runWait(r.ID, "gate")
	if got.State != "satisfied" || got.Generation != 1 || restored.milestoneEvidence(r.ID, "gate").ID == "" {
		t.Fatal("elapsed timer restarted or failed after downtime", got)
	}
}
func TestWaitRecordingFailureRetainsNewestObservation(t *testing.T) {
	s, e, r, _, _, _ := externalWaitFixture(t)
	w, err := e.startWait(r, "gate", false)
	must(t, err)
	w.Result = "previous sample"
	w.Checks = 2
	must(t, s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w))
	_, err = s.db.Exec(`CREATE TRIGGER fail_observation_packet BEFORE INSERT ON records WHEN NEW.kind='milestone-evidence' BEGIN SELECT RAISE(FAIL,'fixture recording failure'); END`)
	must(t, err)
	result := `{"structuredContent":{"target":"fixture:release/v1","published":true,"version":"fresh-v2"}}`
	e.applyWaitResult(w, result, nil, time.Now())
	got := s.runWait(r.ID, "gate")
	if got.State != "failed" || got.Result != result || got.Checks != 3 || got.CheckedAt == "" || got.Version != `"fresh-v2"` {
		t.Fatal("recording error lost newest observation", got)
	}
	if s.milestoneEvidence(r.ID, "gate").ID != "" {
		t.Fatal("partial evidence transaction survived")
	}
}
func TestWaitCatalogOmitsNonReadAndDeniedTools(t *testing.T) {
	s, e, r, _, _, _ := externalWaitFixture(t)
	catalog := e.waitCatalog(r)
	if len(catalog) != 1 {
		t.Fatal("read tool absent")
	}
	var p ToolPolicy
	must(t, s.Get("policy-"+catalog[0].ID, &p))
	p.Class = "change"
	must(t, s.Put("tool-policy", p.Org, p.Connection, "", p.ID, p))
	if len(e.waitCatalog(r)) != 0 {
		t.Fatal("mutation advertised for background observation")
	}
	p.Class = "read"
	p.Mode = "deny"
	must(t, s.Put("tool-policy", p.Org, p.Connection, "", p.ID, p))
	if len(e.waitCatalog(r)) != 0 {
		t.Fatal("denied tool advertised")
	}
}
