package adc

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTeamRevisionPreservesHistoryAndRequiresFreshApproval(t *testing.T) {
	s, e, task, root := fixture(t)
	proposal := []Agent{
		{Name: "New CTO", Description: "Own outcomes", Model: "gpt-6-astra", Category: "supervision", Authority: "draft"},
		{Name: "New reviewer", Description: "Review independently", Model: "claude-opus-5", Category: "review", Authority: "observe", ReportsTo: "New CTO"},
	}
	_, err := call(t, e, root, "adc_decision", decisionInput{Question: "Astra proposal", Kind: "team_proposal", ProposedAgents: proposal})
	must(t, err)
	old := taskDecisions(s, task.ID)[0]
	setRunning(t, s, &root)
	proposal[0].Model = "gpt-5.6-sol"
	revision := decisionInput{Question: "Sol proposal", Kind: "team_proposal", ProposedAgents: proposal}
	if _, err = call(t, e, root, "adc_decision", revision); err == nil || !strings.Contains(err.Error(), "replaces") {
		t.Fatal("a changed proposal silently returned the old approval")
	}
	must(t, s.Get(root.ID, &root))
	if root.State != "running" {
		t.Fatal("correction hint ended the agent turn")
	}
	revision.Replaces = old.ID
	_, err = call(t, e, root, "adc_decision", revision)
	must(t, err)
	must(t, s.Get(old.ID, &old))
	var current Decision
	for _, d := range taskDecisions(s, task.ID) {
		if d.State == "pending" {
			current = d
		}
	}
	if old.State != "superseded" || old.Proposal[0].Model != "gpt-6-astra" || current.ID == old.ID || current.Replaces != old.ID || current.Proposal[0].Model != "gpt-5.6-sol" || current.Proposal[1].Model != "claude-opus-5" {
		t.Fatal("replacement lost the original proposal or model selections")
	}
	var doc Document
	must(t, s.Get(current.Document, &doc))
	if current.Document != old.Document || doc.Revision != 2 || doc.Content != "Sol proposal" {
		t.Fatal("workspace packet did not advance with proposal")
	}
	first, err := documentVersion(s, doc, 1)
	must(t, err)
	if first.Content != "Astra proposal" {
		t.Fatal("original packet lost")
	}
	if len(list[Agent](s, "agent", task.Org)) != 3 {
		t.Fatal("revision created permanent agents")
	}
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','Owner','owner','fixture'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	w := NewWeb(s, e, false)
	page := Page{User: User{ID: "owner", Name: "Owner"}, Org: Organization{ID: task.Org}}
	approve := func(id string) error {
		form := url.Values{"task": {task.ID}, "decision": {id}, "answer": {"Approve"}, "approve_team": {"on"}}
		req := httptest.NewRequest("POST", "/decision", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		must(t, req.ParseForm())
		return w.action(req, page)
	}
	if approve(old.ID) == nil {
		t.Fatal("stale browser form approved changed team")
	}
	must(t, approve(current.ID))
	var defaults CategoryDefault
	must(t, s.Get("default:org:supervision", &defaults))
	var cto Agent
	must(t, s.Get(defaults.Agent, &cto))
	if cto.Model != "gpt-5.6-sol" {
		t.Fatal("approved category default did not use revised model")
	}
	setRunning(t, s, &root)
	revision.Replaces = current.ID
	if _, err = call(t, e, root, "adc_decision", revision); err == nil {
		t.Fatal("resolved approval rewritten")
	}
}

func TestTeamRevisionCannotHijackDecisionOrPartiallyEditPacket(t *testing.T) {
	s, e, task, root := fixture(t)
	proposal := []Agent{{Name: "New CTO", Description: "Own outcomes", Model: "gpt-6-astra", Category: "supervision", Authority: "draft"}}
	_, err := call(t, e, root, "adc_decision", decisionInput{Question: "Original", ProposedAgents: proposal})
	must(t, err)
	old := taskDecisions(s, task.ID)[0]
	setRunning(t, s, &root)
	if _, err = call(t, e, root, "adc_document", map[string]any{"ID": old.Document, "Title": "Changed", "Content": "Claims Sol without changing proposed roles"}); err == nil {
		t.Fatal("packet drifted from approval")
	}
	child := Run{ID: "stranger", Org: root.Org, Task: root.Task, State: "running"}
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	if _, err = call(t, e, child, "adc_decision", decisionInput{Replaces: old.ID, Question: "Hijack", ProposedAgents: proposal}); err == nil {
		t.Fatal("another run replaced this decision")
	}
	proposal[0].ReportsTo = "missing"
	if _, err = call(t, e, root, "adc_decision", decisionInput{Replaces: old.ID, Question: "Invalid", ProposedAgents: proposal}); err == nil {
		t.Fatal("invalid revision accepted")
	}
	must(t, s.Get(old.ID, &old))
	var doc Document
	must(t, s.Get(old.Document, &doc))
	if old.State != "pending" || doc.Revision != 1 || doc.Content != "Original" || len(taskDecisions(s, task.ID)) != 1 {
		t.Fatal("failed revision partially changed stored state")
	}
}
