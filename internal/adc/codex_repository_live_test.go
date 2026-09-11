//go:build linux

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
)

// Real provider/tool qualification in a disposable repository. No remote push,
// real organization edits, or infrastructure calls are part of this fixture.
func TestLiveCodexMCPRepositoryReview(t *testing.T)        { qualifyLiveMCPRepository(t, false) }
func TestLiveDirectClaudeMCPRepositoryReview(t *testing.T) { qualifyLiveMCPRepository(t, true) }
func TestLiveProtectedMCPRepositoryReview(t *testing.T) {
	qualifyLiveMCPRepository(t, true, "protected")
}
func TestLiveProtectedAccessRestartRepositoryReview(t *testing.T) {
	qualifyLiveMCPRepository(t, true, "protected", "request-and-restart")
}
func qualifyLiveMCPRepository(t *testing.T, directClaude bool, execution ...string) {
	requestAndRestart := len(execution) > 1 && execution[1] == "request-and-restart"
	protected := len(execution) > 0 && execution[0] == "protected"
	home, node := os.Getenv("ADC_LIVE_CODEX_HOME"), os.Getenv("ADC_LIVE_MCP_NODE")
	if home == "" || node == "" {
		t.Skip("set ADC_LIVE_CODEX_HOME and ADC_LIVE_MCP_NODE; uses explicitly connected Codex and signed-in Copilot")
	}
	reviewProvider := "copilot"
	if directClaude {
		if os.Getenv("ADC_LIVE_CLAUDE_HOME") == "" {
			t.Skip("connect a private ADC Claude account and set ADC_LIVE_CLAUDE_HOME")
		}
		reviewProvider = "claude"
	}
	s, e, seed, seedRun := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	client, err := startCodex(ctx, home)
	must(t, err)
	e.codexClients["code-account"] = client
	defer func() { e.Stop() }()
	if directClaude {
		cc, err := startClaude(ctx, os.Getenv("ADC_LIVE_CLAUDE_HOME"))
		must(t, err)
		e.codexClients["account"] = cc
	}
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	for _, a := range []Account{{ID: "account", User: "owner", Provider: reviewProvider, Local: !directClaude, Limit: 2}, {ID: "code-account", User: "owner", Provider: "codex", Limit: 2}} {
		must(t, s.Put("account", "", a.User, "", a.ID, a))
	}
	reviewModel := "claude-opus-5"
	if directClaude {
		var account Account
		must(t, s.Get("account", &account))
		reviewModel = liveClaudeReviewModel(t, e, account, ctx)
	}
	for _, a := range list[Agent](s, "agent", seed.Org) {
		a.Tools = []string{"storage"}
		if Family(a.Model) == "openai-gpt" {
			a.Provider, a.Model = "codex", "gpt-5.6-sol"
		} else {
			a.Provider, a.Model = reviewProvider, reviewModel
		}
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	privateSeed := ID()
	sealed, err := s.Seal(privateSeed)
	must(t, err)
	script, err := filepath.Abs("testdata/repository-inventory.cjs")
	must(t, err)
	conn := Connection{ID: "storage", Org: seed.Org, Name: "repository-inventory", Transport: "stdio", Command: node, Args: []string{script}, Env: map[string]string{"ADC_FIXTURE_SEED": sealed}}
	must(t, s.Put("connection", conn.Org, "", "", conn.ID, conn))
	if protected {
		discovered, err := s.DiscoverGateway(ctx, conn.Org, conn.ID)
		must(t, err)
		if len(discovered) != 1 {
			t.Fatal("fixture MCP tool missing")
		}
		mode := "allow"
		if requestAndRestart {
			mode = "approval"
		}
		must(t, s.SaveToolPolicy("owner", conn.Org, ToolPolicy{Tool: discovered[0].ID, Fingerprint: discovered[0].Fingerprint, Mode: mode, Class: "read"}, 0))
	}

	repo := t.TempDir()
	files := map[string]string{
		"AGENTS.md":      "Synthetic qualification repository. Modify only suites.py and inventory.json. Preserve tests and this file. Run python3 -B -m unittest -v before committing. Use a dedicated branch in a clone in your ADC workspace. Never push or contact any remote service. Commit and register the clean repository with adc_code. Reviewer: independently inspect the exact registered commit, run the gate, and compare inventory.json to a fresh MCP inventory call.\n",
		"suites.py":      "def supported_suites(inventory):\n    return ['main']\n",
		"inventory.json": "{\"fixture\":true,\"inventory_id\":\"stale\",\"suites\":[\"main\"]}\n",
		"test_suites.py": "import json, unittest\nfrom suites import supported_suites\n\nclass SuiteTests(unittest.TestCase):\n    def test_current_inventory(self):\n        with open('inventory.json') as f:\n            inventory = json.load(f)\n        self.assertEqual(set(inventory['suites']), {'trixie', 'forky'})\n        self.assertEqual(supported_suites(inventory), ['forky', 'trixie'])\n    def test_empty(self):\n        self.assertEqual(supported_suites({'suites': []}), [])\n    def test_deduplicate(self):\n        self.assertEqual(supported_suites({'suites': ['trixie', 'forky', 'trixie']}), ['forky', 'trixie'])\n",
	}
	if protected {
		files["AGENTS.md"] = strings.ReplaceAll(files["AGENTS.md"], "Never push or contact any remote service.", "Never push or contact external services; the supplied local fixture HTTP repository is allowed.")
	}
	for name, text := range files {
		must(t, os.WriteFile(filepath.Join(repo, name), []byte(text), 0600))
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"config", "user.name", "ADC fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "."}, {"commit", "--quiet", "-m", "Seed synthetic suite bug"}} {
		_, err = gitOutput(repo, args...)
		must(t, err)
	}
	baseline, err := inspectCode(repo)
	must(t, err)
	seed.State, seedRun.State = "ready", "complete"
	must(t, s.Put("assignment", seed.Org, "", seed.State, seed.ID, seed))
	must(t, s.Put("run", seedRun.Org, seedRun.Task, seedRun.State, seedRun.ID, seedRun))
	source := repo
	if protected {
		bare := t.TempDir()
		_, err = gitOutput(repo, "clone", "--bare", repo, bare)
		must(t, err)
		_, err = gitOutput(bare, "update-server-info")
		must(t, err)
		localGit := httptest.NewServer(http.FileServer(http.Dir(bare)))
		defer localGit.Close()
		source = localGit.URL
	}
	prompt := fmt.Sprintf("Synthetic MCP/repository qualification. Local seed repository: %s. Supervisor: delegate to dev with RequiredTools storage. Developer: clone the seed into your own ADC workspace, use a dedicated branch, read AGENTS.md, fetch fresh inventory through the granted repository-inventory MCP tool, save its complete JSON to inventory.json, and fix supported_suites to return sorted distinct suites from its input (including empty input). Preserve tests and AGENTS.md. Run the gate, commit with local fixture identity (ADC fixture, fixture@example.invalid), and register the clean clone with adc_code before finishing. Do not change the seed repository. Supervisor: obtain independent review by qa of the completed author's actual commit using ReviewOf, then finish after PASS. Reviewer must inspect the registered commit, run python3 -B -m unittest -v in that repository, and independently call MCP to compare inventory.json. The public inventory_id is evidence, not a credential. Only use ADC, granted fixture MCP, and local shell/file/git tools. No web, pushes, GitHub operations, installation changes, or real infrastructure. No human decision needed. Use adc_wait for pending work. No need to create an extra document.", source)
	task := Assignment{ID: ID(), Org: seed.Org, Creator: "owner", Owner: "boss", Account: "code-account", ExtraAccount: "account", Title: "Synthetic Codex MCP and repository qualification", Prompt: prompt}
	if protected {
		task.Execution = "protected"
		task.Title = "Synthetic protected MCP and independent repository review"
		task.Prompt += "\nPROTECTED EXECUTION: the seed URL is a local synthetic HTTP Git fixture and cloning it through adc_workspace is authorized. Use adc_tool_catalog with Connection=storage and adc_call_tool for inventory; it is already approved for the assignment. Each independent observation has its own stable Operation ID. Use adc_workspace for ALL shell/file/test/Git work. Register with adc_code using /workspace paths. The reviewer must use adc_checkout_code to copy the author's registered commit into its own workspace before independently checking it. Historical host paths are intentionally inaccessible. No extra access approval is needed for this fixture."
	}
	if requestAndRestart {
		task.Prompt = strings.ReplaceAll(task.Prompt, "No human decision needed.", "")
		task.Prompt = strings.ReplaceAll(task.Prompt, "it is already approved for the assignment", "it requires one initial access approval")
		task.Prompt = strings.ReplaceAll(task.Prompt, "No extra access approval is needed for this fixture.", "Supervisor: BEFORE delegating, discover the inventory tool then call adc_request_access for that tool with Arguments={} and Purpose=Authorize this fixture inventory for development and independent review. This single initial capability request is expected. After approval, continue normally without requesting the same capability again. The human fixture approves assignment access once; that covers independent inventory observations, corrections and review.")
	}
	must(t, e.CreateAssignment(task))
	e.Start(ctx)
	approvedRequests := 0
	approvedAt := ""
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, r := range taskRuns(s, task.ID) {
				t.Logf("%s: %s %s", r.Title, r.State, r.Error)
			}
			t.Fatal("MCP/repository qualification timed out")
		case <-ticker.C:
			must(t, s.Get(task.ID, &task))
			if requestAndRestart {
				pending := []AccessRequest{}
				for _, request := range list[AccessRequest](s, "access-request", task.Org) {
					if request.Task == task.ID && request.State == "pending" {
						pending = append(pending, request)
					}
				}
				if len(pending) > 0 {
					if approvedRequests != 0 || len(pending) != 1 {
						t.Fatal("routine continuation produced repeated access approvals")
					}
					e.Stop()
					reopened, err := Open(s.Dir)
					must(t, err)
					t.Cleanup(func() { reopened.Close() })
					s = reopened
					must(t, s.ResolveAccess("owner", task.Org, pending[0].ID, pending[0].Revision, "assignment", "Approve synthetic inventory for this assignment"))
					e = NewEngine(s)
					codeClient, err := startCodex(ctx, home)
					must(t, err)
					e.codexClients["code-account"] = codeClient
					reviewClient, err := startClaude(ctx, os.Getenv("ADC_LIVE_CLAUDE_HOME"))
					must(t, err)
					e.codexClients["account"] = reviewClient
					approvedRequests++
					approvedAt = now()
					e.Start(ctx)
					t.Log("Reopened persisted state, approved one capability bundle, and restarted both provider adapters")
					continue
				}
			}
			if task.State == "needs input" {
				for _, d := range taskDecisions(s, task.ID) {
					t.Log(d.Question)
				}
				t.Fatal("routine qualification required human continuation")
			}
			if task.State != "ready" {
				continue
			}
			if requestAndRestart && approvedRequests != 1 {
				t.Fatal("missing restart/approval qualification")
			}
			var author Run
			for _, r := range taskRuns(s, task.ID) {
				if r.Agent == "dev" {
					author = r
				}
			}
			if author.Provider != "codex" || len(author.Code) != 1 {
				t.Fatal("missing Codex code artifact")
			}
			must(t, verifyCode(author))
			artifact := author.Code[0]
			if artifact.Commit == baseline.Commit {
				t.Fatal("no implementation commit")
			}
			for _, name := range []string{"AGENTS.md", "test_suites.py"} {
				b, err := os.ReadFile(filepath.Join(artifact.Path, name))
				must(t, err)
				if string(b) != files[name] {
					t.Fatalf("modified required gate %s", name)
				}
			}
			b, err := os.ReadFile(filepath.Join(artifact.Path, "inventory.json"))
			must(t, err)
			var inventory struct {
				InventoryID string `json:"inventory_id"`
			}
			must(t, json.Unmarshal(b, &inventory))
			if inventory.InventoryID != digest(privateSeed) {
				t.Fatal("fresh MCP inventory evidence missing")
			}
			gate := exec.CommandContext(ctx, "python3", "-B", "-m", "unittest", "-v")
			gate.Dir = artifact.Path
			var out []byte
			if protected {
				executor, err := runExecutor(author)
				must(t, err)
				inside, _, err := protectedCodePath(author, artifact.Path)
				must(t, err)
				result, runErr := executor.Execute(ctx, workspaceCommand{Command: "cd " + shellQuote(inside) + " && python3 -B -m unittest -v"})
				err = runErr
				out = []byte(result.Output)
				if result.ExitCode != 0 && err == nil {
					err = fmt.Errorf("gate exit %d", result.ExitCode)
				}
			} else {
				out, err = gate.CombinedOutput()
			}
			if err != nil {
				t.Fatalf("independent gate failed: %s: %v", out, err)
			}
			currentSeed, err := inspectCode(repo)
			must(t, err)
			if currentSeed != baseline {
				t.Fatal("seed repository changed")
			}
			reviewed := false
			for _, review := range taskReviews(s, task.ID) {
				if review.Target == author.ID && review.Verdict == "pass" && review.Family == "anthropic-claude" && review.Revision == e.revision(author) {
					reviewed = true
				}
			}
			if !reviewed {
				t.Fatal("no current independent code review")
			}
			mcpProviders, usageProviders := map[string]bool{}, map[string]bool{}
			commands := false
			for _, trace := range taskTraces(s, task.ID) {
				var r Run
				must(t, s.Get(trace.Run, &r))
				if (strings.Contains(trace.Name, "inventory") || (protected && trace.Name == "adc_call_tool")) && trace.State == "complete" {
					mcpProviders[r.Provider] = true
				}
				if r.Provider == "codex" && (trace.Name == "commandExecution" || (protected && trace.Name == "adc_workspace")) && trace.State == "complete" {
					commands = true
				}
				if (strings.HasPrefix(trace.Name, "adc_") || strings.Contains(trace.Name, "inventory")) && trace.State == "failed" {
					if requestAndRestart && trace.Name == "adc_call_tool" && strings.Contains(trace.Result, "needs approval") && trace.At < approvedAt {
						t.Log("Initial unapproved tool probe was denied before the single human grant")
						continue
					}
					t.Fatalf("failed orchestration/MCP tool: %s at %s (approval %s): %s", trace.Name, trace.At, approvedAt, trace.Result)
				}
			}
			for _, event := range s.Events(task.ID) {
				if strings.Contains(event.Text, privateSeed) {
					t.Fatal("private MCP seed leaked to event history")
				}
				if event.Kind == "usage" {
					var u usageSample
					must(t, json.Unmarshal([]byte(event.Text), &u))
					usageProviders[u.Provider] = true
				}
			}
			if !commands || !mcpProviders["codex"] || !mcpProviders[reviewProvider] || !usageProviders["codex"] || !usageProviders[reviewProvider] {
				t.Fatalf("missing provider evidence: commands=%v MCP=%v usage=%v", commands, mcpProviders, usageProviders)
			}
			t.Logf("PASS: fresh MCP inventory through Codex and %s; committed Codex fix %s; preserved gates passed; exact revision independently reviewed by Claude; both providers attributed.", reviewProvider, artifact.Commit)
			return
		}
	}
}
