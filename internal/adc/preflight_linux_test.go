//go:build linux

package adc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPreflightProtectedRuntimesResourcesAndRetention(t *testing.T) {
	executorFixture(t)
	s, e, _, root := fixture(t)
	first := root
	first.ID = "resource-one"
	first.Execution = "protected"
	first.State = "queued"
	first.Workspace = filepath.Join(s.Dir, "workspaces", first.ID)
	first.Preflight = &PreflightSpec{Commands: []string{"git", "python3"}, Directories: []string{"database"}, Ports: []string{"http"}}
	must(t, s.Put("run", first.Org, first.Task, first.State, first.ID, first))
	second := first
	second.ID = "resource-two"
	second.Workspace = filepath.Join(s.Dir, "workspaces", second.ID)
	must(t, s.Put("run", second.Org, second.Task, second.State, second.ID, second))
	for _, r := range []Run{first, second} {
		v := checkPreflight(t, e, r)
		if v.State != "ready" {
			t.Fatal(v)
		}
	}
	a, b := s.runResources(first.ID), s.runResources(second.ID)
	if a.Ports["http"] == 0 || a.Ports["http"] == b.Ports["http"] {
		t.Fatal("parallel resources share port", a, b)
	}
	evidence := filepath.Join(first.Workspace, ".adc-test", "database", "evidence.txt")
	must(t, os.WriteFile(evidence, []byte("retain fixture observations"), 0600))
	first.State = "cancelled"
	must(t, s.Put("run", first.Org, first.Task, first.State, first.ID, first))
	s.mu.Lock()
	e.releaseRunResources()
	s.mu.Unlock()
	if s.runResources(first.ID).State != "released" || s.runResources(second.ID).State != "owned" {
		t.Fatal("resource cleanup crossed ownership")
	}
	if _, err := os.Stat(evidence); err != nil {
		t.Fatal("cleanup removed evidence", err)
	}
	first.State = "queued"
	must(t, s.Put("run", first.Org, first.Task, first.State, first.ID, first))
	if v := checkPreflight(t, e, first); v.State != "ready" {
		t.Fatal("resumed work could not reacquire resources", v)
	}
	if s.runResources(first.ID).Ports["http"] == s.runResources(second.ID).Ports["http"] || s.runResources(first.ID).State != "owned" {
		t.Fatal("reacquisition collided with another owner")
	}
	if len(list[RunResources](s, "run-resource-history", first.Org)) != 1 {
		t.Fatal("released resource history lost")
	}
	missing := second
	missing.Preflight = &PreflightSpec{Commands: []string{"adc-fixture-missing-runtime"}}
	checks := e.probeReadiness(context.Background(), missing, nil)
	if len(checks) != 1 || checks[0].State != "blocked" {
		t.Fatal("missing runtime accepted", checks)
	}
}
func TestPreflightRepositoryScopeAndRevocation(t *testing.T) {
	rig := githubFixture(t)
	worker := rig.worker
	worker.State = "queued"
	must(t, rig.s.Put("run", worker.Org, worker.Task, worker.State, worker.ID, worker))
	var agent Agent
	must(t, rig.s.Get(worker.Agent, &agent))
	agent.Tools = worker.Tools
	must(t, rig.s.Put("agent", agent.Org, "", "", agent.ID, agent))
	repo := RepositoryPreflight{Connection: "github", Owner: "fixture", Repository: "project", Write: true}
	must(t, rig.e.probeRepository(context.Background(), worker, repo))
	outside := repo
	outside.Repository = "other"
	if rig.e.probeRepository(context.Background(), worker, outside) == nil {
		t.Fatal("repository scope bypassed")
	}
	tool := rig.tools["github_repository"]
	var policy ToolPolicy
	must(t, rig.s.Get("policy-"+tool.ID, &policy))
	policy.Mode = "deny"
	must(t, rig.s.SaveToolPolicy("owner", worker.Org, policy, policy.Revision))
	if rig.e.probeRepository(context.Background(), worker, repo) == nil {
		t.Fatal("revoked metadata probe accepted")
	}
}
