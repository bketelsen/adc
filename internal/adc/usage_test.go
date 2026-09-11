package adc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

func usageAt(t *testing.T, s *Store, org, task, run, kind, payload string, at time.Time) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO events(org,task,run,kind,text,at) VALUES(?,?,?,?,?,?)`, org, task, run, kind, payload, at.UTC().Format(time.RFC3339Nano))
	must(t, err)
}
func TestUsageReadsFullHistoryAndDistinguishesUnknown(t *testing.T) {
	s, _, task, root := fixture(t)
	at := time.Now().Add(time.Second)
	for i := 0; i < 300; i++ {
		usageAt(t, s, task.Org, task.ID, root.ID, "usage", `{"model":"gpt-5.6-sol","input":10,"output":2,"cache_read":0}`, at.Add(-time.Hour))
	}
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", `{"model":"gpt-5.6-sol","input":null,"output":0,"cache_read":-4}`, at.Add(-time.Minute))
	usageAt(t, s, task.Org, task.ID, "missing-run", "started", "no report", at.Add(-time.Minute))
	got, err := s.usageReport(task.Org, "", 0, 0, at)
	must(t, err)
	if got.Total.Samples != 301 || got.Total.Input.Value != 3000 || got.Total.Output.Value != 600 || got.Total.Runs != 2 || got.Total.ReportedRuns != 1 {
		t.Fatalf("lost history or coverage: %+v", got.Total)
	}
	if got.Total.Input.String() != "3,000 · partial" || got.Total.CacheRead.String() != "0 · partial" || got.Total.CacheWrite.String() != "Unknown" {
		t.Fatalf("missing values became zero/full: %+v", got.Total)
	}
	if len(got.Models) != 1 || got.Models[0].ID != "gpt-5.6-sol" {
		t.Fatal("unreported run attributed to a configured model")
	}
	if len(got.Accounts) != 1 || got.Accounts[0].ID != task.Account {
		t.Fatal("legacy funding attribution lost")
	}
}
func TestUsageDedupReceiptWindowAndSnapshotAttribution(t *testing.T) {
	s, _, task, root := fixture(t)
	at := time.Now().Add(time.Second)
	currentAccount := task.Account
	payload := `{"model":"served-model","input":17,"output":3,"cache_read":0,"cache_write":5,"account":"original-account","provider":"copilot","session":"s1","event_id":"event"}`
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", payload, at.Add(-2*time.Hour))
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", payload, at.Add(-time.Hour))
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", strings.Replace(payload, "s1", "s2", 1), at.Add(-time.Hour))
	old := strings.Replace(payload, `"event_id":"event"`, `"event_id":"old"`, 1)
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", old, at.AddDate(0, 0, -40))
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", old, at.Add(-time.Hour))
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", `{"input":999}`, at.Add(time.Hour))
	usageAt(t, s, "foreign", "foreign", "foreign", "usage", payload, at.Add(-time.Hour))
	got, err := s.usageReport(task.Org, task.ID, 1, 0, at)
	must(t, err)
	if got.Total.Samples != 2 || got.Total.Input.Value != 34 || got.Total.CacheWrite.Value != 10 {
		t.Fatalf("duplicates/window wrong: %+v", got.Total)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].ID == currentAccount || got.Models[0].ID != "served-model" {
		t.Fatal("mutable configuration replaced captured attribution")
	}
	if _, err = s.usageReport(task.Org, "foreign", 0, 0, at); err == nil {
		t.Fatal("foreign assignment accepted")
	}
	all, err := s.usageReport(task.Org, "", 0, 0, at)
	must(t, err)
	if all.Total.Samples != 3 {
		t.Fatal("historical event missing")
	}
}
func TestUsageMissingMalformedAndEmpty(t *testing.T) {
	s, _, task, root := fixture(t)
	at := time.Now().Add(time.Second)
	empty, err := s.usageReport(task.Org, task.ID, 7, 0, at)
	must(t, err)
	if empty.Total.Input.String() != "Unknown" || empty.Total.Samples != 0 {
		t.Fatal("empty telemetry reported zero")
	}
	for _, payload := range []string{`{"input":"invalid"}`, `null`, `broken`, `{"input":-5,"output":0}`} {
		usageAt(t, s, task.Org, task.ID, root.ID, "usage", payload, at.Add(-time.Minute))
	}
	got, err := s.usageReport(task.Org, "", 7, 0, at)
	must(t, err)
	if got.Malformed != 3 || got.Total.Input.String() != "Unknown" || got.Total.Output.String() != "0 · partial" {
		t.Fatalf("invalid metrics counted: %+v", got)
	}
}
func TestUsageCaptureAllowlistAndReplay(t *testing.T) {
	s, _, task, root := fixture(t)
	input, output, write := int64(120), int64(30), int64(0)
	d := &copilot.AssistantUsageData{Model: "gpt-5.6-sol", InputTokens: &input, OutputTokens: &output, CacheWriteTokens: &write}
	s.logUsage(task, root, "session", "event", d)
	s.logUsage(task, root, "session", "event", d)
	got, err := s.usageReport(task.Org, task.ID, 0, 0, time.Now().Add(time.Second))
	must(t, err)
	if got.Total.Samples != 1 || got.Total.CacheWrite.String() != "0" || got.Total.CacheRead.String() != "Unknown" {
		t.Fatal("callback replay or missing metric mishandled")
	}
	ev := s.Events(task.ID)
	var saved map[string]any
	must(t, json.Unmarshal([]byte(ev[len(ev)-1].Text), &saved))
	if saved["account"] != task.Account || saved["human"] != task.Creator || saved["agent"] != root.Agent || saved["session"] != "session" || len(saved) != 11 {
		t.Fatalf("metadata not snapshotted or non-allowlisted keys: %v", saved)
	}
}
func TestUsagePaginationKeepsTotals(t *testing.T) {
	s, _, task, root := fixture(t)
	at := time.Now()
	for i := 0; i < 25; i++ {
		usageAt(t, s, task.Org, ID(), root.ID, "usage", `{"model":"fixture","input":1}`, at)
	}
	first, err := s.usageReport(task.Org, "", 0, 0, at)
	must(t, err)
	second, err := s.usageReport(task.Org, "", 0, 1, at)
	must(t, err)
	if first.Total.Input.Value != 25 || second.Total.Input.Value != 25 || first.Next != 1 || len(first.Assignments) != 20 || len(second.Assignments) != 5 || second.Next != 0 {
		t.Fatal("pagination changed totals or lost assignments")
	}
}
func TestUsagePagesRequireOrganizationMembership(t *testing.T) {
	s, e, task, root := fixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Member','member','unused'); INSERT INTO memberships VALUES('owner','org'); INSERT INTO sessions VALUES(?,?,?)`, digest("cookie"), "owner", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	must(t, err)
	usageAt(t, s, task.Org, task.ID, root.ID, "usage", `{"model":"our-model","input":17}`, time.Now())
	usageAt(t, s, "foreign", "other", "other", "usage", `{"model":"private-model","input":999}`, time.Now())
	w := NewWeb(s, e, false)
	for _, path := range []string{"/usage?org=foreign", "/live-usage?org=foreign", "/usage?org=org&task=other"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "adc_session", Value: "cookie"})
		rw := httptest.NewRecorder()
		w.Handler().ServeHTTP(rw, r)
		if rw.Code != 403 && rw.Code != 404 {
			t.Fatalf("foreign usage status %d", rw.Code)
		}
		if strings.Contains(rw.Body.String(), "private-model") {
			t.Fatal("foreign usage leaked")
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/usage?org=org", nil)
	r.AddCookie(&http.Cookie{Name: "adc_session", Value: "cookie"})
	rw := httptest.NewRecorder()
	w.Handler().ServeHTTP(rw, r)
	if rw.Code != 200 || !strings.Contains(rw.Body.String(), "our-model") || strings.Contains(rw.Body.String(), "private-model") {
		t.Fatal("shared org report rendering failed")
	}
}
