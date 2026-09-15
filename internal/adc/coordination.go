package adc

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// Recovery is once per changed impediment, persisted across engine restarts.
// It wakes supervision, never workers held by a human or any completed work.
func (e *Engine) recoverStalledSupervisors() {
	s := e.Store
	for _, root := range list[Run](s, "run", "") {
		if root.Parent != "" || root.State != "waiting" || root.Superseded || pendingDecision(s, root.Task, root.ID) {
			continue
		}
		var task Assignment
		if s.Get(root.Task, &task) != nil || task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
			continue
		}
		stalled, progressing := e.waitProgress(root)
		if !stalled || progressing || e.pendingWait(root) {
			continue
		}
		parts := []string{}
		for _, r := range taskRuns(s, root.Task) {
			if r.ID != root.ID && !r.Superseded && r.State != "complete" && r.State != "cancelled" {
				parts = append(parts, r.ID+":"+r.State+":"+r.Error+":"+r.Result)
			}
		}
		plan := e.inspectPlan(s.taskPlan(root.Task))
		for _, step := range plan.Steps {
			if !stepSatisfied(step) {
				parts = append(parts, step.Key+":"+step.State+":"+step.Reason)
			}
		}
		sort.Strings(parts)
		raw, _ := json.Marshal(parts)
		revision := fmt.Sprintf("%x", sha256.Sum256(raw))
		if root.WaitAssessment == revision {
			continue
		}
		root.WaitAssessment = revision
		root.State, root.Turns = "queued", 0
		root.Prompt += "\n[ADC stalled coordination recovery] No delegated work currently has an automatic path forward. Inspect adc_status View=coordination and View=decisions. Preserve completed work and previous human answers. Recover what is already authorized; otherwise present one concise question with the actual missing information or proposed action. Do not ask the human to interpret plan keys, repeat approvals, run approved commands, or transcribe evidence. Existing holds remain in force until the human changes them. This wake grants no new authority."
		if s.Put("run", root.Org, root.Task, root.State, root.ID, root) == nil {
			s.Log(root.Org, root.Task, root.ID, "recovery", "Supervisor resumed to resolve stalled work or present the specific human question. Existing approvals and holds are preserved.")
		}
	}
}

// Compact, paginated views keep the current impediments out of old transcripts
// and comfortably below the provider's inline output limit.
func statusPage(offset, total int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + 6
	if end > total {
		end = total
	}
	return offset, end
}
func (e *Engine) coordinationView(r Run, offset int) any {
	s := e.Store
	plan := e.inspectPlan(s.taskPlan(r.Task))
	counts := map[string]int{}
	for _, step := range plan.Steps {
		counts[step.State]++
	}
	items := []map[string]any{}
	for _, v := range taskRuns(s, r.Task) {
		if v.ID == r.ID || v.Superseded || v.State == "complete" || v.State == "cancelled" {
			continue
		}
		reason := v.Error
		if reason == "" && v.State == "blocked" {
			reason = v.Result
		}
		if v.State == "waiting" && e.waitingHumanMilestone(v) && !pendingDecision(s, r.Task, v.ID) {
			reason = "Human information/acceptance is missing but no decision is pending. Inspect this step and existing answers; ask only for what cannot be discovered."
		}
		if v.State == "waiting" && v.ReviewOf != "" {
			var target Run
			if s.Get(v.ReviewOf, &target) == nil && !e.reviewable(target) {
				reason = "Review cannot start until its author has a reviewable candidate or completed result."
			}
		}
		items = append(items, map[string]any{"run": v.ID, "title": clipped(v.Title, 160), "state": v.State, "review_of": v.ReviewOf, "reason": clipped(reason, 650)})
	}
	// Show impediments before queue/worker activity, with stable pagination.
	sort.Slice(items, func(i, j int) bool {
		rank := func(v map[string]any) int {
			if v["state"] == "blocked" {
				return 0
			}
			if v["reason"] != "" {
				return 1
			}
			return 2
		}
		if rank(items[i]) != rank(items[j]) {
			return rank(items[i]) < rank(items[j])
		}
		return items[i]["run"].(string) < items[j]["run"].(string)
	})
	start, end := statusPage(offset, len(items))
	blocked, progressing := e.waitProgress(r)
	return map[string]any{"plan_revision": plan.Revision, "plan_state": plan.State, "step_counts": counts, "unresolved": blocked, "automatic_path": progressing, "items": items[start:end], "total": len(items), "next_offset": end, "guidance": "Use Offset=next_offset while next_offset < total. View=step with ID=step key gives current requirements/evidence; View=run with ID gives exact run evidence; View=decisions supplies prior human answers. Restore authorized work or ask a concise concrete question; do not equate a missing human requirement with an actual pending question."}
}
