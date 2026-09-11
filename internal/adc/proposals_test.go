package adc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func proposalFixture(t *testing.T) (*Store, *Engine, Assignment, Run, WorkProposal) {
	s, e, task, root := fixture(t)
	result, err := call(t, e, root, "adc_propose_work", proposalInput{Title: "Inspect storage recovery coverage", Rationale: "Recent evidence leaves restore coverage uncertain", Scope: "Read-only inspection; no restore or configuration changes", Criteria: "Identify verified coverage and remaining unknowns", Evidence: "Fixture assessment", Owner: "boss"})
	must(t, err)
	var out proposalResult
	must(t, json.Unmarshal([]byte(result), &out))
	return s, e, task, root, out.Proposal
}
func proposalForm(p WorkProposal, action string) url.Values {
	return url.Values{"id": {p.ID}, "revision": {strconv.Itoa(p.Revision)}, "action": {action}, "return": {"/proposal?org=" + p.Org + "&id=" + p.ID}}
}
func actProposal(w *Web, user string, p WorkProposal, form url.Values) error {
	req := httptest.NewRequest("POST", "/proposal-action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = req.ParseForm()
	return w.action(req, Page{User: User{ID: user, Name: user}, Org: Organization{ID: p.Org}})
}
func TestProposalCreationDeduplicatesWithoutStartingWorkAndPinsSources(t *testing.T) {
	s, e, task, root, p := proposalFixture(t)
	if len(list[Assignment](s, "assignment", task.Org)) != 1 || len(taskRuns(s, task.ID)) != 1 {
		t.Fatal("proposal started execution")
	}
	in := proposalInput{Title: p.Title, Scope: p.Scope, Rationale: p.Rationale, Criteria: p.Criteria, Evidence: p.Evidence, Owner: p.Owner}
	result, err := call(t, e, root, "adc_propose_work", in)
	must(t, err)
	var duplicate proposalResult
	must(t, json.Unmarshal([]byte(result), &duplicate))
	if !duplicate.Duplicate || duplicate.Proposal.ID != p.ID || len(list[WorkProposal](s, "proposal", p.Org)) != 1 {
		t.Fatal("duplicate was queued")
	}
	must(t, s.Put("assignment", p.Org, "", "ready", "related", Assignment{ID: "related", Org: p.Org, Title: p.Title, State: "ready"}))
	if len(relatedProposals(s, p)) != 1 {
		t.Fatal("related existing assignment not shown")
	}
	d := Document{ID: "source", Org: p.Org, Task: root.Task, Run: root.ID, Revision: 1, Title: "Source", Content: "Observed fixture evidence"}
	must(t, s.Put("document", d.Org, d.Task, "", d.ID, d))
	in.ID = p.ID
	in.Revision = 1
	in.SourceDocument = d.ID
	_, err = call(t, e, root, "adc_propose_work", in)
	must(t, err)
	must(t, s.Get(p.ID, &p))
	if p.SourceRevision != 1 || p.SourceTask != root.Task || len(proposalHistory(s, p)) != 2 {
		t.Fatal("source/history chain lost")
	}
	d.Org = "foreign"
	must(t, s.Put("document", d.Org, d.Task, "", d.ID, d))
	in.Revision = p.Revision
	if _, err = call(t, e, root, "adc_propose_work", in); err == nil {
		t.Fatal("foreign evidence accepted")
	}
}
func TestHumanEditsInvalidateApprovalAndPreserveOrigin(t *testing.T) {
	s, e, _, _, p := proposalFixture(t)
	w := NewWeb(s, e, false)
	stale := proposalForm(p, "accept")
	stale.Set("owner", "boss")
	stale.Set("account", "account")
	stale.Set("authority", "observe")
	stale.Set("authorization", p.Scope)
	edit := proposalForm(p, "edit")
	for k, v := range map[string]string{"title": p.Title, "scope": "Only inspect the backup catalog", "criteria": p.Criteria, "rationale": p.Rationale, "evidence": p.Evidence, "owner": p.Owner} {
		edit.Set(k, v)
	}
	must(t, actProposal(w, "colleague", p, edit))
	if err := actProposal(w, "owner", p, stale); err == nil {
		t.Fatal("stale human approval accepted")
	}
	var fresh WorkProposal
	must(t, s.Get(p.ID, &fresh))
	if fresh.Revision != 2 || fresh.Task != p.Task || fresh.Run != p.Run || fresh.Scope == p.Scope {
		t.Fatal("revision or origin lost")
	}
	if len(list[Assignment](s, "assignment", p.Org)) != 1 {
		t.Fatal("stale acceptance queued work")
	}
	if len(proposalNotes(s, p)) != 2 {
		t.Fatal("shared edit attribution missing")
	}
}
func TestAcceptanceUsesHumanPortfolioScopeAndQueuesExactlyOnce(t *testing.T) {
	s, e, _, _, p := proposalFixture(t)
	w := NewWeb(s, e, false)
	form := proposalForm(p, "accept")
	form.Set("owner", "boss")
	form.Set("account", "account")
	form.Set("authority", "observe")
	form.Set("authorization", "Read only. Do not change storage.")
	if err := actProposal(w, "colleague", p, form); err == nil {
		t.Fatal("another human's subscription accepted")
	}
	if len(list[Assignment](s, "assignment", p.Org)) != 1 {
		t.Fatal("failed acceptance partially queued work")
	}
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
	must(t, s.Get(p.ID, &p))
	var task Assignment
	must(t, s.Get(p.AcceptedTask, &task))
	if p.State != "accepted" || p.AcceptedBy != "owner" || len(list[Assignment](s, "assignment", p.Org)) != 2 || task.Creator != "owner" || task.Publication || task.Proposal != p.ID || !strings.Contains(task.Prompt, "Read only. Do not change storage.") {
		t.Fatal("acceptance lost identity, scope or atomicity")
	}
	runs := taskRuns(s, task.ID)
	if len(runs) != 1 || runs[0].Authority != "observe" || runs[0].State != "queued" {
		t.Fatal("human authority not applied")
	}
}
func TestDiscussAndDeclineNeverAuthorizeExecution(t *testing.T) {
	s, e, _, _, p := proposalFixture(t)
	w := NewWeb(s, e, false)
	discuss := proposalForm(p, "discuss")
	discuss.Set("account", "account")
	discuss.Set("message", "Could this be smaller?")
	must(t, actProposal(w, "owner", p, discuss))
	var discussion Assignment
	for _, task := range list[Assignment](s, "assignment", p.Org) {
		if task.Kind == "proposal" {
			discussion = task
		}
	}
	if discussion.ID == "" {
		t.Fatal("no discussion queued")
	}
	must(t, s.Get(p.ID, &p))
	if p.State != "pending" || p.AcceptedTask != "" {
		t.Fatal("discussion accepted proposal")
	}
	run := taskRuns(s, discussion.ID)[0]
	setRunning(t, s, &run)
	if run.Authority != "observe" || len(run.Tools) != 0 {
		t.Fatal("discussion inherited execution access")
	}
	for _, tool := range e.tools(run) {
		if tool.Name == "adc_delegate" || tool.Name == "adc_code" || tool.Name == "adc_decision" {
			t.Fatal("discussion exposed execution/approval tools")
		}
	}
	decline := proposalForm(p, "decline")
	decline.Set("message", "Not needed right now")
	must(t, actProposal(w, "colleague", p, decline))
	in := proposalInput{ID: p.ID, Revision: p.Revision, Title: p.Title, Scope: "Changed", Rationale: p.Rationale, Criteria: p.Criteria, Evidence: p.Evidence, Owner: p.Owner}
	if _, err := call(t, e, run, "adc_propose_work", in); err == nil {
		t.Fatal("late agent overwrote human decline")
	}
	_, err := call(t, e, run, "adc_finish", map[string]any{"Result": "A narrower inspection could be proposed later."})
	must(t, err)
	must(t, s.Get(p.ID, &p))
	must(t, s.Get(discussion.ID, &discussion))
	if p.State != "declined" || p.AcceptedTask != "" || discussion.State != "ready" || len(taskReviews(s, discussion.ID)) != 0 {
		t.Fatal("discussion failed to finish without work/review")
	}
	if len(proposalNotes(s, p)) != 4 {
		t.Fatal("question, reply or decision missing")
	}
}
func TestProposalOrgAndDependencyChecks(t *testing.T) {
	s, e, _, _, p := proposalFixture(t)
	w := NewWeb(s, e, false)
	p.Dependencies = "Confirm dataset owner"
	must(t, s.Put("proposal", p.Org, p.Task, p.State, p.ID, p))
	form := proposalForm(p, "accept")
	form.Set("owner", "boss")
	form.Set("account", "account")
	form.Set("authority", "observe")
	form.Set("authorization", p.Scope)
	if err := actProposal(w, "owner", p, form); err == nil {
		t.Fatal("dependencies not reviewed")
	}
	foreign := p
	foreign.Org = "foreign"
	if err := actProposal(w, "owner", foreign, form); err == nil {
		t.Fatal("cross-organization proposal action accepted")
	}
	form.Set("dependencies_reviewed", "on")
	must(t, actProposal(w, "owner", p, form))
}

func TestProposalQueueIsSharedButPortfoliosAndActionsAreProtected(t *testing.T) {
	s, e, _, _, p := proposalFixture(t)
	w := NewWeb(s, e, false)
	for _, id := range []string{"owner", "member", "outsider"} {
		_, err := s.db.Exec(`INSERT INTO users VALUES(?,?,?,?)`, id, id, id, []byte("unused fixture hash"))
		must(t, err)
		_, err = s.db.Exec(`INSERT INTO sessions VALUES(?,?,?)`, digest(id+"-token"), id, "2099-01-01T00:00:00Z")
		must(t, err)
		if id != "outsider" {
			_, err = s.db.Exec(`INSERT INTO memberships VALUES(?,?)`, id, p.Org)
			must(t, err)
		}
	}
	var owner Account
	must(t, s.Get("account", &owner))
	owner.Name = "Owner private subscription"
	must(t, s.Put("account", "", owner.User, "", owner.ID, owner))
	member := Account{ID: "member-account", User: "member", Name: "Member subscription", Limit: 1}
	must(t, s.Put("account", "", member.User, "", member.ID, member))
	for _, id := range []string{"owner", "member", "outsider"} {
		req := httptest.NewRequest("GET", "/proposal?org="+p.Org+"&id="+p.ID, nil)
		req.AddCookie(&http.Cookie{Name: "adc_session", Value: id + "-token"})
		rw := httptest.NewRecorder()
		w.Handler().ServeHTTP(rw, req)
		if id == "outsider" {
			if rw.Code != 403 {
				t.Fatal("nonmember read proposal")
			}
			continue
		}
		if rw.Code != 200 || !strings.Contains(rw.Body.String(), p.Title) {
			t.Fatal("organization member cannot see shared proposal")
		}
		if id == "member" && (!strings.Contains(rw.Body.String(), member.Name) || strings.Contains(rw.Body.String(), owner.Name)) {
			t.Fatal("portfolio picker leaked another human's account")
		}
	}
	form := proposalForm(p, "decline")
	form.Set("message", "Not warranted")
	req := httptest.NewRequest("POST", "/proposal-action?org="+p.Org, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "adc_session", Value: "member-token"})
	rw := httptest.NewRecorder()
	w.Handler().ServeHTTP(rw, req)
	if rw.Code != 403 {
		t.Fatal("missing CSRF allowed action")
	}
	form.Set("csrf", digest("csrf:member-token"))
	req = httptest.NewRequest("POST", "/proposal-action?org="+p.Org, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "adc_session", Value: "member-token"})
	rw = httptest.NewRecorder()
	w.Handler().ServeHTTP(rw, req)
	if rw.Code != 303 {
		t.Fatal("equal member could not decide")
	}
	must(t, s.Get(p.ID, &p))
	if p.State != "declined" {
		t.Fatal("member decision not saved")
	}
}
