package adc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunEvidenceScopedBeforePagination(t *testing.T) {
	s, e, task, run := fixture(t)
	for i := 0; i < 260; i++ {
		s.Log(task.Org, task.ID, run.ID, "message", fmt.Sprintf("selected %03d", i))
	}
	for i := 0; i < 300; i++ {
		s.Log(task.Org, task.ID, "sibling", "message", "sibling")
		s.Log("foreign", task.ID, run.ID, "message", "foreign")
	}
	s.Log(task.Org, task.ID, run.ID, "usage", "invisible")
	s.Log(task.Org, task.ID, run.ID, "message", " ")
	events, older := s.runEvents(task.Org, task.ID, run.ID, 0)
	if len(events) != 250 || events[0].Text != "selected 010" || older != events[0].ID {
		t.Fatalf("bad latest page: %d, %d", len(events), older)
	}
	history, next := s.runEvents(task.Org, task.ID, run.ID, older)
	if next != 0 || len(history) < 10 || history[len(history)-1].Text != "selected 009" {
		t.Fatal("bad history cursor")
	}
	trace := ToolTrace{ID: "selected-trace", Org: task.Org, Task: task.ID, Run: run.ID, Name: "selected-tool"}
	must(t, s.Put("tooltrace", trace.Org, trace.Task, "", trace.ID, trace))
	for i := 0; i < 110; i++ {
		trace.ID = fmt.Sprintf("sibling-%d", i)
		trace.Run = "sibling"
		must(t, s.Put("tooltrace", trace.Org, trace.Task, "", trace.ID, trace))
	}
	p := Page{Org: Organization{ID: task.Org}}
	w := NewWeb(s, e, false)
	if !w.populateRun(&p, run.ID) {
		t.Fatal("run missing")
	}
	w.populateTranscript(&p)
	if len(p.Traces) != 1 || p.Traces[0].ID != "selected-trace" {
		t.Fatal("sibling traces crowded out selected run")
	}
}

func transcriptLogin(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture','fixture','unused'); INSERT INTO memberships VALUES('owner','org'); INSERT INTO sessions VALUES(?, 'owner','2099-01-01T00:00:00Z')`, digest("transcript-fixture"))
	must(t, err)
	// This fixture does not connect to any real provider to list models.
	var a Account
	must(t, s.Get("account", &a))
	a.User = "disconnected"
	must(t, s.Put("account", "", a.User, "", a.ID, a))
}
func TestRunPagesEnforceOrganizationAndEscapeEvidence(t *testing.T) {
	s, e, task, run := fixture(t)
	transcriptLogin(t, s)
	s.Log(task.Org, task.ID, run.ID, "message", "<script>selected</script>")
	other := run
	other.ID = "foreign-run"
	other.Org = "other"
	must(t, s.Put("run", other.Org, other.Task, other.State, other.ID, other))
	w := NewWeb(s, e, false)
	for _, test := range []struct {
		url  string
		code int
	}{
		{"/run?org=org&id=" + run.ID, 200}, {"/run?org=org&id=foreign-run", 404}, {"/live-run?org=org&id=foreign-run", 404}, {"/run?org=other&id=foreign-run", 403}, {"/live-team?org=other", 403},
	} {
		r := httptest.NewRequest("GET", test.url, nil)
		r.AddCookie(&http.Cookie{Name: "adc_session", Value: "transcript-fixture"})
		rw := httptest.NewRecorder()
		w.Handler().ServeHTTP(rw, r)
		if rw.Code != test.code {
			t.Fatalf("%s: %d %s", test.url, rw.Code, rw.Body.String())
		}
		if test.code == 200 && (!strings.Contains(rw.Body.String(), "&lt;script&gt;selected&lt;/script&gt;") || strings.Contains(rw.Body.String(), "<script>selected")) {
			t.Fatal("unsafe or missing transcript")
		}
	}
	var after Run
	must(t, s.Get(run.ID, &after))
	if after.State != run.State || after.Activations != run.Activations {
		t.Fatal("view changed run")
	}
}
func TestAgentActiveRunsIncludesConcurrentInstances(t *testing.T) {
	s, e, task, run := fixture(t)
	for i, state := range []string{"running", "waiting", "queued", "blocked", "complete", "cancelled"} {
		r := run
		r.ID = fmt.Sprintf("instance-%d", i)
		r.State = state
		must(t, s.Put("run", r.Org, r.Task, state, r.ID, r))
	}
	p := Page{Org: Organization{ID: task.Org}}
	w := NewWeb(s, e, false)
	w.populateActiveRuns(&p)
	if len(p.ActiveRuns[run.Agent]) != 5 || p.ActiveRuns[run.Agent][0].State != "running" {
		t.Fatal("incorrect active instances", p.ActiveRuns)
	}
	task.State = "paused"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	w.populateActiveRuns(&p)
	if len(p.ActiveRuns) != 0 {
		t.Fatal("paused assignment shown active")
	}
}

func TestRunStreamsStopAfterMembershipRevocation(t *testing.T) {
	for _, path := range []string{"/live-run?org=org&id=", "/live-team?org=org&unused="} {
		t.Run(path, func(t *testing.T) {
			s, e, _, run := fixture(t)
			transcriptLogin(t, s)
			server := httptest.NewServer(NewWeb(s, e, false).Handler())
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", server.URL+path+run.ID, nil)
			must(t, err)
			req.AddCookie(&http.Cookie{Name: "adc_session", Value: "transcript-fixture"})
			res, err := http.DefaultClient.Do(req)
			must(t, err)
			defer res.Body.Close()
			if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
				t.Fatal("not an SSE response")
			}
			reader := bufio.NewReader(res.Body)
			_, err = reader.ReadString('\n')
			must(t, err)
			_, err = s.db.Exec(`DELETE FROM memberships WHERE user_id='owner'`)
			must(t, err)
			_, err = io.ReadAll(reader)
			must(t, err)
			if ctx.Err() != nil {
				t.Fatal("stream survived revoked membership")
			}
		})
	}
}
