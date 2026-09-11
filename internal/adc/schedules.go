package adc

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type Cadence struct {
	Frequency       string
	IntervalMinutes int
	Timezone, At    string
	Weekday         int
}

func (c Cadence) Validate() error {
	switch c.Frequency {
	case "":
		return nil
	case "interval":
		if c.IntervalMinutes < 15 || c.IntervalMinutes > 525600 {
			return fmt.Errorf("interval must be 15–525600 minutes")
		}
		return nil
	case "daily", "weekly":
		if c.Timezone == "" {
			return fmt.Errorf("choose an IANA timezone, such as America/New_York")
		}
		if _, err := time.LoadLocation(c.Timezone); err != nil {
			return fmt.Errorf("unknown timezone")
		}
		if _, err := time.Parse("15:04", c.At); err != nil {
			return fmt.Errorf("choose a time in HH:MM format")
		}
		if c.Frequency == "weekly" && (c.Weekday < 0 || c.Weekday > 6) {
			return fmt.Errorf("weekday must be 0 (Sunday) through 6 (Saturday)")
		}
		return nil
	default:
		return fmt.Errorf("cadence must be interval, daily or weekly; empty means one-off")
	}
}
func (c Cadence) Next(after time.Time) (time.Time, error) {
	if err := c.Validate(); err != nil {
		return time.Time{}, err
	}
	if c.Frequency == "interval" {
		return after.Add(time.Duration(c.IntervalMinutes) * time.Minute).UTC(), nil
	}
	if c.Frequency == "" {
		return time.Time{}, fmt.Errorf("no recurrence configured")
	}
	loc, _ := time.LoadLocation(c.Timezone)
	clock, _ := time.Parse("15:04", c.At)
	local := after.In(loc)
	for n := 0; n < 15; n++ {
		day := time.Date(local.Year(), local.Month(), local.Day()+n, 12, 0, 0, 0, loc)
		candidate := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
		// Spring-forward nonexistent wall times are skipped. Fall-back selects a
		// single time.Date occurrence, so a calendar date never fires twice.
		if candidate.Hour() != clock.Hour() || candidate.Minute() != clock.Minute() {
			continue
		}
		if c.Frequency == "weekly" && int(candidate.Weekday()) != c.Weekday {
			continue
		}
		if candidate.After(after) {
			return candidate.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("could not find next occurrence")
}
func (c Cadence) String() string {
	switch c.Frequency {
	case "interval":
		return fmt.Sprintf("Every %d minutes", c.IntervalMinutes)
	case "daily":
		return "Daily at " + c.At + " · " + c.Timezone
	case "weekly":
		if c.Weekday >= 0 && c.Weekday <= 6 {
			return time.Weekday(c.Weekday).String() + "s at " + c.At + " · " + c.Timezone
		}
	}
	return "One-off work"
}
func cadenceForm(r *http.Request) (Cadence, error) {
	c := Cadence{Frequency: r.FormValue("frequency")}
	if c.Frequency == "interval" {
		c.IntervalMinutes, _ = strconv.Atoi(r.FormValue("interval_minutes"))
	}
	if c.Frequency == "daily" || c.Frequency == "weekly" {
		c.At = r.FormValue("at")
		c.Timezone = r.FormValue("timezone")
		c.Weekday, _ = strconv.Atoi(r.FormValue("weekday"))
	}
	return c, c.Validate()
}

type StandingSchedule struct {
	Scope                                                                              string
	ID, Org, Proposal, Title, State, Created, Updated, NextAt, LastTask, LastDue, Note string
	Revision                                                                           int
	Cadence                                                                            Cadence
	Template                                                                           Assignment
	Runs                                                                               int
}

func (e *Engine) approveSchedule(p WorkProposal, t Assignment, scope string, at time.Time) (StandingSchedule, []Write, error) {
	// Validate account/agent now, but don't save an assignment until it is due.
	writes, err := e.assignmentWrites(t)
	if err != nil {
		return StandingSchedule{}, nil, err
	}
	root := writes[1].Value.(Run)
	prepared := writes[0].Value.(Assignment)
	t.Execution, t.Capabilities, t.ConstrainCapabilities = prepared.Execution, prepared.Capabilities, true
	t.ConstrainTools = true
	t.Tools = append([]string{}, root.Tools...)
	if err := requireConnections(e.Store, t.Org, t.Tools, t.Tools); err != nil {
		return StandingSchedule{}, nil, err
	}
	next, err := p.Cadence.Next(at)
	if err != nil {
		return StandingSchedule{}, nil, err
	}
	schedule := StandingSchedule{Scope: scope, ID: ID(), Org: p.Org, Proposal: p.ID, Title: p.Title, State: "active", Created: at.UTC().Format(time.RFC3339Nano), Updated: at.UTC().Format(time.RFC3339Nano), NextAt: next.Format(time.RFC3339Nano), Revision: 1, Cadence: p.Cadence, Template: t}
	schedule.Template.ID = ""
	schedule.Template.Schedule = schedule.ID
	return schedule, []Write{{"schedule", schedule.Org, schedule.Proposal, schedule.State, schedule.ID, schedule}}, nil
}
func (e *Engine) validateSchedule(s StandingSchedule) error {
	var member int
	if e.Store.db.QueryRow(`SELECT count(*) FROM memberships WHERE user_id=? AND org=?`, s.Template.Creator, s.Org).Scan(&member) != nil || member != 1 {
		return fmt.Errorf("the funding human is no longer an organization member")
	}
	if err := requireConnections(e.Store, s.Org, s.Template.Tools, s.Template.Tools); err != nil {
		return err
	}
	template := s.Template
	if template.Execution == "" {
		template.Execution = "advisory"
	}
	_, err := e.assignmentWrites(template)
	return err
}

// Caller holds Store.mu. Queue and next-occurrence update commit together.
func (e *Engine) dispatchSchedules(at time.Time) {
	for _, schedule := range list[StandingSchedule](e.Store, "schedule", "") {
		if schedule.State != "active" {
			continue
		}
		due, err := time.Parse(time.RFC3339Nano, schedule.NextAt)
		if err != nil {
			e.pauseSchedule(schedule, "Invalid next occurrence; create a corrected proposal.")
			continue
		}
		if due.After(at) {
			continue
		}
		if err = e.validateSchedule(schedule); err != nil {
			e.pauseSchedule(schedule, err.Error())
			continue
		}
		next, err := schedule.Cadence.Next(at)
		if err != nil {
			e.pauseSchedule(schedule, err.Error())
			continue
		}
		busy := false
		for _, task := range list[Assignment](e.Store, "assignment", schedule.Org) {
			if task.Schedule == schedule.ID && task.State != "ready" && task.State != "cancelled" {
				busy = true
				break
			}
		}
		schedule.NextAt = next.Format(time.RFC3339Nano)
		schedule.Updated = at.UTC().Format(time.RFC3339Nano)
		if busy {
			schedule.Note = "Occurrence skipped while earlier work remains unresolved. No overlapping assignment queued."
			_ = e.Store.Put("schedule", schedule.Org, schedule.Proposal, schedule.State, schedule.ID, schedule)
			continue
		}
		t := schedule.Template
		if t.Execution == "" {
			t.Execution = "advisory"
		}
		t.ID = digest("schedule:" + schedule.ID + ":" + due.Format(time.RFC3339Nano))[:32]
		t.ScheduledFor = due.Format(time.RFC3339Nano)
		t.Prompt += "\nSTANDING WORK: this occurrence is authorized only by the saved scope above. Scheduled for " + t.ScheduledFor + "; previous assignment " + schedule.LastTask + ". Inspect prior results and current evidence; do not carry earlier per-run approvals into this occurrence. Do not repeat unresolved or ambiguous external actions. Propose scope/cadence changes for fresh human approval."
		writes, err := e.assignmentWrites(t)
		if err != nil {
			e.pauseSchedule(schedule, err.Error())
			continue
		}
		schedule.LastTask = t.ID
		schedule.LastDue = t.ScheduledFor
		schedule.Runs++
		schedule.Note = ""
		if err = e.Store.Batch(append(writes, Write{"schedule", schedule.Org, schedule.Proposal, schedule.State, schedule.ID, schedule})...); err == nil {
			e.Store.Log(schedule.Org, t.ID, "", "scheduled", "Queued from approved standing work: "+schedule.Title)
		}
	}
}
func (e *Engine) pauseSchedule(schedule StandingSchedule, reason string) {
	schedule.State = "paused"
	schedule.Note = reason
	schedule.Revision++
	schedule.Updated = now()
	_ = e.Store.Put("schedule", schedule.Org, schedule.Proposal, schedule.State, schedule.ID, schedule)
	e.Store.Log(schedule.Org, "", "", "schedule", "Paused "+schedule.Title+": "+reason)
}
func (w *Web) scheduleAction(r *http.Request, p Page) error {
	var schedule StandingSchedule
	if w.Store.Get(r.FormValue("id"), &schedule) != nil || schedule.Org != p.Org.ID {
		return fmt.Errorf("schedule unavailable")
	}
	rev, err := strconv.Atoi(r.FormValue("revision"))
	if err != nil || rev != schedule.Revision {
		return fmt.Errorf("schedule changed; reload before acting")
	}
	switch r.FormValue("action") {
	case "pause":
		if schedule.State != "active" {
			return fmt.Errorf("schedule is already paused")
		}
		schedule.State = "paused"
		schedule.Note = "Paused by " + p.User.Name + ". Existing assignments retain their own controls."
	case "resume":
		if schedule.State != "paused" {
			return fmt.Errorf("schedule is already active")
		}
		if err = w.Engine.validateSchedule(schedule); err != nil {
			return err
		}
		next, err := schedule.Cadence.Next(time.Now())
		if err != nil {
			return err
		}
		schedule.NextAt = next.Format(time.RFC3339Nano)
		schedule.State = "active"
		schedule.Note = ""
	default:
		return fmt.Errorf("unknown schedule action")
	}
	schedule.Revision++
	schedule.Updated = now()
	if err = w.Store.Put("schedule", schedule.Org, schedule.Proposal, schedule.State, schedule.ID, schedule); err != nil {
		return err
	}
	w.Store.Log(schedule.Org, "", "", "schedule", p.User.Name+" set "+schedule.Title+" to "+schedule.State)
	return nil
}

func scheduledActionPlans(p WorkProposal, authority string) error {
	if p.Cadence.Frequency != "" && authority == "approved-action" && (strings.TrimSpace(p.Validation) == "" || strings.TrimSpace(p.Rollback) == "") {
		return fmt.Errorf("recurring actions need explicit validation and rollback/stop plans; edit the proposal before approving")
	}
	return nil
}
