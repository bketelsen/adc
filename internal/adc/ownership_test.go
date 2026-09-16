package adc

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func ownershipFixture(t *testing.T) (*Store, *Engine, Assignment, Run, Area) {
	t.Helper()
	s, e, task, r := fixture(t)
	_, err := s.db.Exec("INSERT INTO users VALUES('owner','Owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')")
	must(t, err)
	c := Connection{ID: "storage", Org: task.Org, Name: "Fixture observation", Transport: "stdio", Command: "fixture"}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	a, err := s.saveArea(Area{Org: task.Org, Owner: r.Agent, Name: "Shared responsibilities", Intent: "Verify agreed outcomes. Do not invent new work."}, 0, "human:owner")
	must(t, err)
	return s, e, task, r, a
}

func completeSource(t *testing.T, s *Store, task Assignment, r Run) {
	t.Helper()
	task.State = "ready"
	r.State = "complete"
	must(t, s.Batch(Write{"assignment", task.Org, "", task.State, task.ID, task}, Write{"run", r.Org, r.Task, r.State, r.ID, r}))
}

func TestOwnerAreaConcurrencyAndHumanIntentBoundary(t *testing.T) {
	s, e, _, r, a := ownershipFixture(t)
	p := ownerNoteInput{Area: a.ID, Revision: a.Revision, Summary: "Agent observation, uncertain until checked", Source: "fixture://source"}
	_, err := call(t, e, r, "adc_remember", p)
	must(t, err)
	result, err := call(t, e, r, "adc_remember", p)
	must(t, err)
	if !strings.Contains(result, "conflict") {
		t.Fatal("stale update not retained for reconciliation")
	}
	must(t, s.Get(a.ID, &a))
	if a.Intent != "Verify agreed outcomes. Do not invent new work." || !strings.HasPrefix(a.UpdatedBy, "run:") {
		t.Fatal("agent changed human intent")
	}
	if len(list[Area](s, "area-history", a.Org)) != 1 {
		t.Fatal("knowledge history lost")
	}
	other := r
	other.ID = "other-run"
	other.Agent = "dev"
	must(t, s.Put("run", other.Org, other.Task, other.State, other.ID, other))
	p.Revision = a.Revision
	if _, err = call(t, e, other, "adc_remember", p); err == nil {
		t.Fatal("another owner overwrote knowledge")
	}
	other.Org = "other"
	must(t, s.Put("run", other.Org, other.Task, other.State, other.ID, other))
	if _, err = call(t, e, other, "adc_owner", map[string]string{"Area": a.ID}); err == nil {
		t.Fatal("cross-organization read accepted")
	}
}

func TestOwnerAreaPage(t *testing.T) {
	s, e, _, _, a := ownershipFixture(t)
	w := NewWeb(s, e, false)
	p := Page{View: "areas", Title: "Areas", Org: Organization{ID: a.Org}, Agents: list[Agent](s, "agent", a.Org), Areas: []Area{a}}
	rw := httptest.NewRecorder()
	w.render(rw, p)
	if !strings.Contains(rw.Body.String(), "Human-maintained intent") || !strings.Contains(rw.Body.String(), a.Name) {
		t.Fatal("area view lacks intent")
	}
}

func TestOwnerKnowledgeRejectsRecognizableAndConfiguredSecrets(t *testing.T) {
	s, e, task, r, a := ownershipFixture(t)
	sealed, err := s.Seal("fixture-known-private-value")
	must(t, err)
	c := Connection{ID: "private", Org: task.Org, Env: map[string]string{"API_KEY": sealed}}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	for _, secret := range []string{"ghp_123456789012345678901234567890", "fixture-known-private-value", "password=fixture-password"} {
		_, err := call(t, e, r, "adc_remember", ownerNoteInput{Area: a.ID, Revision: a.Revision, Summary: "Observed " + secret, Source: "fixture://source"})
		if err == nil {
			t.Fatal("secret retained in knowledge")
		}
	}
	must(t, s.Get(a.ID, &a))
	if a.Summary != "" {
		t.Fatal("failed write changed knowledge")
	}
}
