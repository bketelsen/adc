package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type WaitMatch struct{ Pointer, ExpectedJSON string }

// StableSeconds measures elapsed time with matching samples of the same version;
// it is not a claim of uninterrupted monitoring between samples.
type WaitSpec struct {
	Tool, Arguments, VersionPointer, EventTimePointer string
	Match                                             []WaitMatch
	StableSeconds, PollSeconds, TimeoutSeconds        int
}
type DurableWait struct {
	ID, Org, Task, Run, Requirement, State, Created, Deadline, NextAt   string
	CheckedAt, MatchedSince, Version, Result, Reason, Lease, EventAfter string
	Generation, Checks                                                  int
	Spec                                                                WaitSpec
	Tool                                                                GatewayTool
}

func validateWait(req PlanRequirement) error {
	w := req.Wait
	if w == nil {
		if req.Kind == "elapsed-time" {
			return fmt.Errorf("elapsed-time requires a Wait definition")
		}
		return nil
	}
	if req.Kind == "human-evidence" || req.Kind == "reviewed-code" {
		return fmt.Errorf("this milestone kind cannot use an automatic wait")
	}
	if w.StableSeconds < 0 || w.StableSeconds > 31536000 || w.TimeoutSeconds <= w.StableSeconds || w.TimeoutSeconds > 31622400 {
		return fmt.Errorf("wait timeout must exceed the observation period and be at most 366 days")
	}
	if w.Tool == "" {
		if req.Kind != "elapsed-time" || w.StableSeconds < 1 || len(w.Match) > 0 || w.VersionPointer != "" || w.EventTimePointer != "" || w.Arguments != "" {
			return fmt.Errorf("timer-only waits need elapsed-time and a positive StableSeconds, without a tool or predicates")
		}
		return nil
	}
	if req.Kind == "elapsed-time" || w.PollSeconds < 30 || w.PollSeconds > 86400 || len(w.Match) < 1 || len(w.Match) > 12 || !strings.HasPrefix(w.VersionPointer, "/") || len(w.VersionPointer) > 1000 {
		return fmt.Errorf("external waits need 1–12 predicates, a version JSON pointer and a 30–86400 second polling interval")
	}
	if w.EventTimePointer != "" && (!strings.HasPrefix(w.EventTimePointer, "/") || len(w.EventTimePointer) > 1000) {
		return fmt.Errorf("event time needs a bounded JSON pointer")
	}
	if len(w.Arguments) > 16000 {
		return fmt.Errorf("wait arguments must fit in 16 KiB")
	}
	if _, _, err := canonicalArguments(json.RawMessage(w.Arguments)); err != nil {
		return err
	}
	for _, m := range w.Match {
		if len(m.Pointer) > 1000 || (m.Pointer != "" && !strings.HasPrefix(m.Pointer, "/")) || !json.Valid([]byte(m.ExpectedJSON)) || len(m.ExpectedJSON) > 4000 {
			return fmt.Errorf("each predicate needs a JSON pointer and bounded valid ExpectedJSON")
		}
	}
	return nil
}
func (s *Store) runWait(run, key string) DurableWait {
	var w DurableWait
	_ = s.Get("wait:"+run+":"+key, &w)
	return w
}
func (e *Engine) pendingWait(r Run) bool {
	_, step, ok := e.plannedStep(r)
	if !ok {
		return false
	}
	for _, req := range step.Requirements {
		if req.Wait != nil {
			w := e.Store.runWait(r.ID, req.Key)
			if w.State == "pending" {
				return true
			}
		}
	}
	return false
}
func (e *Engine) waitCatalog(r Run) []GatewayTool {
	out := []GatewayTool{}
	for _, tool := range list[GatewayTool](e.Store, "gateway-tool", r.Org) {
		var policy ToolPolicy
		if Subset([]string{tool.Connection}, r.Tools) && e.Store.Get("policy-"+tool.ID, &policy) == nil && policy.Org == r.Org && policy.Class == "read" && policy.Mode != "deny" && policy.Fingerprint == tool.Fingerprint && (r.Execution == "protected" || policy.Mode == "allow") {
			out = append(out, tool)
		}
	}
	return out
}

// Called under Store.mu both before opening a connection and immediately before
// CallTool. A wait is not an operation grant and cannot reuse a one-shot grant.
func (e *Engine) authorizeWait(r Run, w DurableWait) error {
	s := e.Store
	var task Assignment
	if s.Get(r.Task, &task) != nil || task.Org != r.Org || task.State == "paused" || task.State == "cancelled" || task.State == "ready" || r.Superseded || r.State == "cancelled" || r.State == "blocked" {
		return fmt.Errorf("wait owner is unavailable")
	}
	if !e.planAllowsDispatch(r) {
		return fmt.Errorf("prerequisite evidence changed; observer stopped for plan recovery")
	}
	if w.Spec.Tool == "" {
		return nil
	}
	var current GatewayTool
	var policy ToolPolicy
	var agent Agent
	if s.Get(w.Tool.ID, &current) != nil || current.Org != r.Org || current.Fingerprint != w.Tool.Fingerprint || s.Get("policy-"+current.ID, &policy) != nil || policy.Org != r.Org || policy.Class != "read" || policy.Fingerprint != current.Fingerprint || policy.Mode == "deny" {
		return fmt.Errorf("observation requires current human-classified read access")
	}
	if s.Get(r.Agent, &agent) != nil || agent.Org != r.Org || !Subset([]string{current.Connection}, agent.Tools) || !Subset([]string{current.Connection}, r.Tools) {
		return fmt.Errorf("observer connection is no longer granted to this worker")
	}
	// Reuse the protected capability checks without pretending the waiting model
	// is running. Advisory assignments still require explicit allow/read policy.
	if r.Execution == "protected" {
		_, err := s.authorizeGatewayRun(r, current, "", json.RawMessage(w.Spec.Arguments))
		return err
	}
	var conn Connection
	if s.Get(current.Connection, &conn) != nil || conn.Org != r.Org || connectionRevision(conn) != current.ConnectionRevision {
		return fmt.Errorf("observer connection changed; refresh its classification")
	}
	_, value, err := canonicalArguments(json.RawMessage(w.Spec.Arguments))
	if err != nil {
		return err
	}
	if policy.Mode != "allow" || !constraintsMatch(value, policy.Constraints) {
		return fmt.Errorf("background observations require allowed read arguments")
	}
	if len(policy.StandingRules) > 0 {
		for _, rule := range policy.StandingRules {
			if constraintsMatch(value, rule) {
				return nil
			}
		}
		return fmt.Errorf("observation arguments exceed standing read rules")
	}
	return nil
}
func (e *Engine) startWait(r Run, key string, retry bool) (DurableWait, error) {
	s := e.Store
	_, step, ok := e.plannedStep(r)
	if !ok {
		return DurableWait{}, fmt.Errorf("only a current plan worker can await its requirements")
	}
	var req PlanRequirement
	for _, candidate := range step.Requirements {
		if candidate.Key == key {
			req = candidate
		}
	}
	if req.Wait == nil {
		return DurableWait{}, fmt.Errorf("this requirement has no frozen Wait definition")
	}
	old := s.runWait(r.ID, key)
	if old.ID != "" && (old.State != "failed" || !retry) {
		return old, nil
	}
	at := time.Now().UTC()
	w := DurableWait{ID: "wait:" + r.ID + ":" + key, Org: r.Org, Task: r.Task, Run: r.ID, Requirement: key, State: "pending", Created: at.Format(time.RFC3339Nano), NextAt: at.Format(time.RFC3339Nano), Deadline: at.Add(time.Duration(req.Wait.TimeoutSeconds) * time.Second).Format(time.RFC3339Nano), Generation: old.Generation + 1, Spec: *req.Wait}
	w.EventAfter = w.Created
	if old.ID != "" {
		w.EventAfter = old.EventAfter
		if w.EventAfter == "" {
			w.EventAfter = old.Created
		}
	}
	if w.Spec.Tool != "" {
		if s.Get(w.Spec.Tool, &w.Tool) != nil || w.Tool.Org != r.Org {
			return old, fmt.Errorf("discover the observation tool in this organization first")
		}
	}
	if err := e.authorizeWait(r, w); err != nil {
		return old, err
	}
	if w.Spec.Tool == "" {
		w.NextAt = at.Add(time.Duration(w.Spec.StableSeconds) * time.Second).Format(time.RFC3339Nano)
	}
	writes := []Write{{"durable-wait", w.Org, w.Task, w.State, w.ID, w}}
	if old.ID != "" {
		old.ID = ID()
		writes = append(writes, Write{"wait-history", old.Org, old.Task, old.State, old.ID, old})
	}
	if err := s.Batch(writes...); err != nil {
		return w, err
	}
	s.Log(r.Org, r.Task, r.ID, "wait", "Waiting for "+key+"; deadline "+w.Deadline)
	return w, nil
}

// tick holds Store.mu. Leases survive process loss; fresh observations reconcile
// missed events. Network work is bounded separately from model-worker capacity.
func (e *Engine) dispatchWaits(ctx context.Context, at time.Time) {
	s := e.Store
	for _, w := range list[DurableWait](s, "durable-wait", "") {
		if w.State != "pending" {
			continue
		}
		var r Run
		var task Assignment
		if s.Get(w.Run, &r) != nil || s.Get(w.Task, &task) != nil {
			continue
		}
		if r.Superseded || r.State == "cancelled" || task.State == "cancelled" {
			w.State = "cancelled"
			_ = s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w)
			continue
		}
		if task.State == "paused" || task.State == "ready" || r.State == "blocked" {
			if w.Spec.Tool != "" && w.MatchedSince != "" {
				w.MatchedSince = ""
				_ = s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w)
			}
			continue
		}
		deadline, _ := time.Parse(time.RFC3339Nano, w.Deadline)
		if w.Spec.Tool != "" && !at.Before(deadline) {
			e.failWait(w, r, "Observation deadline reached; inspect the last result before retrying")
			continue
		}
		next, _ := time.Parse(time.RFC3339Nano, w.NextAt)
		lease, _ := time.Parse(time.RFC3339Nano, w.Lease)
		if at.Before(next) || at.Before(lease) {
			continue
		}
		if err := e.authorizeWait(r, w); err != nil {
			e.failWait(w, r, err.Error())
			continue
		}
		if w.Spec.Tool == "" {
			e.applyWaitResult(w, "", nil, at)
			continue
		}
		e.mu.Lock()
		busy := len(e.waitActive) >= 4 || e.waitActive[w.ID]
		if !busy {
			e.waitActive[w.ID] = true
		}
		e.mu.Unlock()
		if busy {
			continue
		}
		w.Lease = at.Add(90 * time.Second).Format(time.RFC3339Nano)
		if err := s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w); err != nil {
			e.mu.Lock()
			delete(e.waitActive, w.ID)
			e.mu.Unlock()
			continue
		}
		e.wg.Add(1)
		go func(claim DurableWait) {
			defer e.wg.Done()
			defer func() { e.mu.Lock(); delete(e.waitActive, claim.ID); e.mu.Unlock() }()
			result, err := s.invokeGateway(ctx, claim.Tool, json.RawMessage(claim.Spec.Arguments), func() error {
				s.mu.Lock()
				defer s.mu.Unlock()
				current := s.runWait(claim.Run, claim.Requirement)
				var owner Run
				if current.State != "pending" || current.Generation != claim.Generation || current.Lease != claim.Lease || s.Get(claim.Run, &owner) != nil {
					return fmt.Errorf("observation was superseded")
				}
				return e.authorizeWait(owner, current)
			})
			s.mu.Lock()
			defer s.mu.Unlock()
			e.applyWaitResult(claim, result, err, time.Now().UTC())
		}(w)
	}
}
func (e *Engine) failWait(w DurableWait, r Run, reason string) {
	w.State = "failed"
	w.Reason = reason
	w.Lease = ""
	if r.State == "waiting" && !pendingDecision(e.Store, r.Task, r.ID) {
		r.State = "queued"
		r.Turns = 0
		r.Attempts = 0
	}
	r.Prompt += "\nDurable wait " + w.Requirement + " stopped: " + reason + ". Inspect adc_status. Retry only after addressing the cause, or report a concrete blocker."
	_ = e.Store.Batch(Write{"durable-wait", w.Org, w.Task, w.State, w.ID, w}, Write{"run", r.Org, r.Task, r.State, r.ID, r})
	e.Store.Log(w.Org, w.Task, w.Run, "wait", w.Requirement+": "+reason)
}
func waitObservation(spec WaitSpec, raw string, started, observed time.Time) (string, error) {
	var envelope struct {
		IsError    bool            `json:"isError"`
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct{ Type, Text string }
	}
	if len(raw) > 65536 {
		return "", fmt.Errorf("observation exceeds 64 KiB; narrow the read tool's output")
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return "", fmt.Errorf("observation is not valid MCP JSON")
	}
	if envelope.IsError {
		return "", fmt.Errorf("observation tool reported an error")
	}
	data := envelope.Structured
	if len(data) == 0 || string(data) == "null" {
		if len(envelope.Content) != 1 || envelope.Content[0].Type != "text" {
			return "", fmt.Errorf("observation needs structuredContent or one JSON text result")
		}
		data = []byte(envelope.Content[0].Text)
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if !json.Valid(data) || decoder.Decode(&value) != nil {
		return "", fmt.Errorf("observation data is not JSON")
	}
	constraints := []ArgumentConstraint{}
	for _, m := range spec.Match {
		constraints = append(constraints, ArgumentConstraint{Pointer: m.Pointer, Allowed: []json.RawMessage{json.RawMessage(m.ExpectedJSON)}})
	}
	if !constraintsMatch(value, constraints) {
		return "", fmt.Errorf("required external conditions are not yet met")
	}
	version, ok := jsonPointer(value, spec.VersionPointer)
	if !ok || version == nil || version == "" {
		return "", fmt.Errorf("observation lacks the required version identity")
	}
	switch version.(type) {
	case string, json.Number:
	default:
		return "", fmt.Errorf("version identity must be a string or number")
	}
	b, _ := json.Marshal(version)
	if len(b) > 2000 {
		return "", fmt.Errorf("version identity is too large")
	}
	identity := string(b)
	if spec.EventTimePointer != "" {
		event, ok := jsonPointer(value, spec.EventTimePointer)
		text, isString := event.(string)
		stamp, err := time.Parse(time.RFC3339Nano, text)
		if !ok || !isString || err != nil || stamp.Before(started) || stamp.After(observed) {
			return "", fmt.Errorf("required event has not occurred within this wait's observation window")
		}
		identity += "@" + stamp.UTC().Format(time.RFC3339Nano)
	}
	return identity, nil
}
func (e *Engine) applyWaitResult(claim DurableWait, result string, callErr error, at time.Time) {
	s := e.Store
	w := s.runWait(claim.Run, claim.Requirement)
	var r Run
	var task Assignment
	if w.State != "pending" || w.Generation != claim.Generation || w.Lease != claim.Lease || s.Get(w.Run, &r) != nil || s.Get(w.Task, &task) != nil || r.Superseded || r.State == "cancelled" || task.State == "cancelled" {
		return
	}
	w.Lease = ""
	if task.State == "paused" || task.State == "ready" || r.State == "blocked" {
		w.MatchedSince = ""
		_ = s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w)
		return
	}
	if err := e.authorizeWait(r, w); err != nil {
		e.failWait(w, r, err.Error())
		return
	}
	deadline, _ := time.Parse(time.RFC3339Nano, w.Deadline)
	if w.Spec.Tool != "" && !at.Before(deadline) {
		e.failWait(w, r, "Observation deadline reached")
		return
	}
	version := "timer"
	if w.Spec.Tool != "" && callErr == nil {
		eventAfter := w.EventAfter
		if eventAfter == "" {
			eventAfter = w.Created
		}
		started, _ := time.Parse(time.RFC3339Nano, eventAfter)
		version, callErr = waitObservation(w.Spec, result, started, at)
	}
	w.Checks++
	w.CheckedAt = at.Format(time.RFC3339Nano)
	w.Result = clipped(result, 65536)
	w.Reason = ""
	if callErr != nil {
		w.MatchedSince = ""
		w.Version = ""
		w.Reason = callErr.Error()
	} else {
		if w.Spec.Tool == "" {
			w.MatchedSince = w.Created
		} else if w.MatchedSince == "" || w.Version != version {
			w.MatchedSince = at.Format(time.RFC3339Nano)
		}
		w.Version = version
		since, _ := time.Parse(time.RFC3339Nano, w.MatchedSince)
		if !at.Before(since.Add(time.Duration(w.Spec.StableSeconds) * time.Second)) {
			w.State = "satisfied"
			_, step, _ := e.plannedStep(r)
			for _, req := range step.Requirements {
				if req.Key == w.Requirement {
					old := s.milestoneEvidence(r.ID, req.Key)
					_, err := e.recordMilestone(r, milestoneInput{Requirement: req.Key, Kind: req.Kind, Target: req.Target, Revision: old.Revision, Summary: fmt.Sprintf("ADC observed the required conditions for %d seconds; %d checks. Version %s. Inspect the durable wait's saved result and source definition.", w.Spec.StableSeconds, w.Checks, w.Version), Reference: "adc-wait:" + w.ID, ObservedAt: w.CheckedAt}, "", &w)
					if err != nil {
						e.failWait(w, r, "Could not record observation: "+err.Error())
					}
					return
				}
			}
			e.failWait(w, r, "The observation requirement is no longer available in the active plan")
			return
		}
	}
	w.NextAt = at.Add(time.Duration(w.Spec.PollSeconds) * time.Second).Format(time.RFC3339Nano)
	_ = s.Put("durable-wait", w.Org, w.Task, w.State, w.ID, w)
}
