package adc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A steward is a permanent agent that owns a domain. The agent record holds
// the identity, model and connections; the steward record holds what the
// human asked it to look after (the charter), what it remembers (facts and a
// journal), and what it wants a human to know (signals). Naming follows the
// agent: "Storage" is the steward and its memory is just its memory.
type Steward struct {
	ID, Org, Agent, Charter, CompletionMode, Created, Updated, UpdatedBy string
	Repositories                                                         []string
	Revision                                                             int
}

// Facts are small, named and current: pool layout, backup targets, the date
// disk 3 was replaced. Replacing a fact keeps the previous value in history.
type Fact struct {
	ID, Org, Steward, Key, Value, Source, ObservedAt, Updated, UpdatedBy string
	Revision                                                             int
}

// The journal is the steward's append-only record of what happened and what
// it found. It never enters a prompt unasked beyond the most recent entries.
type JournalEntry struct {
	ID, Org, Steward, Task, Run, Text, Source, At string
}

const (
	factLimit        = 64
	factKeyLimit     = 80
	factValueLimit   = 2000
	journalTextLimit = 2000
	charterLimit     = 6000
)

func stewardID(agent string) string { return "steward:" + agent }
func factID(steward, key string) string {
	return "fact:" + digest(steward + "\n" + strings.ToLower(key))[:32]
}

func boundedText(v string, limit int) bool { return strings.TrimSpace(v) != "" && len(v) <= limit }

var ownershipSecretKey = regexp.MustCompile(`(?i)(password|token|secret|credential|api.?key|authorization)`)

// Defense in depth before knowledge becomes shared context. Screens known
// configured secrets and recognizable credentials, not arbitrary encodings.
func (s *Store) checkOwnershipText(org string, values ...string) error {
	r := Redactor{}
	add := func(sealed string) {
		if v, err := s.Unseal(sealed); err == nil && v != "" {
			r.Values = append(r.Values, v)
			if scheme, token, ok := strings.Cut(v, " "); ok && (strings.EqualFold(scheme, "Bearer") || strings.EqualFold(scheme, "Basic")) {
				r.Values = append(r.Values, token)
			}
		}
	}
	for _, c := range list[Connection](s, "connection", org) {
		for key, v := range c.Env {
			if ownershipSecretKey.MatchString(key) {
				add(v)
			}
		}
		for key, v := range c.Headers {
			if ownershipSecretKey.MatchString(key) {
				add(v)
			}
		}
	}
	for _, a := range list[Account](s, "account", "") {
		var n int
		if s.db.QueryRow("SELECT count(*) FROM memberships WHERE org=? AND user_id=?", org, a.User).Scan(&n) == nil && n > 0 {
			add(a.Secret)
		}
	}
	for _, v := range values {
		if r.Text(v) != v {
			return fmt.Errorf("remove credential-like content before saving shared knowledge or evidence")
		}
	}
	return nil
}

func observationTime(value string) bool {
	if value == "" {
		return true
	}
	at, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !at.After(time.Now().Add(5*time.Minute))
}

// Delete removes one record. Used for retracted facts and for records whose
// kind no longer exists after a migration; ordinary history is kept instead.
func (s *Store) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM records WHERE id=?`, id)
	return err
}

func (s *Store) steward(agent string) (Steward, bool) {
	var v Steward
	if agent == "" || s.Get(stewardID(agent), &v) != nil {
		return Steward{}, false
	}
	return v, true
}

func (s *Store) stewards(org string) []Steward {
	out := list[Steward](s, "steward", org)
	sort.Slice(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out
}

func (s *Store) prepareSteward(v Steward, revision int, by string) (Steward, []Write, error) {
	var agent Agent
	if s.Get(v.Agent, &agent) != nil || agent.Org != v.Org {
		return Steward{}, nil, fmt.Errorf("a steward is a permanent agent in this organization")
	}
	var kind string
	if s.db.QueryRow("SELECT kind FROM records WHERE id=?", v.Agent).Scan(&kind) != nil || kind != "agent" {
		return Steward{}, nil, fmt.Errorf("a steward is a permanent agent in this organization")
	}
	if !boundedText(v.Charter, charterLimit) {
		return Steward{}, nil, fmt.Errorf("give the steward a charter of at most %d characters", charterLimit)
	}
	if !validCompletionMode(v.CompletionMode) {
		return Steward{}, nil, fmt.Errorf("choose reviewed or routine completion")
	}
	if len(v.Repositories) > 32 {
		return Steward{}, nil, fmt.Errorf("list at most 32 repositories")
	}
	cleaned := []string{}
	for _, repo := range v.Repositories {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		if len(repo) > 200 || !evidenceReference(repo) {
			return Steward{}, nil, fmt.Errorf("repository identities are short credential-free references")
		}
		cleaned = append(cleaned, repo)
	}
	v.Repositories = cleaned
	if err := s.checkOwnershipText(v.Org, append([]string{v.Charter}, v.Repositories...)...); err != nil {
		return Steward{}, nil, err
	}
	v.ID = stewardID(v.Agent)
	var old Steward
	if s.Get(v.ID, &old) == nil {
		if old.Revision != revision {
			return Steward{}, nil, fmt.Errorf("steward changed; inspect revision %d before editing", old.Revision)
		}
		v.Created = old.Created
	} else if revision != 0 {
		return Steward{}, nil, fmt.Errorf("new steward requires revision 0")
	} else {
		v.Created = now()
	}
	v.Revision = revision + 1
	v.Updated, v.UpdatedBy = now(), by
	return v, []Write{{"steward", v.Org, v.Agent, "", v.ID, v}}, nil
}

// Caller holds Store.mu.
func (s *Store) saveSteward(v Steward, revision int, by string) (Steward, error) {
	v, writes, err := s.prepareSteward(v, revision, by)
	if err != nil {
		return Steward{}, err
	}
	return v, s.Batch(writes...)
}

func (s *Store) facts(steward string) []Fact {
	out := []Fact{}
	for _, f := range list[Fact](s, "fact", "") {
		if f.Steward == steward {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// rememberFact sets, replaces or (with an empty value) retracts a fact.
func (s *Store) rememberFact(v Steward, by, key, value, source, observedAt string) (Fact, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if !boundedText(key, factKeyLimit) {
		return Fact{}, fmt.Errorf("a fact needs a short Key (at most %d characters)", factKeyLimit)
	}
	if len(value) > factValueLimit {
		return Fact{}, fmt.Errorf("keep a fact value within %d characters; put longer material in a document and reference it", factValueLimit)
	}
	if source != "" && !evidenceReference(source) {
		return Fact{}, fmt.Errorf("Source is a short credential-free reference")
	}
	if !observationTime(observedAt) {
		return Fact{}, fmt.Errorf("ObservedAt must be a past RFC3339 time")
	}
	if err := s.checkOwnershipText(v.Org, key, value, source); err != nil {
		return Fact{}, err
	}
	id := factID(v.Agent, key)
	var old Fact
	exists := s.Get(id, &old) == nil
	if value == "" {
		if !exists {
			return Fact{}, fmt.Errorf("no fact named %q to retract", key)
		}
		if err := s.Put("fact-history", old.Org, old.ID, "retracted", ID(), old); err != nil {
			return Fact{}, err
		}
		return old, s.Delete(id)
	}
	if !exists && len(s.facts(v.Agent)) >= factLimit {
		return Fact{}, fmt.Errorf("this steward already holds %d facts; consolidate or retract one before adding another", factLimit)
	}
	f := Fact{ID: id, Org: v.Org, Steward: v.Agent, Key: key, Value: value, Source: source, ObservedAt: observedAt, Updated: now(), UpdatedBy: by, Revision: old.Revision + 1}
	writes := []Write{{"fact", f.Org, f.Steward, "", f.ID, f}}
	if exists {
		writes = append(writes, Write{"fact-history", old.Org, old.ID, "", ID(), old})
	}
	return f, s.Batch(writes...)
}

func (s *Store) journal(steward string, limit int) []JournalEntry {
	out := []JournalEntry{}
	for _, j := range list[JournalEntry](s, "journal", "") {
		if j.Steward == steward {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) journalEntry(v Steward, task, run, text, source string) (JournalEntry, error) {
	text = strings.TrimSpace(text)
	if !boundedText(text, journalTextLimit) {
		return JournalEntry{}, fmt.Errorf("a journal entry is at most %d characters", journalTextLimit)
	}
	if source != "" && !evidenceReference(source) {
		return JournalEntry{}, fmt.Errorf("Source is a short credential-free reference")
	}
	if err := s.checkOwnershipText(v.Org, text, source); err != nil {
		return JournalEntry{}, err
	}
	j := JournalEntry{ID: ID(), Org: v.Org, Steward: v.Agent, Task: task, Run: run, Text: text, Source: source, At: now()}
	return j, s.Put("journal", j.Org, j.Steward, "", j.ID, j)
}

func (s *Store) recall(steward, query string, limit int) map[string]any {
	query = strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	facts := []Fact{}
	for _, f := range s.facts(steward) {
		if query == "" || strings.Contains(strings.ToLower(f.Key), query) || strings.Contains(strings.ToLower(f.Value), query) {
			facts = append(facts, f)
		}
	}
	entries := []JournalEntry{}
	for _, j := range s.journal(steward, 0) {
		if query == "" || strings.Contains(strings.ToLower(j.Text), query) {
			entries = append(entries, j)
			if len(entries) == limit {
				break
			}
		}
	}
	return map[string]any{"query": query, "facts": facts, "journal": entries}
}

// stewardContext is what travels with every activation of a steward: its
// charter, facts, the last few journal entries and open signals. Older
// history is one adc_recall away.
func (e *Engine) stewardContext(agent string, full bool) map[string]any {
	s := e.Store
	v, ok := s.steward(agent)
	if !ok {
		return nil
	}
	facts := []map[string]any{}
	for _, f := range s.facts(agent) {
		facts = append(facts, map[string]any{"key": f.Key, "value": clipped(f.Value, 300), "source": f.Source, "observed_at": f.ObservedAt, "updated": f.Updated})
	}
	var a Agent
	_ = s.Get(agent, &a)
	connections := []string{}
	for _, id := range a.Tools {
		var c Connection
		if s.Get(id, &c) == nil {
			connections = append(connections, c.Name)
		}
	}
	ctx := map[string]any{"agent": agent, "name": a.Name, "charter": v.Charter, "repositories": v.Repositories, "connections": connections, "facts": facts, "revision": v.Revision}
	if !full {
		return ctx
	}
	journal := []map[string]any{}
	for _, j := range s.journal(agent, 8) {
		journal = append(journal, map[string]any{"at": j.At, "text": clipped(j.Text, 300), "source": j.Source, "task": j.Task})
	}
	signals := []map[string]any{}
	for _, sig := range s.stewardSignals(agent) {
		if sig.State == "resolved" {
			continue
		}
		signals = append(signals, map[string]any{"key": sig.Key, "severity": sig.Severity, "state": sig.State, "message": sig.Message, "count": sig.Count})
	}
	ctx["recent_journal"] = journal
	ctx["signals"] = signals
	return ctx
}

const stewardInstructions = `

Steward: you own this domain. Your charter, facts, recent journal and open signals travel with you. Before finishing any run, record durable facts with adc_remember (short Key, current Value, Source) and notable events or findings with adc_journal; use adc_recall for older history. Raise adc_signal when a human should know something (capacity, failures, an upcoming decision) and clear it once resolved. Routine findings belong in the journal, not in documents, unless the finding itself needs one.`

// legacyArea reads the retired area record shape for migration only.
type legacyArea struct {
	CompletionMode                                                    string
	PublicIntent, PublicSource, UnderstandingKind, ObservedAt         string
	ID, Org, Owner, Name, Intent, Summary, Source, Updated, UpdatedBy string
	Revision                                                          int
}

// migrateAreasToStewards turns retired area records into stewards keyed by
// their owning agent, moves routed assignments and proposals over, and drops
// the area records. It runs once: with no area records left it does nothing.
func (e *Engine) migrateAreasToStewards() {
	s := e.Store
	records, err := s.Records("area", "")
	if err != nil || len(records) == 0 {
		return
	}
	owner := map[string]string{}
	for _, record := range records {
		var a legacyArea
		if err := json.Unmarshal(record.Data, &a); err != nil {
			continue
		}
		owner[a.ID] = a.Owner
		if _, exists := s.steward(a.Owner); exists {
			continue
		}
		v := Steward{Org: a.Org, Agent: a.Owner, Charter: strings.TrimSpace(a.Intent), CompletionMode: a.CompletionMode}
		if v.Charter == "" {
			v.Charter = a.Name
		}
		v, err := s.saveSteward(v, 0, "migration")
		if err != nil {
			s.Log(a.Org, "", "", "migration", "Could not migrate area "+a.Name+": "+err.Error())
			continue
		}
		if strings.TrimSpace(a.Summary) != "" {
			_, _ = s.rememberFact(v, "migration", "understanding", clipped(a.Summary, factValueLimit), a.Source, a.ObservedAt)
		}
		_, _ = s.journalEntry(v, "", "", "Migrated from area “"+a.Name+"”; earlier notes and revisions were retired.", "")
	}
	for _, t := range list[Assignment](s, "assignment", "") {
		if t.Area != "" {
			if agent := owner[t.Area]; agent != "" {
				t.Steward = agent
			}
			t.Area = ""
			_ = s.Put("assignment", t.Org, "", t.State, t.ID, t)
		}
	}
	for _, p := range list[WorkProposal](s, "proposal", "") {
		if p.Area != "" {
			if agent := owner[p.Area]; agent != "" {
				p.Steward = agent
			}
			p.Area = ""
			_ = s.Put("proposal", p.Org, "", p.State, p.ID, p)
		}
	}
	for _, kind := range []string{"area", "area-history", "area-note", "area-note-history", "public-intent-observation"} {
		_, _ = s.db.Exec(`DELETE FROM records WHERE kind=?`, kind)
	}
}
