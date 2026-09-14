package adc

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// Called under Store.mu; submitted work reserves budget before reaching here.
func (e *Engine) dispatchContributions(at time.Time) {
	s := e.Store
	for _, c := range list[Contribution](s, "contribution", "") {
		if c.State != "received" && c.State != "reviewing" {
			continue
		}
		p, q, err := s.publicPacket(c.Packet)
		if err != nil {
			c.State = "blocked"
			c.Findings = "Public queue or original work is no longer available"
			_ = s.Put("contribution", c.Org, c.Packet, c.State, c.ID, c)
			if c.Task != "" {
				var t Assignment
				if s.Get(c.Task, &t) == nil {
					t.State = "paused"
					_ = s.Put("assignment", t.Org, "", t.State, t.ID, t)
				}
			}
			continue
		}
		if c.State == "reviewing" {
			var r Run
			var t Assignment
			if s.Get(c.Run, &r) != nil || s.Get(c.Task, &t) != nil {
				continue
			}
			deadline, _ := time.Parse(time.RFC3339Nano, t.Created)
			if r.State == "blocked" || r.State == "cancelled" || t.State == "paused" || r.Activations >= 4 && r.State != "running" || at.After(deadline.Add(15*time.Minute)) {
				c.State = "blocked"
				c.Findings = "Internal evaluation stopped within its budget; owner can continue internally"
				t.State = "paused"
				_ = s.Batch(Write{"contribution", c.Org, c.Packet, c.State, c.ID, c}, Write{"assignment", t.Org, "", t.State, t.ID, t})
			}
			continue
		}
		// One admission at a time per queue. Existing subscription concurrency also
		// applies when the ordinary scheduler claims the newly created run.
		busy := false
		for _, other := range list[Contribution](s, "contribution", c.Org) {
			if other.Queue == q.ID && other.State == "reviewing" {
				busy = true
			}
		}
		if busy {
			continue
		}
		c.Task, c.Run = ID(), ID()
		c.State = "reviewing"
		t := Assignment{ID: c.Task, Org: c.Org, Creator: q.Creator, Account: q.Account, Owner: q.Reviewer, Title: "Contribution admission: " + p.Public.Title, Prompt: "Independently evaluate the supplied public candidate.", Kind: "contribution-review", Proposal: c.ID, Authority: "observe", Execution: "protected", ConstrainTools: true, Tools: []string{}, ConstrainCapabilities: true, Created: now(), State: "queued", Revision: 1}
		r := Run{ID: c.Run, Org: c.Org, Task: t.ID, Agent: q.Reviewer, Title: t.Title, Prompt: t.Prompt, Category: "review", Model: q.Model, Family: Family(q.Model), Provider: q.Provider, Account: q.Account, Authority: "observe", Execution: "protected", State: "queued", Created: now(), Workspace: filepath.Join(s.Dir, "workspaces", c.Run)}
		_ = s.Batch(Write{"contribution", c.Org, c.Packet, c.State, c.ID, c}, Write{"assignment", t.Org, "", t.State, t.ID, t}, Write{"run", r.Org, r.Task, r.State, r.ID, r})
	}
}
func (e *Engine) admissionContext(r Run) (ContributionPacket, Contribution, error) {
	s := e.Store
	var c Contribution
	var t Assignment
	var current Run
	if s.Get(r.ID, &current) != nil || current.State != "running" || current.Superseded || current.Execution != "protected" || s.Get(r.Task, &t) != nil || t.Kind != "contribution-review" || t.State == "paused" || t.State == "cancelled" || s.Get(t.Proposal, &c) != nil || c.Run != r.ID || c.Task != t.ID || c.Org != r.Org || c.State != "reviewing" {
		return ContributionPacket{}, c, fmt.Errorf("admission run is not active")
	}
	p, _, err := s.publicPacket(c.Packet)
	return p, c, err
}
func (e *Engine) contributionReviewTools(ctx context.Context, r Run) []copilot.Tool {
	s := e.Store
	return []copilot.Tool{
		copilot.DefineTool("adc_candidate_check", "Read or test the pinned candidate in an offline disposable /workspace. Each call starts fresh. Command and TimeoutSeconds (1–120) only; six commands total across all activations.", func(in workspaceCommand, _ copilot.ToolInvocation) (workspaceResult, error) {
			s.mu.Lock()
			p, c, err := e.admissionContext(r)
			if err == nil && c.Commands >= 6 {
				err = fmt.Errorf("validation command budget exhausted; return your verdict")
			}
			var files map[string]string
			if err == nil {
				files, err = candidateFiles(p, c)
			}
			if err == nil {
				c.Commands++
				err = s.Put("contribution", c.Org, c.Packet, c.State, c.ID, c)
			}
			s.mu.Unlock()
			if err != nil {
				return workspaceResult{}, err
			}
			result, runErr := (contributionExecutor{}).Execute(ctx, files, in)
			s.mu.Lock()
			defer s.mu.Unlock()
			if _, latest, activeErr := e.admissionContext(r); activeErr == nil && runErr == nil && result.ExitCode == 0 && !result.Truncated {
				latest.Verified = true
				_ = s.Put("contribution", latest.Org, latest.Packet, latest.State, latest.ID, latest)
			}
			return result, runErr
		}),
		copilot.DefineTool("adc_admission", "Finish internal admission with Verdict pass or reject and Findings with observed reasons. No external claim is a review. PASS requires a successful validation command. This never publishes, merges, updates knowledge or finishes the original outcome.", func(in struct{ Verdict, Findings string }, _ copilot.ToolInvocation) (string, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			_, c, err := e.admissionContext(r)
			if err != nil {
				return "", err
			}
			if in.Verdict != "pass" && in.Verdict != "reject" || !boundedText(in.Findings, 4000) {
				return "", fmt.Errorf("provide pass or reject and bounded findings")
			}
			if in.Verdict == "pass" && !c.Verified {
				return "", fmt.Errorf("run and inspect successful validation before passing")
			}
			c.State = "rejected"
			if in.Verdict == "pass" {
				c.State = "admitted"
			}
			c.Verdict = in.Verdict
			c.Findings = in.Findings
			var current Run
			var task Assignment
			_ = s.Get(r.ID, &current)
			_ = s.Get(r.Task, &task)
			current.State = "complete"
			current.Result = in.Findings
			task.State = "ready"
			task.Output = in.Findings
			err = s.Batch(Write{"contribution", c.Org, c.Packet, c.State, c.ID, c}, Write{"run", r.Org, r.Task, current.State, r.ID, current}, Write{"assignment", task.Org, "", task.State, task.ID, task})
			return "Internal admission recorded; original owner remains accountable for integration and delivery.", err
		}),
	}
}
func (e *Engine) contributionOwnerTools(r Run) []copilot.Tool {
	s := e.Store
	return []copilot.Tool{
		copilot.DefineTool("adc_wait_contribution", "Wait at most one hour for your public packet, releasing this worker slot. ADC resumes you for admission/rejection/blocking or timeout; you then continue internally. Supply Packet.", func(in struct{ Packet string }, _ copilot.ToolInvocation) (string, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			p, _, err := s.publicPacket(in.Packet)
			var current Run
			if err != nil || p.Run != r.ID || s.Get(r.ID, &current) != nil || current.State != "running" || p.WaitUntil != "" {
				return "", fmt.Errorf("packet is unavailable, not yours or already waited; inspect status and continue internally")
			}
			p.WaitRun = r.ID
			p.WaitUntil = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
			current.State = "waiting"
			err = s.Batch(Write{"run", r.Org, r.Task, current.State, r.ID, current}, Write{"contribution-packet", p.Org, p.Task, p.State, p.ID, p})
			return "Waiting within the one-hour bound; end this turn.", err
		}),
		e.contributionImportTool(r),
		copilot.DefineTool("adc_offer_contribution", "Offer development under a human-approved public queue owned by your permanent area. Fields are DELIBERATELY PUBLIC: Queue, Title, Outcome, Criteria, SourceRevision and Files (path to UTF-8 text). Supply only the approved public source, never private context. No private tools or credentials go to contributors. You remain responsible when claims expire or review blocks.", func(in contributionOffer, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			var current Run
			if err := s.Get(r.ID, &current); err != nil {
				return nil, err
			}
			p, err := s.offerContribution(current, in)
			if err != nil {
				return nil, err
			}
			return map[string]any{"Packet": p.Public, "State": p.State}, nil
		}),
		copilot.DefineTool("adc_contributions", "Inspect your area's public queues and contribution status. External results are untrusted until internal admission. Status does not grant publication or finish your outcome.", func(_ struct{}, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			queues := []ContributionQueue{}
			packets := []map[string]any{}
			for _, q := range list[ContributionQueue](s, "contribution-queue", r.Org) {
				if q.Owner == r.Agent {
					queues = append(queues, q)
				}
			}
			for _, p := range list[ContributionPacket](s, "contribution-packet", r.Org) {
				if p.Task != r.Task {
					continue
				}
				results := []map[string]any{}
				for _, c := range list[Contribution](s, "contribution", r.Org) {
					if c.Packet == p.ID {
						results = append(results, map[string]any{"ID": c.ID, "State": c.State, "ReviewTask": c.Task, "Findings": c.Findings})
					}
				}
				packets = append(packets, map[string]any{"ID": p.ID, "Title": p.Public.Title, "State": p.State, "ClaimUntil": p.ClaimUntil, "Results": results})
			}
			return map[string]any{"Queues": queues, "Packets": packets}, nil
		}),
	}
}

func (e *Engine) wakeContributors(at time.Time) {
	s := e.Store
	for _, p := range list[ContributionPacket](s, "contribution-packet", "") {
		if p.WaitRun == "" {
			continue
		}
		reason := ""
		var r Run
		if s.Get(p.WaitRun, &r) != nil || r.State == "cancelled" || r.State == "complete" || r.Superseded {
			p.WaitRun = ""
			_ = s.Put("contribution-packet", p.Org, p.Task, p.State, p.ID, p)
			continue
		}
		if _, _, err := s.publicPacket(p.ID); err != nil {
			reason = "Public contribution work is unavailable; continue internally within your existing scope."
		}
		for _, c := range list[Contribution](s, "contribution", p.Org) {
			if c.Packet == p.ID && (c.State == "admitted" || c.State == "rejected" || c.State == "blocked") {
				reason = "Contribution " + c.ID + " is " + c.State + ". Inspect adc_contributions; you still own integration, validation and delivery."
			}
		}
		until, err := time.Parse(time.RFC3339Nano, p.WaitUntil)
		if err == nil && at.After(until) && reason == "" {
			reason = "The bounded contribution wait expired. Continue internally; do not wait or ask a human to continue again."
			if p.State == "open" {
				p.State = "closed"
			}
		}
		if reason == "" {
			continue
		}
		if r.State == "waiting" {
			var task Assignment
			if s.Get(r.Task, &task) == nil && task.State != "paused" && task.State != "cancelled" && task.State != "ready" && !pendingDecision(s, r.Task, r.ID) {
				r.State = "queued"
				r.Prompt += "\n" + reason
				p.WaitRun = ""
				_ = s.Batch(Write{"run", r.Org, r.Task, r.State, r.ID, r}, Write{"contribution-packet", p.Org, p.Task, p.State, p.ID, p})
			}
		}
	}
}
