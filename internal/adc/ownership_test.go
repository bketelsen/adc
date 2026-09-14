package adc

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func ownershipFixture(t *testing.T) (*Store, *Engine, Assignment, Run, Area) {
	t.Helper()
	s, e, task, r := fixture(t)
	_, err := s.db.Exec("INSERT INTO users VALUES('owner','Owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')")
	must(t, err)
	c := Connection{ID: "storage", Org: task.Org, Name: "Fixture observation", Transport: "stdio", Command: "fixture"}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	a, err := s.saveArea(Area{Org: task.Org, Owner: r.Agent, Name: "Shared responsibilities", Intent: "Verify agreed outcomes. Do not invent new work."}, 0, "human:owner")
	must(t, err)
	return s, e, task, r, a
}

func followupArgs(a Area) followupInput {
	return followupInput{Area: a.ID, Key: "verification", Outcome: "Confirm the agreed outcome", Criteria: "Observe a successful result after the change", Basis: "Verify the result of this assignment; no new mutation", Due: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
}

func createFollowup(t *testing.T, e *Engine, r Run, p followupInput) Obligation {
	t.Helper()
	raw, err := call(t, e, r, "adc_followup", p)
	must(t, err)
	var o Obligation
	must(t, json.Unmarshal([]byte(raw), &o))
	return o
}

func completeSource(t *testing.T, s *Store, task Assignment, r Run) {
	t.Helper()
	task.State = "ready"
	r.State = "complete"
	must(t, s.Batch(Write{"assignment", task.Org, "", task.State, task.ID, task}, Write{"run", r.Org, r.Task, r.State, r.ID, r}))
}

func TestOwnerFollowupSurvivesSourceCompletionAndDatabaseRestart(t *testing.T) {
	s, e, task, root, area := ownershipFixture(t)
	_, err := call(t, e, root, "adc_remember", ownerNoteInput{Area: area.ID, Revision: area.Revision, Summary: "The next observation, rather than the configuration edit, proves the result.", Source: "fixture://approved-change"})
	must(t, err)
	p := followupArgs(area)
	o := createFollowup(t, e, root, p)
	repeated := createFollowup(t, e, root, p)
	if repeated.ID != o.ID {
		t.Fatal("duplicate registration")
	}
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	if o.Task != "" {
		t.Fatal("source unfinished yet verification dispatched")
	}
	completeSource(t, s, task, root)
	dir := s.Dir
	must(t, s.Close())
	s2, err := Open(dir)
	must(t, err)
	defer s2.Close()
	e = NewEngine(s2)
	e.dispatchObligations(time.Now())
	must(t, s2.Get(o.ID, &o))
	if o.Task != "" {
		t.Fatal("early wake")
	}
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s2.Get(o.ID, &o))
	if o.State != "verifying" || o.Task == "" {
		t.Fatalf("follow-up lost: %+v", o)
	}
	e = NewEngine(s2)
	e.dispatchObligations(time.Now().Add(3 * time.Hour))
	if len(list[Assignment](s2, "assignment", task.Org)) != 2 {
		t.Fatal("duplicate restart dispatch")
	}
	var followup Assignment
	must(t, s2.Get(o.Task, &followup))
	if followup.Creator != task.Creator || followup.Account != task.Account || followup.Authority != "observe" || followup.Publication || !followup.ConstrainCapabilities || !followup.ConstrainTools {
		t.Fatalf("authority or funding snapshot changed: %+v", followup)
	}
	r := taskRuns(s2, o.Task)[0]
	context, _ := json.Marshal(e.ownerContext(r))
	if !strings.Contains(string(context), "next observation") || !strings.Contains(string(context), o.ID) {
		t.Fatal("new run lost knowledge or obligation", string(context))
	}
	setRunning(t, s2, &r)
	if _, err := call(t, e, r, "adc_followup", followupArgs(area)); err == nil {
		t.Fatal("recursive attention accepted")
	}
	finishFollowupObservation(t, s2, e, o, "pass")
	e.dispatchObligations(time.Now().Add(3 * time.Hour))
	must(t, s2.Get(o.ID, &o))
	if o.State != "resolved" {
		t.Fatalf("reviewed verification did not resolve: %+v", o)
	}
	var old Assignment
	must(t, s2.Get(task.ID, &old))
	if old.State != "ready" {
		t.Fatal("source resurrected")
	}
}

func finishFollowupObservation(t *testing.T, s *Store, e *Engine, o Obligation, outcome string) {
	t.Helper()
	r := taskRuns(s, o.Task)[0]
	setRunning(t, s, &r)
	_, err := call(t, e, r, "adc_delegate", map[string]any{"Agent": "dev", "Title": "Observe outcome", "Prompt": "Collect concrete observation"})
	must(t, err)
	var worker Run
	for _, v := range taskRuns(s, o.Task) {
		if v.Agent == "dev" {
			worker = v
		}
	}
	worker.Category = "operations" // Observations require review regardless of work category.
	setRunning(t, s, &worker)
	_, err = call(t, e, worker, "adc_obligation_result", observationInput{Obligation: o.ID, Outcome: outcome, Summary: "Observed the actual fixture outcome", Reference: "fixture://observation"})
	must(t, err)
	_, err = call(t, e, worker, "adc_finish", map[string]string{"Result": "Concrete observation recorded"})
	must(t, err)
	must(t, s.Get(worker.ID, &worker))
	needs := e.reviewNeeds(o.Task)
	if len(needs) != 1 || needs[0]["run"] != worker.ID {
		t.Fatal("operations observation missing from review queue", needs)
	}
	if _, err = call(t, e, r, "adc_finish", map[string]string{"Result": "Premature completion"}); err == nil || !strings.Contains(err.Error(), "independent review required") {
		t.Fatal("unreviewed observation did not hold supervisor", err)
	}
	_, err = call(t, e, r, "adc_delegate", map[string]any{"Agent": "qa", "Title": "Review observation", "Prompt": "Independently assess the observation", "ReviewOf": worker.ID})
	must(t, err)
	var qa Run
	for _, v := range taskRuns(s, o.Task) {
		if v.ReviewOf == worker.ID {
			qa = v
		}
	}
	qa.ReviewedRevision = e.revision(worker)
	qa.State = "running"
	must(t, s.Put("run", qa.Org, qa.Task, qa.State, qa.ID, qa))
	_, err = call(t, e, qa, "adc_review", map[string]string{"Verdict": "pass", "Findings": "Fixture observation independently checked"})
	must(t, err)
	setRunning(t, s, &r)
	_, err = call(t, e, r, "adc_finish", map[string]string{"Result": "Verification activity finished with reviewed observation"})
	must(t, err)
}

func TestOwnerVerificationFailureIsNotDischargedByTaskCompletion(t *testing.T) {
	s, e, task, r, a := ownershipFixture(t)
	o := createFollowup(t, e, r, followupArgs(a))
	completeSource(t, s, task, r)
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	finishFollowupObservation(t, s, e, o, "fail")
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	if o.State != "blocked" {
		t.Fatal("failure discharged the obligation", o.State)
	}
}

func TestOwnerAreaConcurrencyAndHumanIntentBoundary(t *testing.T) {
	s, e, _, r, a := ownershipFixture(t)
	p := ownerNoteInput{Area: a.ID, Revision: a.Revision, Summary: "Agent observation, uncertain until checked", Source: "fixture://source"}
	_, err := call(t, e, r, "adc_remember", p)
	must(t, err)
	result, err := call(t, e, r, "adc_remember", p)
	must(t, err)
	if !strings.Contains(result, "conflict") {
		t.Fatal("stale update not retained for reconciliation")
	}
	must(t, s.Get(a.ID, &a))
	if a.Intent != "Verify agreed outcomes. Do not invent new work." || !strings.HasPrefix(a.UpdatedBy, "run:") {
		t.Fatal("agent changed human intent")
	}
	if len(list[Area](s, "area-history", a.Org)) != 1 {
		t.Fatal("knowledge history lost")
	}
	other := r
	other.ID = "other-run"
	other.Agent = "dev"
	must(t, s.Put("run", other.Org, other.Task, other.State, other.ID, other))
	p.Revision = a.Revision
	if _, err = call(t, e, other, "adc_remember", p); err == nil {
		t.Fatal("another owner overwrote knowledge")
	}
	other.Org = "other"
	must(t, s.Put("run", other.Org, other.Task, other.State, other.ID, other))
	if _, err = call(t, e, other, "adc_owner", map[string]string{"Area": a.ID}); err == nil {
		t.Fatal("cross-organization read accepted")
	}
}

func TestOwnerFollowupSuppressesCancelledPausedAndRevokedSources(t *testing.T) {
	for _, mode := range []string{"cancelled", "paused", "funding", "tools", "superseded"} {
		t.Run(mode, func(t *testing.T) {
			s, e, task, r, a := ownershipFixture(t)
			o := createFollowup(t, e, r, followupArgs(a))
			completeSource(t, s, task, r)
			switch mode {
			case "funding":
				_, err := s.db.Exec("DELETE FROM memberships WHERE user_id='owner'")
				must(t, err)
			case "tools":
				var owner Agent
				must(t, s.Get(r.Agent, &owner))
				owner.Tools = nil
				must(t, s.Put("agent", owner.Org, "", "", owner.ID, owner))
			case "superseded":
				r.Superseded = true
				must(t, s.Put("run", r.Org, r.Task, "complete", r.ID, r))
			default:
				task.State = mode
				must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
			}
			e.dispatchObligations(time.Now().Add(2 * time.Hour))
			must(t, s.Get(o.ID, &o))
			if o.State != "blocked" || o.Task != "" {
				t.Fatal("invalid source dispatched", o)
			}
		})
	}
}

func TestOwnerFollowupObservationPinAndMissingEvidence(t *testing.T) {
	s, e, task, r, a := ownershipFixture(t)
	o := createFollowup(t, e, r, followupArgs(a))
	completeSource(t, s, task, r)
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	w := Run{ID: "worker", Org: o.Org, Task: o.Task, Agent: "dev", Parent: "root", State: "running", Model: "gpt-6-astra"}
	must(t, s.Put("run", w.Org, w.Task, w.State, w.ID, w))
	before := e.revision(w)
	v, err := e.recordObservation(w, observationInput{Obligation: o.ID, Outcome: "pass", Summary: "One observation", Reference: "fixture://one"})
	must(t, err)
	if before == e.revision(w) {
		t.Fatal("observation excluded from review pin")
	}
	if _, err = e.recordObservation(w, observationInput{Obligation: o.ID, Outcome: "pass", Summary: "Changed observation", Reference: "fixture://two", Revision: v.Revision - 1}); err == nil {
		t.Fatal("stale observation accepted")
	}
	var ft Assignment
	must(t, s.Get(o.Task, &ft))
	ft.State = "ready"
	must(t, s.Put("assignment", ft.Org, "", ft.State, ft.ID, ft))
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	if o.State != "blocked" {
		t.Fatal("unreviewed claim discharged obligation")
	}
}

func TestOwnerFollowupLimitsAndNoMutationGrants(t *testing.T) {
	s, e, task, r, a := ownershipFixture(t)
	task.Publication = true
	task.Capabilities = []CapabilityGrant{{Tool: "read", Class: "read", Scope: "assignment"}, {Tool: "write", Class: "change", Scope: "assignment"}, {Tool: "once", Class: "read", Scope: "assignment", Operation: "once"}}
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	for i := 0; i < 4; i++ {
		p := followupArgs(a)
		p.Key = fmt.Sprintf("check%d", i)
		createFollowup(t, e, r, p)
	}
	p := followupArgs(a)
	p.Key = "overflow"
	if _, err := call(t, e, r, "adc_followup", p); err == nil {
		t.Fatal("unbounded registration")
	}
	for _, f := range list[ObligationFunding](s, "obligation-funding", a.Org) {
		if len(f.Template.Capabilities) != 1 || f.Template.Capabilities[0].Tool != "read" || f.Template.Publication {
			t.Fatal("mutation/one-time authority leaked", f.Template.Capabilities)
		}
	}
}

func TestOwnerAreaPageAndCancellation(t *testing.T) {
	s, e, _, r, a := ownershipFixture(t)
	o := createFollowup(t, e, r, followupArgs(a))
	w := NewWeb(s, e, false)
	p := Page{View: "areas", Title: "Areas", Org: Organization{ID: a.Org}, Agents: list[Agent](s, "agent", a.Org), Areas: []Area{a}, Obligations: []Obligation{o}}
	rw := httptest.NewRecorder()
	w.render(rw, p)
	if !strings.Contains(rw.Body.String(), "Human-maintained intent") || !strings.Contains(rw.Body.String(), o.Outcome) {
		t.Fatal("area view lacks intent or obligations")
	}
	form := url.Values{"action": {"cancel-obligation"}, "obligation": {o.ID}, "revision": {"1"}, "reason": {"No longer needed"}}
	req := httptest.NewRequest("POST", "/area-action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	s.mu.Lock()
	err := w.areaAction(req, p)
	s.mu.Unlock()
	must(t, err)
	must(t, s.Get(o.ID, &o))
	if o.State != "cancelled" || o.Note != "No longer needed" {
		t.Fatal("cancellation not retained")
	}
}

func TestOwnerKnowledgeRejectsRecognizableAndConfiguredSecrets(t *testing.T) {
	s, e, task, r, a := ownershipFixture(t)
	sealed, err := s.Seal("fixture-known-private-value")
	must(t, err)
	c := Connection{ID: "private", Org: task.Org, Env: map[string]string{"API_KEY": sealed}}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	for _, secret := range []string{"ghp_123456789012345678901234567890", "fixture-known-private-value", "password=fixture-password"} {
		_, err := call(t, e, r, "adc_remember", ownerNoteInput{Area: a.ID, Revision: a.Revision, Summary: "Observed " + secret, Source: "fixture://source"})
		if err == nil {
			t.Fatal("secret retained in knowledge")
		}
		p := followupArgs(a)
		p.Basis = secret
		if _, err = call(t, e, r, "adc_followup", p); err == nil {
			t.Fatal("secret retained in follow-up")
		}
	}
	must(t, s.Get(a.ID, &a))
	if a.Summary != "" {
		t.Fatal("failed write changed knowledge")
	}
}

func TestOwnerCompletionAtBudgetAndSupersededEvidence(t *testing.T) {
	s, e, task, r, a := ownershipFixture(t)
	task.Output = "Old result"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	o := createFollowup(t, e, r, followupArgs(a))
	completeSource(t, s, task, r)
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	var followup Assignment
	must(t, s.Get(o.Task, &followup))
	if followup.Output != "" {
		t.Fatal("source output inherited")
	}
	finishFollowupObservation(t, s, e, o, "pass")
	old := Run{ID: "old", Org: o.Org, Task: o.Task, State: "cancelled", Superseded: true, Activations: 48}
	must(t, s.Put("run", old.Org, old.Task, old.State, old.ID, old))
	observation := ObligationObservation{ID: "observation:" + old.ID, Org: o.Org, Task: o.Task, Run: old.ID, Obligation: o.ID, Outcome: "fail", Summary: "Old superseded observation"}
	must(t, s.Put("obligation-observation", o.Org, o.Task, "fail", observation.ID, observation))
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	if o.State != "resolved" {
		t.Fatal("completed current evidence rejected", o.Note)
	}
}

func TestOwnerSourceCancellationStopsActiveVerification(t *testing.T) {
	s, e, task, r, a := ownershipFixture(t)
	o := createFollowup(t, e, r, followupArgs(a))
	completeSource(t, s, task, r)
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	active := taskRuns(s, o.Task)[0]
	setRunning(t, s, &active)
	task.State = "cancelled"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := call(t, e, active, "adc_status", struct{}{}); err == nil {
		t.Fatal("source cancellation ignored by active tools")
	}
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	var followup Assignment
	must(t, s.Get(o.Task, &followup))
	if o.State != "blocked" || followup.State != "paused" {
		t.Fatal("ongoing verification not held")
	}
}

func TestOwnerFundingRevocationStopsActiveVerification(t *testing.T) {
	s, e, task, r, a := ownershipFixture(t)
	o := createFollowup(t, e, r, followupArgs(a))
	completeSource(t, s, task, r)
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	active := taskRuns(s, o.Task)[0]
	setRunning(t, s, &active)
	_, err := s.db.Exec("DELETE FROM memberships WHERE user_id=? AND org=?", task.Creator, task.Org)
	must(t, err)
	if _, err = call(t, e, active, "adc_status", struct{}{}); err == nil {
		t.Fatal("revoked funding remained active")
	}
	e.dispatchObligations(time.Now().Add(2 * time.Hour))
	must(t, s.Get(o.ID, &o))
	var followup Assignment
	must(t, s.Get(o.Task, &followup))
	if o.State != "blocked" || followup.State != "paused" {
		t.Fatal("funding revocation did not stop follow-up")
	}
}
