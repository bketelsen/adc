package adc

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// stewardFixture makes the fixture supervisor ("boss") a steward with a
// charter, a member row for the owner and a storage connection.
func stewardFixture(t *testing.T) (*Store, *Engine, Assignment, Run, Steward) {
	t.Helper()
	s, e, task, r := fixture(t)
	_, err := s.db.Exec("INSERT INTO users VALUES('owner','Owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')")
	must(t, err)
	c := Connection{ID: "storage", Org: task.Org, Name: "Fixture observation", Transport: "stdio", Command: "fixture"}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	v, err := s.saveSteward(Steward{Org: task.Org, Agent: r.Agent, Charter: "Look after shared storage: where data lives, how it is backed up, disk health.", CompletionMode: "routine"}, 0, "human:owner")
	must(t, err)
	return s, e, task, r, v
}

func TestStewardMemoryFactsJournalAndRecall(t *testing.T) {
	s, e, _, r, v := stewardFixture(t)
	_, err := call(t, e, r, "adc_remember", map[string]any{"Key": "pool-layout", "Value": "tank: 4x4TB raidz1; fast: 2x1TB nvme mirror", "Source": "truenas:pool_list", "ObservedAt": now()})
	must(t, err)
	_, err = call(t, e, r, "adc_remember", map[string]any{"Key": "disk3-replaced", "Value": "2026-08-02, WD Red 4TB"})
	must(t, err)
	if facts := s.facts(v.Agent); len(facts) != 2 || facts[0].Key != "disk3-replaced" {
		t.Fatal("facts not recorded and sorted", facts)
	}
	_, err = call(t, e, r, "adc_remember", map[string]any{"Key": "pool-layout", "Value": "tank: 4x8TB raidz1 after the upgrade"})
	must(t, err)
	if f := s.facts(v.Agent)[1]; f.Revision != 2 || !strings.Contains(f.Value, "8TB") || len(list[Fact](s, "fact-history", "")) != 1 {
		t.Fatal("replacement lost history", f)
	}
	_, err = call(t, e, r, "adc_remember", map[string]any{"Key": "disk3-replaced", "Value": ""})
	must(t, err)
	if len(s.facts(v.Agent)) != 1 {
		t.Fatal("retraction did not remove the fact")
	}
	for i := 0; i < factLimit-1; i++ {
		_, err = s.rememberFact(v, "test", "k"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+string(rune('a'+i/26)), "v", "", "")
		must(t, err)
	}
	if _, err = s.rememberFact(v, "test", "one-too-many", "v", "", ""); err == nil || !strings.Contains(err.Error(), "consolidate") {
		t.Fatal("fact cap not enforced", err)
	}
	if _, err = call(t, e, r, "adc_remember", map[string]any{"Key": "pool-layout", "Value": "password=hunter2"}); err == nil {
		t.Fatal("secret stored in memory")
	}
	_, err = call(t, e, r, "adc_journal", map[string]any{"Text": "Scrub finished clean on tank; 0 errors.", "Source": "truenas:scrub"})
	must(t, err)
	_, err = call(t, e, r, "adc_journal", map[string]any{"Text": "Replaced disk 3 after SMART warnings."})
	must(t, err)
	raw, err := call(t, e, r, "adc_recall", map[string]any{"Query": "disk 3"})
	must(t, err)
	var recalled struct {
		Facts   []Fact
		Journal []JournalEntry
	}
	must(t, json.Unmarshal([]byte(raw), &recalled))
	if len(recalled.Journal) != 1 || !strings.Contains(recalled.Journal[0].Text, "Replaced disk 3") || recalled.Journal[0].Run != r.ID {
		t.Fatal("recall missed the journal entry", raw)
	}
	if entries := s.journal(v.Agent, 1); len(entries) != 1 || !strings.Contains(entries[0].Text, "Replaced disk 3") {
		t.Fatal("journal not newest first")
	}
}

func TestSignalsDedupeAcknowledgeSnoozeAndReopen(t *testing.T) {
	s, e, _, r, _ := stewardFixture(t)
	raise := func(sev, msg string) Signal {
		t.Helper()
		raw, err := call(t, e, r, "adc_signal", map[string]any{"Key": "nvme-capacity", "Severity": sev, "Message": msg, "Suggestion": "Plan a larger mirror before 70%."})
		must(t, err)
		var sig Signal
		must(t, json.Unmarshal([]byte(raw), &sig))
		return sig
	}
	first := raise("warning", "fast pool is at 52% capacity")
	again := raise("warning", "fast pool is at 52% capacity")
	if again.ID != first.ID || again.Count != 2 || again.State != "open" || len(list[Signal](s, "signal", "org")) != 1 {
		t.Fatal("repeat did not dedupe", again)
	}
	ack, err := s.signalAction(first.ID, again.Revision, "acknowledge", "owner", time.Now())
	must(t, err)
	if len(s.openSignals("org", time.Now())) != 0 {
		t.Fatal("acknowledged signal still on the home page")
	}
	if quiet := raise("warning", "fast pool is at 52% capacity"); quiet.State != "acknowledged" || quiet.Count != 3 {
		t.Fatal("unchanged repeat reopened an acknowledged signal", quiet)
	}
	if worse := raise("urgent", "fast pool is at 52% capacity"); worse.State != "open" {
		t.Fatal("higher severity did not reopen", worse)
	}
	if _, err = s.signalAction(first.ID, ack.Revision, "snooze", "owner", time.Now()); err == nil {
		t.Fatal("stale revision acted on a signal")
	}
	var current Signal
	must(t, s.Get(first.ID, &current))
	snoozed, err := s.signalAction(first.ID, current.Revision, "snooze", "owner", time.Now())
	must(t, err)
	if len(s.openSignals("org", time.Now())) != 0 || len(s.openSignals("org", time.Now().Add(8*24*time.Hour))) != 1 {
		t.Fatal("snooze did not hide for a week", snoozed.SnoozedUntil)
	}
	_, err = call(t, e, r, "adc_signal", map[string]any{"Key": "nvme-capacity", "Clear": true})
	must(t, err)
	must(t, s.Get(first.ID, &current))
	if current.State != "resolved" || current.ResolvedBy != "run:"+r.ID {
		t.Fatal("clear did not resolve", current)
	}
	if back := raise("info", "fast pool is at 40% after the migration"); back.State != "open" || back.Count != 5 {
		t.Fatal("new observation after resolution did not reopen", back)
	}
	raise2, err := call(t, e, r, "adc_signal", map[string]any{"Key": "backup-failed", "Severity": "urgent", "Message": "Nightly replication to offsite failed twice."})
	must(t, err)
	_ = raise2
	open := s.openSignals("org", time.Now())
	if len(open) != 2 || open[0].Key != "backup-failed" {
		t.Fatal("urgent signal not ordered first", open)
	}
	// The home page strip and its actions.
	w := NewWeb(s, e, false)
	p := Page{View: "work", User: User{ID: "owner", Name: "Owner"}, Org: Organization{ID: "org", Name: "Fixture"}, Agents: list[Agent](s, "agent", "org"), Signals: open}
	rw := httptest.NewRecorder()
	w.render(rw, p)
	if !strings.Contains(rw.Body.String(), "Nightly replication") || !strings.Contains(rw.Body.String(), "Snooze a week") {
		t.Fatal("home page did not show signals")
	}
	form := url.Values{"id": {open[0].ID}, "revision": {"1"}, "action": {"acknowledge"}}
	req := httptest.NewRequest("POST", "/signal-action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	s.mu.Lock()
	err = w.signalWebAction(req, p)
	s.mu.Unlock()
	must(t, err)
	if len(s.openSignals("org", time.Now())) != 1 {
		t.Fatal("web acknowledge did not apply")
	}
}

func TestStewardToolsContextAndRouting(t *testing.T) {
	s, e, task, root, v := stewardFixture(t)
	names := func(r Run) map[string]bool {
		out := map[string]bool{}
		for _, tool := range e.tools(r) {
			out[tool.Name] = true
		}
		return out
	}
	if tools := names(root); !tools["adc_remember"] || !tools["adc_signal"] || !tools["adc_recall"] || !tools["adc_journal"] {
		t.Fatal("steward lacks memory tools")
	}
	worker := Run{ID: "worker", Org: task.Org, Task: task.ID, Parent: root.ID, Agent: "dev", Category: "implementation", State: "running", Model: "gpt-6-astra"}
	must(t, s.Put("run", worker.Org, worker.Task, worker.State, worker.ID, worker))
	if tools := names(worker); tools["adc_remember"] || tools["adc_signal"] {
		t.Fatal("non-steward offered memory tools")
	}
	reviewer := Run{ID: "reviewer", Org: task.Org, Task: task.ID, Parent: root.ID, Agent: root.Agent, ReviewOf: worker.ID, Category: "review", State: "running", Model: "claude-opus-5"}
	must(t, s.Put("run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer))
	if tools := names(reviewer); tools["adc_remember"] {
		t.Fatal("reviewer offered memory tools")
	}
	_, err := s.rememberFact(v, "test", "backup-target", "offsite: rsync.net nightly", "", "")
	must(t, err)
	var boss Agent
	must(t, s.Get(root.Agent, &boss))
	if p := e.systemPrompt(root, task, boss, ""); !strings.Contains(p, "Steward: you own this domain") {
		t.Fatal("steward prompt missing")
	}
	var dev Agent
	must(t, s.Get("dev", &dev))
	if p := e.systemPrompt(worker, task, dev, ""); strings.Contains(p, "Steward: you own") {
		t.Fatal("worker got steward prompt")
	}
	// Routed work: the assignment carries the steward and the worker sees its facts read-only.
	task.Steward = v.Agent
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	b, err := json.Marshal(e.activationContext(worker, task, nil, nil, nil))
	must(t, err)
	if !strings.Contains(string(b), "context_steward") || !strings.Contains(string(b), "rsync.net") || strings.Contains(string(b), "recent_journal") {
		t.Fatal("delegated worker missing read-only steward context", string(b)[:400])
	}
	own := e.activationContext(root, task, nil, nil, nil)
	if own["owner"] != nil || own["steward"] == nil {
		t.Fatal("steward context missing", own["steward"])
	}
	if steward, ok := own["steward"].(map[string]any); !ok || steward["recent_journal"] == nil || steward["charter"] == "" {
		t.Fatal("steward context incomplete", own["steward"])
	}
	// Work handed to a steward is routed to it and takes its completion default.
	v.CompletionMode = "reviewed"
	v, err = s.saveSteward(v, v.Revision, "human:owner")
	must(t, err)
	routed := Assignment{ID: "routed", Org: task.Org, Owner: v.Agent, Creator: "owner", Account: "account", Title: "Check the pool", Prompt: "Look"}
	must(t, e.CreateAssignment(routed))
	must(t, s.Get(routed.ID, &routed))
	if routed.Steward != v.Agent || routineCompletion(routed) {
		t.Fatal("assignment to a steward not routed with its default", routed.Steward, routed.Completion)
	}
	byStewardOnly := Assignment{ID: "by-steward", Org: task.Org, Steward: v.Agent, Creator: "owner", Account: "account", Title: "Check the pool again", Prompt: "Look"}
	must(t, e.CreateAssignment(byStewardOnly))
	must(t, s.Get(byStewardOnly.ID, &byStewardOnly))
	if byStewardOnly.Owner != v.Agent {
		t.Fatal("steward did not become the owner")
	}
	raw, err := call(t, e, root, "adc_status", statusInput{View: "stewards"})
	must(t, err)
	if !strings.Contains(raw, "\"open_signals\"") || !strings.Contains(raw, "Supervisor") {
		t.Fatal("status view missing stewards", raw)
	}
}

func TestStewardPageActionsAndDiscovery(t *testing.T) {
	s, e, _, _, v := stewardFixture(t)
	w := NewWeb(s, e, false)
	p := Page{View: "stewards", User: User{ID: "owner", Name: "Owner"}, Org: Organization{ID: "org", Name: "Fixture"}, Agents: list[Agent](s, "agent", "org"), Stewards: w.stewardViews("org"), Accounts: []Account{{ID: "account", User: "owner", Name: "Fixture"}}}
	rw := httptest.NewRecorder()
	w.render(rw, p)
	if !strings.Contains(rw.Body.String(), "Look after shared storage") || !strings.Contains(rw.Body.String(), "Ask for a steward") {
		t.Fatal("steward page incomplete")
	}
	post := func(values url.Values) error {
		req := httptest.NewRequest("POST", "/steward-action", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		must(t, req.ParseForm())
		s.mu.Lock()
		defer s.mu.Unlock()
		return w.stewardAction(req, p)
	}
	must(t, post(url.Values{"agent": {v.Agent}, "revision": {"1"}, "action": {"charter"}, "charter": {"Look after storage and backups."}, "repositories": {"github.com/example/backup-scripts\n\n"}, "completion_mode": {"reviewed"}}))
	must(t, s.Get(v.ID, &v))
	if v.Charter != "Look after storage and backups." || len(v.Repositories) != 1 || v.CompletionMode != "reviewed" || v.Revision != 2 {
		t.Fatal("charter edit not saved", v)
	}
	if err := post(url.Values{"agent": {v.Agent}, "revision": {"1"}, "action": {"charter"}, "charter": {"Stale"}}); err == nil {
		t.Fatal("stale charter edit accepted")
	}
	must(t, post(url.Values{"agent": {v.Agent}, "revision": {"2"}, "action": {"journal"}, "text": {"Disk 3 replaced today."}}))
	if entries := s.journal(v.Agent, 5); len(entries) != 1 || !strings.HasPrefix(entries[0].Text, "Owner: Disk 3") {
		t.Fatal("human journal note missing", entries)
	}
	must(t, post(url.Values{"agent": {v.Agent}, "revision": {"2"}, "action": {"learn"}, "account": {"account"}, "brief": {"Start with the pools."}}))
	var learn Assignment
	for _, t2 := range list[Assignment](s, "assignment", "org") {
		if strings.HasPrefix(t2.Title, "Learn: ") {
			learn = t2
		}
	}
	if learn.ID == "" || learn.Steward != v.Agent || learn.Authority != "observe" || !learn.ConstrainTools || !routineCompletion(learn) || !strings.Contains(learn.Prompt, "Start with the pools") {
		t.Fatal("discovery assignment wrong", learn)
	}
	if err := post(url.Values{"agent": {v.Agent}, "revision": {"2"}, "action": {"learn"}, "account": {"account"}}); err == nil {
		t.Fatal("second discovery started while one runs")
	}
}

func TestMigrateAreasToStewards(t *testing.T) {
	s, e, task, root := fixture(t)
	area := map[string]any{"ID": "area-1", "Org": "org", "Owner": root.Agent, "Name": "Storage and data protection", "Intent": "Keep the pools healthy.", "Summary": "tank is a 4-disk raidz1; backups go offsite nightly.", "Source": "fixture://truenas", "CompletionMode": "reviewed", "Revision": 3}
	must(t, s.Put("area", "org", root.Agent, "", "area-1", area))
	must(t, s.Put("area-note", "org", "area-1", "open", "note-1", map[string]any{"ID": "note-1", "Area": "area-1", "Text": "old note"}))
	task.Area = "area-1"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	proposal := WorkProposal{ID: "prop-1", Org: "org", Title: "Check", State: "pending", Area: "area-1"}
	must(t, s.Put("proposal", "org", "", "pending", "prop-1", proposal))
	e.migrateAreasToStewards()
	v, ok := s.steward(root.Agent)
	if !ok || v.Charter != "Keep the pools healthy." || v.CompletionMode != "reviewed" {
		t.Fatal("area not migrated", v)
	}
	if facts := s.facts(root.Agent); len(facts) != 1 || facts[0].Key != "understanding" || facts[0].Source != "fixture://truenas" {
		t.Fatal("understanding fact missing", facts)
	}
	if entries := s.journal(root.Agent, 5); len(entries) != 1 || !strings.Contains(entries[0].Text, "Migrated from area") {
		t.Fatal("migration journal entry missing")
	}
	var moved Assignment
	var movedProposal WorkProposal
	must(t, s.Get(task.ID, &moved))
	must(t, s.Get("prop-1", &movedProposal))
	if moved.Steward != root.Agent || moved.Area != "" || movedProposal.Steward != root.Agent || movedProposal.Area != "" {
		t.Fatal("routed records not moved", moved.Steward, moved.Area, movedProposal.Steward, movedProposal.Area)
	}
	if records, _ := s.Records("area", ""); len(records) != 0 {
		t.Fatal("area records retained")
	}
	if records, _ := s.Records("area-note", ""); len(records) != 0 {
		t.Fatal("area notes retained")
	}
	e.migrateAreasToStewards() // idempotent: nothing to do, nothing broken
	if _, ok := s.steward(root.Agent); !ok {
		t.Fatal("second run lost the steward")
	}
}
