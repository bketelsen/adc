package adc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// Only allowlisted usage metadata is persisted; never serialize a whole provider event.
type usageSample struct {
	Model      string `json:"model"`
	Input      *int64 `json:"input"`
	Output     *int64 `json:"output"`
	CacheRead  *int64 `json:"cache_read"`
	CacheWrite *int64 `json:"cache_write"`
	Account    string `json:"account,omitempty"`
	Human      string `json:"human,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Session    string `json:"session,omitempty"`
	EventID    string `json:"event_id,omitempty"`
}

func (s *Store) logUsage(t Assignment, r Run, session, eventID string, d *copilot.AssistantUsageData) {
	account := t.Account
	if r.Account != "" {
		account = r.Account
	}
	b, err := json.Marshal(usageSample{Model: d.Model, Input: d.InputTokens, Output: d.OutputTokens, CacheRead: d.CacheReadTokens, CacheWrite: d.CacheWriteTokens, Account: account, Human: t.Creator, Agent: r.Agent, Provider: "copilot", Session: session, EventID: eventID})
	if err == nil {
		s.Log(r.Org, r.Task, r.ID, "usage", string(b))
	}
}

type TokenCount struct {
	Value            int64
	Reports, Missing int
}

func (c *TokenCount) add(v *int64) {
	if v == nil || *v < 0 || *v > 9223372036854775807-c.Value {
		c.Missing++
		return
	}
	c.Value += *v
	c.Reports++
}
func tokenNumber(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func (c TokenCount) String() string {
	if c.Reports == 0 {
		return "Unknown"
	}
	s := tokenNumber(c.Value)
	if c.Missing > 0 {
		s += " · partial"
	}
	return s
}

type UsageTotals struct {
	Input, Output, CacheRead, CacheWrite TokenCount
	Samples, Runs, ReportedRuns          int
}

func (t *UsageTotals) add(s usageSample) {
	t.Samples++
	t.Input.add(s.Input)
	t.Output.add(s.Output)
	t.CacheRead.add(s.CacheRead)
	t.CacheWrite.add(s.CacheWrite)
}
func (t *UsageTotals) missingRun() {
	t.Input.Missing++
	t.Output.Missing++
	t.CacheRead.Missing++
	t.CacheWrite.Missing++
}

type UsageRow struct {
	ID, Name, Detail, URL string
	UsageTotals
}
type UsageReport struct {
	Total                               UsageTotals
	Models, Accounts, Assignments, Runs []UsageRow
	Days, Page, Next, Malformed         int
	Task, TaskTitle, Updated            string
}
type usageEvent struct {
	ID                        int64
	Task, Run, Kind, Text, At string
}

// Read the durable event history, not the paginated activity window. Deduplicate
// new SDK callbacks by session/event ID before filtering their receipt date.
func (s *Store) usageReport(org, task string, days, page int, at time.Time) (UsageReport, error) {
	out := UsageReport{Days: days, Page: page, Task: task, Updated: at.UTC().Format("Jan 2, 15:04:05 UTC")}
	tasks := map[string]Assignment{}
	runs := map[string]Run{}
	accounts := map[string]Account{}
	agents := map[string]Agent{}
	humans := map[string]string{}
	for _, t := range list[Assignment](s, "assignment", org) {
		tasks[t.ID] = t
	}
	if task != "" {
		t, ok := tasks[task]
		if !ok {
			return out, fmt.Errorf("assignment unavailable")
		}
		out.TaskTitle = t.Title
	}
	for _, r := range list[Run](s, "run", org) {
		runs[r.ID] = r
	}
	for _, a := range list[Account](s, "account", "") {
		accounts[a.ID] = a
	}
	for _, a := range append(list[Agent](s, "agent", org), list[Agent](s, "guide", org)...) {
		agents[a.ID] = a
	}
	rows, err := s.db.Query(`SELECT id,task,run,kind,text,at FROM events WHERE org=? AND kind IN ('usage','started') ORDER BY id`, org)
	if err != nil {
		return out, err
	}
	var events []usageEvent
	for rows.Next() {
		var ev usageEvent
		if err = rows.Scan(&ev.ID, &ev.Task, &ev.Run, &ev.Kind, &ev.Text, &ev.At); err != nil {
			break
		}
		events = append(events, ev)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	// No credentials or account objects are returned in the report.
	for _, a := range accounts {
		if _, ok := humans[a.User]; !ok {
			var name string
			_ = s.db.QueryRow(`SELECT name FROM users WHERE id=?`, a.User).Scan(&name)
			humans[a.User] = name
		}
	}
	groups := []map[string]*UsageRow{{}, {}, {}, {}}
	observed := map[string]bool{}
	reported := map[string]bool{}
	seen := map[string]bool{}
	groupRuns := []map[string]map[string]bool{{}, {}, {}, {}}
	groupReported := []map[string]map[string]bool{{}, {}, {}, {}}
	add := func(ev usageEvent, u usageSample, hasSample bool) {
		t := tasks[ev.Task]
		r := runs[ev.Run]
		accountID := u.Account
		if accountID == "" {
			accountID = t.Account
		}
		humanID := u.Human
		if humanID == "" {
			humanID = t.Creator
		}
		accountName := "Unknown subscription"
		if a, ok := accounts[accountID]; ok {
			accountName = a.Name
		}
		if accountID != "" && accountName == "Unknown subscription" {
			accountName = "Unavailable subscription"
		}
		model := strings.TrimSpace(u.Model)
		if model == "" {
			model = "Unreported model"
		}
		agentID := u.Agent
		if agentID == "" {
			agentID = r.Agent
		}
		agentName := agents[agentID].Name
		if agentName == "" {
			agentName = "Temporary worker"
		}
		title := t.Title
		if title == "" {
			title = "Unavailable assignment"
		}
		url := "/task?org=" + org + "&id=" + ev.Task
		entries := []UsageRow{
			{ID: model, Name: model},
			{ID: accountID, Name: accountName, Detail: humans[humanID]},
			{ID: ev.Task, Name: title, Detail: t.State, URL: "/usage?org=" + org + "&task=" + ev.Task + "&days=" + strconv.Itoa(days)},
			{ID: ev.Run, Name: agentName, Detail: r.Title + " · requested " + r.Model + " · " + r.State, URL: url},
		}
		for i, entry := range entries {
			// A start event cannot tell us which model actually served a request.
			if !hasSample && i == 0 {
				continue
			}
			row := groups[i][entry.ID]
			if row == nil {
				copy := entry
				row = &copy
				groups[i][entry.ID] = row
				groupRuns[i][entry.ID] = map[string]bool{}
				groupReported[i][entry.ID] = map[string]bool{}
			}
			groupRuns[i][entry.ID][ev.Run] = true
			if hasSample {
				row.add(u)
				groupReported[i][entry.ID][ev.Run] = true
			}
		}
		observed[ev.Run] = true
		if hasSample {
			out.Total.add(u)
			reported[ev.Run] = true
		}
	}
	for _, ev := range events {
		if task != "" && ev.Task != task {
			continue
		}
		var sample usageSample
		malformed := false
		if ev.Kind == "usage" {
			if err := json.Unmarshal([]byte(ev.Text), &sample); err != nil || strings.TrimSpace(ev.Text) == "null" {
				malformed = true
				sample = usageSample{}
			}
			if sample.EventID != "" && sample.Session != "" {
				key := sample.Provider + "\x00" + sample.Session + "\x00" + sample.EventID
				if seen[key] {
					continue
				}
				seen[key] = true
			}
		}
		timestamp, err := time.Parse(time.RFC3339Nano, ev.At)
		if err != nil {
			continue
		}
		if timestamp.After(at) || (days > 0 && timestamp.Before(at.Add(-time.Duration(days)*24*time.Hour))) {
			continue
		}
		if malformed {
			out.Malformed++
		}
		add(ev, sample, ev.Kind == "usage")
	}
	out.Total.Runs = len(observed)
	out.Total.ReportedRuns = len(reported)
	for id := range observed {
		if !reported[id] {
			out.Total.missingRun()
		}
	}
	lists := []*[]UsageRow{&out.Models, &out.Accounts, &out.Assignments, &out.Runs}
	for i, g := range groups {
		for key, row := range g {
			row.Runs = len(groupRuns[i][key])
			row.ReportedRuns = len(groupReported[i][key])
			for id := range groupRuns[i][key] {
				if !groupReported[i][key][id] {
					row.missingRun()
				}
			}
			*lists[i] = append(*lists[i], *row)
		}
		sort.Slice(*lists[i], func(a, b int) bool {
			x, y := (*lists[i])[a], (*lists[i])[b]
			if x.Input.Value != y.Input.Value {
				return x.Input.Value > y.Input.Value
			}
			if x.Output.Value != y.Output.Value {
				return x.Output.Value > y.Output.Value
			}
			return x.ID < y.ID
		})
	}
	// Bound the main assignment table while totals retain the entire selection.
	start := page * 20
	if start > len(out.Assignments) {
		start = len(out.Assignments)
	}
	end := start + 20
	if end < len(out.Assignments) {
		out.Next = page + 1
	} else {
		end = len(out.Assignments)
	}
	out.Assignments = out.Assignments[start:end]
	return out, nil
}
