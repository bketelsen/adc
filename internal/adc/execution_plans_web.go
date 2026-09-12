package adc

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Caller holds Store.mu through the authenticated, CSRF-checked action router.
func (w *Web) executionPlanAction(r *http.Request, page Page) error {
	s := w.Store
	p := s.taskPlan(r.FormValue("task"))
	revision, err := strconv.Atoi(r.FormValue("revision"))
	if p.ID == "" || p.Org != page.Org.ID || err != nil || revision != p.Revision {
		return fmt.Errorf("plan unavailable or revision changed; reload before starting")
	}
	if r.FormValue("action") == "milestone" || r.FormValue("action") == "withdraw-milestone" {
		var worker Run
		if s.Get(r.FormValue("run"), &worker) != nil || worker.Task != p.Task || worker.Org != p.Org {
			return fmt.Errorf("current plan worker unavailable")
		}
		evidenceRevision, err := strconv.Atoi(r.FormValue("evidence_revision"))
		if err != nil {
			return fmt.Errorf("invalid evidence revision")
		}
		observedAt := r.FormValue("observed_at")
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if observed, parseErr := time.ParseInLocation(layout, observedAt, time.UTC); parseErr == nil {
				observedAt = observed.Format(time.RFC3339)
				break
			}
		}
		_, err = w.Engine.submitMilestone(worker, milestoneInput{Requirement: r.FormValue("requirement"), Kind: r.FormValue("kind"), Target: r.FormValue("target"), Summary: r.FormValue("summary"), Reference: r.FormValue("reference"), ObservedAt: observedAt, Revision: evidenceRevision, Withdraw: r.FormValue("action") == "withdraw-milestone"}, page.User.ID)
		return err
	}
	if r.FormValue("action") != "start" {
		return fmt.Errorf("unsupported plan action")
	}
	if p.State != "draft" {
		return nil
	} // Repeated submission does not repeat work.
	var task Assignment
	var root Run
	if s.Get(p.Task, &task) != nil || task.Org != p.Org || s.Get(p.Supervisor, &root) != nil || root.Task != task.ID || root.Parent != "" {
		return fmt.Errorf("assignment supervisor unavailable")
	}
	if task.State == "paused" || task.State == "cancelled" {
		return fmt.Errorf("resume the assignment before starting its plan")
	}
	if root.State != "running" && root.State != "queued" && root.State != "waiting" && root.State != "complete" {
		return fmt.Errorf("resume the supervisor before starting its plan")
	}
	if pendingDecision(s, task.ID, root.ID) {
		return fmt.Errorf("resolve the supervisor's pending decision before starting its plan")
	}
	for _, step := range p.Steps {
		if _, err := w.Engine.planRun(task, root, step, step.Worker, false); err != nil {
			return err
		}
		if _, err := w.Engine.planRun(task, root, step, step.Verifier, true); err != nil {
			return err
		}
	}
	p.State = "active"
	p.StartedBy = page.User.ID
	if root.State == "complete" {
		root.State = "queued"
		root.Turns = 0
		root.Attempts = 0
		task.State = "queued"
	}
	root.Prompt += fmt.Sprintf("\nHuman %s started execution plan revision %d within this assignment's existing scope and authority. ADC dispatches its steps and reviews automatically; inspect adc_status and wait or handle concrete blockers. This grants no additional publication or operational authority.", page.User.Name, p.Revision)
	if root.State == "running" {
		root.UpdatesPending = true
	}
	if err := s.Batch(Write{"execution-plan", p.Org, p.Task, p.State, p.ID, p}, Write{"run", root.Org, root.Task, root.State, root.ID, root}, Write{"assignment", task.Org, "", task.State, task.ID, task}); err != nil {
		return err
	}
	s.Log(p.Org, p.Task, root.ID, "human", fmt.Sprintf("%s started execution plan revision %d", page.User.Name, p.Revision))
	return nil
}
