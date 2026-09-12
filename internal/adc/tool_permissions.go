package adc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ArgumentConstraint deliberately supports exact finite values only. It does
// not pretend to understand arbitrary shell commands or API request bodies.
type ArgumentConstraint struct {
	Pointer string
	Allowed []json.RawMessage
}
type ToolPolicy struct {
	StandingRules                                              [][]ArgumentConstraint
	GrantEpoch, StandingEpoch                                  int
	ID, Org, Connection, Tool, Fingerprint, Mode, Class, Human string
	Revision                                                   int
	Constraints                                                []ArgumentConstraint
}
type CapabilityGrant struct {
	Binding                                                      *OperationBinding
	GrantEpoch, StandingEpoch                                    int
	Class                                                        string
	Tool, Fingerprint, Scope, Operation, ArgumentsHash, Approval string
	PolicyRevision                                               int
	Constraints                                                  []ArgumentConstraint
}
type AccessEntry struct {
	Binding                                     *OperationBinding
	Arguments                                   json.RawMessage
	Tool, Fingerprint, Operation, ArgumentsHash string
	Constraints                                 []ArgumentConstraint
}
type AccessRequest struct {
	ID, Org, Task, Decision, Purpose, State, Created, Resolved, Human string
	Revision                                                          int
	Runs                                                              []string
	Entries                                                           []AccessEntry
}
type GatewayOperation struct {
	Grant                                                                             CapabilityGrant
	PolicyRevision                                                                    int
	ID, Org, Task, Run, Key, Tool, Fingerprint, ArgumentsHash, State, Result, Created string
}

func canonicalArguments(raw json.RawMessage) (json.RawMessage, any, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON arguments")
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, nil, fmt.Errorf("tool arguments must be an object")
	}
	// Unmarshal separately ensures there is no trailing JSON after the object.
	if !json.Valid(raw) {
		return nil, nil, fmt.Errorf("invalid JSON arguments")
	}
	b, err := json.Marshal(value)
	return b, value, err
}
func jsonPointer(value any, pointer string) (any, bool) {
	if pointer == "" {
		return value, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	for _, key := range strings.Split(pointer[1:], "/") {
		for i := 0; i < len(key); i++ {
			if key[i] == '~' {
				if i+1 >= len(key) || (key[i+1] != '0' && key[i+1] != '1') {
					return nil, false
				}
				i++
			}
		}
		key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[key]
			if !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(v) || strconv.Itoa(i) != key {
				return nil, false
			}
			value = v[i]
		default:
			return nil, false
		}
	}
	return value, true
}
func constraintsMatch(value any, constraints []ArgumentConstraint) bool {
	for _, constraint := range constraints {
		got, ok := jsonPointer(value, constraint.Pointer)
		if !ok || len(constraint.Allowed) == 0 {
			return false
		}
		b, err := json.Marshal(got)
		if err != nil {
			return false
		}
		matched := false
		for _, allowed := range constraint.Allowed {
			var v any
			dec := json.NewDecoder(bytes.NewReader(allowed))
			dec.UseNumber()
			if dec.Decode(&v) != nil || !json.Valid(allowed) {
				continue
			}
			a, _ := json.Marshal(v)
			if bytes.Equal(a, b) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// Installation policy updates require a human context supplied by Web. The
// revision check prevents a stale browser form from overwriting newer policy.
type PolicyChange struct {
	Policy   ToolPolicy
	Expected int
}

func (s *Store) SaveToolPolicy(human, org string, policy ToolPolicy, expected int) error {
	return s.SaveToolPolicies(human, org, []PolicyChange{{Policy: policy, Expected: expected}})
}
func (s *Store) SaveToolPolicies(human, org string, changes []PolicyChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(changes) == 0 || len(changes) > 1000 {
		return fmt.Errorf("select 1–1000 tools to classify")
	}
	writes := []Write{}
	seen := map[string]bool{}
	for _, change := range changes {
		if seen[change.Policy.Tool] {
			return fmt.Errorf("each tool may appear only once")
		}
		seen[change.Policy.Tool] = true
		next, err := s.toolPolicyWrites(human, org, change.Policy, change.Expected)
		if err != nil {
			return err
		}
		writes = append(writes, next...)
	}
	return s.Batch(writes...)
}
func (s *Store) toolPolicyWrites(human, org string, policy ToolPolicy, expected int) ([]Write, error) {
	if !s.permissionHuman(human, org) {
		return nil, fmt.Errorf("organization membership required")
	}
	var tool GatewayTool
	if s.Get(policy.Tool, &tool) != nil || tool.Org != org || tool.Fingerprint != policy.Fingerprint {
		return nil, fmt.Errorf("refresh the discovered tool before classifying it")
	}
	if policy.Mode != "allow" && policy.Mode != "approval" && policy.Mode != "deny" {
		return nil, fmt.Errorf("choose allow, approval or deny")
	}
	if policy.Class != "read" && policy.Class != "change" && policy.Class != "broad" {
		return nil, fmt.Errorf("classify the tool as read, change or broad")
	}
	var previous ToolPolicy
	id := "policy-" + tool.ID
	_ = s.Get(id, &previous)
	if previous.Revision != expected {
		return nil, fmt.Errorf("policy changed; reload before saving")
	}
	for _, c := range policy.Constraints {
		if c.Pointer != "" && !strings.HasPrefix(c.Pointer, "/") || len(c.Allowed) == 0 {
			return nil, fmt.Errorf("constraints require a JSON pointer and finite allowed values")
		}
		for _, v := range c.Allowed {
			if !json.Valid(v) {
				return nil, fmt.Errorf("constraint values must be JSON")
			}
		}
	}
	// The human policy form explicitly replaces standing rules with its reviewed ceiling.
	policy.StandingRules = nil
	policy.ID, policy.Org, policy.Connection, policy.Human = id, org, tool.Connection, human
	policy.Revision = previous.Revision + 1
	policy.GrantEpoch, policy.StandingEpoch = previous.GrantEpoch, previous.StandingEpoch
	if previous.ID != "" && ((policy.Mode == "deny" && previous.Mode != "deny") || policy.Class != previous.Class || policy.Fingerprint != previous.Fingerprint) {
		policy.GrantEpoch++
	}
	if previous.Mode == "allow" && policy.Mode != "allow" {
		policy.StandingEpoch++
	}
	return []Write{{"tool-policy-history", org, id, previous.Mode, ID(), previous}, {"tool-policy", org, tool.Connection, policy.Mode, id, policy}}, nil
}
func (s *Store) permissionHuman(human, org string) bool {
	var count int
	_ = s.db.QueryRow(`SELECT count(*) FROM memberships WHERE user_id=? AND org=?`, human, org).Scan(&count)
	return human != "" && count > 0
}

// Caller holds Store.mu. Existing work never inherits later standing grants.
func (s *Store) initialCapabilities(org string, connections []string) []CapabilityGrant {
	grants := []CapabilityGrant{}
	for _, p := range list[ToolPolicy](s, "tool-policy", org) {
		var tool GatewayTool
		if p.Mode != "allow" || !Subset([]string{p.Connection}, connections) || s.Get(p.Tool, &tool) != nil || tool.Fingerprint != p.Fingerprint {
			continue
		}
		rules := p.StandingRules
		if len(rules) == 0 {
			rules = [][]ArgumentConstraint{nil}
		}
		for _, rule := range rules {
			constraints := append(append([]ArgumentConstraint{}, p.Constraints...), rule...)
			grants = append(grants, CapabilityGrant{GrantEpoch: p.GrantEpoch, StandingEpoch: p.StandingEpoch, Class: p.Class, Tool: p.Tool, Fingerprint: p.Fingerprint, Scope: "assignment", PolicyRevision: p.Revision, Constraints: constraints, Approval: "standing:" + p.ID})
		}
	}
	return grants
}

// Caller holds Store.mu. A caller cannot obtain another run's authority merely
// by supplying its ID through the model: provider handlers bind runID themselves.
func (s *Store) authorizeGateway(runID string, tool GatewayTool, operation string, raw json.RawMessage) (Run, error) {
	var run Run
	if s.Get(runID, &run) != nil || run.State != "running" || run.Org != tool.Org || !Subset([]string{tool.Connection}, run.Tools) {
		return Run{}, fmt.Errorf("this run does not hold this connection")
	}
	return s.authorizeGatewayRun(run, tool, operation, raw)
}
func (s *Store) authorizeGatewayRun(run Run, tool GatewayTool, operation string, raw json.RawMessage) (Run, error) {
	var task Assignment
	if s.Get(run.Task, &task) != nil || task.Org != run.Org || task.State == "paused" || task.State == "cancelled" {
		return Run{}, fmt.Errorf("assignment is unavailable")
	}
	var policy ToolPolicy
	if s.Get("policy-"+tool.ID, &policy) != nil || policy.Org != run.Org || policy.Fingerprint != tool.Fingerprint || policy.Mode == "deny" {
		return Run{}, fmt.Errorf("tool needs current human classification or is denied")
	}
	var conn Connection
	if s.Get(tool.Connection, &conn) != nil || conn.Org != run.Org || connectionRevision(conn) != tool.ConnectionRevision {
		return Run{}, fmt.Errorf("connection changed or is unavailable; refresh and review access")
	}
	canonical, value, err := canonicalArguments(raw)
	if err != nil {
		return Run{}, err
	}
	if !constraintsMatch(value, policy.Constraints) {
		return Run{}, fmt.Errorf("arguments exceed installation policy")
	}
	for _, g := range task.Capabilities {
		if !s.operationGrantMatches(run, policy, g) {
			continue
		}
		if strings.HasPrefix(g.Approval, "standing:") && (policy.Mode != "allow" || g.StandingEpoch != policy.StandingEpoch) {
			continue
		}
		if g.GrantEpoch != policy.GrantEpoch || g.Tool != tool.ID || g.Fingerprint != tool.Fingerprint || g.Class != policy.Class || !constraintsMatch(value, g.Constraints) {
			continue
		}
		if g.Scope == "operation" && (operation == "" || g.Operation != operation || g.ArgumentsHash != digest(string(canonical))) {
			continue
		}
		if g.Scope != "operation" && g.Scope != "assignment" {
			continue
		}
		return run, nil
	}
	return Run{}, fmt.Errorf("this task needs approval for the requested capability")
}

func (s *Store) CallGateway(ctx context.Context, runID, toolID, operation string, raw json.RawMessage) (string, error) {
	if strings.TrimSpace(operation) == "" || len(operation) > 200 {
		return "", fmt.Errorf("supply a stable operation identifier of at most 200 characters; reuse it only for the same intended operation")
	}
	args, _, err := canonicalArguments(raw)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	var tool GatewayTool
	if s.Get(toolID, &tool) != nil {
		s.mu.Unlock()
		return "", fmt.Errorf("discover the tool first")
	}
	run, err := s.authorizeGateway(runID, tool, operation, args)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	id := "operation-" + digest(run.Task+":"+operation)
	var prior GatewayOperation
	reconcile := false
	if s.Get(id, &prior) == nil {
		if prior.Org != run.Org || prior.Tool != tool.ID || prior.Fingerprint != tool.Fingerprint || prior.ArgumentsHash != digest(string(args)) {
			s.mu.Unlock()
			return "", fmt.Errorf("operation identifier is already bound to different arguments or tool")
		}
		if prior.State == "complete" {
			var c Connection
			if prior.Run != runID && tool.Name == "github_fetch" && s.Get(tool.Connection, &c) == nil && c.Transport == "github" {
				s.mu.Unlock()
				return "", fmt.Errorf("fetch belongs to another worker workspace; use this run's own fetch operation identifier")
			}
			s.mu.Unlock()
			return prior.Result, nil
		}
		reconcile = s.retryableGitHubDelivery(tool, prior)
		if !reconcile {
			s.mu.Unlock()
			return "", fmt.Errorf("prior operation has an uncertain or in-flight outcome; reconcile before starting another mutation")
		}
	}
	s.mu.Unlock()
	admitted := false
	record := GatewayOperation{ID: id, Org: run.Org, Task: run.Task, Run: runID, Key: operation, Tool: tool.ID, Fingerprint: tool.Fingerprint, ArgumentsHash: digest(string(args)), State: "in-flight", Created: now()}
	mutationAttempted := false
	ctx = context.WithValue(ctx, githubActorKey{}, githubActor{Run: run.ID, Operation: operation, Arguments: args, MutationAttempted: &mutationAttempted})
	result, err := s.invokeGateway(ctx, tool, args, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, err := s.authorizeGateway(runID, tool, operation, args); err != nil {
			return err
		}
		var current GatewayTool
		if s.Get(toolID, &current) != nil || current.Fingerprint != tool.Fingerprint {
			return fmt.Errorf("tool changed during admission")
		}
		if s.Get(id, &prior) == nil && (!reconcile || !s.retryableGitHubDelivery(tool, prior)) {
			return fmt.Errorf("another call already claimed this operation")
		}
		var policy ToolPolicy
		_ = s.Get("policy-"+tool.ID, &policy)
		var task Assignment
		_ = s.Get(run.Task, &task)
		_, value, _ := canonicalArguments(args)
		record.PolicyRevision = policy.Revision
		for _, grant := range task.Capabilities {
			if !s.operationGrantMatches(run, policy, grant) {
				continue
			}
			if grant.GrantEpoch != policy.GrantEpoch || grant.Tool != tool.ID || grant.Fingerprint != tool.Fingerprint || grant.Class != policy.Class || !constraintsMatch(value, grant.Constraints) {
				continue
			}
			if strings.HasPrefix(grant.Approval, "standing:") && (policy.Mode != "allow" || grant.StandingEpoch != policy.StandingEpoch) {
				continue
			}
			if grant.Scope == "operation" && (grant.Operation != operation || grant.ArgumentsHash != digest(string(args))) {
				continue
			}
			if grant.Scope != "operation" && grant.Scope != "assignment" {
				continue
			}
			record.Grant = grant
			break
		}
		if record.Grant.Approval == "" {
			return fmt.Errorf("effective grant unavailable")
		}
		writes := []Write{{"gateway-operation", run.Org, run.Task, record.State, id, record}}
		if reconcile {
			history := prior
			history.ID = ID()
			writes = append(writes, Write{"gateway-operation-history", run.Org, run.Task, history.State, history.ID, history})
		}
		if err := s.Batch(writes...); err != nil {
			return err
		}
		admitted = true
		return nil
	})
	if admitted {
		s.mu.Lock()
		defer s.mu.Unlock()
		var current GatewayOperation
		if s.Get(id, &current) != nil || current.Created != record.Created || current.State != "in-flight" {
			return "", fmt.Errorf("operation was superseded during recovery; inspect its current outcome")
		}
		record.State, record.Result = "complete", result
		if err != nil {
			record.State, record.Result = "uncertain", err.Error()
			var c Connection
			if tool.Name == "github_draft_pr" && !mutationAttempted && s.Get(tool.Connection, &c) == nil && c.Transport == "github" {
				record.State = "failed"
			}
		}
		if saveErr := s.Put("gateway-operation", run.Org, run.Task, record.State, id, record); saveErr != nil {
			return "", fmt.Errorf("external operation returned but recording failed; reconcile before retrying")
		}
	}
	return result, err
}

func canonicalEntries(entries []AccessEntry) string {
	copy := append([]AccessEntry(nil), entries...)
	sort.Slice(copy, func(i, j int) bool {
		a, _ := json.Marshal(copy[i])
		b, _ := json.Marshal(copy[j])
		return string(a) < string(b)
	})
	b, _ := json.Marshal(copy)
	return string(b)
}
