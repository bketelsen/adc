package adc

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func stewardConversation(t *testing.T) (*Store, *Engine, *Web, Assignment, Run) {
	t.Helper()
	s, e, _, root := fixture(t)
	_, err := s.db.Exec("INSERT INTO users VALUES('owner','Owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')")
	must(t, err)
	for _, c := range []Connection{{ID: "truenas", Org: "org", Name: "TrueNAS", Transport: "http", URL: "http://fixture"}, {ID: "storage", Org: "org", Name: "Fixture observation", Transport: "stdio", Command: "fixture"}} {
		must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	}
	must(t, s.Put("default", "org", "", "", "default:org:supervision", CategoryDefault{"org", "supervision", root.Agent}))
	w := NewWeb(s, e, false)
	form := url.Values{"prompt": {"Storage on the homelab: where data lives, how it is backed up, disk health, and tell me before the NVMe pool fills up."}}
	req := httptest.NewRequest("POST", "/steward-proposals", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	page := Page{User: User{ID: "owner", Name: "Owner"}, Org: Organization{ID: "org"}}
	s.mu.Lock()
	err = w.startStewardConversation(req, page)
	s.mu.Unlock()
	must(t, err)
	var task Assignment
	for _, candidate := range list[Assignment](s, "assignment", "org") {
		if candidate.StewardCreation {
			task = candidate
		}
	}
	if task.ID == "" || task.Kind != "proposal" || task.Account != "account" || task.Authority != "observe" {
		t.Fatal("conversation not started with the implied subscription", task)
	}
	run := taskRuns(s, task.ID)[0]
	setRunning(t, s, &run)
	return s, e, w, task, run
}

func TestStewardConversationCreatesAgentCharterAndRoutines(t *testing.T) {
	s, e, w, task, run := stewardConversation(t)
	names := map[string]bool{}
	for _, tool := range e.tools(run) {
		names[tool.Name] = true
	}
	if !names["adc_propose_steward"] || names["adc_propose_work"] || names["adc_delegate"] || names["adc_remember"] {
		t.Fatal("designer has the wrong tools", names)
	}
	ctx := e.activationContext(run, task, nil, nil, nil)
	if ctx["org_connections"] == nil || ctx["existing_stewards"] == nil {
		t.Fatal("designer context lacks connections or stewards")
	}
	spec := map[string]any{"Name": "Storage", "Description": "Owns homelab storage: pools, backups, disk health.", "Model": "gpt-6-astra", "Tools": []string{"truenas"}, "Repositories": []string{"github.com/example/homelab"}, "Charter": "Know where data lives and how it is protected; watch capacity and disk health; raise a signal before the NVMe pool passes 70%.", "Routines": []map[string]any{{"Title": "Weekly capacity and backup check", "Brief": "Read pool usage and the last backup results.", "Cadence": map[string]any{"Frequency": "weekly", "Timezone": "UTC", "At": "07:00", "Weekday": 1}}}}
	if _, err := call(t, e, run, "adc_propose_steward", map[string]any{"Name": "Storage", "Description": "x", "Model": "gpt-6-astra", "Charter": "c", "Routines": []map[string]any{{"Title": "No cadence", "Brief": "b"}}}); err == nil {
		t.Fatal("routine without cadence accepted")
	}
	raw, err := call(t, e, run, "adc_propose_steward", spec)
	must(t, err)
	var d Decision
	must(t, json.Unmarshal([]byte(raw), &d))
	if d.ProposedSteward == nil || d.Kind != "steward-proposal" || !strings.Contains(d.Brief, "Create the steward Storage") {
		t.Fatal("proposal decision malformed", d.Brief)
	}
	if len(list[Agent](s, "agent", "org")) != 3 || len(s.stewards("org")) != 0 {
		t.Fatal("proposal created records before approval")
	}
	must(t, decisionPost(t, w, d, "approve", "", nil))
	var storage Agent
	for _, a := range list[Agent](s, "agent", "org") {
		if a.Name == "Storage" {
			storage = a
		}
	}
	if storage.ID == "" || storage.Model != "gpt-6-astra" || storage.Category != "supervision" || len(storage.Tools) != 1 || storage.Tools[0] != "truenas" {
		t.Fatal("agent not created as proposed", storage)
	}
	v, ok := s.steward(storage.ID)
	if !ok || !strings.Contains(v.Charter, "NVMe") || v.CompletionMode != "routine" || len(v.Repositories) != 1 {
		t.Fatal("steward not created", v)
	}
	schedules := list[StandingSchedule](s, "schedule", "org")
	if len(schedules) != 1 || schedules[0].Template.Owner != storage.ID || schedules[0].Template.Steward != storage.ID || schedules[0].State != "active" || schedules[0].Cadence.Frequency != "weekly" || !strings.Contains(schedules[0].Template.Prompt, "one of your routines") {
		t.Fatal("routine schedule not created", schedules)
	}
	must(t, s.Get(task.ID, &task))
	must(t, s.Get(run.ID, &run))
	must(t, s.Get(d.ID, &d))
	if task.State != "ready" || run.State != "complete" || d.CreatedSteward != storage.ID {
		t.Fatal("conversation did not finish on approval", task.State, run.State)
	}
	// The new steward's run gets memory tools and its prompt.
	work := Assignment{ID: "storage-work", Org: "org", Owner: storage.ID, Account: "account", Creator: "owner", Title: "Check the pools", Prompt: "Look"}
	must(t, e.CreateAssignment(work))
	must(t, s.Get(work.ID, &work))
	if work.Steward != storage.ID {
		t.Fatal("work for the new steward not routed")
	}
}

func TestStewardConversationAttachRefineAndBoundaries(t *testing.T) {
	s, e, w, _, run := stewardConversation(t)
	if _, err := call(t, e, run, "adc_propose_steward", map[string]any{"Name": "Developer", "Description": "dup", "Model": "gpt-6-astra", "Charter": "c"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatal("duplicate agent name accepted", err)
	}
	if _, err := call(t, e, run, "adc_propose_steward", map[string]any{"Agent": "dev", "Charter": "Look after the build tooling."}); err != nil {
		t.Fatal(err)
	}
	d := taskDecisions(s, run.Task)[0]
	if d.ProposedSteward.Agent != "dev" || !strings.Contains(d.Brief, "Make Developer a steward") {
		t.Fatal("attach proposal malformed", d.Brief)
	}
	// Refinement keeps the conversation open; a revised proposal replaces the pending one.
	must(t, decisionPost(t, w, d, "refine", "Give it the storage connection too.", nil))
	must(t, s.Get(run.ID, &run))
	if run.State != "queued" || !strings.Contains(run.Prompt, "REFINEMENT REQUESTED") {
		t.Fatal("refinement did not return to the designer", run.State)
	}
	setRunning(t, s, &run)
	raw, err := call(t, e, run, "adc_propose_steward", map[string]any{"Agent": "dev", "Tools": []string{"truenas"}, "Charter": "Look after the build tooling and its storage."})
	must(t, err)
	var revised Decision
	must(t, json.Unmarshal([]byte(raw), &revised))
	must(t, decisionPost(t, w, revised, "approve", "", nil))
	v, ok := s.steward("dev")
	var dev Agent
	must(t, s.Get("dev", &dev))
	if !ok || !strings.Contains(v.Charter, "storage") || len(dev.Tools) != 1 || dev.Tools[0] != "truenas" || len(list[Agent](s, "agent", "org")) != 3 {
		t.Fatal("attach did not update the existing agent", v, dev.Tools)
	}
	if _, err := call(t, e, run, "adc_propose_steward", map[string]any{"Agent": "dev", "Charter": "again"}); err == nil {
		t.Fatal("proposal accepted after the conversation ended")
	}
	// Outside a steward conversation the tool is not offered and the decision is refused.
	_, _, other, otherRoot := fixture(t)
	_ = other
	for _, tool := range e.tools(otherRoot) {
		if tool.Name == "adc_propose_steward" {
			t.Fatal("steward proposal offered outside its conversation")
		}
	}
	if _, err := e.submitDecision(otherRoot, decisionInput{Question: "q", ProposedSteward: &StewardSpec{Agent: "dev", Charter: "c"}}); err == nil {
		t.Fatal("steward decision accepted outside its conversation")
	}
}
