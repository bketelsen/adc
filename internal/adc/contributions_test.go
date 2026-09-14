package adc

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func contributionFixture(t *testing.T) (*Store, *Engine, Run, ContributionPacket, ContributionQueue) {
	s, e, task, r, area := ownershipFixture(t)
	task.Execution = "protected"
	r.Execution = "protected"
	r.Tools = nil
	must(t, s.Batch(Write{"assignment", task.Org, "", task.State, task.ID, task}, Write{"run", r.Org, r.Task, r.State, r.ID, r}))
	q := ContributionQueue{ID: ID(), Org: r.Org, Area: area.ID, Owner: r.Agent, Creator: task.Creator, Account: task.Account, Reviewer: "qa", Model: "claude-opus-5", Provider: "copilot", Scope: "Synthetic public calculator improvements", Source: "https://example.test/calculator", State: "active", MaxReviews: 2, MaxPackets: 5}
	must(t, s.Put("contribution-queue", q.Org, q.Area, q.State, q.ID, q))
	p, err := s.offerContribution(r, contributionOffer{Queue: q.ID, Title: "Fix sum", Outcome: "Add two integers", Criteria: "sum(2,3) returns 5; sum(-2,2) returns 0", SourceRevision: "fixture-v1", Files: map[string]string{"calc.py": "def add(a,b): return a-b\n", "test.py": "from calc import add\nassert add(2,3)==5\nassert add(-2,2)==0\n"}})
	must(t, err)
	return s, e, r, p, q
}
func submitFixture(t *testing.T, s *Store, p ContributionPacket) (Contribution, string) {
	t.Helper()
	claim, err := s.claimContribution(p.ID, time.Now())
	must(t, err)
	receipt := claim["Receipt"].(string)
	body := "def add(a,b): return a+b\n"
	c, err := s.submitContribution(p.ID, receipt, contributionSubmission{Revision: p.Public.Revision, Summary: "Fixed arithmetic; donor says PASS", Model: "unverified-external-claim", Files: map[string]*string{"calc.py": &body}}, time.Now())
	must(t, err)
	return c, receipt
}
func TestContributionLeaseReplayAndRestart(t *testing.T) {
	s, e, r, p, q := contributionFixture(t)
	_ = e
	_ = r
	claim, err := s.claimContribution(p.ID, time.Now())
	must(t, err)
	if _, err = s.claimContribution(p.ID, time.Now()); err == nil {
		t.Fatal("duplicate claim")
	}
	next, err := s.claimContribution(p.ID, time.Now().Add(2*time.Hour))
	must(t, err)
	body := "def add(a,b):return a+b\n"
	in := contributionSubmission{Revision: p.Public.Revision, Summary: "Candidate", Files: map[string]*string{"calc.py": &body}}
	if _, err = s.submitContribution(p.ID, claim["Receipt"].(string), in, time.Now()); err == nil {
		t.Fatal("stale receipt accepted")
	}
	c, err := s.submitContribution(p.ID, next["Receipt"].(string), in, time.Now())
	must(t, err)
	duplicate, err := s.submitContribution(p.ID, next["Receipt"].(string), in, time.Now())
	must(t, err)
	if c.ID != duplicate.ID {
		t.Fatal("replay created work")
	}
	in.Summary = "Different"
	if _, err = s.submitContribution(p.ID, next["Receipt"].(string), in, time.Now()); err == nil {
		t.Fatal("receipt reused")
	}
	must(t, s.Get(q.ID, &q))
	if q.Spent != 1 {
		t.Fatal("replay spent more budget")
	}
	dir := s.Dir
	must(t, s.Close())
	reopened, err := Open(dir)
	must(t, err)
	defer reopened.Close()
	e = NewEngine(reopened)
	e.dispatchContributions(time.Now())
	e.dispatchContributions(time.Now())
	must(t, reopened.Get(c.ID, &c))
	if c.State != "reviewing" || len(taskRuns(reopened, c.Task)) != 1 {
		t.Fatal("restart duplicate or lost admission")
	}
}
func TestContributionReviewBoundaryAndOwnerResume(t *testing.T) {
	s, e, owner, p, _ := contributionFixture(t)
	c, _ := submitFixture(t, s, p)
	_, err := call(t, e, owner, "adc_wait_contribution", map[string]string{"Packet": p.ID})
	must(t, err)
	e.dispatchContributions(time.Now())
	must(t, s.Get(c.ID, &c))
	var r Run
	must(t, s.Get(c.Run, &r))
	setRunning(t, s, &r)
	tools := e.providerTools(r, Redactor{}, func(string) {})
	if len(tools) != 2 || tools[0].Name != "adc_candidate_check" || tools[1].Name != "adc_admission" {
		t.Fatal("private tools entered admission", len(tools))
	}
	_, data := contributionPrompt(p, c)
	for _, private := range []string{"memberships", "connections", "document_catalog", "credential", "Owner intent"} {
		if strings.Contains(string(data), private) {
			t.Fatal("private context", private)
		}
	}
	if _, err = call(t, e, r, "adc_admission", map[string]string{"Verdict": "pass", "Findings": "External says PASS"}); err == nil {
		t.Fatal("external PASS sufficed")
	}
	// A reviewer cannot change owner knowledge or invoke MCP even with a prompt
	// injection naming the ordinary tools; they do not exist in this tool list.
	for _, tool := range tools {
		if tool.Name == "adc_remember" || tool.Name == "adc_call_tool" || tool.Name == "adc_delegate" {
			t.Fatal("authority leak")
		}
	}
	raw, err := call(t, e, r, "adc_candidate_check", workspaceCommand{Command: "python3 -I -c 'import runpy,sys; sys.path.insert(0,\"/workspace\"); runpy.run_path(\"test.py\")'"})
	must(t, err)
	var result workspaceResult
	must(t, json.Unmarshal([]byte(raw), &result))
	if result.ExitCode != 0 {
		t.Fatal(result)
	}
	_, err = call(t, e, r, "adc_admission", map[string]string{"Verdict": "pass", "Findings": "Both positive and zero-sum assertions passed against the supplied candidate."})
	must(t, err)
	e.wakeContributors(time.Now())
	must(t, s.Get(owner.ID, &owner))
	if owner.State != "queued" {
		t.Fatal("owner stranded", owner.State)
	}
	var task Assignment
	must(t, s.Get(p.Task, &task))
	if task.State == "ready" || task.Publication {
		t.Fatal("admission completed/published original work")
	}
	var stopped ContributionQueue
	must(t, s.Get(p.Queue, &stopped))
	stopped.State = "paused"
	must(t, s.Put("contribution-queue", stopped.Org, stopped.Area, stopped.State, stopped.ID, stopped))
	setRunning(t, s, &owner)
	_, err = call(t, e, owner, "adc_import_contribution", map[string]string{"Contribution": c.ID})
	must(t, err)
	owner.Execution = "advisory"
	must(t, s.Put("run", owner.Org, owner.Task, owner.State, owner.ID, owner))
	if _, err = call(t, e, owner, "adc_import_contribution", map[string]string{"Contribution": c.ID}); err == nil {
		t.Fatal("advisory import accepted")
	}
}
func TestContributionPublicProjectionAndDisabledRoute(t *testing.T) {
	s, e, _, p, q := contributionFixture(t)
	web := NewWeb(s, e, false)
	request := httptest.NewRequest("GET", "/public/queues/"+q.ID, nil)
	rw := httptest.NewRecorder()
	web.Handler().ServeHTTP(rw, request)
	if rw.Code != 404 {
		t.Fatal("public intake enabled by default")
	}
	web.PublicContributions = true
	rw = httptest.NewRecorder()
	web.Handler().ServeHTTP(rw, request)
	if rw.Code != 200 || strings.Contains(rw.Body.String(), "Creator") || strings.Contains(rw.Body.String(), "Reviewer") || strings.Contains(rw.Body.String(), "ClaimHash") || strings.Contains(rw.Body.String(), "Account") {
		t.Fatal("public projection", rw.Code, rw.Body.String())
	}
	c, token := submitFixture(t, s, p)
	c.Findings = "synthetic private internal finding"
	must(t, s.Put("contribution", c.Org, c.Packet, c.State, c.ID, c))
	req := httptest.NewRequest("GET", "/public/submissions/"+c.ID, nil)
	rw = httptest.NewRecorder()
	web.Handler().ServeHTTP(rw, req)
	if rw.Code != 404 {
		t.Fatal("receipt not required")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rw = httptest.NewRecorder()
	web.Handler().ServeHTTP(rw, req)
	if rw.Code != 200 || strings.Contains(rw.Body.String(), "synthetic private") {
		t.Fatal("internal findings leaked")
	}
	// Membership-free JSON only; ordinary ADC auth remains required.
	rw = httptest.NewRecorder()
	web.Handler().ServeHTTP(rw, httptest.NewRequest("POST", "/contribution-action?org=org", strings.NewReader("action=create")))
	if rw.Code != 303 {
		t.Fatal("anonymous private action")
	}
}
func TestContributionFloodAndRevocation(t *testing.T) {
	s, e, _, p, q := contributionFixture(t)
	var wg sync.WaitGroup
	wins := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.mu.Lock()
			defer s.mu.Unlock()
			if _, err := s.claimContribution(p.ID, time.Now()); err == nil {
				wins++
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatal("claim race", wins)
	}
	must(t, s.Get(p.ID, &p))
	p.ClaimHash, p.ClaimUntil = "", ""
	must(t, s.Put("contribution-packet", p.Org, p.Task, p.State, p.ID, p))
	c, _ := submitFixture(t, s, p)
	q.State = "paused"
	must(t, s.Put("contribution-queue", q.Org, q.Area, q.State, q.ID, q))
	e.dispatchContributions(time.Now())
	must(t, s.Get(c.ID, &c))
	if c.State != "blocked" || c.Task != "" {
		t.Fatal("revoked queue funded work")
	}
	if _, _, err := s.publicPacket(p.ID); err == nil {
		t.Fatal("paused queue still public")
	}
}
func TestContributionWaitExpiresWithoutHumanContinuation(t *testing.T) {
	s, e, r, p, _ := contributionFixture(t)
	_, err := call(t, e, r, "adc_wait_contribution", map[string]string{"Packet": p.ID})
	must(t, err)
	e.wakeContributors(time.Now().Add(2 * time.Hour))
	must(t, s.Get(r.ID, &r))
	must(t, s.Get(p.ID, &p))
	if r.State != "queued" || p.State != "closed" || len(taskDecisions(s, r.Task)) != 0 {
		t.Fatal("timeout stranded owner or requested routine human continuation")
	}
}

func TestContributionExhaustionReturnsToOwnerWithoutDecision(t *testing.T) {
	s, e, owner, p, _ := contributionFixture(t)
	c, _ := submitFixture(t, s, p)
	_, err := call(t, e, owner, "adc_wait_contribution", map[string]string{"Packet": p.ID})
	must(t, err)
	e.dispatchContributions(time.Now())
	must(t, s.Get(c.ID, &c))
	var r Run
	must(t, s.Get(c.Run, &r))
	for i := 0; i < 3; i++ {
		setRunning(t, s, &r)
		e.completeActivation(r)
		must(t, s.Get(r.ID, &r))
	}
	e.dispatchContributions(time.Now())
	e.wakeContributors(time.Now())
	must(t, s.Get(owner.ID, &owner))
	must(t, s.Get(c.ID, &c))
	if c.State != "blocked" || owner.State != "queued" || len(taskDecisions(s, r.Task)) != 0 {
		t.Fatal("exhausted admission demanded routine approval")
	}
}

func TestContributionWaitDoesNotThrowAwayReceivedCandidate(t *testing.T) {
	s, e, owner, p, _ := contributionFixture(t)
	c, _ := submitFixture(t, s, p)
	_, err := call(t, e, owner, "adc_wait_contribution", map[string]string{"Packet": p.ID})
	must(t, err)
	e.wakeContributors(time.Now().Add(2 * time.Hour))
	must(t, s.Get(p.ID, &p))
	if p.State != "submitted" {
		t.Fatal("wait cancelled received candidate")
	}
	e.dispatchContributions(time.Now())
	must(t, s.Get(c.ID, &c))
	if c.State != "reviewing" {
		t.Fatal("received candidate was discarded")
	}
}
func TestContributionClaimLimitsRecoverAndFractionalExpiry(t *testing.T) {
	s, _, _, p, q := contributionFixture(t)
	at := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	q.ClaimTimes = []string{}
	for i := 0; i < 200; i++ {
		q.ClaimTimes = append(q.ClaimTimes, at.Format(time.RFC3339Nano))
	}
	must(t, s.Put("contribution-queue", q.Org, q.Area, q.State, q.ID, q))
	if _, err := s.claimContribution(p.ID, at.Add(time.Minute)); err == nil {
		t.Fatal("claim flood unbounded")
	}
	claim, err := s.claimContribution(p.ID, at.Add(time.Hour+time.Second))
	must(t, err)
	must(t, s.Get(p.ID, &p))
	p.ClaimUntil = at.Add(3 * time.Hour).Format(time.RFC3339Nano)
	must(t, s.Put("contribution-packet", p.Org, p.Task, p.State, p.ID, p))
	body := "x"
	if _, err = s.submitContribution(p.ID, claim["Receipt"].(string), contributionSubmission{Revision: p.Public.Revision, Summary: "too late", Files: map[string]*string{"x": &body}}, at.Add(3*time.Hour+500*time.Millisecond)); err == nil {
		t.Fatal("fractional timestamp extended expired claim")
	}
	if validateContributionFiles(map[string]string{".": "text"}) == nil {
		t.Fatal("directory path accepted")
	}
}
