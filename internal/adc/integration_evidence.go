package adc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// Definitions are frozen in the plan. Results never redefine their own gates.
type ValidationRequirement struct {
	Key, Kind, Verifier, Criteria string
	Observed                      bool
}
type RepositoryEvidence struct {
	Identity, Path, Base, Commit, PullRequest, Release string
}
type ConsumedArtifact struct{ Step, Repository, Commit string }
type EnvironmentVersion struct{ Name, Version string }
type ValidationResult struct {
	Key, Outcome, Verifier, ArtifactRevision, Provenance, Output, Created string
	References                                                            []string
}
type IntegrationEvidence struct {
	ID, Org, Task, Run, Created string
	Revision                    int
	Repositories                []RepositoryEvidence
	Consumes                    []ConsumedArtifact
	Environment                 []EnvironmentVersion
	Checks                      []ValidationResult
}
type integrationInput struct {
	Revision     int
	Repositories []RepositoryEvidence
	Consumes     []ConsumedArtifact
	Environment  []EnvironmentVersion
}
type validationInput struct {
	Key, ArtifactRevision, Outcome, Output string
	References                             []string
	Revision                               int
}
type ValidationView struct {
	ValidationRequirement
	Result ValidationResult
	State  string
}

func validateIntegrationSpec(spec PlanStepSpec) error {
	if len(spec.Repositories) > 20 || len(spec.Checks) > 20 {
		return fmt.Errorf("use at most 20 repository identities and 20 required checks per step")
	}
	seen := map[string]bool{}
	for _, identity := range spec.Repositories {
		if !evidenceReference(identity) || seen[identity] {
			return fmt.Errorf("repository identities must be unique, bounded, credential-free references")
		}
		seen[identity] = true
	}
	seen = map[string]bool{}
	for _, c := range spec.Checks {
		if !planKey.MatchString(c.Key) || seen[c.Key] || strings.TrimSpace(c.Verifier) == "" || len(c.Verifier) > 4000 || strings.TrimSpace(c.Criteria) == "" || len(c.Criteria) > 8000 {
			return fmt.Errorf("each check needs a unique Key, bounded Verifier and Criteria")
		}
		seen[c.Key] = true
		if c.Kind != "command" && c.Kind != "integration" && c.Kind != "visual" {
			return fmt.Errorf("check kind must be command, integration or visual")
		}
		if c.Observed && c.Kind != "command" {
			return fmt.Errorf("ADC-observed checks currently require kind command")
		}
	}
	return nil
}

// References are labels, not fetch instructions. Reject URL credentials rather
// than placing them in durable evidence or handing them to a reviewer.
func evidenceReference(v string) bool {
	if strings.TrimSpace(v) != v || v == "" || len(v) > 2000 || strings.ContainsAny(v, "\r\n\x00") {
		return false
	}
	u, err := url.Parse(v)
	return err == nil && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func (s *Store) integrationEvidence(run string) IntegrationEvidence {
	var v IntegrationEvidence
	_ = s.Get("integration:"+run, &v)
	return v
}

// This pin covers what a check actually exercised: the registered code, the
// recorded tested combination (repositories, consumed inputs, environment) and
// the frozen prerequisite inputs. It deliberately excludes the completion
// message and the check results themselves (writing a result must not
// invalidate itself), and also documents and milestone observations, so a
// note or a recorded release does not make a passing build stale.
func (e *Engine) artifactRevision(r Run) string {
	v := e.Store.integrationEvidence(r.ID)
	v.ID, v.Org, v.Task, v.Run, v.Created = "", "", "", "", ""
	v.Revision, v.Checks = 0, nil
	_, step, _ := e.plannedStep(r)
	b, _ := json.Marshal([]any{r.Code, v, step.Inputs})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (e *Engine) validationViews(r Run) []ValidationView {
	_, step, _ := e.plannedStep(r)
	v := e.Store.integrationEvidence(r.ID)
	pin := e.artifactRevision(r)
	views := []ValidationView{}
	for _, req := range step.Checks {
		view := ValidationView{ValidationRequirement: req, State: "missing"}
		for _, result := range v.Checks {
			if result.Key != req.Key {
				continue
			}
			view.Result = result
			view.State = result.Outcome
			if result.ArtifactRevision != pin || result.Verifier != req.Verifier {
				view.State = "stale"
			} else if req.Observed && result.Provenance != "adc-command" {
				view.State = "reported only"
			}
		}
		views = append(views, view)
	}
	return views
}
func (e *Engine) integrationMissing(r Run) string {
	plan, step, _ := e.plannedStep(r)
	v := e.Store.integrationEvidence(r.ID)
	for _, repo := range v.Repositories {
		current := false
		for _, code := range r.Code {
			current = current || (repo.Path == code.Path && repo.Commit == code.Commit)
		}
		if !current {
			return "Repository " + repo.Identity + " output changed; refresh adc_integration"
		}
	}
	for _, identity := range step.Repositories {
		found := false
		for _, repo := range v.Repositories {
			if repo.Identity == identity {
				for _, code := range r.Code {
					found = found || (repo.Path == code.Path && repo.Commit == code.Commit)
				}
			}
		}
		if !found {
			return "Repository " + identity + " needs current base/output evidence via adc_integration"
		}
	}
	// An integration check must identify every repository output on its direct
	// dependency edges. Omitting a consumed version cannot manufacture a tested
	// combination. Other kinds of steps may record only the inputs they consume.
	for _, check := range step.Checks {
		if check.Kind != "integration" {
			continue
		}
		for _, upstream := range plan.Steps {
			if upstream.Omission != nil || !containsString(step.DependsOn, upstream.Key) {
				continue
			}
			for _, repo := range e.Store.integrationEvidence(upstream.Run).Repositories {
				found := false
				for _, dep := range v.Consumes {
					found = found || (dep.Step == upstream.Key && dep.Repository == repo.Identity && dep.Commit == repo.Commit)
				}
				if !found {
					return "Integration requires exact consumed output from " + upstream.Key + " / " + repo.Identity
				}
			}
		}
	}
	for _, check := range e.validationViews(r) {
		if check.State == "stale" {
			how := "re-run it and record the result with adc_check"
			if check.Kind == "command" {
				how = "re-run `" + check.Verifier + "` and record the result with adc_check (ADC re-runs protected command checks itself when you finish)"
			}
			return "Required check " + check.Key + " is stale because the artifacts changed since it was recorded; " + how
		}
		if check.State != "pass" {
			return "Required check " + check.Key + " is " + check.State + "; inspect the evidence and correct or report the blocker"
		}
	}
	return ""
}

// validateCheck runs one frozen command check in the protected workspace and
// records the observed result. The command runs without the store lock.
func (e *Engine) validateCheck(ctx context.Context, runID, key, artifactRevision string, revision int) (IntegrationEvidence, error) {
	s := e.Store
	s.mu.Lock()
	r, err := e.integrationWorker(runID)
	_, step, _ := e.plannedStep(r)
	var req ValidationRequirement
	for _, c := range step.Checks {
		if c.Key == key {
			req = c
		}
	}
	if err == nil && (r.Execution != "protected" || req.Kind != "command" || e.artifactRevision(r) != artifactRevision || s.integrationEvidence(r.ID).Revision != revision) {
		err = fmt.Errorf("protected execution, a frozen command and current artifact/evidence revisions are required")
	}
	if err == nil {
		err = verifyCode(r)
	}
	s.mu.Unlock()
	if err != nil {
		return IntegrationEvidence{}, err
	}
	output, exit, err := executeValidationCommand(ctx, r, req.Verifier)
	if err != nil {
		output += "\nExecution error: " + err.Error()
		exit = -1
	}
	if strings.TrimSpace(output) == "" {
		output = fmt.Sprintf("Command exited %d with no output", exit)
	}
	outcome := "failed"
	if exit == 0 {
		outcome = "pass"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return e.recordValidation(r, validationInput{Key: key, ArtifactRevision: artifactRevision, Revision: revision, Outcome: outcome, Output: clipped(output, 32000)}, true)
}

// rerunStaleCommandChecks re-executes protected command checks whose recorded
// result no longer matches the current artifacts, instead of blocking the
// worker on a check it already knows how to run. Failures stay recorded.
func (e *Engine) rerunStaleCommandChecks(ctx context.Context, runID string) {
	s := e.Store
	s.mu.Lock()
	r, err := e.integrationWorker(runID)
	if err != nil || r.Execution != "protected" {
		s.mu.Unlock()
		return
	}
	type rerun struct {
		key, pin string
		revision int
	}
	pending := []rerun{}
	pin := e.artifactRevision(r)
	revision := s.integrationEvidence(r.ID).Revision
	for _, check := range e.validationViews(r) {
		if check.State == "stale" && check.Kind == "command" {
			pending = append(pending, rerun{check.Key, pin, revision})
		}
	}
	s.mu.Unlock()
	for _, p := range pending {
		if v, err := e.validateCheck(ctx, runID, p.key, p.pin, p.revision); err == nil {
			revision = v.Revision
			s.Log(r.Org, r.Task, r.ID, "validation", "Re-ran stale check "+p.key+" before finishing")
		}
	}
}

// All mutations are from the current, running planned worker. Old attempts and
// decisions cannot be bypassed through the evidence tools.
func (e *Engine) integrationWorker(id string) (Run, error) {
	var r Run
	if e.Store.Get(id, &r) != nil || r.State != "running" || r.Superseded || r.ReviewOf != "" {
		return r, fmt.Errorf("current running planned worker required")
	}
	p, _, ok := e.plannedStep(r)
	var task Assignment
	if !ok || p.State != "active" || e.Store.Get(r.Task, &task) != nil || task.Org != r.Org || task.State == "paused" || task.State == "cancelled" || pendingDecision(e.Store, r.Task, r.ID) || !e.planAllowsDispatch(r) {
		return r, fmt.Errorf("assignment, prerequisites or human decision currently hold this step")
	}
	return r, nil
}

func (e *Engine) saveIntegration(r Run, p integrationInput) (IntegrationEvidence, error) {
	r, err := e.integrationWorker(r.ID)
	if err != nil {
		return IntegrationEvidence{}, err
	}
	old := e.Store.integrationEvidence(r.ID)
	if old.Revision != p.Revision {
		return old, fmt.Errorf("integration evidence changed; inspect adc_status before updating")
	}
	if len(p.Repositories) > 20 || len(p.Consumes) > 80 || len(p.Environment) > 40 {
		return old, fmt.Errorf("limit evidence to 20 repositories, 80 consumed artifacts and 40 environment versions")
	}
	if err := verifyCode(r); err != nil {
		return old, err
	}
	seen, paths := map[string]bool{}, map[string]bool{}
	for _, repo := range p.Repositories {
		if !evidenceReference(repo.Identity) || seen[repo.Identity] || paths[repo.Path] || (repo.PullRequest != "" && !evidenceReference(repo.PullRequest)) || (repo.Release != "" && !evidenceReference(repo.Release)) {
			return old, fmt.Errorf("repository identities and paths must be unique; references must be credential-free")
		}
		seen[repo.Identity], paths[repo.Path] = true, true
		matched := false
		for _, code := range r.Code {
			matched = matched || (code.Path == repo.Path && code.Commit == repo.Commit)
		}
		if !matched || !gitObjectID(repo.Base) {
			return old, fmt.Errorf("register current output using adc_code first and supply an exact base commit")
		}
		if err := verifyRepositoryBase(r, repo); err != nil {
			return old, err
		}
	}
	plan, step, _ := e.plannedStep(r)
	seen = map[string]bool{}
	for _, dep := range p.Consumes {
		key := dep.Step + ":" + dep.Repository
		if seen[key] || !containsString(step.DependsOn, dep.Step) {
			return old, fmt.Errorf("consumed repositories must name unique direct prerequisites")
		}
		seen[key] = true
		matched := false
		for _, upstream := range plan.Steps {
			if upstream.Key != dep.Step {
				continue
			}
			if upstream.Omission != nil {
				return old, fmt.Errorf("omitted steps have no admitted output to consume")
			}
			var source Run
			if e.Store.Get(upstream.Run, &source) != nil || step.Inputs[dep.Step] != source.ID+":"+e.dependencyRevision(source) {
				return old, fmt.Errorf("consumed input changed; wait for reconciliation")
			}
			for _, repo := range e.Store.integrationEvidence(source.ID).Repositories {
				matched = matched || (repo.Identity == dep.Repository && repo.Commit == dep.Commit)
			}
		}
		if !matched {
			return old, fmt.Errorf("consumed artifact must match its prerequisite's exact repository output")
		}
	}
	seen = map[string]bool{}
	for _, env := range p.Environment {
		if !evidenceReference(env.Name) || !evidenceReference(env.Version) || seen[env.Name] {
			return old, fmt.Errorf("environment names must be unique with bounded exact versions")
		}
		seen[env.Name] = true
	}
	sort.Slice(p.Repositories, func(i, j int) bool { return p.Repositories[i].Identity < p.Repositories[j].Identity })
	sort.Slice(p.Consumes, func(i, j int) bool {
		return p.Consumes[i].Step+p.Consumes[i].Repository < p.Consumes[j].Step+p.Consumes[j].Repository
	})
	sort.Slice(p.Environment, func(i, j int) bool { return p.Environment[i].Name < p.Environment[j].Name })
	v := old
	v.Repositories, v.Consumes, v.Environment = p.Repositories, p.Consumes, p.Environment
	return e.writeIntegration(r, old, v)
}

func gitObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func (e *Engine) writeIntegration(r Run, old, v IntegrationEvidence) (IntegrationEvidence, error) {
	v.ID, v.Org, v.Task, v.Run = "integration:"+r.ID, r.Org, r.Task, r.ID
	v.Revision, v.Created = old.Revision+1, now()
	writes := []Write{{"integration-evidence", r.Org, r.Task, "", v.ID, v}}
	if old.ID != "" {
		history := old
		history.ID = ID()
		writes = append(writes, Write{"integration-evidence-history", r.Org, r.Task, "", history.ID, history})
	}
	_, step, _ := e.plannedStep(r)
	var reviewer Run
	if e.Store.Get(step.Review, &reviewer) == nil && reviewer.State == "complete" {
		reviewer.State, reviewer.Turns = "waiting", 0
		writes = append(writes, Write{"run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer})
	}
	if err := e.Store.Batch(writes...); err != nil {
		return old, err
	}
	e.Store.Log(r.Org, r.Task, r.ID, "validation", fmt.Sprintf("Integration evidence revision %d recorded", v.Revision))
	return v, nil
}

func (e *Engine) recordValidation(r Run, p validationInput, observed bool) (IntegrationEvidence, error) {
	r, err := e.integrationWorker(r.ID)
	if err != nil {
		return IntegrationEvidence{}, err
	}
	old := e.Store.integrationEvidence(r.ID)
	if old.Revision != p.Revision || e.artifactRevision(r) != p.ArtifactRevision {
		return old, fmt.Errorf("artifacts or evidence changed; inspect current pins and rerun the check")
	}
	_, step, _ := e.plannedStep(r)
	var req ValidationRequirement
	for _, c := range step.Checks {
		if c.Key == p.Key {
			req = c
		}
	}
	if req.Key == "" || (req.Observed && !observed) {
		return old, fmt.Errorf("unknown check or ADC-observed command required; use adc_validate")
	}
	if p.Outcome != "pass" && p.Outcome != "failed" && p.Outcome != "inapplicable" {
		return old, fmt.Errorf("outcome must be pass, failed or inapplicable")
	}
	if strings.TrimSpace(p.Output) == "" || len(p.Output) > 32000 || len(p.References) > 12 {
		return old, fmt.Errorf("provide supporting output up to 32000 characters and at most 12 references")
	}
	for _, ref := range p.References {
		if !evidenceReference(ref) {
			return old, fmt.Errorf("invalid evidence reference")
		}
	}
	if req.Kind == "visual" && p.Outcome == "pass" && len(p.References) == 0 {
		return old, fmt.Errorf("visual checks require inspectable screenshot/interaction evidence references")
	}
	if err := verifyCode(r); err != nil {
		return old, err
	}
	provenance := "agent-reported"
	if observed {
		provenance = "adc-command"
	}
	result := ValidationResult{Key: p.Key, Outcome: p.Outcome, Verifier: req.Verifier, ArtifactRevision: p.ArtifactRevision, Provenance: provenance, Output: p.Output, References: p.References, Created: now()}
	v := old
	v.Checks = append([]ValidationResult(nil), old.Checks...)
	replaced := false
	for i, check := range v.Checks {
		if check.Key == p.Key {
			v.Checks[i], replaced = result, true
		}
	}
	if !replaced {
		v.Checks = append(v.Checks, result)
	}
	return e.writeIntegration(r, old, v)
}

func (e *Engine) integrationTools(ctx context.Context, original Run) []copilot.Tool {
	s := e.Store
	return []copilot.Tool{
		copilot.DefineTool("adc_integration", "Record the tested combination for your current plan step. Revision is the current Integration.Revision in adc_status. Repositories contain Identity, registered host Path and Commit from adc_code, exact ancestor Base commit, optional PullRequest/Release references. Consumes contains direct prerequisite Step, Repository identity and exact Commit. Environment contains Name/Version pins. Updates replace these lists and invalidate checks when material inputs change. References and environment pins are agent-reported, not observed remote state. Never include credentials.", func(p integrationInput, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return e.saveIntegration(original, p)
		}),
		copilot.DefineTool("adc_check", "Record agent-reported validation against a frozen check Key. Supply current Integration.Revision, ArtifactRevision from adc_status, Outcome (pass, failed, inapplicable), supporting Output and optional evidence References. Inapplicable does not pass a required gate. ADC-observed requirements use adc_validate instead. This cannot redefine requirements. Capture phone/desktop flows, browser errors and baseline availability for visual checks; reference workspace evidence, with publication only if separately authorized.", func(p validationInput, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return e.recordValidation(original, p, false)
		}),
		copilot.DefineTool("adc_validate", "Execute a frozen command check Key inside the protected workspace and record its exit/output against ArtifactRevision and current Integration.Revision. Requires protected execution. No arbitrary replacement command. Timeout is 300 seconds. Use for inexpensive deterministic checks before finishing; failures remain inspectable and correctable. Command execution has the assignment's existing authority only. A stale command check is re-run automatically when you finish.", func(p struct {
			Key, ArtifactRevision string
			Revision              int
		}, _ copilot.ToolInvocation) (any, error) {
			return e.validateCheck(ctx, original.ID, p.Key, p.ArtifactRevision, p.Revision)
		}),
	}
}
