package adc

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Only disposable fixture records and ADC documents are used. No repository,
// publication, shell or infrastructure operation is part of this qualification.
func TestLiveExecutionPlanCompletion(t *testing.T) {
	if os.Getenv("ADC_LIVE_EXECUTION_PLAN") != "1" {
		t.Skip("set ADC_LIVE_EXECUTION_PLAN=1 and explicit connected provider homes")
	}
	runLiveExecutionPlan(t, "plan")
}
func TestLiveMilestoneCompletion(t *testing.T) {
	if os.Getenv("ADC_LIVE_MILESTONES") != "1" {
		t.Skip("set ADC_LIVE_MILESTONES=1 and explicit connected provider homes")
	}
	runLiveExecutionPlan(t, "milestone")
}
func TestLiveWaitCompletion(t *testing.T) {
	if os.Getenv("ADC_LIVE_WAITS") != "1" {
		t.Skip("set ADC_LIVE_WAITS=1 and explicit connected provider homes")
	}
	runLiveExecutionPlan(t, "wait")
}
func TestLiveIntegrationCompletion(t *testing.T) {
	if os.Getenv("ADC_LIVE_INTEGRATION") != "1" {
		t.Skip("set ADC_LIVE_INTEGRATION=1 and explicit connected provider homes")
	}
	runLiveExecutionPlan(t, "integration")
}
func TestLivePreflightCompletion(t *testing.T) {
	if os.Getenv("ADC_LIVE_PREFLIGHT") != "1" {
		t.Skip("set ADC_LIVE_PREFLIGHT=1 with connected homes")
	}
	runLiveExecutionPlan(t, "preflight")
}
func TestLiveSelfhostedCompletion(t *testing.T) {
	if os.Getenv("ADC_LIVE_SELFHOSTED") != "1" {
		t.Skip("set ADC_LIVE_SELFHOSTED=1 with explicit endpoint/model and Claude home")
	}
	runLiveExecutionPlan(t, "selfhosted")
}
func runLiveExecutionPlan(t *testing.T, mode string) {
	milestones := mode == "milestone"
	local := mode == "selfhosted"
	codeHome, reviewHome := os.Getenv("ADC_LIVE_CODEX_HOME"), os.Getenv("ADC_LIVE_CLAUDE_HOME")
	if (!local && codeHome == "") || reviewHome == "" {
		t.Fatal("explicit Codex and Claude account homes required")
	}
	s, e, seed, seedRun := fixture(t)
	timeout := 7 * time.Minute
	if local {
		timeout = 12 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer e.Stop()
	if !local {
		client, err := startCodex(ctx, codeHome)
		must(t, err)
		e.codexClients["code-account"] = client
	}
	reviewer, err := startClaude(ctx, reviewHome)
	must(t, err)
	e.codexClients["account"] = reviewer
	_, err = s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','unused'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	code := Account{ID: "code-account", User: "owner", Provider: "codex", Limit: 2}
	review := Account{ID: "account", User: "owner", Provider: "claude", Limit: 2}
	localModel := os.Getenv("ADC_LIVE_SELFHOSTED_MODEL")
	if local {
		code.Provider = "selfhosted"
		code.BaseURL = os.Getenv("ADC_LIVE_SELFHOSTED_BASE")
		code.Limit = 1
		if code.BaseURL == "" || localModel == "" {
			t.Fatal("explicit self-hosted base and model required")
		}
	}
	for _, a := range []Account{code, review} {
		must(t, s.Put("account", "", a.User, "", a.ID, a))
	}
	reviewModel := liveClaudeReviewModel(t, e, review, ctx)
	for _, a := range list[Agent](s, "agent", seed.Org) {
		a.Tools = nil
		if Family(a.Model) == "openai-gpt" {
			a.Provider = "codex"
			a.Model = "gpt-5.6-sol"
			if local {
				if a.ID == "boss" {
					a.Provider = "claude"
					a.Model = reviewModel
				} else {
					a.Provider = "selfhosted"
					a.Model = localModel
				}
			}
		} else {
			a.Provider = "claude"
			a.Model = reviewModel
		}
		must(t, s.Put("agent", a.Org, "", "", a.ID, a))
	}
	seed.State = "ready"
	seedRun.State = "complete"
	must(t, s.Put("assignment", seed.Org, "", seed.State, seed.ID, seed))
	must(t, s.Put("run", seedRun.Org, seedRun.Task, seedRun.State, seedRun.ID, seedRun))
	task := Assignment{ID: ID(), Org: seed.Org, Owner: "boss", Account: code.ID, ExtraAccount: review.ID, Creator: "owner", Title: "Synthetic execution-plan qualification", Prompt: `This is an authorized synthetic ADC-document workflow. Use only ADC tools, no shell, web, repository, publication or infrastructure actions. No human decisions needed.
Supervisor: use adc_plan once to create and START a durable plan with exactly two steps. Source: 'Synthetic fixture facts supplied by the human in this assignment'. Step A, owner dev and reviewer qa, saves an ADC document stating these fixture facts: Snow and Floe ship; NBC is retired. Its criterion is all three facts correct. Step B, owner dev and reviewer qa, DependsOn [A], reads A's reviewed document and saves a separate ADC document listing the shipping names alphabetically: Floe, Snow; excludes NBC. Its criterion is the exact sorted list derived from A's reviewed evidence. These names are synthetic, not a claim about any live organization.
ADC automatically dispatches steps and reviewers. Do not call adc_delegate or make duplicate reviewers. Use adc_wait, then inspect adc_status; finish the assignment after both steps have passed independent review. Do not author a supervisor document.`}
	if milestones {
		task.Title = "Synthetic milestone qualification"
		task.Prompt += "\nAdditional required gate on step A: Requirements=[{Key:canary, Kind:verified-canary, Target:fixture:canary/Trixie/gchlog, Criteria:Record the supplied synthetic canary observation and its exact reference and timestamp; code readiness alone cannot satisfy it}]. The human supplies this PRIMARY SYNTHETIC OBSERVATION, not a request to operate anything: fixture:canary/Trixie/gchlog completed one bounded synthetic publication and its fixture consumer probe passed. Source Reference is fixture:report/canary-123. ObservedAt is " + now() + ". A's worker must explicitly record this observation using adc_milestone before adc_finish, and its independent reviewer must check that the packet exactly matches the primary fixture observation. This is a qualification fixture, never evidence of a real canary. Retain the original two documents and dependency as specified. B should inspect the reviewed typed evidence as well as A's document. No real operations or extra approvals."
	}
	if mode == "wait" {
		task.Title = "Synthetic durable wait qualification"
		task.Prompt += "\nStep A additionally requires a durable elapsed-time milestone: Key=quiet, Kind=elapsed-time, Target=fixture:observation-window, Criteria=ADC records that 30 seconds elapsed after registration, Wait={StableSeconds:30, TimeoutSeconds:300, Tool:empty, Arguments:empty, Match:empty, VersionPointer:empty, EventTimePointer:empty, PollSeconds:0}. The worker calls adc_await Requirement=quiet to register, then adc_wait to yield. Inspect adc_status after automatic resumption and finish with the fixture document when the wait is satisfied. Never use adc_milestone for this automatic requirement. The reviewer checks the saved wait timestamps and ADC evidence before adc_review pass. Preserve the original two documents and dependency. This fixture needs no external calls or human continuation."
	}
	if mode == "integration" {
		task.Title = "Synthetic structured validation qualification"
		task.Prompt += "\nStep A additionally has Checks=[{Key:facts, Kind:integration, Verifier:compare document with primary synthetic fixture facts, Criteria:Snow and Floe ship and NBC is retired with no extra shipping products, Observed:false}]. After authoring its final document A uses adc_integration to record Environment=[{Name:fixture-data,Version:v1}], with no repositories or consumed repository outputs because this is document-only work. It reads adc_status for A's ArtifactRevision and Integration.Revision, independently compares the document to the primary facts, then uses adc_check with Key=facts, those exact revisions, Outcome=pass, Output describing the concrete comparison, References=[fixture:facts/v1]. This evidence is explicitly agent-reported and must be independently checked by qa using the review_brief and original fixture facts. Do not change the document after checks without recording a new check. Do not use adc_validate, shell, repositories or external services; observed command verification has a separate isolated qualification. Preserve the original two documents, exactly two steps and independent reviews."
	}
	if mode == "preflight" || local {
		task.Title = "Synthetic execution readiness qualification"
		task.Prompt += "\nOn BOTH steps, declare Preflight={Models:true} and ReviewPreflight={Models:true}. ADC must check availability of the funded execution model and independent Claude reviewer before dispatch. This workflow is document-only, so no repository, command, directory or port declaration is needed. Preserve exactly two steps, two independently reviewed documents, and the stated dependency."
	}
	if local {
		task.Execution = "protected"
		task.Title = "Synthetic self-hosted Qwen qualification"
		task.Account = review.ID
		task.ExtraAccount = code.ID
	}
	must(t, e.CreateAssignment(task))
	e.Start(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			t.Fatal("plan workflow did not finish before timeout")
		case <-ticker.C:
			must(t, s.Get(task.ID, &task))
			p := s.taskPlan(task.ID)
			snapshot := task.State + " " + p.State
			for _, step := range p.Steps {
				snapshot += " " + step.Key + ":" + step.State
			}
			if snapshot != last {
				t.Log(snapshot)
				last = snapshot
			}
			if task.State == "needs input" {
				t.Fatalf("routine plan requested human continuation: %+v", taskDecisions(s, task.ID))
			}
			if task.State != "ready" {
				continue
			}
			if p.State != "complete" || len(p.Steps) != 2 {
				t.Fatal("durable plan was bypassed")
			}
			if len(taskRuns(s, task.ID)) != 5 {
				t.Fatal("duplicated or missing worker/review runs")
			}
			for _, step := range p.Steps {
				if step.State != "complete" || step.Run == "" || step.Review == "" {
					t.Fatal("step lacks review evidence")
				}
			}
			if len(taskDocs(s, task.ID)) != 2 || len(taskReviews(s, task.ID)) < 2 {
				t.Fatal("missing concrete documents or reviews")
			}
			for _, trace := range taskTraces(s, task.ID) {
				if trace.State == "failed" {
					t.Fatalf("tool friction: %s: %s", trace.Name, trace.Result)
				}
			}
			if milestones {
				if len(p.Steps[0].Requirements) != 1 || p.Steps[0].Requirements[0].Kind != "verified-canary" {
					t.Fatal("model omitted typed milestone")
				}
				observation := s.milestoneEvidence(p.Steps[0].Run, "canary")
				if observation.Reference != "fixture:report/canary-123" || observation.Kind != "verified-canary" {
					t.Fatal("missing or wrong typed evidence", observation)
				}
			}
			if mode == "wait" {
				wait := s.runWait(p.Steps[0].Run, "quiet")
				started, _ := time.Parse(time.RFC3339Nano, wait.Created)
				checked, _ := time.Parse(time.RFC3339Nano, wait.CheckedAt)
				if wait.State != "satisfied" || checked.Sub(started) < 30*time.Second || s.milestoneEvidence(wait.Run, "quiet").Revision != 1 {
					t.Fatal("durable timer bypassed", wait)
				}
			}
			if mode == "integration" {
				var worker Run
				must(t, s.Get(p.Steps[0].Run, &worker))
				checks := e.validationViews(worker)
				if len(checks) != 1 || checks[0].State != "pass" || checks[0].Result.Provenance != "agent-reported" {
					t.Fatal("required structured validation missing or stale", checks)
				}
			}
			if mode == "preflight" || local {
				for _, step := range p.Steps {
					for _, id := range []string{step.Run, step.Review} {
						if v := s.runReadiness(id); v.State != "ready" {
							t.Fatalf("preflight bypassed for %s: %+v", id, v)
						}
					}
				}
			}
			evidence, _ := json.MarshalIndent(map[string]any{"plan": p, "documents": taskDocs(s, task.ID), "reviews": taskReviews(s, task.ID), "readiness": taskReadiness(s, task.ID)}, "", "  ")
			must(t, os.MkdirAll("../../work", 0700))
			reportPath := "../../work/execution-plan-live-qualification.json"
			if milestones {
				reportPath = "../../work/milestones-live-qualification.json"
			}
			if mode == "wait" {
				reportPath = "../../work/waits-live-qualification.json"
			}
			if mode == "integration" {
				reportPath = "../../work/integration-live-qualification.json"
			}
			if mode == "preflight" {
				reportPath = "../../work/preflight-live-qualification.json"
			}
			if local {
				reportPath = "../../work/selfhosted-live-qualification.json"
				report, err := s.usageReport(task.Org, task.ID, 0, 0, time.Now())
				must(t, err)
				if len(report.Accounts) != 2 || report.Total.ReportedRuns != 5 {
					t.Fatal("missing cross-provider usage", report.Total)
				}
			}
			must(t, os.WriteFile(reportPath, evidence, 0600))
			t.Log("Real provider-backed plan creation, automatic dependency dispatch and independent direct Claude review completed without human continuation.")
			return
		}
	}
}
