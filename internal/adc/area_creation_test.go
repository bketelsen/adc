package adc

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func areaConversationFixture(t *testing.T) (*Store, *Engine, Assignment, Run) {
	s, e, task, root := fixture(t)
	task.Kind = "proposal"
	task.AreaCreation = true
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	root.Tools = nil
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	return s, e, task, root
}
func TestAreaConversationApprovalRefinementAndNoExecution(t *testing.T) {
	s, e, task, root := areaConversationFixture(t)
	transcriptLogin(t, s)
	proposal := areaProposalInput{AreaSpec: AreaSpec{Name: "Backups", Owner: "dev", Intent: "Own recoverability across storage and hosting. Propose improvements; no autonomous infrastructure changes."}}
	_, err := call(t, e, root, "adc_propose_area", proposal)
	must(t, err)
	d := taskDecisions(s, task.ID)[0]
	if len(list[Area](s, "area", task.Org)) != 0 {
		t.Fatal("proposal created area without approval")
	}
	setRunning(t, s, &root)
	proposal.Intent = "Bounded responsibility for backup restore verification."
	proposal.Replaces = d.ID
	_, err = call(t, e, root, "adc_propose_area", proposal)
	must(t, err)
	var revised Decision
	for _, x := range taskDecisions(s, task.ID) {
		if x.State == "pending" {
			revised = x
		}
	}
	if revised.ID == d.ID || revised.ProposedArea.Intent != proposal.Intent {
		t.Fatal("revision not replaced")
	}
	w := NewWeb(s, e, false)
	page := Page{Org: Organization{ID: task.Org}, User: User{ID: "owner", Name: "Owner"}}
	approve := func(id string) error {
		values := url.Values{"task": {task.ID}, "decision": {id}, "outcome": {"approve"}}
		r := httptest.NewRequest("POST", "/decision", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		must(t, r.ParseForm())
		return w.action(r, page)
	}
	if approve(d.ID) == nil {
		t.Fatal("stale proposal approved")
	}
	must(t, approve(revised.ID))
	areas := list[Area](s, "area", task.Org)
	if len(areas) != 1 || areas[0].Intent != proposal.Intent || areas[0].UpdatedBy != "human:owner" || areas[0].Owner != "dev" {
		t.Fatal(areas)
	}
	if approve(revised.ID) == nil || len(list[Area](s, "area", task.Org)) != 1 {
		t.Fatal("approval duplicated area")
	}
	must(t, s.Get(task.ID, &task))
	must(t, s.Get(root.ID, &root))
	if task.State != "ready" || root.State != "complete" || len(taskRuns(s, task.ID)) != 1 {
		t.Fatal("area creation started work")
	}
}
func TestAreaConversationBoundariesAndToolSchema(t *testing.T) {
	s, e, task, root := areaConversationFixture(t)
	allowed := map[string]bool{"adc_status": true, "adc_read_document": true, "adc_propose_area": true, "adc_finish": true, "adc_blocked": true}
	found := false
	for _, tool := range e.tools(root) {
		if !allowed[tool.Name] {
			t.Fatal("execution tool in area conversation", tool.Name)
		}
		if tool.Name == "adc_propose_area" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing area proposal tool")
	}
	for _, owner := range []string{"missing", "foreign", "temporary"} {
		kind := "agent"
		org := "elsewhere"
		if owner == "temporary" {
			kind = "guide"
			org = task.Org
		}
		if owner != "missing" {
			must(t, s.Put(kind, org, "", "", owner, Agent{ID: owner, Org: org}))
		}
		_, err := call(t, e, root, "adc_propose_area", areaProposalInput{AreaSpec: AreaSpec{Name: "Area", Owner: owner, Intent: "Bounded intent"}})
		if err == nil {
			t.Fatal("invalid permanent owner accepted", owner)
		}
	}
	task.AreaCreation = false
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := e.submitDecision(root, decisionInput{Question: "Area", ProposedArea: &AreaSpec{Name: "Area", Owner: "dev", Intent: "Scope"}}); err == nil {
		t.Fatal("ordinary proposal became area creation")
	}
}
func TestStartAreaConversationUsesOwnAccountAndCreatesNoPermanentAgent(t *testing.T) {
	s, e, _, _ := fixture(t)
	w := NewWeb(s, e, false)
	page := Page{Org: Organization{ID: "org"}, User: User{ID: "owner"}}
	request := func(account string) *http.Request {
		v := url.Values{"prompt": {"Help me define backups"}, "account": {account}, "model": {"gpt-5.6-sol"}}
		r := httptest.NewRequest("POST", "/area-proposals", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		must(t, r.ParseForm())
		return r
	}
	if w.startAreaConversation(request("missing"), page) == nil {
		t.Fatal("foreign/missing account accepted")
	}
	if len(list[Agent](s, "guide", "org")) != 0 {
		t.Fatal("failed setup orphaned guide")
	}
	before := len(list[Agent](s, "agent", "org"))
	r := request("account")
	must(t, w.startAreaConversation(r, page))
	if !strings.HasPrefix(r.FormValue("return"), "/task?org=org&id=") || len(list[Agent](s, "agent", "org")) != before || len(list[Area](s, "area", "org")) != 0 {
		t.Fatal("setup created permanent state")
	}
	for _, a := range list[Assignment](s, "assignment", "org") {
		if a.AreaCreation {
			if a.Kind != "proposal" || a.Publication || !a.ConstrainTools {
				t.Fatal("unrestricted setup")
			}
			return
		}
	}
	t.Fatal("no area conversation")
}
