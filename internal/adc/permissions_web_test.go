package adc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPermissionEndpointsRequireHumanAndCancellationClearsQueue(t *testing.T) {
	s, _, conn, calls := gatewayFixture(t)
	for _, id := range []string{"owner", "outsider"} {
		_, err := s.db.Exec(`INSERT INTO users VALUES(?,?,?,?)`, id, id, id, "unused")
		must(t, err)
		_, err = s.db.Exec(`INSERT INTO sessions VALUES(?,?,?)`, digest(id+"-token"), id, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
		must(t, err)
	}
	_, err := s.db.Exec(`INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", conn.ID)
	must(t, err)
	run := taskRuns(s, "task")[0]
	run.Execution = "protected"
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Execution = "protected"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	request, err := s.RequestAccess(run.ID, "Read fixture NAS", []AccessWant{{Tool: tools[0].ID, Operation: "fixture-read", Arguments: json.RawMessage(`{"target":"fixture-nas"}`)}})
	must(t, err)
	w := NewWeb(s, NewEngine(s), false)
	for _, path := range []string{"/permission-default", "/permission-policy", "/permission-bulk", "/permission-discover", "/permission-edit", "/permission-resolve"} {
		for _, identity := range []string{"", "outsider", "owner"} {
			req := httptest.NewRequest("POST", path+"?org=org", strings.NewReader("mode=protected"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if identity != "" {
				req.AddCookie(&http.Cookie{Name: "adc_session", Value: identity + "-token"})
			}
			out := httptest.NewRecorder()
			w.Handler().ServeHTTP(out, req)
			if identity == "" {
				if out.Code != 303 {
					t.Fatalf("unauthenticated %s status %d", path, out.Code)
				}
			} else if out.Code != 403 {
				t.Fatalf("unauthorized %s status %d", path, out.Code)
			}
		}
	}
	form := url.Values{"task": {task.ID}, "action": {"cancel"}, "csrf": {digest("csrf:owner-token")}}
	req := httptest.NewRequest("POST", "/task-action?org=org", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "adc_session", Value: "owner-token"})
	out := httptest.NewRecorder()
	w.Handler().ServeHTTP(out, req)
	if out.Code != 303 {
		t.Fatal("human cancellation failed", out.Code)
	}
	must(t, s.Get(request.ID, &request))
	var decision Decision
	must(t, s.Get(request.Decision, &decision))
	if request.State != "cancelled" || decision.State != "cancelled" || len(w.permissionPage("org", "").Requests) != 0 {
		t.Fatal("cancelled assignment left an approval in queue")
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized operation occurred")
	}
}
