package adc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func connectionEditFixture(t *testing.T) (*Store, *Web, Connection, url.Values) {
	t.Helper()
	s, e, _, _ := fixture(t)
	secret, err := s.Seal("synthetic-private-key")
	must(t, err)
	c := Connection{ID: "editable", Org: "org", Name: "Fixture MCP", Transport: "stdio", Command: "/bin/true", Args: []string{"--fixture"}, Env: map[string]string{"API_KEY": secret}, Headers: map[string]string{"Authorization": secret}}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	f := url.Values{"id": {c.ID}, "revision": {connectionRevision(c)}, "name": {c.Name}, "transport": {c.Transport}, "command": {c.Command}, "args": {`["--fixture"]`}}
	return s, NewWeb(s, e, false), c, f
}

func TestConnectionEditPreservesIdentitySecretsAndRejectsStaleOrForeignEdit(t *testing.T) {
	s, w, c, f := connectionEditFixture(t)
	var agent Agent
	must(t, s.Get("dev", &agent))
	agent.Tools = []string{c.ID}
	must(t, s.Put("agent", agent.Org, "", "", agent.ID, agent))
	must(t, w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}))
	var saved Connection
	saved = Connection{}
	must(t, s.Get(c.ID, &saved))
	if connectionRevision(saved) != connectionRevision(c) {
		t.Fatal("no-op edit changed identity or sealed values")
	}
	f.Set("name", "Renamed MCP")
	f.Set("args", `["--new-fixture"]`)
	f.Set("env", `{"NEW_KEY":"replacement","API_KEY":null}`)
	must(t, w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}))
	saved = Connection{}
	must(t, s.Get(c.ID, &saved))
	value, err := s.Unseal(saved.Env["NEW_KEY"])
	must(t, err)
	if saved.Name != "Renamed MCP" || len(list[Connection](s, "connection", c.Org)) != 1 || value != "replacement" || saved.Env["NEW_KEY"] == value || saved.Env["API_KEY"] != "" || saved.Headers["Authorization"] != c.Headers["Authorization"] {
		t.Fatal("connection update or secret patch failed")
	}
	must(t, s.Get(agent.ID, &agent))
	if len(agent.Tools) != 1 || agent.Tools[0] != c.ID {
		t.Fatal("agent assignment lost")
	}
	if err := w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatal("stale form accepted", err)
	}
	f.Set("revision", connectionRevision(saved))
	if w.action(formRequest("/connections", f), Page{Org: Organization{ID: "foreign"}}) == nil {
		t.Fatal("cross-org mutation")
	}
	f.Set("id", "dev")
	if w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}) == nil {
		t.Fatal("non-connection record overwritten")
	}
}

func TestConnectionEditRejectsActiveUseButNotUnrelatedWork(t *testing.T) {
	s, w, c, f := connectionEditFixture(t)
	r := taskRuns(s, "task")[0]
	r.Tools = []string{c.ID}
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	f.Set("command", "/bin/echo")
	if err := w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}); err == nil || !strings.Contains(err.Error(), "worker is using") {
		t.Fatal("active use not protected", err)
	}
	r.State = "waiting"
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	w.Engine.active[r.ID] = func() {}
	if w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}) == nil {
		t.Fatal("stopping activation not protected")
	}
	delete(w.Engine.active, r.ID)
	must(t, w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}))
}

func TestConnectionEditHTTPAndGitHubSecretReplacement(t *testing.T) {
	s, w, c, f := connectionEditFixture(t)
	c.Transport = "http"
	c.Command = ""
	c.Args = nil
	c.URL = "https://fixture.invalid/mcp"
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	f.Set("revision", connectionRevision(c))
	f.Set("transport", "http")
	f.Set("command", "")
	f.Set("url", "https://fixture.invalid/new")
	f.Set("headers", `{"Authorization":"Bearer new-key","X-Old":null}`)
	must(t, w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}))
	var updated Connection
	must(t, s.Get(c.ID, &updated))
	value, err := s.Unseal(updated.Headers["Authorization"])
	must(t, err)
	if value != "Bearer new-key" || updated.URL != "https://fixture.invalid/new" {
		t.Fatal("HTTP update failed")
	}
	for _, bad := range []string{`{"Authorization":123}`, `null`, `{"Authorization":"secret\r\ninjection"}`} {
		f.Set("revision", connectionRevision(updated))
		f.Set("headers", bad)
		if w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}) == nil {
			t.Fatal("invalid headers accepted")
		}
	}
	c.Transport = "github"
	c.URL = ""
	c.Args = []string{"fixture/repo"}
	c.Headers = map[string]string{"GitHubToken": c.Headers["Authorization"]}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	f.Set("revision", connectionRevision(c))
	f.Set("transport", "github")
	f.Set("github_scopes", "fixture/repo\nfixture/second")
	must(t, w.action(formRequest("/connections", f), Page{Org: Organization{ID: c.Org}}))
	must(t, s.Get(c.ID, &updated))
	if len(updated.Args) != 2 || updated.Headers["GitHubToken"] != c.Headers["GitHubToken"] {
		t.Fatal("GitHub token not preserved")
	}
}

func TestConnectionEditWebAuthenticationAndNoSecretEcho(t *testing.T) {
	s, w, c, f := connectionEditFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture','fixture','unused'); INSERT INTO memberships VALUES('owner','org'); INSERT INTO sessions VALUES(?,'owner','2099-01-01T00:00:00Z')`, digest("edit-session"))
	must(t, err)
	get := httptest.NewRequest("GET", "/connection?org=org&id="+c.ID, nil)
	out := httptest.NewRecorder()
	w.Handler().ServeHTTP(out, get)
	if out.Code != http.StatusSeeOther {
		t.Fatal("unauthenticated settings exposed")
	}
	get.AddCookie(&http.Cookie{Name: "adc_session", Value: "edit-session"})
	out = httptest.NewRecorder()
	w.Handler().ServeHTTP(out, get)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Environment changes") || strings.Contains(out.Body.String(), "synthetic-private-key") || strings.Contains(out.Body.String(), c.Env["API_KEY"]) {
		t.Fatal("editor missing or exposed a secret")
	}
	post := formRequest("/connections?org=org", f)
	post.AddCookie(&http.Cookie{Name: "adc_session", Value: "edit-session"})
	out = httptest.NewRecorder()
	w.Handler().ServeHTTP(out, post)
	if out.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	_, csrf := w.user(get)
	f.Set("csrf", csrf)
	f.Set("env", `{"LEAK":"new-secret",`)
	post = formRequest("/connections?org=org", f)
	post.AddCookie(&http.Cookie{Name: "adc_session", Value: "edit-session"})
	out = httptest.NewRecorder()
	w.Handler().ServeHTTP(out, post)
	if !strings.Contains(out.Body.String(), "JSON object") || !strings.Contains(out.Body.String(), "Environment changes") || strings.Contains(out.Body.String(), "new-secret") {
		t.Fatal("error page lost editor or echoed replacement")
	}
}

func TestConnectionEditInvalidatesOldGatewayAdmission(t *testing.T) {
	s, _, c, _ := gatewayFixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture','fixture','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), c.Org, c.ID)
	must(t, err)
	must(t, s.SaveToolPolicy("owner", c.Org, ToolPolicy{Tool: tools[0].ID, Fingerprint: tools[0].Fingerprint, Mode: "allow", Class: "read"}, 0))
	r := taskRuns(s, "task")[0]
	r.Tools = []string{c.ID}
	var task Assignment
	must(t, s.Get(r.Task, &task))
	task.Capabilities = s.initialCapabilities(c.Org, r.Tools)
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	if _, err := s.authorizeGateway(r.ID, tools[0], "", []byte(`{"target":"fixture"}`)); err != nil {
		t.Fatal("fixture not authorized before edit", err)
	}
	c.Name = "Changed configuration"
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	if _, err := s.authorizeGateway(r.ID, tools[0], "", []byte(`{"target":"fixture"}`)); err == nil {
		t.Fatal("old gateway admitted changed connection")
	}
}
