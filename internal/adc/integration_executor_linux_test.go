//go:build linux

package adc

import (
	"context"
	"strings"
	"testing"
)

func TestIntegrationObservedCommandAndSpoofBoundary(t *testing.T) {
	s, e, _, root := fixture(t)
	x := executorFixture(t) // Qualify availability of the actual Linux boundary.
	root.Execution = "protected"
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	p := planFixtureInput()
	p.Start = true
	p.Steps = p.Steps[:1]
	p.Steps[0].Checks = []ValidationRequirement{{Key: "check", Kind: "command", Verifier: "test -f /workspace/proof && cat /workspace/proof", Criteria: "Fixture proof is present", Observed: true}}
	_, err := e.saveExecutionPlan(root, p)
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	var r Run
	must(t, s.Get(step.Run, &r))
	r.State = "running"
	r.Workspace = x.Workspace
	must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
	if _, err := e.recordValidation(r, validationInput{Key: "check", ArtifactRevision: e.artifactRevision(r), Outcome: "pass", Output: "I ran it"}, false); err == nil {
		t.Fatal("reported check spoofed observed outcome")
	}
	payload := func() map[string]any {
		return map[string]any{"Key": "check", "ArtifactRevision": e.artifactRevision(r), "Revision": s.integrationEvidence(r.ID).Revision}
	}
	_, err = call(t, e, r, "adc_validate", payload())
	must(t, err)
	if e.validationViews(r)[0].State != "failed" {
		t.Fatal("nonzero command passed")
	}
	result, err := x.Execute(context.Background(), workspaceCommand{Command: "printf fixture-observed > /workspace/proof"})
	must(t, err)
	if result.ExitCode != 0 {
		t.Fatal(result)
	}
	_, err = call(t, e, r, "adc_validate", payload())
	must(t, err)
	v := s.integrationEvidence(r.ID)
	if len(v.Checks) != 1 || v.Checks[0].Provenance != "adc-command" || !strings.Contains(v.Checks[0].Output, "fixture-observed") || e.milestoneMissing(r) != "" {
		t.Fatal("actual command result not recorded", v)
	}
	// A changed tested combination stales the check; finishing re-runs it instead of blocking.
	v, err = e.saveIntegration(r, integrationInput{Revision: v.Revision, Environment: []EnvironmentVersion{{Name: "os", Version: "v2"}}})
	must(t, err)
	if e.validationViews(r)[0].State != "stale" {
		t.Fatal("environment change did not stale the observed check")
	}
	completePlanWorker(t, e, step)
	if views := e.validationViews(r); views[0].State != "pass" || views[0].Result.Provenance != "adc-command" {
		t.Fatal("stale command check was not re-run on finish", views[0].State)
	}
	reviewPlanStep(t, e, step, "pass")
	if e.inspectPlan(s.taskPlan(r.Task)).Steps[0].State != "complete" {
		t.Fatal("observed command did not complete through independent review")
	}
}
