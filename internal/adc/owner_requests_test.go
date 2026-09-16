package adc

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func requestArgs() ownerRequestInput {
	return ownerRequestInput{Recipient: "dev", Key: "nas-evidence", Subject: "Confirm backup coverage", Question: "Which hosting volumes have observed backup coverage?", Criteria: "Identify actual evidence and remaining gaps; no changes", Reference: "fixture://hosting-scope"}
}
func makeOwnerRequest(t *testing.T, e *Engine, r Run) OwnerRequest {
	t.Helper()
	raw, err := call(t, e, r, "adc_owner_request", requestArgs())
	must(t, err)
	var q OwnerRequest
	must(t, json.Unmarshal([]byte(raw), &q))
	return q
}
func recipientFixture(t *testing.T, s *Store, e *Engine) Run {
	t.Helper()
	_, err := s.db.Exec("INSERT OR IGNORE INTO users VALUES('recipient-human','Recipient','recipient','unused'); INSERT OR IGNORE INTO memberships VALUES('recipient-human','org')")
	must(t, err)
	a := Account{ID: "recipient-account", User: "recipient-human", Limit: 1}
	must(t, s.Put("account", "", a.User, "", a.ID, a))
	task := Assignment{ID: ID(), Org: "org", Owner: "dev", Creator: a.User, Account: a.ID, Authority: "observe", Title: "Authorized NAS observation", Prompt: "Read known storage evidence only"}
	must(t, e.CreateAssignment(task))
	r := taskRuns(s, task.ID)[0]
	setRunning(t, s, &r)
	return r
}

func TestOwnerRequestSurvivesSourceCompletionAndRestartWithoutBorrowingAuthority(t *testing.T) {
	s, e, task, r, _ := stewardFixture(t)
	q := makeOwnerRequest(t, e, r)
	same := makeOwnerRequest(t, e, r)
	if same.ID != q.ID {
		t.Fatal("duplicate request")
	}
	completeSource(t, s, task, r)
	e.dispatchOwnerRequests()
	must(t, s.Get(q.ID, &q))
	if q.TargetRun != "" || len(list[Assignment](s, "assignment", task.Org)) != 1 {
		t.Fatal("request created unfunded work")
	}
	must(t, s.Close())
	s2, err := Open(s.Dir)
	must(t, err)
	defer s2.Close()
	e = NewEngine(s2)
	receiver := recipientFixture(t, s2, e)
	original := receiver
	e.dispatchOwnerRequests()
	e.dispatchOwnerRequests()
	must(t, s2.Get(q.ID, &q))
	must(t, s2.Get(receiver.ID, &receiver))
	if q.TargetRun != receiver.ID || q.Deliveries != 1 || receiver.Account != original.Account || receiver.Authority != original.Authority || !Subset(receiver.Tools, original.Tools) {
		t.Fatal("delivery duplicated or borrowed source authority")
	}
	response := map[string]any{"Request": q.ID, "Outcome": "answered", "Response": "Observed evidence covers volume A; volume B remains unverified.", "Reference": "fixture://nas-result", "Revision": q.Revision}
	_, err = call(t, e, receiver, "adc_owner_respond", response)
	must(t, err)
	_, err = call(t, e, receiver, "adc_owner_respond", response)
	must(t, err) // Lost-response retry.
	must(t, s2.Get(q.ID, &q))
	if q.State != "answered" {
		t.Fatal("answer lost")
	}
	must(t, s2.Get(task.ID, &task))
	if task.State != "ready" {
		t.Fatal("source task resurrected")
	}
	next := Assignment{ID: "next-owner-work", Org: task.Org, Owner: "boss", Creator: task.Creator, Account: task.Account, Title: "Next authorized hosting task", Prompt: "Use known evidence"}
	must(t, e.CreateAssignment(next))
	lead := taskRuns(s2, next.ID)[0]
	setRunning(t, s2, &lead)
	e.dispatchOwnerRequests()
	e.dispatchOwnerRequests()
	must(t, s2.Get(q.ID, &q))
	if q.ReceiptRun != lead.ID {
		t.Fatal("response did not reach continuing owner")
	}
	context, _ := json.Marshal(e.requestContext(lead))
	if !strings.Contains(string(context), "volume B remains unverified") {
		t.Fatal("owner lost response")
	}
	if len(taskReviews(s2, next.ID)) != 0 {
		t.Fatal("correspondence manufactured independent review")
	}
}

func TestOwnerRequestCancellationRevocationAndUnrelatedWork(t *testing.T) {
	s, e, task, r, _ := stewardFixture(t)
	q := makeOwnerRequest(t, e, r)
	receiver := recipientFixture(t, s, e)
	e.dispatchOwnerRequests()
	must(t, s.Get(q.ID, &q))
	other := receiver
	other.ID = "other-run"
	other.Org = "other-org"
	must(t, s.Put("run", other.Org, other.Task, other.State, other.ID, other))
	if _, err := call(t, e, other, "adc_owner_inbox", map[string]string{"Request": q.ID}); err == nil {
		t.Fatal("cross-org inbox leaked")
	}
	task.State = "cancelled"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := e.respondOwnerRequest(receiver, q.ID, "answered", "Assume done", "fixture://claim", q.Revision); err == nil {
		t.Fatal("cancelled request accepted response")
	}
	e.dispatchOwnerRequests()
	must(t, s.Get(q.ID, &q))
	if q.State != "cancelled" {
		t.Fatal("source cancellation ignored")
	}
	must(t, s.Get(receiver.ID, &receiver))
	if receiver.State != "running" {
		t.Fatal("unrelated receiving work cancelled")
	}
}

func TestOwnerRequestBlockedResponseReachesLeadWithoutRetryLoop(t *testing.T) {
	s, e, _, r, _ := stewardFixture(t)
	q := makeOwnerRequest(t, e, r)
	receiver := recipientFixture(t, s, e)
	e.dispatchOwnerRequests()
	must(t, s.Get(q.ID, &q))
	_, err := e.respondOwnerRequest(receiver, q.ID, "blocked", "Current scope lacks the needed observation; no mutation authorized.", "fixture://missing-evidence", q.Revision)
	must(t, err)
	for i := 0; i < 4; i++ {
		e.dispatchOwnerRequests()
	}
	must(t, s.Get(q.ID, &q))
	if q.State != "blocked" || q.ReceiptRun != r.ID || q.Deliveries != 1 {
		t.Fatal("blocker stranded or repeatedly dispatched", q)
	}
	var root Run
	must(t, s.Get(r.ID, &root))
	if root.State != "running" {
		t.Fatal("supervisor stopped")
	}
}

func TestOwnerRequestAttemptBudgetAndLeadHistory(t *testing.T) {
	s, e, _, r, _ := stewardFixture(t)
	q := makeOwnerRequest(t, e, r)
	for i := 0; i < 3; i++ {
		receiver := recipientFixture(t, s, e)
		e.dispatchOwnerRequests()
		must(t, s.Get(q.ID, &q))
		if q.TargetRun != receiver.ID {
			t.Fatal("not delivered")
		}
		receiver.State = "complete"
		must(t, s.Put("run", receiver.Org, receiver.Task, receiver.State, receiver.ID, receiver))
		e.dispatchOwnerRequests()
	}
	must(t, s.Get(q.ID, &q))
	if q.State != "blocked" || q.Deliveries != 3 {
		t.Fatal("unbounded receiver retries")
	}
	_, err := call(t, e, r, "adc_owner_lead", map[string]any{"Request": q.ID, "Lead": "qa", "Reason": "A different expert will coordinate the unresolved evidence", "Revision": q.Revision})
	must(t, err)
	must(t, s.Get(q.ID, &q))
	if q.Lead != "qa" || q.Supervisor != r.Agent || q.Deliveries != 3 {
		t.Fatal("lead transfer discarded accountability or effort")
	}
	if len(list[OwnerRequest](s, "owner-request-history", q.Org)) < 3 {
		t.Fatal("handoff history lost")
	}
}

func TestOwnerRequestsBoundCreationAndRejectCredentials(t *testing.T) {
	s, e, _, r, _ := stewardFixture(t)
	p := requestArgs()
	p.Question = "password=not-for-shared-context"
	if _, err := e.createOwnerRequest(r, p); err == nil {
		t.Fatal("credential accepted")
	}
	for i := 0; i < 4; i++ {
		p = requestArgs()
		p.Key = fmt.Sprint("request", i)
		p.Question = fmt.Sprint("Different evidence question ", i)
		_, err := e.createOwnerRequest(r, p)
		must(t, err)
	}
	p = requestArgs()
	p.Key = "extra"
	if _, err := e.createOwnerRequest(r, p); err == nil {
		t.Fatal("request budget bypassed")
	}
	if len(list[OwnerRequest](s, "owner-request", r.Org)) != 4 {
		t.Fatal("rejected request persisted")
	}
}

func TestOwnerRequestDoesNotWakeWaitingReceiverOrLead(t *testing.T) {
	s, e, task, r, _ := stewardFixture(t)
	q := makeOwnerRequest(t, e, r)
	receiver := recipientFixture(t, s, e)
	receiver.State = "waiting"
	receiver.CandidateRevision = "pending-candidate"
	must(t, s.Put("run", receiver.Org, receiver.Task, receiver.State, receiver.ID, receiver))
	e.dispatchOwnerRequests()
	must(t, s.Get(receiver.ID, &receiver))
	must(t, s.Get(q.ID, &q))
	if receiver.State != "waiting" || receiver.Activations != 0 || q.TargetRun != "" {
		t.Fatal("cross-task request bypassed existing wait gates")
	}
	receiver.CandidateRevision = ""
	setRunning(t, s, &receiver)
	e.dispatchOwnerRequests()
	must(t, s.Get(q.ID, &q))
	_, err := e.respondOwnerRequest(receiver, q.ID, "answered", "Evidence is available", "fixture://evidence", q.Revision)
	must(t, err)
	r.State = "waiting"
	must(t, s.Put("run", r.Org, task.ID, r.State, r.ID, r))
	e.dispatchOwnerRequests()
	must(t, s.Get(r.ID, &r))
	must(t, s.Get(q.ID, &q))
	if r.State != "waiting" || r.Activations != 0 || q.ReceiptRun != "" {
		t.Fatal("receipt bypassed lead wait gates")
	}
}

func TestBlockedRequestCanBeAnsweredByLaterOwnerRun(t *testing.T) {
	s, e, _, r, _ := stewardFixture(t)
	q := makeOwnerRequest(t, e, r)
	receiver := recipientFixture(t, s, e)
	e.dispatchOwnerRequests()
	must(t, s.Get(q.ID, &q))
	_, err := e.respondOwnerRequest(receiver, q.ID, "blocked", "Observation missing", "fixture://gap", q.Revision)
	must(t, err)
	receiver.State = "complete"
	must(t, s.Put("run", receiver.Org, receiver.Task, receiver.State, receiver.ID, receiver))
	next := recipientFixture(t, s, e)
	must(t, s.Get(q.ID, &q))
	_, err = e.respondOwnerRequest(next, q.ID, "answered", "New observed evidence closes the gap", "fixture://new-observation", q.Revision)
	must(t, err)
	must(t, s.Get(q.ID, &q))
	if q.State != "answered" || q.Responder != next.ID || q.Deliveries != 1 {
		t.Fatal("stale blocker claim prevented recovery")
	}
}

func TestOwnerRoutingCachePreservesAllMessages(t *testing.T) {
	s, e, _, r, _ := stewardFixture(t)
	q1 := makeOwnerRequest(t, e, r)
	p := requestArgs()
	p.Key = "second"
	p.Question = "A separate observation request"
	q2, err := e.createOwnerRequest(r, p)
	must(t, err)
	receiver := recipientFixture(t, s, e)
	e.dispatchOwnerRequests()
	must(t, s.Get(receiver.ID, &receiver))
	for _, id := range []string{q1.ID, q2.ID} {
		var q OwnerRequest
		must(t, s.Get(id, &q))
		if q.TargetRun != receiver.ID || !strings.Contains(receiver.Prompt, id) {
			t.Fatal("cached delivery lost a message", id)
		}
	}
	var invalid Run
	if s.Get("", &invalid) == nil {
		t.Fatal("routing created an empty run")
	}
}
