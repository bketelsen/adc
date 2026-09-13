package adc

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func decisionPost(t *testing.T, w *Web, d Decision, outcome, notes string, extras url.Values) error {
	t.Helper()
	form := url.Values{"task": {d.Task}, "decision": {d.ID}, "outcome": {outcome}, "answer": {notes}}
	for k, v := range extras {
		form[k] = v
	}
	req := httptest.NewRequest("POST", "/decision", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	return w.action(req, Page{User: User{ID: "owner", Name: "Owner"}, Org: Organization{ID: d.Org}})
}
func acceptanceFixture(t *testing.T, supervisor bool) (*Store, *Engine, Run, Decision) {
	t.Helper()
	s, e, r, _ := milestoneFixture(t, "human-evidence")
	transcriptLogin(t, s)
	requester := r
	if supervisor {
		must(t, s.Get(s.taskPlan(r.Task).Supervisor, &requester))
	}
	d, err := e.submitDecision(requester, decisionInput{Brief: "Accept the proposed support boundary. This records policy acceptance only.", Question: "The support policy and its evidence are ready for acceptance.", Acceptance: &decisionAcceptanceInput{Run: r.ID, Requirement: "gate"}})
	must(t, err)
	return s, e, r, d
}
func TestDecisionAcceptanceCommitsAndResumes(t *testing.T) {
	for _, supervisor := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker", true: "supervisor"}[supervisor], func(t *testing.T) {
			s, e, r, d := acceptanceFixture(t, supervisor)
			if supervisor {
				r.State = "blocked"
				must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			}
			must(t, decisionPost(t, NewWeb(s, e, false), d, "approve", "Ben is the backup operator.", nil))
			must(t, s.Get(d.ID, &d))
			must(t, s.Get(r.ID, &r))
			v := s.milestoneEvidence(r.ID, "gate")
			if d.Outcome != "approve" || d.State != "answered" || d.ResolvedBy != "owner" || d.ResolvedAt == "" || v.Actor != "human:owner" || v.ObservedAt != d.ResolvedAt || !strings.Contains(v.Reference, d.ID) || !strings.Contains(v.Summary, "Ben is the backup operator") || r.State != "queued" {
				t.Fatalf("acceptance not durable or worker not resumed: decision=%+v evidence=%+v state=%s", d, v, r.State)
			}
			var task Assignment
			must(t, s.Get(r.Task, &task))
			if task.Publication {
				t.Fatal("policy acceptance granted publication")
			}
			if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State == "complete" {
				t.Fatal("human approval bypassed independent review")
			}
			if decisionPost(t, NewWeb(s, e, false), d, "approve", "", nil) == nil {
				t.Fatal("duplicate approval accepted")
			}
			if s.milestoneEvidence(r.ID, "gate").Revision != 1 {
				t.Fatal("duplicate evidence")
			}
		})
	}
}
func TestDecisionRejectRefineNeverAccept(t *testing.T) {
	for _, outcome := range []string{"reject", "refine"} {
		t.Run(outcome, func(t *testing.T) {
			s, e, r, d := acceptanceFixture(t, false)
			w := NewWeb(s, e, false)
			if outcome == "refine" && decisionPost(t, w, d, outcome, "", nil) == nil {
				t.Fatal("refinement without notes accepted")
			}
			must(t, decisionPost(t, w, d, outcome, "Change the proposed boundary.", url.Values{"publish": {"on"}, "approve_team": {"on"}}))
			must(t, s.Get(d.ID, &d))
			must(t, s.Get(r.ID, &r))
			var task Assignment
			must(t, s.Get(r.Task, &task))
			if d.Outcome != outcome || s.milestoneEvidence(r.ID, "gate").ID != "" || task.Publication || r.State != "queued" || !strings.Contains(r.Prompt, "Change the proposed boundary") {
				t.Fatal("rejection/refinement granted authority or failed to resume")
			}
			_, err := e.submitDecision(r, decisionInput{Brief: "Revised boundary", Question: "Revised evidence", Acceptance: &decisionAcceptanceInput{Run: r.ID, Requirement: "gate"}})
			must(t, err)
		})
	}
}
func TestDecisionAcceptanceRejectsStaleBindings(t *testing.T) {
	for _, change := range []string{"plan", "artifact", "evidence", "superseded", "foreign", "paused"} {
		t.Run(change, func(t *testing.T) {
			s, e, r, d := acceptanceFixture(t, true)
			switch change {
			case "plan":
				p := s.taskPlan(r.Task)
				p.Revision++
				must(t, s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p))
			case "artifact":
				doc := Document{ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, Title: "Changed", Content: "New material", Revision: 1}
				must(t, s.Put("document", r.Org, r.Task, "", doc.ID, doc))
			case "evidence":
				_, err := e.submitMilestone(r, milestonePayload("human-evidence"), "owner")
				must(t, err)
			case "superseded":
				r.Superseded = true
				must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			case "foreign":
				r.Org = "other"
				must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			case "paused":
				var task Assignment
				must(t, s.Get(r.Task, &task))
				task.State = "paused"
				must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
			}
			if decisionPost(t, NewWeb(s, e, false), d, "approve", "", nil) == nil {
				t.Fatal("stale acceptance approved")
			}
			must(t, s.Get(d.ID, &d))
			if d.State != "pending" {
				t.Fatal("failed approval partially resolved")
			}
			if change != "evidence" && s.milestoneEvidence(r.ID, "gate").ID != "" {
				t.Fatal("failed approval wrote evidence")
			}
			// Humans can still reject/refine a stale proposal without approving its new contents.
			if change != "foreign" {
				must(t, decisionPost(t, NewWeb(s, e, false), d, "refine", "Refresh the stale request.", nil))
			}
		})
	}
}
func TestDecisionAcceptanceAtomicFailure(t *testing.T) {
	s, e, r, d := acceptanceFixture(t, false)
	_, err := s.db.Exec(`CREATE TRIGGER fail_acceptance BEFORE INSERT ON records WHEN NEW.kind = 'milestone-evidence' BEGIN SELECT RAISE(ABORT, 'fixture failure'); END;`)
	must(t, err)
	if decisionPost(t, NewWeb(s, e, false), d, "approve", "", nil) == nil {
		t.Fatal("fixture database failure ignored")
	}
	must(t, s.Get(d.ID, &d))
	must(t, s.Get(r.ID, &r))
	if d.State != "pending" || r.State != "waiting" || s.milestoneEvidence(r.ID, "gate").ID != "" {
		t.Fatal("partial decision transaction")
	}
}
func TestGeneralDecisionReplacementAndTeamButtons(t *testing.T) {
	s, e, task, r := fixture(t)
	transcriptLogin(t, s)
	d, err := e.submitDecision(r, decisionInput{Brief: "Original brief", Question: "Original detail"})
	must(t, err)
	next, err := e.submitDecision(r, decisionInput{Replaces: d.ID, Brief: "Revised brief", Question: "Revised detail"})
	must(t, err)
	w := NewWeb(s, e, false)
	if decisionPost(t, w, d, "approve", "", nil) == nil {
		t.Fatal("superseded decision approved")
	}
	must(t, decisionPost(t, w, next, "approve", "", nil))
	proposal := []Agent{{Name: "New CTO", Description: "Own outcomes", Model: "gpt-5.6-sol", Category: "supervision", Authority: "draft"}}
	team, err := e.submitDecision(r, decisionInput{Question: "Create proposed team", ProposedAgents: proposal})
	must(t, err)
	must(t, decisionPost(t, w, team, "approve", "", nil))
	if len(list[Agent](s, "agent", task.Org)) != 4 {
		t.Fatal("Approve did not create team")
	}
	permission := Decision{ID: ID(), Org: task.Org, Task: task.ID, Run: r.ID, State: "pending", Kind: "permission", Question: "Access"}
	must(t, s.Put("decision", task.Org, task.ID, permission.State, permission.ID, permission))
	if decisionPost(t, w, permission, "approve", "", nil) == nil {
		t.Fatal("generic buttons bypassed permission bundle")
	}
}

func TestDecisionAcceptanceRetryRefreshesStalePin(t *testing.T) {
	for _, change := range []string{"artifact", "plan", "evidence"} {
		t.Run(change, func(t *testing.T) {
			s, e, r, d := acceptanceFixture(t, false)
			input := decisionInput{Brief: "  " + d.Brief + "  ", Question: d.Question, Acceptance: &decisionAcceptanceInput{Run: r.ID, Requirement: "gate"}}
			repeated, err := e.submitDecision(r, input)
			must(t, err)
			if repeated.ID != d.ID {
				t.Fatal("idempotent retry created another decision")
			}
			switch change {
			case "artifact":
				doc := Document{ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, Content: "Changed proposal", Revision: 1}
				must(t, s.Put("document", r.Org, r.Task, "", doc.ID, doc))
			case "plan":
				p := s.taskPlan(r.Task)
				p.Revision++
				must(t, s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p))
			case "evidence":
				_, err := e.submitMilestone(r, milestonePayload("human-evidence"), "owner")
				must(t, err)
			}
			setRunning(t, s, &r)
			_, err = e.submitDecision(r, input)
			if err == nil || !strings.Contains(err.Error(), "replaces="+d.ID) {
				t.Fatal("stale retry did not explain how to refresh", err)
			}
			must(t, s.Get(r.ID, &r))
			if r.State != "running" {
				t.Fatal("stale retry yielded instead of correcting")
			}
			input.Replaces = d.ID
			next, err := e.submitDecision(r, input)
			if change == "evidence" {
				if err == nil {
					t.Fatal("asked to accept evidence already recorded")
				}
				return
			}
			must(t, err)
			must(t, decisionPost(t, NewWeb(s, e, false), next, "approve", "", nil))
			must(t, s.Get(d.ID, &d))
			if d.State != "superseded" {
				t.Fatal("original stale card still pending")
			}
		})
	}

}

func TestDecisionApprovalWebAuthentication(t *testing.T) {
	s, e, _, d := acceptanceFixture(t, false)
	w := NewWeb(s, e, false)
	for _, scenario := range []string{"anonymous", "csrf", "nonmember"} {
		t.Run(scenario, func(t *testing.T) {
			form := url.Values{"task": {d.Task}, "decision": {d.ID}, "outcome": {"approve"}, "csrf": {digest("csrf:transcript-fixture")}}
			if scenario == "csrf" {
				form.Set("csrf", "wrong")
			}
			req := httptest.NewRequest("POST", "/decision?org=org", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if scenario != "anonymous" {
				req.AddCookie(&http.Cookie{Name: "adc_session", Value: "transcript-fixture"})
			}
			if scenario == "nonmember" {
				_, err := s.db.Exec(`DELETE FROM memberships WHERE user_id='owner'`)
				must(t, err)
			}
			out := httptest.NewRecorder()
			w.Handler().ServeHTTP(out, req)
			if scenario == "anonymous" && out.Code != 303 || scenario != "anonymous" && out.Code != 403 {
				t.Fatalf("unexpected status %d", out.Code)
			}
			var stored Decision
			must(t, s.Get(d.ID, &stored))
			if stored.State != "pending" {
				t.Fatal("unauthorized approval changed state")
			}
		})
	}
}
