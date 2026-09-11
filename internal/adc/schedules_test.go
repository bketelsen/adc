package adc

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func instant(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	must(t, err)
	return v
}
func TestCadenceTimezoneDSTAndValidation(t *testing.T) {
	daily := Cadence{Frequency: "daily", Timezone: "America/New_York", At: "09:00"}
	for _, tc := range []struct{ after, want string }{{"2026-03-07T15:00:00Z", "2026-03-08T13:00:00Z"}, {"2026-10-31T15:00:00Z", "2026-11-01T14:00:00Z"}} {
		next, err := daily.Next(instant(t, tc.after))
		must(t, err)
		if !next.Equal(instant(t, tc.want)) {
			t.Fatalf("DST %s got %s", tc.after, next)
		}
	}
	gap := daily
	gap.At = "02:30"
	next, err := gap.Next(instant(t, "2026-03-08T05:00:00Z"))
	must(t, err)
	if !next.Equal(instant(t, "2026-03-09T06:30:00Z")) {
		t.Fatal("nonexistent time not skipped", next)
	}
	repeated := daily
	repeated.At = "01:30"
	first, err := repeated.Next(instant(t, "2026-11-01T00:00:00Z"))
	must(t, err)
	next, err = repeated.Next(first)
	must(t, err)
	if first.In(mustLocation(t, "America/New_York")).Day() == next.In(mustLocation(t, "America/New_York")).Day() {
		t.Fatal("fall-back fired twice")
	}
	weekly := Cadence{Frequency: "weekly", Timezone: "UTC", At: "10:00", Weekday: 1}
	next, err = weekly.Next(instant(t, "2026-09-11T00:00:00Z"))
	must(t, err)
	if !next.Equal(instant(t, "2026-09-14T10:00:00Z")) {
		t.Fatal(next)
	}
	for _, bad := range []Cadence{{Frequency: "interval", IntervalMinutes: 1}, {Frequency: "daily", At: "09:00"}, {Frequency: "weekly", Timezone: "UTC", At: "25:00"}, {Frequency: "weekly", Timezone: "UTC", At: "10:00", Weekday: 8}, {Frequency: "sometimes"}} {
		if bad.Validate() == nil {
			t.Fatal("invalid cadence accepted", bad)
		}
	}
}
func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	must(t, err)
	return loc
}
func scheduleFixture(t *testing.T) (*Store, *Engine, StandingSchedule, time.Time) {
	s, e, _, _, p := proposalFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	c := Connection{ID: "storage", Org: "org", Name: "Storage", Transport: "stdio", Command: "fixture"}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	p.Cadence = Cadence{Frequency: "interval", IntervalMinutes: 60}
	at := instant(t, "2026-09-11T10:00:00Z")
	template := Assignment{Org: p.Org, Owner: "boss", Account: "account", Creator: "owner", Authority: "observe", Proposal: p.ID, Title: p.Title, Prompt: "Observe only"}
	schedule, writes, err := e.approveSchedule(p, template, "Observe only", at)
	must(t, err)
	must(t, s.Batch(writes...))
	return s, e, schedule, at
}
func TestScheduleAtomicDispatchRestartNoOverlapAndCatchup(t *testing.T) {
	s, e, schedule, at := scheduleFixture(t)
	e.dispatchSchedules(at)
	if len(list[Assignment](s, "assignment", "org")) != 1 {
		t.Fatal("early dispatch")
	}
	due := at.Add(time.Hour)
	e.dispatchSchedules(due)
	must(t, s.Get(schedule.ID, &schedule))
	first := schedule.LastTask
	if first == "" || schedule.Runs != 1 {
		t.Fatal("due occurrence not queued")
	}
	tasks := list[Assignment](s, "assignment", "org")
	if len(tasks) != 2 {
		t.Fatal("wrong assignment count")
	}
	// New engine using persisted data cannot replay the same occurrence.
	e = NewEngine(s)
	e.dispatchSchedules(due)
	if len(list[Assignment](s, "assignment", "org")) != 2 {
		t.Fatal("restart duplicated occurrence")
	}
	var task Assignment
	must(t, s.Get(first, &task))
	if task.Schedule != schedule.ID || task.Authority != "observe" || !task.ConstrainTools || task.Publication || task.Creator != "owner" {
		t.Fatal("approved snapshot lost")
	}
	for _, state := range []string{"queued", "running", "waiting", "needs input", "paused"} {
		task.State = state
		must(t, s.Put("assignment", task.Org, "", state, task.ID, task))
		due = due.Add(time.Hour)
		e.dispatchSchedules(due)
		if len(list[Assignment](s, "assignment", "org")) != 2 {
			t.Fatal("overlap while " + state)
		}
	}
	task.State = "ready"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	overdue := due.Add(24 * time.Hour)
	e.dispatchSchedules(overdue)
	must(t, s.Get(schedule.ID, &schedule))
	if schedule.Runs != 2 || len(list[Assignment](s, "assignment", "org")) != 3 {
		t.Fatal("missed occurrences produced backlog")
	}
	next, err := time.Parse(time.RFC3339Nano, schedule.NextAt)
	must(t, err)
	if !next.After(overdue) {
		t.Fatal("next occurrence remains overdue")
	}
	if len(taskRuns(s, schedule.LastTask)) != 1 {
		t.Fatal("normal durable root missing")
	}
}
func TestSchedulePinsToolCeilingAndPausesOnRevocation(t *testing.T) {
	for _, reason := range []string{"membership", "account", "grant", "connection"} {
		t.Run(reason, func(t *testing.T) {
			s, e, schedule, at := scheduleFixture(t)
			var a Agent
			must(t, s.Get("boss", &a))
			a.Tools = append(a.Tools, "extra")
			must(t, s.Put("agent", a.Org, "", "", a.ID, a))
			e.dispatchSchedules(at.Add(time.Hour))
			must(t, s.Get(schedule.ID, &schedule))
			r := taskRuns(s, schedule.LastTask)[0]
			if len(r.Tools) != 1 || r.Tools[0] != "storage" {
				t.Fatal("new role grants silently expanded standing approval")
			}
			var task Assignment
			must(t, s.Get(schedule.LastTask, &task))
			task.State = "ready"
			must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
			switch reason {
			case "membership":
				_, err := s.db.Exec(`DELETE FROM memberships WHERE user_id='owner'`)
				must(t, err)
			case "account":
				var account Account
				must(t, s.Get("account", &account))
				account.User = "someone-else"
				must(t, s.Put("account", "", account.User, "", account.ID, account))
			case "grant":
				a.Tools = nil
				must(t, s.Put("agent", a.Org, "", "", a.ID, a))
			case "connection":
				_, err := s.db.Exec(`DELETE FROM records WHERE id='storage'`)
				must(t, err)
			}
			e.dispatchSchedules(at.Add(2 * time.Hour))
			must(t, s.Get(schedule.ID, &schedule))
			if schedule.State != "paused" || schedule.Note == "" || schedule.Runs != 1 {
				t.Fatal("revoked prerequisite did not pause schedule")
			}
		})
	}
}
func TestRecurringAcceptanceIdempotencyAndActionPlans(t *testing.T) {
	s, e, _, _, p := proposalFixture(t)
	w := NewWeb(s, e, false)
	p.Cadence = Cadence{Frequency: "daily", Timezone: "UTC", At: "10:00"}
	must(t, s.Put("proposal", p.Org, p.Task, p.State, p.ID, p))
	must(t, s.Put("connection", p.Org, "", "", "storage", Connection{ID: "storage", Org: p.Org, Name: "Storage"}))
	form := proposalForm(p, "accept")
	form.Set("owner", "boss")
	form.Set("account", "account")
	form.Set("authority", "approved-action")
	form.Set("authorization", "Perform the bounded approved operation")
	if err := actProposal(w, "owner", p, form); err == nil {
		t.Fatal("recurring action accepted without plans")
	}
	p.Validation = "Verify the intended outcome"
	p.Rollback = "Stop and request a decision if rollback requires unavailable access"
	must(t, s.Put("proposal", p.Org, p.Task, p.State, p.ID, p))
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- actProposal(w, "owner", p, form) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		must(t, err)
	}
	if len(list[StandingSchedule](s, "schedule", p.Org)) != 1 || len(list[Assignment](s, "assignment", p.Org)) != 1 {
		t.Fatal("approval queued immediate work or duplicate schedule")
	}
	must(t, s.Get(p.ID, &p))
	if p.AcceptedSchedule == "" || p.AcceptedTask != "" {
		t.Fatal("wrong accepted outcome")
	}
}
func TestSchedulePauseResumeAndOrgBoundary(t *testing.T) {
	s, e, schedule, at := scheduleFixture(t)
	w := NewWeb(s, e, false)
	act := func(org, action string, rev int) error {
		form := url.Values{"id": {schedule.ID}, "revision": {strconv.Itoa(rev)}, "action": {action}}
		req := httptest.NewRequest("POST", "/schedule-action", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_ = req.ParseForm()
		return w.action(req, Page{Org: Organization{ID: org}, User: User{ID: "owner", Name: "Owner"}})
	}
	if act("foreign", "pause", 1) == nil {
		t.Fatal("cross-org pause accepted")
	}
	must(t, act("org", "pause", 1))
	e.dispatchSchedules(at.Add(24 * time.Hour))
	if len(list[Assignment](s, "assignment", "org")) != 1 {
		t.Fatal("paused schedule dispatched")
	}
	if act("org", "resume", 1) == nil {
		t.Fatal("stale control accepted")
	}
	must(t, act("org", "resume", 2))
	must(t, s.Get(schedule.ID, &schedule))
	next, err := time.Parse(time.RFC3339Nano, schedule.NextAt)
	must(t, err)
	if schedule.State != "active" || !next.After(time.Now()) {
		t.Fatal("resume replayed backlog")
	}
}
func TestManualRootTakesPriorityOverScheduledRoot(t *testing.T) {
	s, e, schedule, at := scheduleFixture(t)
	e.dispatchSchedules(at.Add(time.Hour))
	must(t, s.Get(schedule.ID, &schedule))
	var account Account
	must(t, s.Get("account", &account))
	account.Limit = 1
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	for _, r := range taskRuns(s, "task") {
		r.State = "queued"
		must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	}
	// Advance the schedule beyond the real clock to keep this scheduler test fixed.
	schedule.NextAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	must(t, s.Put("schedule", schedule.Org, schedule.Proposal, schedule.State, schedule.ID, schedule))
	started := controlScheduler(t, e)
	e.tick(context.Background())
	r := awaitRun(t, started)
	if r.Task != "task" {
		t.Fatal("scheduled root took foreground capacity")
	}
}
func TestCadenceProposalRevisionInvalidatesApproval(t *testing.T) {
	s, e, _, root, p := proposalFixture(t)
	cadence := Cadence{Frequency: "weekly", Timezone: "UTC", At: "09:00", Weekday: 1}
	result, err := call(t, e, root, "adc_propose_work", proposalInput{ID: p.ID, Revision: p.Revision, Title: p.Title, Rationale: p.Rationale, Scope: p.Scope, Criteria: p.Criteria, Evidence: p.Evidence, Owner: p.Owner, Cadence: &cadence})
	must(t, err)
	var updated proposalResult
	must(t, json.Unmarshal([]byte(result), &updated))
	if updated.Proposal.Revision != 2 || updated.Proposal.Cadence != cadence {
		t.Fatal("cadence not versioned")
	}
	form := proposalForm(p, "accept")
	form.Set("owner", "boss")
	form.Set("account", "account")
	form.Set("authority", "observe")
	form.Set("authorization", p.Scope)
	if actProposal(NewWeb(s, e, false), "owner", p, form) == nil {
		t.Fatal("old cadence approved")
	}
}
