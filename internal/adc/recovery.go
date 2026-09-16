package adc

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Caller holds Store.mu. A stalled specialist returns responsibility to its
// supervisor; it does not demand a routine human "go ahead".
func (e *Engine) escalate(r *Run, reason string) {
	writes := e.escalationWrites(r, reason)
	_ = e.Store.Batch(append(writes, Write{"run", r.Org, r.Task, r.State, r.ID, *r})...)
}

func (e *Engine) escalationWrites(r *Run, reason string) []Write {
	r.State = "blocked"
	r.Error = reason
	s := e.Store
	if r.Parent != "" {
		var parent Run
		if s.Get(r.Parent, &parent) == nil {
			guidance := "Assess all sides and change approach; use adc_reassign with an explicit new approach."
			if strings.HasPrefix(reason, "Invalid preflight declaration:") {
				guidance = "Use adc_repair_step to correct the invalid preflight declaration; reassigning the unchanged declaration cannot fix it."
			}
			parent.Prompt += "\nA delegated run is blocked: " + r.ID + " (" + r.Title + "). Evidence: " + reason + ". " + guidance + " Escalate to the human only for a real decision or exhausted approaches."
			if parent.State == "running" {
				parent.UpdatesPending = true
			} else if parent.State != "complete" && parent.State != "cancelled" && parent.State != "blocked" && !pendingDecision(s, parent.Task, parent.ID) {
				parent.State = "queued"
			}
			parent.Turns = 0
			s.Log(r.Org, r.Task, r.ID, "escalation", "Returned blocked work to supervisor: "+reason)
			return []Write{{"run", parent.Org, parent.Task, parent.State, parent.ID, parent}}
		}
	}
	var task Assignment
	if s.Get(r.Task, &task) == nil && task.Kind == "proposal" {
		// A conversation that cannot proceed says so in its own transcript; the
		// human answers or refines the pending proposal rather than a new card.
		return nil
	}
	d := Decision{ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, State: "pending", Kind: "blocker", Question: reason}
	return []Write{{"decision", d.Org, d.Task, d.State, d.ID, d}}
}

func (e *Engine) refreshTaskStates() {
	s := e.Store
	for _, task := range list[Assignment](s, "assignment", "") {
		if task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
			continue
		}
		state := "waiting"
		for _, r := range taskRuns(s, task.ID) {
			if r.State == "running" {
				state = "running"
				break
			}
			if r.State == "queued" {
				state = "queued"
			}
		}
		for _, r := range taskRuns(s, task.ID) {
			if r.State == "waiting" && e.waitingHumanMilestone(r) {
				state = "needs input"
				break
			}
		}
		for _, d := range taskDecisions(s, task.ID) {
			if d.State == "pending" {
				state = "needs input"
				break
			}
		}
		if state != task.State {
			task.State = state
			_ = s.Put("assignment", task.Org, "", state, task.ID, task)
		}
	}
}
func retryTime(attempt int) string {
	if attempt > 15 {
		attempt = 15
	}
	return time.Now().Add(time.Duration(attempt) * time.Minute).UTC().Format(time.RFC3339Nano)
}
func (e *Engine) Reassign(parent Run, id, agent, prompt string) (Run, error) {
	s := e.Store
	var target Run
	if s.Get(id, &target) != nil || target.Parent != parent.ID || target.Task != parent.Task {
		return Run{}, fmt.Errorf("only directly delegated work may be reassigned")
	}
	if target.Superseded {
		return Run{}, fmt.Errorf("this plan attempt was superseded; inspect adc_status and use its current run")
	}
	if target.ReviewOf != "" && target.State == "complete" {
		var source Run
		if s.Get(target.ReviewOf, &source) == nil && target.ReviewedRevision == e.revision(source) && e.hasCurrentReview(source, taskReviews(s, source.Task)) {
			return Run{}, fmt.Errorf("this review already passed current evidence; resume the owner or supervisor, not the completed reviewer")
		}
	}
	if target.Reassignments >= 3 {
		return Run{}, fmt.Errorf("three recovery approaches have been attempted; present the evidence with adc_decision before further recovery")
	}
	if target.CandidateRevision != "" {
		return Run{}, fmt.Errorf("candidate review is pending; recover its reviewer or wait for its verdict before reassigning the author")
	}
	if target.State == "running" {
		return Run{}, fmt.Errorf("wait for the active run to yield before reassignment")
	}
	var a Agent
	if s.Get(agent, &a) != nil || a.Org != parent.Org {
		return Run{}, fmt.Errorf("select an expert in this organization")
	}
	for _, step := range s.taskPlan(parent.Task).Steps {
		if step.Run == target.ID {
			var reviewer Run
			if s.Get(step.Review, &reviewer) == nil {
				if err := CanReview(a.Model, reviewer.Model); err != nil {
					return Run{}, fmt.Errorf("reassignment would conflict with the plan's independent reviewer: %w", err)
				}
			}
		}
	}
	if target.ReviewOf != "" {
		var implementation Run
		if s.Get(target.ReviewOf, &implementation) != nil {
			return Run{}, fmt.Errorf("review target missing")
		}
		if err := CanReview(implementation.Model, a.Model); err != nil {
			return Run{}, err
		}
	}
	authority, err := NarrowAuthority(parent.Authority, a.Authority)
	if err != nil {
		return Run{}, err
	}
	var assignment Assignment
	if err := s.Get(target.Task, &assignment); err != nil {
		return Run{}, err
	}
	target.Provider = providerName(a.Provider)
	if err := s.bindRunAccount(assignment, &target); err != nil {
		return Run{}, err
	}
	target.Agent = a.ID
	target.Model = a.Model
	target.Family = Family(a.Model)
	target.Authority = authority
	target.Tools = nil
	for _, id := range a.Tools {
		if Subset([]string{id}, parent.Tools) {
			target.Tools = append(target.Tools, id)
		}
	}
	if err := requireConnections(s, target.Org, target.RequiredTools, target.Tools); err != nil {
		return Run{}, err
	}
	target.Prompt += "\nSupervisor changed approach: " + prompt
	target.State = "queued"
	target.Error = ""
	target.NextAt = ""
	target.Turns = 0
	target.Attempts = 0
	target.ReviewRounds = 0
	target.Reassignments++
	writes := []Write{}
	for _, d := range taskDecisions(s, target.Task) {
		if d.Run == target.ID && d.State == "pending" && d.Kind == "blocker" {
			d.State = "resolved"
			d.Answer = "Supervisor reassigned work"
			writes = append(writes, Write{"decision", d.Org, d.Task, d.State, d.ID, d})
		}
	}
	err = s.Batch(append(writes, Write{"run", target.Org, target.Task, target.State, target.ID, target})...)
	s.Log(target.Org, target.Task, parent.ID, "reassigned", target.Title+" → "+a.Name)
	return target, err
}

func (e *Engine) handleFailure(ctx context.Context, r Run, failure error) {
	s := e.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	var current Run
	if s.Get(r.ID, &current) != nil || current.State != "running" {
		return
	}
	current.Error = failure.Error()
	var task Assignment
	if s.Get(current.Task, &task) != nil {
		return
	}
	switch {
	case task.State == "cancelled":
		current.State = "cancelled"
		current.NextAt = ""
	case ctx.Err() != nil:
		current.State = "queued"
		current.NextAt = ""
		current.Prompt += "\nADC interrupted this activation. Inspect existing work and external outcomes before retrying any action."
		if ctx.Err() == context.DeadlineExceeded {
			current.NextAt = time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
		}
	default:
		current.Attempts++
		lower := strings.ToLower(failure.Error())
		if strings.Contains(lower, "rate limit") || strings.Contains(lower, "429") || strings.Contains(lower, "unavailable; no substitution") {
			current.State = "queued"
			current.NextAt = retryTime(current.Attempts)
		} else if current.Attempts < 4 {
			current.State = "queued"
			current.NextAt = retryTime(current.Attempts)
		} else {
			e.escalate(&current, "Provider could not complete this run after bounded retries: "+failure.Error())
		}
	}
	_ = s.Put("run", current.Org, current.Task, current.State, current.ID, current)
	s.Log(current.Org, current.Task, current.ID, "error", failure.Error())
}

func pendingDecision(s *Store, task, run string) bool {
	for _, d := range taskDecisions(s, task) {
		if d.State == "pending" && (run == "" || d.Run == run) {
			return true
		}
	}
	return false
}
