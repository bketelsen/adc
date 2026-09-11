package adc

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTeamProposalReferencesAndCycles(t *testing.T) {
	s, _, _, _ := fixture(t)
	proposed := []Agent{
		{ID: "leader", Name: "CTO", Description: "Own delivery", Model: "gpt-6-astra", Category: "supervision"},
		{ID: "reviewer", Name: "QA", Description: "Independent review", Model: "claude-opus-5", Category: "review", ReportsTo: "leader"},
	}
	prepared, err := PrepareTeam(s, "org", proposed)
	must(t, err)
	if prepared[1].ReportsTo != prepared[0].ID || prepared[0].Authority != "draft" {
		t.Fatal("references not resolved")
	}
	if len(list[Agent](s, "agent", "org")) != 3 {
		t.Fatal("proposal mutated permanent team")
	}
	proposed[0].ReportsTo = "QA"
	if _, err := PrepareTeam(s, "org", proposed); err == nil {
		t.Fatal("cyclic proposal accepted")
	}
	proposed[0].ReportsTo = ""
	proposed[1].ID = "leader"
	if _, err := PrepareTeam(s, "org", proposed); err == nil {
		t.Fatal("ambiguous IDs accepted")
	}
}

func TestCategoryDefaultIsExplicitAndAuthorityIsNarrowed(t *testing.T) {
	s, e, _, root := fixture(t)
	a := Agent{ID: "specialist", Org: "org", Name: "Specialist", Description: "Implement", Model: "gpt-5.6-sol", Category: "implementation", Authority: "approved-action"}
	must(t, SaveAgent(s, a, true))
	_, err := call(t, e, root, "adc_delegate", map[string]any{"Category": "implementation", "Title": "Temporary worker", "Prompt": "Implement this bounded change"})
	must(t, err)
	for _, r := range taskRuns(s, "task") {
		if r.Parent == root.ID && (r.Model != a.Model || r.Authority != "draft") {
			t.Fatal("default or inherited limit lost")
		}
	}
}

func TestRepeatedReviewFindingsReturnToSupervisor(t *testing.T) {
	s, e, _, root := fixture(t)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	dev := Run{ID: "devrun", Org: root.Org, Task: root.Task, Parent: root.ID, Agent: "dev", Category: "implementation", Model: "gpt-6-astra", State: "complete", Result: "candidate", Authority: "draft"}
	qa := Run{ID: "qarun", Org: root.Org, Task: root.Task, Parent: root.ID, Agent: "qa", Category: "review", Model: "claude-opus-5", ReviewOf: dev.ID, State: "running", Authority: "observe"}
	sibling := Run{ID: "sibling", Org: root.Org, Task: root.Task, Parent: root.ID, State: "queued"}
	must(t, s.Put("run", sibling.Org, sibling.Task, sibling.State, sibling.ID, sibling))
	for i := 0; i < 3; i++ {
		dev.State = "complete"
		qa.State = "running"
		qa.ReviewedRevision = e.revision(dev)
		must(t, s.Put("run", dev.Org, dev.Task, dev.State, dev.ID, dev))
		must(t, s.Put("run", qa.Org, qa.Task, qa.State, qa.ID, qa))
		_, err := call(t, e, qa, "adc_review", map[string]any{"Verdict": "changes", "Findings": "Shipping claim still lacks supporting release evidence"})
		must(t, err)
		must(t, s.Get(dev.ID, &dev))
		must(t, s.Get(qa.ID, &qa))
	}
	must(t, s.Get(root.ID, &root))
	must(t, s.Get(sibling.ID, &sibling))
	if dev.State != "blocked" || qa.State != "waiting" || root.State != "queued" || sibling.State != "queued" {
		t.Fatal("review escalation did not preserve independent progress")
	}
	if len(taskDecisions(s, "task")) != 0 {
		t.Fatal("routine reassessment was sent to human")
	}
	reassigned, err := e.Reassign(root, dev.ID, "dev", "Collect release evidence first, then revise the claim")
	must(t, err)
	if reassigned.ReviewRounds != 0 || reassigned.State != "queued" || reassigned.Result != "candidate" {
		t.Fatal("reassignment lost evidence or failed to resume")
	}
}

func TestReassignmentPreservesReviewFamilyAndScope(t *testing.T) {
	s, e, _, root := fixture(t)
	target := Run{ID: "target", Org: root.Org, Task: root.Task, Parent: root.ID, State: "complete", Model: "gpt-6-astra"}
	qa := Run{ID: "reviewrun", Org: root.Org, Task: root.Task, Parent: root.ID, State: "blocked", Model: "claude-opus-5", ReviewOf: target.ID}
	must(t, s.Put("run", target.Org, target.Task, target.State, target.ID, target))
	must(t, s.Put("run", qa.Org, qa.Task, qa.State, qa.ID, qa))
	if _, err := e.Reassign(root, qa.ID, "dev", "Try this reviewer"); err == nil {
		t.Fatal("same-family reviewer accepted")
	}
	if _, err := e.Reassign(Run{ID: "stranger", Task: root.Task, Org: root.Org}, qa.ID, "qa", "Retry"); err == nil {
		t.Fatal("unowned work reassigned")
	}
}

func TestSelectedPassageKeepsHistoricalRevisionAndPause(t *testing.T) {
	s, e, task, root := fixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Owner','owner','fixture'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	_, err = call(t, e, root, "adc_document", map[string]any{"Title": "Evidence", "Content": "Original paragraph"})
	must(t, err)
	doc := taskDocs(s, task.ID)[0]
	_, err = call(t, e, root, "adc_document", map[string]any{"ID": doc.ID, "Title": "Evidence", "Content": "Revised paragraph"})
	must(t, err)
	task.State = "paused"
	root.State = "waiting"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	w := NewWeb(s, e, false)
	fields := url.Values{"task": {task.ID}, "document": {doc.ID}, "revision": {"1"}, "selection": {"Original paragraph"}, "message": {"Why this wording?"}}
	req := httptest.NewRequest("POST", "http://adc.test/steer", strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	must(t, w.action(req, Page{User: User{ID: "owner", Name: "Owner"}, Org: Organization{ID: "org"}}))
	must(t, s.Get(root.ID, &root))
	must(t, s.Get(task.ID, &task))
	if !strings.Contains(root.Prompt, "revision 1") || !strings.Contains(root.Prompt, "Original paragraph") || task.State != "paused" {
		t.Fatal("feedback lost its anchor or resumed paused work")
	}
	req.Form.Set("revision", "999")
	if err := w.action(req, Page{User: User{ID: "owner"}}); err == nil {
		t.Fatal("invented document revision accepted")
	}
	var wrong Run
	if s.Get(doc.ID, &wrong) == nil {
		t.Fatal("document accepted as run")
	}
}

func TestSubscriptionCannotBeBorrowedFromAnotherHuman(t *testing.T) {
	_, e, task, _ := fixture(t)
	task.ID = "unauthorized"
	task.Creator = "another-human"
	if e.CreateAssignment(task) == nil {
		t.Fatal("used another human's portfolio")
	}
}

func TestSupervisorCannotWaitForeverOnBlockedWorker(t *testing.T) {
	s, e, _, root := fixture(t)
	child := Run{ID: "blocked", Org: root.Org, Task: root.Task, Parent: root.ID, State: "blocked", Title: "Stalled specialist"}
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	if _, err := call(t, e, root, "adc_wait", struct{}{}); err == nil {
		t.Fatal("supervisor silently parked on blocked work")
	}
	must(t, s.Get(root.ID, &root))
	if root.State != "running" {
		t.Fatal("supervisor lost its chance to reassess")
	}
}

func TestChangingRoleCategoryRetiresOldDefault(t *testing.T) {
	s, e, _, root := fixture(t)
	a := Agent{ID: "default-agent", Org: "org", Name: "Role", Description: "Work", Model: "gpt-6-astra", Category: "implementation", Authority: "draft"}
	must(t, SaveAgent(s, a, true))
	a.Category = "review"
	a.Model = "claude-opus-5"
	must(t, SaveAgent(s, a, true))
	if _, err := call(t, e, root, "adc_delegate", map[string]any{"Category": "implementation", "Title": "Implement", "Prompt": "Implement change"}); err == nil {
		t.Fatal("stale category default silently changed the kind of delegated work")
	}
}

func TestPendingDecisionCannotBeBypassedByAnotherCompletion(t *testing.T) {
	s, e, _, root := fixture(t)
	child := Run{ID: "author", Org: root.Org, Task: root.Task, Parent: root.ID, State: "running", Model: "gpt-6-astra"}
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	decision := Decision{ID: "conflict", Org: root.Org, Task: root.Task, Run: child.ID, Kind: "conflict", State: "pending", Question: "Two members gave incompatible scope directions; choose the agreed scope."}
	must(t, s.Put("decision", decision.Org, decision.Task, decision.State, decision.ID, decision))
	if _, err := call(t, e, child, "adc_finish", map[string]any{"Result": "Proceeding with the later instruction"}); err == nil {
		t.Fatal("worker completed past an unresolved conflict")
	}
	_, err := call(t, e, child, "adc_decision", map[string]any{"Question": "May I ignore the first instruction?"})
	must(t, err)
	if len(taskDecisions(s, root.Task)) != 1 {
		t.Fatal("same run created a competing approval request")
	}
}

func TestTeamDecisionHasSelectableWorkspacePacket(t *testing.T) {
	s, e, _, root := fixture(t)
	_, err := call(t, e, root, "adc_decision", map[string]any{"Question": "Approve the proposed team?\n\nEvidence and rationale.", "Kind": "team_proposal", "proposed_agents": []Agent{{Name: "CTO", Model: "gpt-6-astra", Category: "supervision", Authority: "draft", Description: "Own outcomes"}}})
	must(t, err)
	decisions := taskDecisions(s, root.Task)
	if len(decisions) != 1 || decisions[0].Document == "" {
		t.Fatal("proposal document missing")
	}
	var doc Document
	must(t, s.Get(decisions[0].Document, &doc))
	if doc.Content != decisions[0].Question || doc.Task != root.Task || doc.Run != root.ID {
		t.Fatal("proposal lost origin or content")
	}
}
func TestMarkdownKeepsTablesWithoutAcceptingActiveHTML(t *testing.T) {
	output := string(renderMarkdown("| Role | Model |\n| --- | --- |\n| CTO | GPT |\n\n<script>alert('x')</script>\n\n[bad](javascript:alert%281%29)"))
	if !strings.Contains(output, "<table>") || strings.Contains(output, "<script>") || strings.Contains(output, `href="javascript:`) {
		t.Fatal("markdown rendering lost table support or accepted active content")
	}
}
