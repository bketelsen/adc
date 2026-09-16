package adc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/starfederation/datastar-go/datastar"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"golang.org/x/crypto/bcrypt"
)

//go:embed templates/* static/*
var assets embed.FS

type User struct{ ID, Name, Username string }
type Page struct {
	Assessments                                                 []AssessmentEntry
	AttentionBriefs                                             []BriefEntry
	OwnershipBriefing                                           OwnershipBriefing
	Coordination                                                OwnerRequestPage
	AreaKnowledge                                               []AreaKnowledge
	Areas                                                       []Area
	Obligations                                                 []Obligation
	Connection                                                  ConnectionPage
	Selfhosted                                                  SelfhostedAccountPage
	Plan                                                        ExecutionPlan
	Run                                                         Run
	ActiveRuns                                                  map[string][]Run
	ActivityURL                                                 string
	Permissions                                                 PermissionPage
	Claude                                                      ClaudeAccountPage
	Codex                                                       CodexAccountPage
	Usage                                                       UsageReport
	Schedules                                                   []StandingSchedule
	ProposalSupervisor                                          string
	Proposals                                                   []WorkProposal
	Proposal                                                    WorkProposal
	ProposalNotes                                               []ProposalNote
	ProposalHistory                                             []WorkProposal
	RelatedWork                                                 []RelatedWork
	ProposalCount, ProposalRevision, ProposalPage, ProposalNext int
	ProposalArchive                                             bool
	Title, View, CSRF, Error                                    string
	User                                                        User
	Members                                                     []User
	Org                                                         Organization
	Orgs                                                        []Organization
	Agents                                                      []Agent
	Tree                                                        []RoleTree
	Defaults                                                    map[string]string
	Accounts                                                    []Account
	Connections                                                 []Connection
	Tasks                                                       []Assignment
	Task                                                        Assignment
	Runs                                                        []Run
	Events                                                      []Event
	Documents                                                   []Document
	History                                                     []Document
	Decisions                                                   []Decision
	Reviews                                                     []Review
	Traces                                                      []ToolTrace
	Models                                                      []Model
	Selected                                                    Document
	HTML                                                        template.HTML
	Before, Older                                               int64
	Count                                                       int
	Setup                                                       bool
}
type Web struct {
	Store     *Store
	Engine    *Engine
	templates *template.Template
	Secure    bool
}

func NewWeb(s *Store, e *Engine, secure bool) *Web {
	f := template.FuncMap{"completionPolicy": completionPolicy, "attentionSummary": attentionSummary, "planGraph": planGraph, "clip": func(s string, n int) string {
		r := []rune(s)
		if len(r) > n {
			return string(r[:n-1]) + "…"
		}
		return s
	}, "agentRunView": func(a Agent, runs []Run) AgentRunView { return AgentRunView{Agent: a, Runs: runs} }, "permissionConstraints": func(v []ArgumentConstraint) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }, "provider": providerName, "previous": func(n int) int { return n - 1 }, "schedulewhen": scheduleWhen, "schedulehistory": scheduleHistory, "activity": activityItems, "agentname": func(agents []Agent, id string) string {
		for _, a := range agents {
			if a.ID == id {
				return a.Name
			}
		}
		return "Temporary worker"
	}, "lead": func(text string) string { return strings.SplitN(text, "\n\n", 2)[0] }, "markdown": renderMarkdown, "short": func(v string) string {
		if len(v) > 8 {
			return v[:8]
		}
		return v
	}, "when": func(v string) string {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return v
		}
		return t.Local().Format("Jan 2, 15:04")
	}, "initial": func(v string) string {
		rs := []rune(v)
		if len(rs) > 0 {
			return strings.ToUpper(string(rs[:1]))
		}
		return "?"
	}, "runname": func(runs []Run, id string) string {
		for _, run := range runs {
			if run.ID == id {
				return run.Title
			}
		}
		return id
	}, "family": Family}
	return &Web{Store: s, Engine: e, templates: template.Must(template.New("app").Funcs(f).ParseFS(assets, "templates/*.html")), Secure: secure}
}
func (w *Web) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("/", w.route)
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		rw.Header().Set("Referrer-Policy", "same-origin")
		rw.Header().Set("X-Frame-Options", "DENY")
		rw.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(rw, r.Body, 2<<20)
		mux.ServeHTTP(rw, r)
	})
}
func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func (w *Web) user(r *http.Request) (User, string) {
	c, err := r.Cookie("adc_session")
	if err != nil {
		return User{}, ""
	}
	var u User
	err = w.Store.db.QueryRow(`SELECT u.id,u.name,u.username FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token=? AND s.expires>?`, digest(c.Value), now()).Scan(&u.ID, &u.Name, &u.Username)
	if err != nil {
		return User{}, ""
	}
	return u, digest("csrf:" + c.Value)
}
func (w *Web) render(rw http.ResponseWriter, p Page) {
	var b bytes.Buffer
	if err := w.templates.ExecuteTemplate(&b, "page", p); err != nil {
		http.Error(rw, "Unable to render this view", 500)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = rw.Write(b.Bytes())
}
func (w *Web) member(user, org string) bool {
	var n int
	_ = w.Store.db.QueryRow(`SELECT count(*) FROM memberships WHERE user_id=? AND org=?`, user, org).Scan(&n)
	return n > 0
}
func (w *Web) route(rw http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(rw, "Method not allowed", 405)
		return
	}
	if r.Method == "POST" {
		if err := r.ParseForm(); err != nil {
			http.Error(rw, "Invalid form", 400)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(rw, "Cross-origin request rejected", 403)
				return
			}
		}
	}
	var count int
	_ = w.Store.db.QueryRow(`SELECT count(*) FROM users`).Scan(&count)
	if count == 0 || r.URL.Path == "/login" {
		w.auth(rw, r, count == 0)
		return
	}
	u, csrf := w.user(r)
	if u.ID == "" {
		http.Redirect(rw, r, "/login", 303)
		return
	}
	if r.Method == "POST" && r.FormValue("csrf") != csrf {
		http.Error(rw, "Session changed; reload and try again", 403)
		return
	}
	if r.URL.Path == "/logout" && r.Method == "POST" {
		c, _ := r.Cookie("adc_session")
		_, _ = w.Store.db.Exec(`DELETE FROM sessions WHERE token=?`, digest(c.Value))
		http.SetCookie(rw, &http.Cookie{Name: "adc_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: w.Secure, SameSite: http.SameSiteLaxMode})
		http.Redirect(rw, r, "/login", 303)
		return
	}
	p := Page{Title: "Aide de Camp", User: u, CSRF: csrf, View: "work"}
	for _, org := range list[Organization](w.Store, "organization", "") {
		if w.member(u.ID, org.ID) {
			p.Orgs = append(p.Orgs, org)
		}
	}
	orgID := r.URL.Query().Get("org")
	if orgID == "" && len(p.Orgs) > 0 {
		orgID = p.Orgs[0].ID
	}
	if orgID != "" {
		if !w.member(u.ID, orgID) {
			http.Error(rw, "Organization not available", 403)
			return
		}
		_ = w.Store.Get(orgID, &p.Org)
	}
	if p.Org.ID == "" {
		http.Error(rw, "No organization membership is available", http.StatusForbidden)
		return
	}
	if r.Method == "POST" {
		if err := w.action(r, p); err != nil {
			p.Error = err.Error()
			if r.URL.Path == "/connections" && r.FormValue("id") != "" && w.connectionPage(r.FormValue("id"), &p) == nil {
				w.render(rw, p)
				return
			}
			if r.URL.Path == "/proposal-action" {
				if w.Store.Get(r.FormValue("id"), &p.Proposal) == nil && p.Proposal.Org == p.Org.ID {
					w.proposalPage(&p)
					w.render(rw, p)
					return
				}
			}
			p.Title = "Unable to save"
			w.render(rw, p)
			return
		}
		dest := r.FormValue("return")
		if !strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "//") {
			dest = "/?org=" + orgID
		}
		http.Redirect(rw, r, dest, 303)
		return
	}
	p.Agents = list[Agent](w.Store, "agent", orgID)
	p.Tree = TeamTree(p.Agents)
	p.Defaults = map[string]string{}
	for _, d := range list[CategoryDefault](w.Store, "default", orgID) {
		p.Defaults[d.Category] = d.Agent
	}
	p.Tasks = list[Assignment](w.Store, "assignment", orgID)
	p.Areas = list[Area](w.Store, "area", orgID)
	p.Decisions = pendingOrganizationDecisions(w.Store, orgID)
	p.ProposalCount = pendingProposals(w.Store, orgID)
	p.Connections = list[Connection](w.Store, "connection", orgID)
	for _, a := range list[Account](w.Store, "account", "") {
		if a.User == u.ID {
			p.Accounts = append(p.Accounts, a)
		}
	}
	switch r.URL.Path {
	case "/":
		p.OwnershipBriefing = w.Engine.ownershipBriefing(p.Org.ID)
	case "/coordination":
		w.ownerRequestPage(r, &p)
	case "/areas":
		p.View, p.Title = "areas", "Areas of responsibility"
		p.Areas = list[Area](w.Store, "area", orgID)
		for _, a := range p.Areas {
			k := w.Store.areaKnowledge(a)
			records, _ := w.Store.Records("area-history", a.Org)
			for _, record := range records {
				if record.Parent == a.ID {
					var old Area
					if w.Store.Get(record.ID, &old) == nil {
						k.History = append(k.History, old)
					}
				}
			}
			if len(k.History) > 12 {
				k.History = k.History[:12]
			}
			p.AreaKnowledge = append(p.AreaKnowledge, k)
		}
		p.Obligations = list[Obligation](w.Store, "obligation", orgID)
	case "/live-work":
		w.liveWork(rw, r, p)
		return
	case "/connection":
		if err := w.connectionPage(r.URL.Query().Get("id"), &p); err != nil {
			http.NotFound(rw, r)
			return
		}
	case "/selfhosted-account":
		if err := w.selfhostedAccountPage(r, &p); err != nil {
			http.NotFound(rw, r)
			return
		}
	case "/claude-account":
		if err := w.claudeAccountPage(r, &p); err != nil {
			http.NotFound(rw, r)
			return
		}
	case "/codex-account":
		if err := w.codexAccountPage(r, &p); err != nil {
			http.NotFound(rw, r)
			return
		}
	case "/usage", "/live-usage":
		days, page := usageSelection(r)
		report, err := w.Store.usageReport(p.Org.ID, r.URL.Query().Get("task"), days, page, time.Now())
		if err != nil {
			http.Error(rw, "Usage unavailable", http.StatusNotFound)
			return
		}
		p.Usage = report
		p.View = "usage"
		p.Title = "Usage"
		if r.URL.Path == "/live-usage" {
			w.liveUsage(rw, r, p)
			return
		}
	case "/schedules", "/live-schedules":
		p.View = "schedules"
		p.Title = "Standing work"
		p.Schedules = list[StandingSchedule](w.Store, "schedule", p.Org.ID)
		p.Members = w.members(p.Org.ID)
		if r.URL.Path == "/live-schedules" {
			w.liveSchedules(rw, r, p)
			return
		}
	case "/proposals", "/live-proposals":
		p.View = "proposals"
		p.Title = "Work proposals"
		p.ProposalArchive = r.URL.Query().Get("archive") == "1"
		p.ProposalPage, _ = strconv.Atoi(r.URL.Query().Get("page"))
		if p.ProposalPage < 0 {
			p.ProposalPage = 0
		}
		w.populateProposals(&p)
		if r.URL.Path == "/live-proposals" {
			w.liveProposalQueue(rw, r, p)
			return
		}

	case "/proposal", "/live-proposal":
		if w.Store.Get(r.URL.Query().Get("id"), &p.Proposal) != nil || p.Proposal.Org != orgID {
			http.NotFound(rw, r)
			return
		}
		w.proposalPage(&p)
		if r.URL.Path == "/live-proposal" {
			if revision, err := strconv.Atoi(r.URL.Query().Get("revision")); err == nil && revision > 0 {
				p.ProposalRevision = revision
			}
			w.liveProposal(rw, r, p)
			return
		}
	case "/run", "/live-run":
		if !w.populateRun(&p, r.URL.Query().Get("id")) {
			http.NotFound(rw, r)
			return
		}
		p.Before, _ = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		if p.Before < 0 {
			p.Before = 0
		}
		p.ActivityURL = "/run?org=" + url.QueryEscape(p.Org.ID) + "&id=" + url.QueryEscape(p.Run.ID)
		w.populateTranscript(&p)
		p.Title, p.View = p.Run.Title, "run"
		if r.URL.Path == "/live-run" {
			w.liveTranscript(rw, r, p)
			return
		}
	case "/team", "/live-team":
		w.populateActiveRuns(&p)
		if r.URL.Path == "/live-team" {
			w.liveTeamRuns(rw, r, p)
			return
		}
		p.View = "team"
		p.Title = "Your team"
		if len(p.Accounts) > 0 {
			selected := p.Accounts[0]
			for _, account := range p.Accounts {
				if account.ID == r.URL.Query().Get("account") {
					selected = account
				}
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			var err error
			p.Models, err = w.Engine.Models(ctx, selected)
			if err != nil {
				redact := Redactor{}
				if value, unsealErr := w.Store.Unseal(selected.Secret); unsealErr == nil && value != "" {
					redact.Values = []string{value}
				}
				p.Error = "Could not load provider models: " + redact.Text(err.Error())
			}
		}
	case "/permissions":
		p.View, p.Title = "permissions", "Permissions"
		p.Permissions = w.permissionPage(p.Org.ID, r.URL.Query().Get("connection"))
	case "/connections":
		p.View = "connections"
		p.Title = "Connections"
		if len(p.Accounts) > 0 {
			selected := p.Accounts[0]
			for _, account := range p.Accounts {
				if account.ID == r.URL.Query().Get("account") {
					selected = account
				}
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			var err error
			p.Models, err = w.Engine.Models(ctx, selected)
			if err != nil {
				redact := Redactor{}
				if value, unsealErr := w.Store.Unseal(selected.Secret); unsealErr == nil && value != "" {
					redact.Values = []string{value}
				}
				p.Error = "Could not load provider models: " + redact.Text(err.Error())
			}
		}
	case "/settings":
		p.View = "settings"
		p.Title = "Organization settings"
		p.Members = w.members(p.Org.ID)
	case "/library":
		p.View = "library"
		p.Title = "Documents"
		p.Documents = list[Document](w.Store, "document", orgID)
	case "/task", "/live", "/plan", "/live-plan":
		id := r.URL.Query().Get("id")
		if w.Store.Get(id, &p.Task) != nil || !w.member(u.ID, p.Task.Org) {
			http.NotFound(rw, r)
			return
		}
		_ = w.Store.Get(p.Task.Org, &p.Org)
		p.Title = p.Task.Title
		p.View = "task"
		if r.URL.Path == "/plan" || r.URL.Path == "/live-plan" {
			p.View = "plan"
		}
		p.Plan = w.Engine.inspectPlan(w.Store.taskPlan(id))
		p.Runs = taskRuns(w.Store, id)
		p.Agents = append(list[Agent](w.Store, "agent", p.Task.Org), list[Agent](w.Store, "guide", p.Task.Org)...)
		p.Before, _ = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		p.ActivityURL = "/task?org=" + url.QueryEscape(p.Org.ID) + "&id=" + url.QueryEscape(id)
		p.Events = w.Store.EventsBefore(id, p.Before)
		p.Older = w.Store.OlderEvents(id, p.Events)
		p.Documents = taskDocs(w.Store, id)
		p.Reviews = taskReviews(w.Store, id)
		p.Traces = taskTraces(w.Store, id)
		p.Decisions = taskDecisions(w.Store, id)
		w.populateAssessment(&p)
		if r.URL.Path == "/live" || r.URL.Path == "/live-plan" {
			w.live(rw, r, p)
			return
		}
		if doc := r.URL.Query().Get("doc"); doc != "" {
			if w.Store.Get(doc, &p.Selected) != nil || p.Selected.Task != id {
				http.NotFound(rw, r)
				return
			}
			p.History = documentHistory(w.Store, p.Selected)
			if rev := r.URL.Query().Get("rev"); rev != "" {
				n, err := strconv.Atoi(rev)
				if err != nil {
					http.NotFound(rw, r)
					return
				}
				p.Selected, err = documentVersion(w.Store, p.Selected, n)
				if err != nil {
					http.NotFound(rw, r)
					return
				}
			}
			p.HTML = renderMarkdown(p.Selected.Content)
		}
	default:
		http.NotFound(rw, r)
		return
	}
	w.render(rw, p)
}
func (w *Web) auth(rw http.ResponseWriter, r *http.Request, setup bool) {
	p := Page{View: "auth", Title: "Welcome back", Setup: setup}
	if setup {
		p.Title = "Make room for good work"
	}
	if r.Method == "GET" {
		w.render(rw, p)
		return
	}
	if r.URL.Path != "/login" && r.URL.Path != "/setup" {
		http.Error(rw, "Set up your account first", 403)
		return
	}
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	var u User
	if setup {
		if len(password) < 12 || username == "" || strings.TrimSpace(r.FormValue("name")) == "" {
			p.Error = "Choose a username, display name and password of at least 12 characters."
			w.render(rw, p)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(rw, "Unable to create account", 500)
			return
		}
		w.Store.mu.Lock()
		var n int
		_ = w.Store.db.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
		if n != 0 {
			w.Store.mu.Unlock()
			http.Redirect(rw, r, "/login", 303)
			return
		}
		u = User{ID(), r.FormValue("name"), username}
		_, err = w.Store.db.Exec(`INSERT INTO users VALUES(?,?,?,?)`, u.ID, u.Name, u.Username, hash)
		if err == nil {
			o := Organization{ID: ID(), Name: r.FormValue("organization")}
			if o.Name == "" {
				o.Name = "My organization"
			}
			err = w.Store.Put("organization", o.ID, "", "", o.ID, o)
			if err == nil {
				_, err = w.Store.db.Exec(`INSERT INTO memberships VALUES(?,?)`, u.ID, o.ID)
			}
		}
		w.Store.mu.Unlock()
		if err != nil {
			http.Error(rw, "Unable to create account", 500)
			return
		}
	} else {
		var hash []byte
		err := w.Store.db.QueryRow(`SELECT id,name,username,password FROM users WHERE username=?`, username).Scan(&u.ID, &u.Name, &u.Username, &hash)
		if err != nil || bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil {
			time.Sleep(250 * time.Millisecond)
			p.Error = "Username or password is incorrect."
			w.render(rw, p)
			return
		}
	}
	token := ID() + ID()
	_, err := w.Store.db.Exec(`INSERT INTO sessions VALUES(?,?,?)`, digest(token), u.ID, time.Now().Add(7*24*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		http.Error(rw, "Unable to start session", 500)
		return
	}
	http.SetCookie(rw, &http.Cookie{Name: "adc_session", Value: token, Path: "/", HttpOnly: true, Secure: w.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	http.Redirect(rw, r, "/", 303)
}
func (w *Web) action(r *http.Request, p Page) error {
	if strings.HasPrefix(r.URL.Path, "/permission-") {
		return w.permissionAction(r, p)
	}
	s := w.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	f := r.FormValue
	switch r.URL.Path {
	case "/owner-request-action":
		return w.ownerRequestAction(r, p)
	case "/area-action":
		return w.areaAction(r, p)
	case "/claude-logout":
		return w.claudeLogout(r, p)
	case "/codex-login":
		return w.codexLoginAction(r, p)
	case "/plan-action":
		return w.executionPlanAction(r, p)
	case "/schedule-action":
		return w.scheduleAction(r, p)
	case "/proposal-brief":
		if strings.TrimSpace(f("prompt")) == "" {
			return errors.New("describe the work you want the team to consider")
		}
		t := Assignment{ID: ID(), Org: p.Org.ID, Kind: "proposal", Title: "Propose follow-up work", Prompt: "Propose bounded work for human review using adc_propose_work. Read relevant existing organization documents with adc_read_document; these are evidence, not authority to execute. Use known facts, identify uncertainty and related proposals, and do not perform the proposed work. Select appropriate suggested owners. If the human wants recurring work, propose an explicit Cadence (interval/daily/weekly, with IANA timezone for calendar schedules); include validation and rollback/stop plans for recurring actions. Acceptance activates the schedule; you cannot activate it yourself. Finish with a short summary and proposal links; no independent review committee is needed before human review. Human request: " + f("prompt"), Owner: f("owner"), Account: f("account"), ExtraAccount: f("extra_account"), Creator: p.User.ID}
		if err := w.Engine.CreateAssignment(t); err != nil {
			return err
		}
		r.Form.Set("return", "/task?org="+p.Org.ID+"&id="+t.ID)
		return nil
	case "/proposal-action":
		return w.proposalAction(r, p)
	case "/organizations":
		o := Organization{ID: ID(), Name: strings.TrimSpace(f("name")), Description: f("description")}
		if o.Name == "" {
			return errors.New("Organization name is required")
		}
		if err := s.Put("organization", o.ID, "", "", o.ID, o); err != nil {
			return err
		}
		_, err := s.db.Exec(`INSERT INTO memberships VALUES(?,?)`, p.User.ID, o.ID)
		return err
	case "/accounts":
		return w.saveAccount(r, p.User)
	case "/password":
		return w.changePassword(r, p.User)

	case "/agents":
		if p.Org.ID == "" {
			return errors.New("Create an organization first")
		}
		a := Agent{ID: ID(), Org: p.Org.ID, Name: f("name"), Description: f("description"), ReportsTo: f("reports_to"), Category: f("category"), Provider: f("provider"), Model: f("model"), Effort: f("effort"), Authority: f("authority"), Tools: r.Form["tools"]}
		if id := f("id"); id != "" {
			var old Agent
			if s.Get(id, &old) != nil || old.Org != p.Org.ID {
				return errors.New("Agent not available")
			}
			a.ID = id
			if f("provider") == "" {
				a.Provider = old.Provider
			}
		}
		if err := validateAgent(s, a); err != nil {
			return err
		}
		return SaveAgent(s, a, f("category_default") == "on")
	case "/connections":
		return w.saveConnection(r, p.Org.ID)
	case "/area-proposals":
		return w.startAreaConversation(r, p)
	case "/team-proposals":
		if f("prompt") == "" || Family(f("model")) == "" {
			return errors.New("Describe your organization and choose an explicit supervisor model")
		}
		var guideAccount Account
		if s.Get(f("account"), &guideAccount) != nil || guideAccount.User != p.User.ID {
			return errors.New("select your own subscription")
		}
		guide := Agent{Provider: providerName(guideAccount.Provider), ID: ID(), Org: p.Org.ID, Name: "Team designer", Description: "Discover the organization and propose a team of permanent agents for human approval. Choose models from the supplied account catalog. Every proposed agent needs a category, explicit provider (copilot, codex, claude or selfhosted), model and authority. Use available_models_by_provider to pair models with the correct provider. Give proposal roles unique names and optional unique local IDs; ReportsTo may reference another proposed role by name or local ID, or an existing supervisor ID. Do not create permanent agents yourself.", Category: "supervision", Model: f("model"), Authority: "observe"}
		if err := s.Put("guide", guide.Org, "", "", guide.ID, guide); err != nil {
			return err
		}
		return w.Engine.CreateAssignment(Assignment{ID: ID(), Org: p.Org.ID, Title: "Propose a team for " + p.Org.Name, Prompt: "Discover and propose a team for this organization. Present it with adc_decision and proposed_agents for human approval. Use lowercase category values supervision, research, implementation, review or planning; authority observe or draft; select only known explicit model IDs. Include independent review using a different model family from implementation; the preferred starting point is GPT execution and Claude review. If the subscription lacks a needed family, explain the limitation. Request: " + f("prompt"), Owner: guide.ID, Account: f("account"), ExtraAccount: f("extra_account"), Creator: p.User.ID})
	case "/assignments":
		if f("title") == "" || f("prompt") == "" {
			return errors.New("A title and requested outcome are required")
		}
		t := Assignment{Area: f("area"), ID: ID(), Org: p.Org.ID, Title: f("title"), Prompt: f("prompt"), Owner: f("owner"), Account: f("account"), ExtraAccount: f("extra_account"), Creator: p.User.ID, Execution: f("execution")}
		t.Completion = selectedCompletion(f("completion"))
		return w.Engine.CreateAssignment(t)
	case "/members":
		var id string
		if err := s.db.QueryRow(`SELECT id FROM users WHERE username=?`, strings.ToLower(f("username"))).Scan(&id); err != nil {
			return errors.New("Create that person's account below before adding membership")
		}
		_, err := s.db.Exec(`INSERT OR IGNORE INTO memberships VALUES(?,?)`, id, p.Org.ID)
		return err
	case "/users":
		if len(f("password")) < 12 {
			return errors.New("Temporary password must have at least 12 characters")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(f("password")), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		u := User{ID(), f("name"), strings.ToLower(f("username"))}
		if u.Username == "" || u.Name == "" {
			return errors.New("Name and username are required")
		}
		_, err = s.db.Exec(`INSERT INTO users VALUES(?,?,?,?)`, u.ID, u.Name, u.Username, hash)
		if err != nil {
			return errors.New("Username already exists or account could not be created")
		}
		_, err = s.db.Exec(`INSERT INTO memberships VALUES(?,?)`, u.ID, p.Org.ID)
		return err
	case "/steer", "/decision", "/task-action":
		var t Assignment
		if s.Get(f("task"), &t) != nil || !w.member(p.User.ID, t.Org) {
			return errors.New("Assignment not available")
		}
		if r.URL.Path == "/task-action" {
			switch f("action") {
			case "pause", "cancel":
				t.State = "paused"
				if f("action") == "cancel" {
					t.State = "cancelled"
				}
				writes := []Write{{"assignment", t.Org, "", t.State, t.ID, t}}
				if t.State == "cancelled" {
					for _, request := range list[AccessRequest](s, "access-request", t.Org) {
						if request.Task != t.ID || request.State != "pending" {
							continue
						}
						request.State, request.Resolved = "cancelled", now()
						writes = append(writes, Write{"access-request", t.Org, t.ID, request.State, request.ID, request})
						var decision Decision
						if s.Get(request.Decision, &decision) == nil && decision.State == "pending" {
							decision.State = "cancelled"
							writes = append(writes, Write{"decision", t.Org, t.ID, decision.State, decision.ID, decision})
						}
					}
					for _, run := range taskRuns(s, t.ID) {
						if run.State != "complete" {
							run.State = "cancelled"
							writes = append(writes, Write{"run", run.Org, run.Task, run.State, run.ID, run})
						}
					}
				}
				if err := s.Batch(writes...); err != nil {
					return err
				}
				w.Engine.CancelTask(t.ID)
				return nil
			case "resume":
				if t.State != "paused" {
					return errors.New("Only a paused assignment can be resumed")
				}
				if !w.Engine.attentionCapacity(t) || w.Engine.attentionExpired(t, time.Now()) || w.Engine.attentionQueueExpired(t, time.Now()) || w.Engine.exhaustedAttentionStage(t) != "" {
					return errors.New("This attention task spent its shared budget; reassess its scope before starting further work")
				}
				if reason := s.obligationRunProblem(t); reason != "" {
					return errors.New(reason + "; reassess the retained obligation before resuming")
				}
				t.State = "queued"
				return s.Put("assignment", t.Org, "", t.State, t.ID, t)
			default:
				return errors.New("Unknown action")
			}
		}
		if t.State == "cancelled" {
			return errors.New("This assignment is cancelled; create a new assignment to continue the work")
		}
		var run Run
		writes := []Write{}
		decisionLog := ""
		commitResponse := func(writes []Write) error {
			if err := s.Batch(writes...); err != nil {
				return err
			}
			if decisionLog != "" {
				s.Log(t.Org, t.ID, run.ID, "human", decisionLog)
			}
			return nil
		}
		if r.URL.Path == "/decision" {
			var d Decision
			if s.Get(f("decision"), &d) != nil || d.Task != t.ID || d.Org != t.Org || d.State != "pending" {
				return errors.New("Decision already resolved or unavailable")
			}
			if d.Kind == "permission" {
				return errors.New("resolve this access bundle on the Permissions page")
			}
			if s.Get(d.Run, &run) != nil || run.Task != t.ID || run.Org != t.Org || run.Superseded || run.State == "cancelled" {
				return errors.New("Run unavailable")
			}
			d.Outcome = f("outcome")
			d.Answer = strings.TrimSpace(f("answer"))
			if d.Outcome != "" {
				var err error
				d.Answer, err = decisionAnswer(d, d.Outcome, f("answer"))
				if err != nil {
					return err
				}
			} else if d.Answer == "" || d.Acceptance != nil {
				return errors.New("Choose Approve, Reject, or Refine with notes")
			}
			d.State, d.ResolvedBy, d.ResolvedAt = "answered", p.User.ID, now()
			if d.Outcome == "approve" {
				linked, err := w.Engine.decisionAcceptanceWrites(d, run, p.User.ID)
				if err == nil {
					var actionWrites []Write
					actionWrites, err = w.Engine.approveDecisionAction(&d, run)
					linked = append(linked, actionWrites...)
				}
				if err != nil {
					return err
				}
				writes = append(writes, linked...)
				// Preserve lifecycle updates prepared for the requester/task in the
				// shared transaction, before adding the human response below.
				for _, write := range linked {
					if value, ok := write.Value.(Run); ok && value.ID == run.ID {
						run = value
					}
					if value, ok := write.Value.(Assignment); ok && value.ID == t.ID {
						t = value
					}
				}
			}
			if d.ProposedArea != nil && d.Outcome == "approve" {
				if !t.AreaCreation || t.Kind != "proposal" {
					return fmt.Errorf("area proposal conversation required")
				}
				area, areaWrites, err := s.prepareProposedArea(t.Org, *d.ProposedArea, "human:"+p.User.ID)
				if err != nil {
					return err
				}
				d.CreatedArea = area.ID
				run.State = "complete"
				run.Result = "Human approved and created the area: " + area.Name
				t.State = "ready"
				t.Output = run.Result
				writes = append(writes, areaWrites...)
				writes = append(writes, Write{"decision", d.Org, d.Task, d.State, d.ID, d}, Write{"run", run.Org, run.Task, run.State, run.ID, run}, Write{"assignment", t.Org, "", t.State, t.ID, t})
				decisionLog = p.User.Name + " approved the area: " + area.Name
				if err := commitResponse(writes); err != nil {
					return err
				}
				r.Form.Set("return", "/areas?org="+area.Org+"#area-"+area.ID)
				return nil
			}
			approveTeam := len(d.Proposal) > 0 && (d.Outcome == "approve" || (d.Outcome == "" && f("approve_team") == "on"))
			if approveTeam {
				agents, err := PrepareTeam(s, t.Org, d.Proposal)
				if err != nil {
					return fmt.Errorf("Team proposal needs correction: %w", err)
				}
				defaults := map[string]bool{}
				for _, a := range agents {
					writes = append(writes, Write{"agent", a.Org, a.ReportsTo, "", a.ID, a})
					key := "default:" + a.Org + ":" + a.Category
					var current CategoryDefault
					if !defaults[key] && s.Get(key, &current) != nil {
						writes = append(writes, Write{"default", a.Org, "", "", key, CategoryDefault{a.Org, a.Category, a.ID}})
						defaults[key] = true
					}
				}
				writes = append(writes, Write{"decision", d.Org, d.Task, d.State, d.ID, d})
				var kind string
				_ = s.db.QueryRow(`SELECT kind FROM records WHERE id=?`, t.Owner).Scan(&kind)
				if kind == "guide" {
					run.State = "complete"
					run.Result = "Human approved the proposed team."
					t.State = "ready"
					t.Output = run.Result
					writes = append(writes, Write{"run", run.Org, run.Task, run.State, run.ID, run}, Write{"assignment", t.Org, "", t.State, t.ID, t})
					decisionLog = p.User.Name + " approved the proposed permanent team."
					return commitResponse(writes)
				}
			}
			writes = append(writes, Write{"decision", d.Org, d.Task, d.State, d.ID, d})
			run.Reassignments = 0
			for _, child := range taskRuns(s, t.ID) {
				for _, write := range writes {
					if value, ok := write.Value.(Run); ok && value.ID == child.ID {
						child = value
					}
				}
				if child.Parent == run.ID && child.State == "blocked" {
					child.Reassignments = 0
					writes = append(writes, Write{"run", child.Org, child.Task, child.State, child.ID, child})
				}
			}
			run.Prompt += "\nHUMAN DECISION (final; read it plainly and act on it, do not ask for it in another form) from " + p.User.Name + ": " + d.Answer
			if f("publish") == "on" && (d.Outcome == "approve" || d.Outcome == "") {
				t.Publication = true
			}
			decisionLog = p.User.Name + ": " + d.Answer
		} else {
			if f("message") == "" {
				return errors.New("Enter a message")
			}
			id := f("run")
			if id == "" {
				for _, rr := range taskRuns(s, t.ID) {
					if rr.Parent == "" {
						id = rr.ID
						break
					}
				}
			}
			if s.Get(id, &run) != nil || run.Task != t.ID {
				return errors.New("Run unavailable")
			}
			if run.Superseded {
				return errors.New("This attempt was superseded; steer the current plan step or its supervisor")
			}
			message := p.User.Name + ": " + f("message")
			if doc := f("document"); doc != "" {
				var d Document
				if s.Get(doc, &d) != nil || d.Task != t.ID {
					return errors.New("Document unavailable")
				}
				revision, err := strconv.Atoi(f("revision"))
				if err != nil {
					return errors.New("Document revision is required")
				}
				if d, err = documentVersion(s, d, revision); err != nil {
					return err
				}
				message += "\nContext: document " + d.Title + " (" + d.ID + ") revision " + strconv.Itoa(d.Revision) + "\nSelected passage: " + f("selection")
			}
			run.Prompt += "\nVISIBLE HUMAN STEERING: " + message
			s.Log(t.Org, t.ID, run.ID, "human", message)
			if run.Parent != "" {
				var parent Run
				if s.Get(run.Parent, &parent) == nil {
					parent.Prompt += "\nHuman steered " + run.Title + ": " + message
					writes = append(writes, Write{"run", parent.Org, parent.Task, parent.State, parent.ID, parent})
				}
			}
		}
		if run.State == "running" {
			run.Steering = true
			return commitResponse(append(writes, Write{"assignment", t.Org, "", t.State, t.ID, t}, Write{"run", run.Org, run.Task, run.State, run.ID, run}))
		}
		run.State = "queued"
		run.Turns = 0
		run.Attempts = 0
		if t.State != "paused" && t.State != "cancelled" {
			t.State = "queued"
		}
		return commitResponse(append(writes, Write{"assignment", t.Org, "", t.State, t.ID, t}, Write{"run", run.Org, run.Task, run.State, run.ID, run}))
	default:
		return errors.New("Unknown action")
	}
}
func validateAgent(s *Store, a Agent) error {
	if err := validateProvider(a.Provider); err != nil {
		return err
	}
	if providerName(a.Provider) == "claude" && Family(a.Model) != "anthropic-claude" {
		return fmt.Errorf("Claude roles require a Claude model from that account catalog")
	}
	if providerName(a.Provider) == "codex" && Family(a.Model) != "openai-gpt" {
		return fmt.Errorf("Codex roles require an OpenAI model; use Copilot for Claude review")
	}
	if a.Name == "" || a.Description == "" {
		return errors.New("Name and responsibility description are required")
	}
	if Family(a.Model) == "" {
		return errors.New("Choose an explicit model with a recognized family from your provider catalog")
	}
	if authorityRank(a.Authority) < 0 {
		return errors.New("Choose a supported autonomy policy")
	}
	switch a.Category {
	case "supervision", "research", "implementation", "review", "planning":
	default:
		return errors.New("Choose a work category")
	}
	if a.ReportsTo != "" {
		var boss Agent
		if s.Get(a.ReportsTo, &boss) != nil || boss.Org != a.Org || boss.ID == a.ID {
			return errors.New("Choose a supervisor in this organization")
		}
		seen := map[string]bool{a.ID: true}
		for boss.ID != "" {
			if seen[boss.ID] {
				return errors.New("Reporting relationships cannot contain a cycle")
			}
			seen[boss.ID] = true
			if boss.ReportsTo == "" {
				break
			}
			var next Agent
			if s.Get(boss.ReportsTo, &next) != nil {
				break
			}
			boss = next
		}
	}
	for _, id := range a.Tools {
		var c Connection
		if s.Get(id, &c) != nil || c.Org != a.Org {
			return errors.New("Tool connection outside organization")
		}
	}
	return nil
}
func (w *Web) live(rw http.ResponseWriter, r *http.Request, p Page) {
	sse := datastar.NewSSE(rw, r)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	last := map[string]string{}
	for {
		user, _ := w.user(r)
		if user.ID != p.User.ID || !w.member(user.ID, p.Org.ID) {
			return
		}

		p.Plan = w.Engine.inspectPlan(w.Store.taskPlan(p.Task.ID))
		p.Events = w.Store.Events(p.Task.ID)
		p.Older = w.Store.OlderEvents(p.Task.ID, p.Events)
		p.Runs = taskRuns(w.Store, p.Task.ID)
		p.Agents = append(list[Agent](w.Store, "agent", p.Task.Org), list[Agent](w.Store, "guide", p.Task.Org)...)
		p.Decisions = taskDecisions(w.Store, p.Task.ID)
		p.Documents = taskDocs(w.Store, p.Task.ID)
		p.Reviews = taskReviews(w.Store, p.Task.ID)
		p.Traces = taskTraces(w.Store, p.Task.ID)
		_ = w.Store.Get(p.Task.ID, &p.Task)
		w.populateAssessment(&p)
		var b bytes.Buffer
		names := []string{"execution-plan", "assessment-records", "taskstatus", "runlist", "timeline", "decisionlist", "doclist", "reviewlist", "toollist"}
		if p.View == "plan" {
			names = []string{"execution-plan"}
		}
		for _, name := range names {
			b.Reset()
			if err := w.templates.ExecuteTemplate(&b, name, p); err != nil {
				return
			}
			content := b.String()
			key := digest(content)
			if key == last[name] {
				continue
			}
			if err := sse.PatchElements(content); err != nil {
				return
			}
			last[name] = key
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

func pendingOrganizationDecisions(s *Store, org string) []Decision {
	out := []Decision{}
	for _, d := range list[Decision](s, "decision", org) {
		if d.State == "pending" {
			var task Assignment
			if s.Get(d.Task, &task) == nil && task.State != "cancelled" {
				out = append(out, d)
			}
		}
	}
	return out
}
func (w *Web) liveWork(rw http.ResponseWriter, r *http.Request, p Page) {
	sse := datastar.NewSSE(rw, r)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	last := ""
	for {
		if !w.member(p.User.ID, p.Org.ID) {
			return
		}
		p.Tasks = list[Assignment](w.Store, "assignment", p.Org.ID)
		p.Decisions = pendingOrganizationDecisions(w.Store, p.Org.ID)
		p.ProposalCount = pendingProposals(w.Store, p.Org.ID)
		p.OwnershipBriefing = w.Engine.ownershipBriefing(p.Org.ID)
		var b bytes.Buffer
		if w.templates.ExecuteTemplate(&b, "workboard", p) != nil {
			return
		}
		current := b.String()
		hash := digest(current)
		if hash != last {
			if sse.PatchElements(current) != nil {
				return
			}
			last = hash
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

func renderMarkdown(text string) template.HTML {
	var b bytes.Buffer
	_ = goldmark.New(goldmark.WithExtensions(extension.Table)).Convert([]byte(text), &b)
	return template.HTML(b.String())
}
