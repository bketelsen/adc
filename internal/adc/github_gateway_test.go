//go:build linux

package adc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type githubTestRig struct {
	s              *Store
	e              *Engine
	worker, caller Run
	tools          map[string]GatewayTool
	remote         string
	posts, patches atomic.Int32
	drop           atomic.Bool
	closed         atomic.Bool
	branch         string
	title, body    string
	beforePull     func()
}

func gitFixtureCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func githubFixture(t *testing.T) *githubTestRig {
	t.Helper()
	executorFixture(t)
	s, e, task, root := fixture(t)
	rig := &githubTestRig{s: s, e: e, caller: root, tools: map[string]GatewayTool{}}
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused');INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	seed := t.TempDir()
	gitFixtureCommand(t, seed, "init", "-q", "-b", "main")
	must(t, os.WriteFile(filepath.Join(seed, "file.txt"), []byte("initial fixture\n"), 0600))
	gitFixtureCommand(t, seed, "add", ".")
	gitFixtureCommand(t, seed, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Fixture base")
	rig.remote = filepath.Join(t.TempDir(), "private.git")
	gitFixtureCommand(t, seed, "clone", "--bare", seed, rig.remote)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-github-secret" {
			http.Error(w, "missing fixture credentials", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		base := "/repos/fixture/project"
		switch {
		case r.URL.Path == base:
			json.NewEncoder(w).Encode(map[string]any{"full_name": "fixture/project", "private": true, "permissions": map[string]bool{"pull": true, "push": true}, "default_branch": "main"})
		case strings.HasPrefix(r.URL.Path, base+"/commits/"):
			sha := gitFixtureCommand(t, rig.remote, "rev-parse", "refs/heads/main")
			json.NewEncoder(w).Encode(map[string]string{"sha": sha})
		case r.URL.Path == base+"/pulls" && r.Method == "POST":
			var input struct {
				Title, Body, Head, Base string
				Draft                   bool
			}
			if json.NewDecoder(r.Body).Decode(&input) != nil || !input.Draft || input.Base != "main" {
				http.Error(w, "wrong draft payload", 400)
				return
			}
			if rig.posts.Load() > 0 {
				http.Error(w, "already exists", 422)
				return
			}
			rig.branch, rig.title, rig.body = input.Head, input.Title, input.Body
			rig.posts.Add(1)
			if rig.drop.Swap(false) {
				http.Error(w, "fixture lost POST response after creating draft", 503)
				return
			}
			json.NewEncoder(w).Encode(map[string]int{"number": 1})
		case r.URL.Path == base+"/pulls" && r.Method == "GET":
			if rig.beforePull != nil {
				rig.beforePull()
			}
			if rig.posts.Load() == 0 {
				json.NewEncoder(w).Encode([]any{})
				return
			}
			sha := gitFixtureCommand(t, rig.remote, "rev-parse", "refs/heads/"+rig.branch)
			json.NewEncoder(w).Encode([]any{map[string]any{"number": 1, "state": map[bool]string{false: "open", true: "closed"}[rig.closed.Load()], "draft": true, "html_url": "https://github.example/fixture/project/pull/1", "head": map[string]any{"ref": rig.branch, "sha": sha, "repo": map[string]string{"full_name": "fixture/project"}}, "base": map[string]string{"ref": "main"}}})
		case r.URL.Path == base+"/pulls/1" && r.Method == "PATCH":
			rig.patches.Add(1)
			json.NewEncoder(w).Encode(map[string]int{"number": 1})
		default:
			http.Error(w, "unexpected fixture request", 404)
		}
	}))
	t.Cleanup(server.Close)
	s.githubFixture = &githubFixtureConfig{API: server.URL, Remote: func(owner, repo string) string {
		if owner != "fixture" || repo != "project" {
			t.Error("scope escaped")
		}
		return rig.remote
	}}
	secret, err := s.Seal("synthetic-github-secret")
	must(t, err)
	c := Connection{ID: "github", Org: task.Org, Name: "Synthetic GitHub", Transport: "github", Args: []string{"fixture/project"}, Headers: map[string]string{"GitHubToken": secret}}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	tools, err := s.DiscoverGateway(context.Background(), c.Org, c.ID)
	must(t, err)
	for _, tool := range tools {
		rig.tools[tool.Name] = tool
		class := "read"
		if tool.Name == "github_draft_pr" {
			class = "change"
		}
		must(t, s.SaveToolPolicy("owner", task.Org, ToolPolicy{Tool: tool.ID, Fingerprint: tool.Fingerprint, Class: class, Mode: "allow"}, 0))
	}
	task.Execution = "protected"
	task.Publication = true
	task.Capabilities = s.initialCapabilities(task.Org, []string{c.ID})
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	rig.caller.Execution = "protected"
	rig.caller.Tools = []string{c.ID}
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, rig.caller))
	rig.worker = Run{ID: "github-author", Org: task.Org, Task: task.ID, Parent: root.ID, Agent: "dev", Category: "implementation", Model: "gpt-5.6-sol", Family: "openai-gpt", State: "running", Execution: "protected", Tools: []string{c.ID}, Workspace: filepath.Join(s.Dir, "work", "github-author")}
	must(t, s.Put("run", task.Org, task.ID, rig.worker.State, rig.worker.ID, rig.worker))
	return rig
}
func (r *githubTestRig) call(t *testing.T, run Run, name, operation string, args any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return r.s.CallGateway(context.Background(), run.ID, r.tools[name].ID, operation, raw)
}
func (r *githubTestRig) prepare(t *testing.T) githubArguments {
	t.Helper()
	text, err := r.call(t, r.worker, "github_fetch", "fetch-author", map[string]any{"Owner": "fixture", "Repository": "project", "Ref": "main"})
	must(t, err)
	var result struct {
		Structured struct{ Data struct{ Path, Commit string } } `json:"structuredContent"`
	}
	must(t, json.Unmarshal([]byte(text), &result))
	x, err := runExecutor(r.worker)
	must(t, err)
	command := "cd " + shellQuote(result.Structured.Data.Path) + " && printf 'reviewed fixture\\n' > file.txt && git add file.txt && git -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm 'Reviewed fixture change'"
	out, err := x.Execute(context.Background(), workspaceCommand{Command: command})
	must(t, err)
	if out.ExitCode != 0 {
		t.Fatal(out.Output)
	}
	code, err := r.e.captureCode(&r.worker, result.Structured.Data.Path)
	must(t, err)
	r.worker.State = "complete"
	r.worker.Result = "Fixture code passes isolated checks"
	must(t, r.s.Put("run", r.worker.Org, r.worker.Task, r.worker.State, r.worker.ID, r.worker))
	// Synthetic review record for deterministic backend qualification. The new
	// application code separately receives real independent Claude review.
	review := Review{ID: ID(), Org: r.worker.Org, Task: r.worker.Task, Run: "fixture-review", Target: r.worker.ID, Revision: r.e.revision(r.worker), Model: "claude-opus-5", Family: "anthropic-claude", Verdict: "pass", Findings: "Synthetic fixture review of registered commit"}
	must(t, r.s.Put("review", review.Org, review.Task, review.Verdict, review.ID, review))
	return githubArguments{Owner: "fixture", Repository: "project", SourceRun: r.worker.ID, Commit: code.Commit, Ref: "main", Title: "Reviewed fixture", Body: "Synthetic delivery qualification"}
}
func TestGitHubPrivateFetchExactReviewedDraftAndCredentialBoundary(t *testing.T) {
	rig := githubFixture(t)
	args := rig.prepare(t)
	result, err := rig.call(t, rig.caller, "github_draft_pr", "deliver", args)
	must(t, err)
	if !strings.Contains(result, args.Commit) || !strings.Contains(result, "github.example") || rig.posts.Load() != 1 {
		t.Fatal("exact reviewed draft missing", result)
	}
	again, err := rig.call(t, rig.caller, "github_draft_pr", "deliver", args)
	must(t, err)
	if again != result || rig.posts.Load() != 1 || rig.patches.Load() != 0 {
		t.Fatal("delivery replay mutated remote")
	}
	if gitFixtureCommand(t, rig.remote, "rev-parse", "refs/heads/"+rig.branch) != args.Commit {
		t.Fatal("published different commit")
	}
	for _, dir := range []string{rig.worker.Workspace, rig.worker.Workspace + ".home", rig.caller.Workspace} {
		must(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if bytes.Contains(b, []byte("synthetic-github-secret")) {
					return fmt.Errorf("credential entered worker files")
				}
			}
			return nil
		}))
	}
	left, err := filepath.Glob(filepath.Join(rig.s.Dir, "github-*"))
	must(t, err)
	if len(left) != 0 {
		t.Fatal("trusted credential workspace not cleaned up", left)
	}
}
func TestGitHubDeliveryReconcilesLostResponseWithoutDuplicatePR(t *testing.T) {
	rig := githubFixture(t)
	args := rig.prepare(t)
	rig.drop.Store(true)
	if _, err := rig.call(t, rig.caller, "github_draft_pr", "deliver", args); err == nil {
		t.Fatal("fixture should lose first response")
	}
	if rig.posts.Load() != 1 {
		t.Fatal("fixture did not create draft before lost response")
	}
	reopened, err := Open(rig.s.Dir)
	must(t, err)
	defer reopened.Close()
	reopened.githubFixture = rig.s.githubFixture
	rig.s = reopened
	rig.e = NewEngine(reopened)
	result, err := rig.call(t, rig.caller, "github_draft_pr", "deliver", args)
	must(t, err)
	if rig.posts.Load() != 1 || !strings.Contains(result, args.Commit) {
		t.Fatal("recovery duplicated draft or lost commit")
	}
	if len(list[GatewayOperation](reopened, "gateway-operation-history", rig.caller.Org)) != 1 {
		t.Fatal("uncertain operation history missing")
	}
}
func TestGitHubDeliveryRejectsScopeUnreviewedAndChangedArtifacts(t *testing.T) {
	rig := githubFixture(t)
	args := rig.prepare(t)
	bad := args
	bad.Repository = "unapproved"
	if _, err := rig.call(t, rig.caller, "github_draft_pr", "wrong-repo", bad); err == nil {
		t.Fatal("repository allowlist bypassed")
	}
	bad = args
	bad.Commit = strings.Repeat("a", 40)
	if _, err := rig.call(t, rig.caller, "github_draft_pr", "wrong-commit", bad); err == nil {
		t.Fatal("wrong commit accepted")
	}
	var task Assignment
	must(t, rig.s.Get(rig.caller.Task, &task))
	task.Publication = false
	must(t, rig.s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if _, err := rig.call(t, rig.caller, "github_draft_pr", "no-publication", args); err == nil {
		t.Fatal("publication scope bypassed")
	}
	task.Publication = true
	must(t, rig.s.Put("assignment", task.Org, "", task.State, task.ID, task))
	rig.worker.Result = "changed after review"
	must(t, rig.s.Put("run", rig.worker.Org, rig.worker.Task, rig.worker.State, rig.worker.ID, rig.worker))
	if _, err := rig.call(t, rig.caller, "github_draft_pr", "stale-review", args); err == nil {
		t.Fatal("stale review accepted")
	}
	if rig.posts.Load() != 0 {
		t.Fatal("rejected delivery reached GitHub")
	}
}

func TestGitHubDeliveryPreservesExternalBranchAndClosedPull(t *testing.T) {
	for _, condition := range []string{"changed", "deleted", "closed"} {
		t.Run(condition, func(t *testing.T) {
			rig := githubFixture(t)
			args := rig.prepare(t)
			_, err := rig.call(t, rig.caller, "github_draft_pr", "first", args)
			must(t, err)
			switch condition {
			case "changed":
				gitFixtureCommand(t, rig.remote, "update-ref", "refs/heads/"+rig.branch, gitFixtureCommand(t, rig.remote, "rev-parse", "refs/heads/main"))
			case "deleted":
				gitFixtureCommand(t, rig.remote, "update-ref", "-d", "refs/heads/"+rig.branch)
			case "closed":
				rig.closed.Store(true)
			}
			if _, err := rig.call(t, rig.caller, "github_draft_pr", "reconcile-external-state", args); err == nil {
				t.Fatal("external branch or closed pull overwritten")
			}
			if rig.posts.Load() != 1 || rig.patches.Load() != 0 {
				t.Fatal("rejected reconciliation mutated pull")
			}
		})
	}
}
func TestGitHubFetchReplayBelongsToItsWorker(t *testing.T) {
	rig := githubFixture(t)
	args := map[string]string{"Owner": "fixture", "Repository": "project", "Ref": "main"}
	_, err := rig.call(t, rig.worker, "github_fetch", "fetch-owned", args)
	must(t, err)
	if _, err := rig.call(t, rig.caller, "github_fetch", "fetch-owned", args); err == nil || !strings.Contains(err.Error(), "another worker") {
		t.Fatal("cross-worker fetch replay returned unavailable private workspace", err)
	}
}
func TestGitHubChecksPaginationAndIncompleteObservation(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		t.Run(fmt.Sprint(truncated), func(t *testing.T) {
			var pages atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pages.Add(1)
				count := 100
				if r.URL.Query().Get("page") == "2" {
					count = 1
					if truncated {
						count = 0
					}
				}
				checks := []any{}
				for i := 0; i < count; i++ {
					checks = append(checks, map[string]any{"name": fmt.Sprint(i), "status": "completed"})
				}
				json.NewEncoder(w).Encode(map[string]any{"total_count": 101, "check_runs": checks})
			}))
			defer server.Close()
			g := &githubGateway{api: server.URL, client: server.Client()}
			result, err := g.checks(context.Background(), "/repos/fixture/project", "abc")
			if truncated {
				if err == nil {
					t.Fatal("partial CI reported as complete")
				}
				return
			}
			must(t, err)
			if pages.Load() != 2 || result.(map[string]any)["total_count"] != 101 {
				t.Fatal(result)
			}
		})
	}
}
func TestGitHubDeliveryLeaseAndPersistedErrorRedaction(t *testing.T) {
	rig := githubFixture(t)
	g := &githubGateway{store: rig.s, token: "synthetic-github-secret"}
	v := GitHubDelivery{ID: "fixture-lease", Org: rig.worker.Org, Task: rig.worker.Task, State: "in-flight", Generation: 2, Error: "synthetic-github-secret"}
	must(t, g.saveDelivery(v))
	var stored GitHubDelivery
	must(t, rig.s.Get(v.ID, &stored))
	if strings.Contains(stored.Error, g.token) {
		t.Fatal("credential persisted in delivery error")
	}
	stale := v
	stale.Generation = 1
	if g.ownsDelivery(stale) == nil {
		t.Fatal("stale delivery owns newer attempt")
	}
	must(t, g.ownsDelivery(v))
}

func TestGitHubDeliveryRevocationBeforePushRecordsNoMutation(t *testing.T) {
	rig := githubFixture(t)
	args := rig.prepare(t)
	rig.beforePull = func() {
		tool := rig.tools["github_draft_pr"]
		var policy ToolPolicy
		must(t, rig.s.Get("policy-"+tool.ID, &policy))
		policy.Mode = "deny"
		must(t, rig.s.SaveToolPolicy("owner", rig.caller.Org, policy, policy.Revision))
	}
	if _, err := rig.call(t, rig.caller, "github_draft_pr", "revoked", args); err == nil {
		t.Fatal("revoked delivery proceeded")
	}
	branch := "adc/" + digest(rig.caller.Task + ":" + rig.worker.ID)[:32]
	if err := exec.Command("git", "-C", rig.remote, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run(); err == nil {
		t.Fatal("pushed after policy revocation")
	}
	var operation GatewayOperation
	must(t, rig.s.Get("operation-"+digest(rig.caller.Task+":revoked"), &operation))
	if operation.State != "failed" {
		t.Fatal("pre-mutation rejection reported uncertain", operation.State)
	}
	for _, delivery := range list[GitHubDelivery](rig.s, "github-delivery", rig.caller.Org) {
		if delivery.State != "failed" {
			t.Fatal(delivery.State)
		}
	}
}
