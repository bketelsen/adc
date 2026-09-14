package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// Real public repository and provider work, in a separate qualification store.
// Never manufactures production runs or grants publication authority.
func TestLiveUpdexContributionPilot(t *testing.T) {
	if os.Getenv("ADC_LIVE_UPDEX") != "1" {
		t.Skip("set ADC_LIVE_UPDEX with ADC_UPDEX_SOURCE and pinned admission runtime")
	}
	source := os.Getenv("ADC_UPDEX_SOURCE")
	revision := "373717fe158dcdb8c8411f1083484370f93bb165"
	files := map[string]string{}
	for _, name := range []string{"go.mod", "go.sum", "version/pattern.go", "version/pattern_test.go"} {
		cmd := exec.Command("git", "show", revision+":"+name)
		cmd.Dir = source
		body, err := cmd.Output()
		must(t, err)
		files[name] = string(body)
	}
	s, e, task, owner, area := ownershipFixture(t)
	defer e.Stop()
	task.Title, task.Execution = "Updex public contribution pilot (qualification)", "protected"
	owner.Execution, owner.Tools = "protected", nil
	must(t, s.Batch(Write{"assignment", task.Org, "", task.State, task.ID, task}, Write{"run", owner.Org, owner.Task, owner.State, owner.ID, owner}))
	account := Account{ID: task.Account, User: task.Creator, Local: true, Limit: 2}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	q := ContributionQueue{ID: ID(), Org: task.Org, Area: area.ID, Owner: owner.Agent, Creator: task.Creator, Account: account.ID, Reviewer: "qa", Model: "claude-opus-5", Provider: "copilot", Scope: "Executable public SDK examples for Updex version patterns; no production code, dependencies, release or workflow changes", Source: "https://github.com/frostyard/updex", State: "active", MaxReviews: 3, MaxPackets: 3, Runtime: os.Getenv("ADC_CONTRIBUTION_RUNTIME_ID")}
	if q.Runtime == "" {
		t.Fatal("pinned Go runtime required")
	}
	must(t, s.Put("contribution-queue", q.Org, q.Area, q.State, q.ID, q))
	p, err := s.offerContribution(owner, contributionOffer{Queue: q.ID, Title: "Document the version SDK with executable examples", Outcome: "Help SDK users parse extension filename patterns, extract a version, construct a filename, and sort available versions, through executable Go examples.", Criteria: "Add ONLY version/example_test.go using external package version_test. Provide conventional ExampleParsePattern, ExamplePattern_ExtractVersion, ExamplePattern_BuildFilename, and ExampleSort functions with deterministic Output comments. Handle parse errors and demonstrate match/non-match behavior. Use readable realistic names; Sort is newest first. No production changes, dependencies, release/workflow edits or privilege use. Admission requires go test ./version, go vet ./version and clean gofmt. Installed offline runtime provides exact Go1.26.7 and hashicorp/go-version v1.9.0. The snapshot intentionally contains only this package and root module files: admission checks package scope only. Full repository make ci and node scripts/check-docs.mjs remain required during internal integration before any PR; admission itself cannot publish or declare those wider gates passed.", SourceRevision: revision, Files: files})
	must(t, err)
	_, err = call(t, e, owner, "adc_wait_contribution", map[string]string{"Packet": p.ID})
	must(t, err)
	web := NewWeb(s, e, false)
	web.PublicContributions = true
	server := httptest.NewServer(web.Handler())
	defer server.Close()
	request := func(method, path, receipt string, body any) (json.RawMessage, error) {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(string(raw)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if receipt != "" {
			req.Header.Set("Authorization", "Bearer "+receipt)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		var data json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
			return nil, err
		}
		if response.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, data)
		}
		return data, nil
	}
	raw, err := request("POST", "/public/packets/"+p.ID+"/claim", "", struct{}{})
	must(t, err)
	var claim struct {
		Receipt string
		Packet  PublicPacket
	}
	must(t, json.Unmarshal(raw, &claim))
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	var submitted string
	tool := copilot.DefineTool("submit_candidate", "Submit UTF-8 replacement Files and Summary for this public packet. This is a candidate, not internal approval. End after submission.", func(in struct {
		Summary string
		Files   map[string]*string
	}, _ copilot.ToolInvocation) (string, error) {
		raw, err := request("POST", "/public/packets/"+p.ID+"/submit", claim.Receipt, contributionSubmission{Revision: claim.Packet.Revision, Summary: in.Summary, Model: "gpt-5.6-sol", Files: in.Files})
		if err != nil {
			return "", err
		}
		var status struct{ ID string }
		if err := json.Unmarshal(raw, &status); err != nil {
			return "", err
		}
		submitted = status.ID
		return "Received for independent internal admission. End this contribution.", nil
	})
	client, err := e.executionCopilot(ctx, account, "protected")
	must(t, err)
	available, permission := protectedCopilotTools([]copilot.Tool{tool})
	session, err := client.CreateSession(ctx, &copilot.SessionConfig{Model: "gpt-5.6-sol", Tools: []copilot.Tool{tool}, AvailableTools: available, OnPermissionRequest: permission, EnableConfigDiscovery: copilot.Bool(false), EnableSessionStore: copilot.Bool(false), SystemMessage: &copilot.SystemMessageConfig{Content: "You are an unaffiliated developer contributing to a public source task. Inspect the supplied source and implement only the requested example file. Submit using submit_candidate, then finish. You have no ADC membership, private context or publication authority. Validation will occur independently after submission; do not claim unrun tests passed."}})
	must(t, err)
	defer session.Disconnect()
	packet, _ := json.Marshal(claim.Packet)
	_, err = session.SendAndWait(ctx, copilot.MessageOptions{Prompt: string(packet)})
	must(t, err)
	if submitted == "" {
		t.Fatal("donor did not submit")
	}
	var candidate Contribution
	defer func() {
		_ = s.Get(submitted, &candidate)
		report, _ := json.MarshalIndent(map[string]any{"Environment": "separate qualification store; real public Updex source and real providers", "Packet": p.Public, "Candidate": candidate, "Traces": list[ToolTrace](s, "tooltrace", q.Org)}, "", "  ")
		must(t, os.WriteFile(filepath.Join("../../work", "updex-contribution-pilot.json"), report, 0600))
	}()
	e.dispatchContributions(time.Now())
	must(t, s.Get(submitted, &candidate))
	var reviewTask Assignment
	must(t, s.Get(candidate.Task, &reviewTask))
	for turn := 0; turn < 4; turn++ {
		var reviewer Run
		must(t, s.Get(candidate.Run, &reviewer))
		reviewer.Activations++
		setRunning(t, s, &reviewer)
		e.execute(ctx, reviewer, reviewTask, account)
		must(t, s.Get(submitted, &candidate))
		if candidate.State == "admitted" || candidate.State == "rejected" {
			break
		}
	}
	t.Logf("Admission %s: %s", candidate.State, candidate.Findings)
	if candidate.State != "admitted" {
		t.Fatal("candidate did not pass internal admission")
	}
	e.wakeContributors(time.Now())
	must(t, s.Get(owner.ID, &owner))
	if owner.State != "queued" {
		t.Fatal("accountable owner not resumed", owner.State)
	}
	setRunning(t, s, &owner)
	_, err = call(t, e, owner, "adc_import_contribution", map[string]string{"Contribution": submitted})
	must(t, err)
	must(t, s.Get(task.ID, &task))
	if task.State == "ready" || task.Publication {
		t.Fatal("admission bypassed integration/delivery")
	}
	raw, err = request("GET", "/public/submissions/"+submitted, claim.Receipt, nil)
	must(t, err)
	if !strings.Contains(string(raw), "admitted") {
		t.Fatal("public receipt cannot see result")
	}
	t.Log("PASS: real Sol donation, isolated Opus admission, automatic owner wakeup and protected import; full Updex integration gates still required")
}
