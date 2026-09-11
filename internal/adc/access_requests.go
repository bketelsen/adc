package adc

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type AccessWant struct {
	Tool, Operation string
	Arguments       json.RawMessage
	Constraints     []ArgumentConstraint
}

func (s *Store) RequestAccess(runID, purpose string, wants []AccessWant) (AccessRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var run Run
	if s.Get(runID, &run) != nil || run.State != "running" {
		return AccessRequest{}, fmt.Errorf("requesting run is not active")
	}
	var task Assignment
	if s.Get(run.Task, &task) != nil || task.Org != run.Org || task.Execution != "protected" || task.Kind == "proposal" {
		return AccessRequest{}, fmt.Errorf("capability requests require a protected execution assignment")
	}
	if strings.TrimSpace(purpose) == "" || len(wants) == 0 || len(wants) > 30 {
		return AccessRequest{}, fmt.Errorf("provide the purpose and a bundle of 1–30 tools")
	}
	entries := []AccessEntry{}
	for _, want := range wants {
		var tool GatewayTool
		if s.Get(want.Tool, &tool) != nil || tool.Org != run.Org {
			return AccessRequest{}, fmt.Errorf("discover the requested tool in this organization first")
		}
		var policy ToolPolicy
		if s.Get("policy-"+tool.ID, &policy) == nil && policy.Mode == "deny" {
			return AccessRequest{}, fmt.Errorf("tool is explicitly denied; a human must change installation policy")
		}
		entry := AccessEntry{Tool: tool.ID, Fingerprint: tool.Fingerprint, Operation: want.Operation, Constraints: want.Constraints}
		if len(want.Arguments) > 0 {
			canonical, value, err := canonicalArguments(want.Arguments)
			if err != nil {
				return AccessRequest{}, err
			}
			if err := validateGatewayArguments(tool.Schema, canonical); err != nil {
				return AccessRequest{}, err
			}
			entry.Arguments = canonical
			entry.ArgumentsHash = digest(string(canonical))
			if !constraintsMatch(value, entry.Constraints) {
				return AccessRequest{}, fmt.Errorf("proposed arguments do not satisfy the proposed restrictions")
			}
			// Supplied arguments describe an exact operation, including absent
			// optional keys. A model cannot add force/method/recursive later.
			// To propose a variable capability, omit Arguments and show explicit
			// Constraints, or let the human edit the exact constraint.
			entry.Constraints = append(entry.Constraints, ArgumentConstraint{Pointer: "", Allowed: []json.RawMessage{canonical}})
		}
		entries = append(entries, entry)
	}
	request := AccessRequest{ID: ID(), Org: run.Org, Task: run.Task, Purpose: purpose, State: "pending", Created: now(), Revision: 1, Runs: []string{run.ID}, Entries: entries}
	writes := []Write{}
	for _, pending := range list[AccessRequest](s, "access-request", run.Org) {
		if pending.Task != run.Task || pending.State != "pending" {
			continue
		}
		if canonicalEntries(pending.Entries) == canonicalEntries(entries) && slices.Contains(pending.Runs, run.ID) {
			run.State = "waiting"
			return pending, s.Put("run", run.Org, run.Task, run.State, run.ID, run)
		}
		request = pending
		history := pending
		history.ID = ID()
		writes = append(writes, Write{"access-request-history", run.Org, request.ID, pending.State, history.ID, history})
		for _, entry := range entries {
			if !slices.ContainsFunc(request.Entries, func(old AccessEntry) bool {
				return canonicalEntries([]AccessEntry{old}) == canonicalEntries([]AccessEntry{entry})
			}) {
				request.Entries = append(request.Entries, entry)
			}
		}
		if len(request.Entries) > 30 {
			return AccessRequest{}, fmt.Errorf("pending bundle is full; resolve it before requesting more tools")
		}
		if !slices.Contains(request.Runs, run.ID) {
			request.Runs = append(request.Runs, run.ID)
		}
		request.Revision++
		if purpose != request.Purpose {
			request.Purpose += "\n\n" + purpose
		}
		var old Decision
		if s.Get(request.Decision, &old) == nil {
			old.State = "superseded"
			writes = append(writes, Write{"decision", old.Org, old.Task, old.State, old.ID, old})
		}
		break
	}
	decision := Decision{ID: ID(), Org: run.Org, Task: run.Task, Run: run.ID, Kind: "permission", State: "pending", Question: "Additional access requested\n\n" + request.Purpose}
	request.Decision = decision.ID
	run.State = "waiting"
	writes = append(writes, Write{"access-request", run.Org, run.Task, request.State, request.ID, request}, Write{"decision", run.Org, run.Task, decision.State, decision.ID, decision}, Write{"run", run.Org, run.Task, run.State, run.ID, run})
	if err := s.Batch(writes...); err != nil {
		return AccessRequest{}, err
	}
	s.Log(run.Org, run.Task, run.ID, "permission-request", request.Purpose)
	return request, nil
}

func (s *Store) ResolveAccess(human, org, id string, revision int, scope, answer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.permissionHuman(human, org) {
		return fmt.Errorf("organization membership required")
	}
	var request AccessRequest
	if s.Get(id, &request) != nil || request.Org != org || request.State != "pending" || request.Revision != revision {
		return fmt.Errorf("request changed or was already resolved; reload before deciding")
	}
	if scope != "decline" && scope != "operation" && scope != "assignment" && scope != "standing" {
		return fmt.Errorf("choose operation, assignment, standing access or decline")
	}
	var task Assignment
	if s.Get(request.Task, &task) != nil || task.Org != org || task.State == "cancelled" || task.State == "paused" {
		return fmt.Errorf("assignment is unavailable")
	}
	writes := []Write{}
	connections := []string{}
	seen := map[string]bool{}
	if scope != "decline" {
		for _, entry := range request.Entries {
			var tool GatewayTool
			if s.Get(entry.Tool, &tool) != nil || tool.Org != org || tool.Fingerprint != entry.Fingerprint {
				return fmt.Errorf("requested tool changed; refresh the access proposal")
			}
			var conn Connection
			if s.Get(tool.Connection, &conn) != nil || conn.Org != org || connectionRevision(conn) != tool.ConnectionRevision {
				return fmt.Errorf("connection changed; refresh the access proposal")
			}
			var policy ToolPolicy
			_ = s.Get("policy-"+tool.ID, &policy)
			if policy.Mode == "deny" {
				return fmt.Errorf("installation policy now denies this tool")
			}
			if policy.ID != "" && policy.Fingerprint != tool.Fingerprint {
				return fmt.Errorf("tool definition changed; a human must reclassify it before approving access")
			}
			if policy.ID == "" {
				policy = ToolPolicy{ID: "policy-" + tool.ID, Org: org, Connection: tool.Connection, Tool: tool.ID, Fingerprint: tool.Fingerprint, Mode: "approval", Class: "broad", Revision: policy.Revision + 1, Human: human}
				writes = append(writes, Write{"tool-policy", org, tool.Connection, policy.Mode, policy.ID, policy})
			}
			grant := CapabilityGrant{GrantEpoch: policy.GrantEpoch, StandingEpoch: policy.StandingEpoch, Class: policy.Class, Tool: tool.ID, Fingerprint: tool.Fingerprint, PolicyRevision: policy.Revision, Scope: "assignment", Constraints: append(append([]ArgumentConstraint{}, policy.Constraints...), entry.Constraints...), Approval: request.ID}
			if scope == "operation" {
				if entry.Operation == "" || entry.ArgumentsHash == "" {
					return fmt.Errorf("one-operation approval requires proposed arguments and an operation identifier for every entry")
				}
				grant.Scope, grant.Operation, grant.ArgumentsHash = "operation", entry.Operation, entry.ArgumentsHash
			}
			if scope == "standing" {
				if seen[tool.ID] {
					return fmt.Errorf("standing approval needs one rule per tool; use assignment approval for this bundle")
				}
				writes = append(writes, Write{"tool-policy-history", org, policy.ID, policy.Mode, ID(), policy})
				// Standing approvals are alternative grant rules beneath the
				// installation ceiling, never new global restrictions. Adding
				// target Y must neither disable target X nor widen old tasks.
				if len(entry.Arguments) > 0 && string(entry.Arguments) != "null" {
					_, value, err := canonicalArguments(entry.Arguments)
					if err != nil || !constraintsMatch(value, policy.Constraints) {
						return fmt.Errorf("proposed arguments exceed installation policy; edit the request or review that policy")
					}
				}
				if policy.Mode != "allow" {
					policy.StandingRules = [][]ArgumentConstraint{entry.Constraints}
				} else if len(policy.StandingRules) > 0 {
					rule, _ := json.Marshal(entry.Constraints)
					found := false
					for _, existing := range policy.StandingRules {
						b, _ := json.Marshal(existing)
						if string(b) == string(rule) {
							found = true
						}
					}
					if !found {
						policy.StandingRules = append(policy.StandingRules, entry.Constraints)
					}
				}
				policy.Mode, policy.Human = "allow", human
				policy.Revision++
				grant.PolicyRevision = policy.Revision
				writes = append(writes, Write{"tool-policy", org, tool.Connection, policy.Mode, policy.ID, policy})
			}
			seen[tool.ID] = true
			task.Capabilities = append(task.Capabilities, grant)
			if !slices.Contains(connections, tool.Connection) {
				connections = append(connections, tool.Connection)
			}
		}
	}
	request.State, request.Human, request.Resolved = "approved", human, now()
	if scope == "decline" {
		request.State = "declined"
	}
	var decision Decision
	if s.Get(request.Decision, &decision) != nil || decision.State != "pending" {
		return fmt.Errorf("permission decision is no longer pending")
	}
	decision.State, decision.Answer = "answered", scope+": "+answer
	if task.State == "needs input" {
		stillPending := false
		for _, other := range taskDecisions(s, task.ID) {
			if other.ID != decision.ID && other.State == "pending" {
				stillPending = true
			}
		}
		if !stillPending {
			task.State = "queued"
		}
	}
	// A human may add a connection to this assignment's requesting runs and
	// their supervising chain. This is explicit expansion by the human, never
	// authority borrowed from a more powerful delegate or an unrelated role.
	updates := map[string]Run{}
	for _, id := range request.Runs {
		walkSeen := map[string]bool{}
		for id != "" {
			if walkSeen[id] {
				return fmt.Errorf("invalid supervising chain")
			}
			walkSeen[id] = true
			run, ok := updates[id]
			if !ok {
				if s.Get(id, &run) != nil || run.Org != org || run.Task != task.ID {
					return fmt.Errorf("requesting run is unavailable")
				}
			}
			for _, conn := range connections {
				if !slices.Contains(run.Tools, conn) {
					run.Tools = append(run.Tools, conn)
				}
			}
			if run.State == "waiting" || run.State == "blocked" {
				run.State, run.Attempts, run.Turns, run.NextAt = "queued", 0, 0, ""
			} else if run.State == "running" {
				run.UpdatesPending = true
			}
			updates[id] = run
			id = run.Parent
		}
	}
	for _, run := range updates {
		writes = append(writes, Write{"run", org, task.ID, run.State, run.ID, run})
	}
	writes = append(writes, Write{"access-request", org, task.ID, request.State, request.ID, request}, Write{"decision", org, task.ID, decision.State, decision.ID, decision}, Write{"assignment", org, "", task.State, task.ID, task})
	if err := s.Batch(writes...); err != nil {
		return err
	}
	s.Log(org, task.ID, "", "permission-decision", "Human "+human+" selected "+scope+" access. "+answer)
	return nil
}

// A human can revise constraints before deciding. Edits create a new visible
// revision and decision, so an approval of the previous bundle cannot race it.
func (s *Store) EditAccess(human, org, id string, revision int, constraints [][]ArgumentConstraint, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.permissionHuman(human, org) {
		return fmt.Errorf("organization membership required")
	}
	var request AccessRequest
	if s.Get(id, &request) != nil || request.Org != org || request.State != "pending" || request.Revision != revision || len(constraints) != len(request.Entries) {
		return fmt.Errorf("request changed; reload before editing")
	}
	history := request
	history.ID = ID()
	request.Entries = append([]AccessEntry(nil), request.Entries...)
	for i, rules := range constraints {
		for _, rule := range rules {
			if (rule.Pointer != "" && !strings.HasPrefix(rule.Pointer, "/")) || len(rule.Allowed) == 0 {
				return fmt.Errorf("provide JSON pointers and finite allowed values")
			}
			for _, value := range rule.Allowed {
				if !json.Valid(value) {
					return fmt.Errorf("allowed values must be JSON")
				}
			}
		}
		before, _ := json.Marshal(request.Entries[i].Constraints)
		after, _ := json.Marshal(rules)
		if string(before) != string(after) {
			request.Entries[i].Operation = ""
			request.Entries[i].ArgumentsHash = ""
			request.Entries[i].Arguments = nil
		}
		request.Entries[i].Constraints = rules
	}
	var old Decision
	if s.Get(request.Decision, &old) != nil || old.State != "pending" {
		return fmt.Errorf("decision is no longer pending")
	}
	old.State = "superseded"
	decision := Decision{ID: ID(), Org: org, Task: request.Task, Run: old.Run, Kind: "permission", State: "pending", Question: "Revised access request\n\n" + request.Purpose + "\n\nHuman edit: " + note, Replaces: old.ID}
	request.Revision++
	request.Decision = decision.ID
	if err := s.Batch(Write{"access-request-history", org, request.ID, history.State, history.ID, history}, Write{"access-request", org, request.Task, request.State, request.ID, request}, Write{"decision", org, request.Task, old.State, old.ID, old}, Write{"decision", org, request.Task, decision.State, decision.ID, decision}); err != nil {
		return err
	}
	s.Log(org, request.Task, "", "permission-edit", "Human "+human+" revised the proposed access constraints. "+note)
	return nil
}
