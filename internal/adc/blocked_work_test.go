package adc

import (
	"encoding/json"
	"strings"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
)

func TestBlockedWorkerReturnsToSupervisorWithoutReviewAndRecovers(t *testing.T) {
	s, e, task, root := fixture(t)
	result, err := call(t, e, root, "adc_delegate", map[string]any{"Agent": "dev", "Title": "Inventory", "Prompt": "Observe storage"})
	must(t, err)
	var dev Run
	must(t, json.Unmarshal([]byte(result), &dev))
	setRunning(t, s, &dev)
	// Preserve diagnostic evidence without treating it as a finished deliverable.
	_, err = call(t, e, dev, "adc_document", map[string]any{"Title": "Attempt evidence", "Content": "Connection unavailable"})
	must(t, err)
	root.State = "waiting"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	_, err = call(t, e, dev, "adc_blocked", map[string]any{"Reason": "MCP not attached", "Needed": "Grant the configured connection"})
	must(t, err)
	must(t, s.Get(dev.ID, &dev))
	must(t, s.Get(root.ID, &root))
	if dev.State != "blocked" || root.State != "queued" || len(e.reviewNeeds(task.ID)) != 0 || len(taskDecisions(s, task.ID)) != 0 {
		t.Fatal("blocker became review work or premature human decision")
	}
	setRunning(t, s, &root)
	if _, err = call(t, e, root, "adc_delegate", map[string]any{"Agent": "qa", "Title": "Review failure", "Prompt": "Approve failure", "ReviewOf": dev.ID}); err == nil {
		t.Fatal("review of blocked attempt accepted")
	}
	if _, err = call(t, e, root, "adc_finish", map[string]any{"Result": "Failure report reviewed"}); err == nil {
		t.Fatal("blocked objective became ready")
	}
	// The supervisor can recover routinely, and actual completion still needs QA.
	_, err = e.Reassign(root, dev.ID, "dev", "Connection restored; perform observations")
	must(t, err)
	setRunning(t, s, &dev)
	_, err = call(t, e, dev, "adc_finish", map[string]any{"Result": "Inventory obtained and verified"})
	must(t, err)
	if len(e.reviewNeeds(task.ID)) != 1 {
		t.Fatal("recovered output escaped review")
	}
	if _, err = call(t, e, root, "adc_finish", map[string]any{"Result": "Inventory complete"}); err == nil {
		t.Fatal("review gate bypassed")
	}
}

func TestRootBlockerYieldsAndCreatesOneConcreteHumanDecision(t *testing.T) {
	s, e, task, root := fixture(t)
	yielded := ""
	for _, tool := range e.providerTools(root, Redactor{}, func(id string) { yielded = id }) {
		if tool.Name != "adc_blocked" {
			continue
		}
		result, err := tool.Handler(copilot.ToolInvocation{ToolCallID: "blocked", Arguments: map[string]any{"Reason": "No role can access storage", "Needed": "Administrator grant"}})
		must(t, err)
		if result.ResultType != "success" {
			t.Fatal(result)
		}
	}
	must(t, s.Get(root.ID, &root))
	must(t, s.Get(task.ID, &task))
	if yielded != "blocked" || root.State != "blocked" || task.State != "needs input" || len(taskDecisions(s, task.ID)) != 1 {
		t.Fatal("root blocker did not persist/yield/escalate")
	}
	if _, err := call(t, e, root, "adc_blocked", map[string]any{"Reason": "Again", "Needed": "Grant"}); err == nil || len(taskDecisions(s, task.ID)) != 1 {
		t.Fatal("inactive run duplicated blocker")
	}
}

func TestRequiredConnectionPreflightAndCatalogDoNotGrantOrExposeSecrets(t *testing.T) {
	s, e, _, root := fixture(t)
	conn := Connection{ID: "storage", Org: root.Org, Name: "TrueNAS", Command: "private-command", URL: "private-address", Env: map[string]string{"KEY": "sealed-secret"}}
	must(t, s.Put("connection", conn.Org, "", "", conn.ID, conn))
	must(t, s.Put("connection", "other", "", "", "foreign", Connection{ID: "foreign", Org: "other", Name: "Foreign"}))
	var qa Agent
	must(t, s.Get("qa", &qa))
	args := map[string]any{"Agent": "qa", "Title": "Observe", "Prompt": "Read storage", "RequiredTools": []string{"storage"}}
	if _, err := call(t, e, root, "adc_delegate", args); err == nil || !strings.Contains(err.Error(), "not granted") {
		t.Fatal("tool-less worker started")
	}
	if len(taskRuns(s, root.Task)) != 1 {
		t.Fatal("failed preflight created run")
	}
	qa.Tools = []string{"storage"}
	must(t, s.Put("agent", qa.Org, "", "", qa.ID, qa))
	result, err := call(t, e, root, "adc_delegate", args)
	must(t, err)
	var child Run
	must(t, json.Unmarshal([]byte(result), &child))
	if len(child.Tools) != 1 || len(child.RequiredTools) != 1 {
		t.Fatal("required access lost")
	}
	root.Tools = nil
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	if _, err = call(t, e, root, "adc_delegate", args); err == nil {
		t.Fatal("idempotent handoff bypassed parent access check")
	}
	if _, err = e.Reassign(root, child.ID, "qa", "Try again"); err == nil {
		t.Fatal("reassignment dropped required access")
	}
	status, err := call(t, e, root, "adc_status", map[string]any{})
	must(t, err)
	for _, secret := range []string{"sealed-secret", "private-command", "private-address", "Foreign"} {
		if strings.Contains(status, secret) {
			t.Fatal("catalog leaked configuration")
		}
	}
	access := connectionAccess(s, root)
	if len(access) != 1 || access[0].Granted || access[0].Name != "TrueNAS" {
		t.Fatal("catalog is not scoped to org/current grants")
	}
}
