package adc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	githubFixture *githubFixtureConfig // Test-only transport endpoints; never persisted or model-configurable.
	db            *sql.DB
	mu            sync.Mutex
	key           []byte
	Dir           string
}
type Record struct {
	ID, Kind, Org, Parent, State string
	Data                         json.RawMessage
	Updated                      string
}
type Event struct {
	ID                             int64
	Org, Task, Run, Kind, Text, At string
}

type Write struct {
	Kind, Org, Parent, State, ID string
	Value                        any
}

func durableRecord(v any) any {
	switch p := v.(type) {
	case ExecutionPlan:
		return durablePlan(p)
	case *ExecutionPlan:
		if p != nil {
			return durablePlan(*p)
		}
	}
	return v
}

func (s *Store) Batch(writes ...Write) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, w := range writes {
		b, err := json.Marshal(durableRecord(w.Value))
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO records VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,org=excluded.org,parent=excluded.parent,state=excluded.state,data=excluded.data,updated=excluded.updated`, w.ID, w.Kind, w.Org, w.Parent, w.State, string(b), now())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func Open(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(abs, "credential.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		_, err = f.Write(key)
		f.Close()
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid credential key")
	}
	db, err := sql.Open("sqlite", filepath.Join(abs, "adc.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS records(id TEXT PRIMARY KEY,kind TEXT NOT NULL,org TEXT NOT NULL DEFAULT '',parent TEXT NOT NULL DEFAULT '',state TEXT NOT NULL DEFAULT '',data TEXT NOT NULL,updated TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS records_kind_org ON records(kind,org);
 CREATE INDEX IF NOT EXISTS records_parent ON records(parent);
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,org TEXT NOT NULL,task TEXT NOT NULL,run TEXT NOT NULL,kind TEXT NOT NULL,text TEXT NOT NULL,at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS events_task_id ON events(task,id);
 CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY,name TEXT NOT NULL,username TEXT UNIQUE NOT NULL,password BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY,user_id TEXT NOT NULL REFERENCES users(id),expires TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS memberships(user_id TEXT NOT NULL REFERENCES users(id),org TEXT NOT NULL,PRIMARY KEY(user_id,org));`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, key: key, Dir: abs}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Put(kind, org, parent, state, id string, v any) error {
	b, err := json.Marshal(durableRecord(v))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO records VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,org=excluded.org,parent=excluded.parent,state=excluded.state,data=excluded.data,updated=excluded.updated`, id, kind, org, parent, state, string(b), now())
	return err
}
func (s *Store) Get(id string, v any) error {
	var kind, b string
	if err := s.db.QueryRow(`SELECT kind,data FROM records WHERE id=?`, id).Scan(&kind, &b); err != nil {
		return err
	}
	allowed := false
	switch v.(type) {
	case *Steward:
		allowed = kind == "steward"
	case *Fact:
		allowed = kind == "fact" || kind == "fact-history"
	case *JournalEntry:
		allowed = kind == "journal"
	case *Signal:
		allowed = kind == "signal"
	case *OwnerRequest:
		allowed = kind == "owner-request" || kind == "owner-request-history"
	case *CompletionEvidence:
		allowed = kind == "completion-evidence" || kind == "completion-evidence-history"
	case *GatewayTool:
		allowed = kind == "gateway-tool"
	case *ToolPolicy:
		allowed = kind == "tool-policy" || kind == "tool-policy-history"
	case *AccessRequest:
		allowed = kind == "access-request" || kind == "access-request-history"
	case *GatewayOperation:
		allowed = kind == "gateway-operation" || kind == "gateway-operation-history"
	case *ExecutionReadiness:
		allowed = kind == "preflight"
	case *RunResources:
		allowed = kind == "run-resources" || kind == "run-resource-history"
	case *GitHubDelivery:
		allowed = kind == "github-delivery"
	case *DurableWait:
		allowed = kind == "durable-wait" || kind == "wait-history"
	case *MilestoneEvidence:
		allowed = kind == "milestone-evidence" || kind == "milestone-evidence-history"
	case *IntegrationEvidence:
		allowed = kind == "integration-evidence" || kind == "integration-evidence-history"
	case *ExecutionPlan:
		allowed = kind == "execution-plan" || kind == "execution-plan-history"
	case *Organization:
		allowed = kind == "organization"
	case *Agent:
		allowed = kind == "agent" || kind == "guide"
	case *Account:
		allowed = kind == "account"
	case *Assignment:
		allowed = kind == "assignment"
	case *Run:
		allowed = kind == "run"
	case *Document:
		allowed = kind == "document" || kind == "revision"
	case *Review:
		allowed = kind == "review"
	case *Decision:
		allowed = kind == "decision"
	case *StandingSchedule:
		allowed = kind == "schedule"
	case *WorkProposal:
		allowed = kind == "proposal"
	case *ProposalNote:
		allowed = kind == "proposal-note"
	case *Connection:
		allowed = kind == "connection"
	case *ToolTrace:
		allowed = kind == "tooltrace"
	case *CategoryDefault:
		allowed = kind == "default"
	}
	if !allowed {
		return fmt.Errorf("record type does not match request")
	}
	return json.Unmarshal([]byte(b), v)
}
func (s *Store) Records(kind, org string) ([]Record, error) {
	rows, err := s.db.Query(`SELECT id,kind,org,parent,state,data,updated FROM records WHERE kind=? AND (?='' OR org=?) ORDER BY updated DESC`, kind, org, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		var b string
		if err = rows.Scan(&r.ID, &r.Kind, &r.Org, &r.Parent, &r.State, &b, &r.Updated); err != nil {
			return nil, err
		}
		r.Data = []byte(b)
		out = append(out, r)
	}
	return out, rows.Err()
}
func list[T any](s *Store, kind, org string) []T {
	rs, err := s.Records(kind, org)
	if err != nil {
		return nil
	}
	out := make([]T, 0, len(rs))
	for _, r := range rs {
		var v T
		if json.Unmarshal(r.Data, &v) == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *Store) Log(org, task, run, kind, text string) {
	_, _ = s.db.Exec(`INSERT INTO events(org,task,run,kind,text,at) VALUES(?,?,?,?,?,?)`, org, task, run, kind, text, now())
}
func (s *Store) Events(task string) []Event { return s.EventsBefore(task, 0) }
func (s *Store) EventsBefore(task string, before int64) []Event {
	rows, err := s.db.Query(`SELECT id,org,task,run,kind,text,at FROM (SELECT * FROM events WHERE task=? AND (?=0 OR id<?) ORDER BY id DESC LIMIT 250) ORDER BY id`, task, before, before)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if rows.Scan(&e.ID, &e.Org, &e.Task, &e.Run, &e.Kind, &e.Text, &e.At) == nil {
			out = append(out, e)
		}
	}
	return out
}
func (s *Store) OlderEvents(task string, events []Event) int64 {
	if len(events) == 0 {
		return 0
	}
	var count int
	if s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE task=? AND id<?)`, task, events[0].ID).Scan(&count) == nil && count > 0 {
		return events[0].ID
	}
	return 0
}

func (s *Store) Seal(text string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(text), nil)), nil
}
func (s *Store) Unseal(text string) (string, error) {
	if text == "" {
		return "", nil
	}
	b, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(b) < g.NonceSize() {
		return "", fmt.Errorf("invalid credential")
	}
	p, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], nil)
	return string(p), err
}
