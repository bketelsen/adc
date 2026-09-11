package adc

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBrowserPermissionReviewWorkflow(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for isolated permission UI qualification")
	}
	s, _, conn, calls := gatewayFixture(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("ui-fixture-password"), bcrypt.MinCost)
	must(t, err)
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','UI fixture','fixture',?);INSERT INTO memberships VALUES('owner','org')`, hash)
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
	request, err := s.RequestAccess(run.ID, "Inspect the selected fixture NAS", []AccessWant{{Tool: tools[0].ID, Operation: "fixture-inventory", Arguments: json.RawMessage(`{"target":"fixture-nas"}`)}})
	must(t, err)
	e := NewEngine(s)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/permissions-check.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	var updated AccessRequest
	must(t, s.Get(request.ID, &updated))
	if updated.State != "approved" || updated.Revision != 2 {
		t.Fatal("edited request not approved")
	}
	var policy ToolPolicy
	must(t, s.Get("policy-"+tools[0].ID, &policy))
	if policy.Mode != "allow" || policy.Class != "read" {
		t.Fatal("bulk policy not recorded")
	}
	var org Organization
	must(t, s.Get("org", &org))
	if org.Execution != "protected" {
		t.Fatal("default not saved")
	}
	if calls.Load() != 0 {
		t.Fatal("review UI executed an MCP operation")
	}
}
