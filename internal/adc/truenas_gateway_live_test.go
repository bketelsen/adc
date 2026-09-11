package adc

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit opt-in: read the existing connection through a read-only database,
// reseal only that connection into temporary state, and execute two audited
// observations. No working policies, provider settings or NAS data are changed.
func TestLiveTrueNASProtectedGateway(t *testing.T) {
	dir, id := os.Getenv("ADC_LIVE_TRUENAS_DATA"), os.Getenv("ADC_LIVE_TRUENAS_CONNECTION")
	if dir == "" || id == "" {
		t.Skip("explicitly select the previously authorized TrueNAS connection")
	}
	u := url.URL{Scheme: "file", Path: filepath.Join(dir, "adc.db"), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	must(t, err)
	defer db.Close()
	var raw []byte
	must(t, db.QueryRow(`SELECT data FROM records WHERE kind='connection' AND id=?`, id).Scan(&raw))
	var connection Connection
	must(t, json.Unmarshal(raw, &connection))
	key, err := os.ReadFile(filepath.Join(dir, "credential.key"))
	must(t, err)
	source := &Store{key: key}
	s, _, _, _ := fixture(t)
	secrets := []string{}
	for _, values := range []map[string]string{connection.Env, connection.Headers} {
		for key, value := range values {
			plain, err := source.Unseal(value)
			must(t, err)
			secrets = append(secrets, plain)
			values[key], err = s.Seal(plain)
			must(t, err)
		}
	}
	connection.ID, connection.Org = "authorized-nas-observation", "org"
	must(t, s.Put("connection", "org", "", "", connection.ID, connection))
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	tools, err := s.DiscoverGateway(context.Background(), "org", connection.ID)
	must(t, err)
	selected := []GatewayTool{}
	for _, tool := range tools {
		// Source-audited handlers call only system.info and pool.query.
		if tool.Name != "truenas_system_info" && tool.Name != "truenas_pool_list" {
			continue
		}
		must(t, s.SaveToolPolicy("owner", "org", ToolPolicy{Tool: tool.ID, Fingerprint: tool.Fingerprint, Mode: "allow", Class: "read", Constraints: []ArgumentConstraint{{Pointer: "", Allowed: []json.RawMessage{json.RawMessage(`{}`)}}}}, 0))
		selected = append(selected, tool)
	}
	if len(selected) != 2 {
		for _, tool := range tools {
			t.Logf("Discovered tool: %s", tool.Name)
		}
		t.Fatal("audited observation tools unavailable")
	}
	run := taskRuns(s, "task")[0]
	run.Tools = []string{connection.ID}
	run.Execution = "protected"
	must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
	var task Assignment
	must(t, s.Get(run.Task, &task))
	task.Execution = "protected"
	task.Capabilities = s.initialCapabilities("org", run.Tools)
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	for _, tool := range selected {
		result, err := s.CallGateway(context.Background(), run.ID, tool.ID, "qualification-"+tool.Name, json.RawMessage(`{}`))
		must(t, err)
		var response struct {
			IsError bool              `json:"isError"`
			Content []json.RawMessage `json:"content"`
		}
		must(t, json.Unmarshal([]byte(result), &response))
		if response.IsError || len(response.Content) == 0 {
			t.Fatal("observation did not return successful evidence")
		}
		for _, secret := range secrets {
			if len(secret) > 8 && strings.Contains(result, secret) {
				t.Fatal("connection credential exposed")
			}
		}
		t.Logf("%s: successful gateway observation; evidence retained only in temporary fixture", tool.Name)
	}
	for _, tool := range tools {
		if tool.Name == "truenas_system_info" || tool.Name == "truenas_pool_list" {
			continue
		}
		if _, err := s.CallGateway(context.Background(), run.ID, tool.ID, "ungranted-negative", json.RawMessage(`{}`)); err == nil {
			t.Fatal("unclassified tool accepted")
		}
		break
	}
}
