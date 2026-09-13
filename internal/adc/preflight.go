package adc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Optional declarations of known prerequisites, not a finite permission plan.
type PreflightSpec struct {
	Models             bool
	Commands           []string
	Repositories       []RepositoryPreflight
	Directories, Ports []string
}
type RepositoryPreflight struct {
	Connection, Owner, Repository string
	Write                         bool
}
type PreflightCheck struct{ Name, State, Detail string }
type ExecutionReadiness struct {
	ID, Org, Task, Run, Fingerprint, State, Lease, CheckedAt, NextAt string
	Generation                                                       int
	Checks                                                           []PreflightCheck
}
type RunResources struct {
	ID, Org, Task, Run, State, Created, Released string
	Directories                                  map[string]string
	Ports                                        map[string]int
}

var prerequisiteCommand = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]{0,79}$`)

func validatePreflight(p *PreflightSpec) error {
	if p == nil {
		return nil
	}
	if len(p.Commands) > 30 || len(p.Repositories) > 20 || len(p.Directories) > 12 || len(p.Ports) > 8 {
		return fmt.Errorf("keep preflight to 30 commands, 20 repositories, 12 directories and 8 ports")
	}
	for _, command := range p.Commands {
		if !prerequisiteCommand.MatchString(command) {
			return fmt.Errorf("preflight commands must be executable names, not shell commands or paths")
		}
	}
	for _, names := range [][]string{p.Directories, p.Ports} {
		seen := map[string]bool{}
		for _, name := range names {
			if !planKey.MatchString(name) || seen[name] {
				return fmt.Errorf("resource names must be unique bounded identifiers")
			}
			seen[name] = true
		}
	}
	for _, repo := range p.Repositories {
		if repo.Connection == "" || !validGitHubRepository(repo.Owner, repo.Repository) {
			return fmt.Errorf("repository preflight needs a connection and valid owner/repository")
		}
	}
	return nil
}
func (s *Store) runReadiness(run string) ExecutionReadiness {
	var v ExecutionReadiness
	_ = s.Get("preflight:"+run, &v)
	return v
}
func (s *Store) runResources(run string) RunResources {
	var v RunResources
	_ = s.Get("resources:"+run, &v)
	return v
}
func (e *Engine) readinessModels(r Run) []Run {
	runs := []Run{r}
	if r.Preflight != nil && r.Preflight.Models && r.ReviewOf == "" {
		if _, step, ok := e.plannedStep(r); ok {
			var reviewer Run
			if e.Store.Get(step.Review, &reviewer) == nil && reviewer.Org == r.Org && reviewer.Task == r.Task && reviewer.ReviewOf == r.ID && reviewer.Agent == step.Verifier.ID {
				runs = append(runs, reviewer)
			} else {
				runs = append(runs, Run{ReviewOf: r.ID, Model: "missing designated reviewer", Provider: "unavailable"})
			}
		}
	}
	return runs
}

// Contains sealed values only in memory; the persistent fingerprint is a hash.
func (e *Engine) readinessFingerprint(r Run) string {
	s := e.Store
	var task Assignment
	_ = s.Get(r.Task, &task)
	// Progress and task state are checked separately at admission. Sibling work
	// must not invalidate otherwise unchanged prerequisite observations.
	configuration := struct {
		Org, Creator, Account, ExtraAccount, Execution, Authority string
		Tools                                                     []string
		ConstrainTools, ConstrainCapabilities                     bool
		Capabilities                                              []CapabilityGrant
	}{task.Org, task.Creator, task.Account, task.ExtraAccount, task.Execution, task.Authority, task.Tools, task.ConstrainTools, task.ConstrainCapabilities, task.Capabilities}
	values := []any{r.Preflight, r.Model, r.Provider, r.Account, r.Tools, r.RequiredTools, r.Execution, r.Workspace, configuration}
	for _, candidate := range e.readinessModels(r) {
		a, err := s.runAccount(task, candidate)
		var agent Agent
		_ = s.Get(candidate.Agent, &agent)
		values = append(values, candidate.Model, candidate.Provider, a, fmt.Sprint(err), agent)
	}
	if r.Preflight != nil {
		for _, repo := range r.Preflight.Repositories {
			var c Connection
			_ = s.Get(repo.Connection, &c)
			var tool GatewayTool
			_ = s.Get(digest(r.Org+":"+repo.Connection+":github_repository"), &tool)
			var policy ToolPolicy
			_ = s.Get("policy-"+tool.ID, &policy)
			values = append(values, c, tool, policy)
		}
	}
	b, _ := json.Marshal(values)
	return digest(string(b))
}

// Called under Store.mu. Checks run independently of model concurrency slots.
func (e *Engine) ensurePreflight(ctx context.Context, r Run) bool {
	if r.Preflight == nil {
		return true
	}
	s := e.Store
	fingerprint := e.readinessFingerprint(r)
	prior := s.runReadiness(r.ID)
	if prior.Fingerprint == fingerprint && timeFuture(prior.NextAt) {
		if prior.State == "ready" {
			if len(r.Preflight.Directories)+len(r.Preflight.Ports) == 0 || s.runResources(r.ID).State == "owned" {
				return true
			}
		}
		if prior.State == "blocked" {
			return false
		}
	}
	e.mu.Lock()
	if e.preflightActive[r.ID] || len(e.preflightActive) >= 4 {
		e.mu.Unlock()
		return false
	}
	e.mu.Unlock()
	if prior.State == "checking" && timeFuture(prior.Lease) {
		return false
	}
	claim := ExecutionReadiness{ID: "preflight:" + r.ID, Org: r.Org, Task: r.Task, Run: r.ID, Fingerprint: fingerprint, State: "checking", Generation: prior.Generation + 1, Lease: time.Now().Add(90 * time.Second).UTC().Format(time.RFC3339Nano)}
	if s.Put("preflight", r.Org, r.Task, claim.State, claim.ID, claim) != nil {
		return false
	}
	if r.Error == "" {
		r.Error = "Checking declared execution prerequisites"
	}
	_ = s.Put("run", r.Org, r.Task, r.State, r.ID, r)
	models := e.readinessModels(r)
	e.mu.Lock()
	e.preflightActive[r.ID] = true
	e.mu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() { e.mu.Lock(); delete(e.preflightActive, r.ID); e.mu.Unlock() }()
		checkCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		checks := e.probeReadiness(checkCtx, r, models)
		s.mu.Lock()
		defer s.mu.Unlock()
		current := s.runReadiness(r.ID)
		var run Run
		var task Assignment
		if current.Generation != claim.Generation {
			return
		}
		if s.Get(r.ID, &run) != nil {
			claim.State = "stopped"
			claim.Lease = ""
			_ = s.Put("preflight", r.Org, r.Task, claim.State, claim.ID, claim)
			return
		}
		stop := func(state string) {
			claim.State = state
			claim.Lease = ""
			if run.Error == "Checking declared execution prerequisites" {
				run.Error = ""
			}
			_ = s.Batch(Write{"preflight", r.Org, r.Task, claim.State, claim.ID, claim}, Write{"run", run.Org, run.Task, run.State, run.ID, run})
		}
		if run.State != "queued" || run.Superseded || s.Get(r.Task, &task) != nil || task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
			stop("stopped")
			return
		}
		if e.readinessFingerprint(run) != claim.Fingerprint {
			stop("stale")
			return
		}
		claim.Checks = checks
		claim.Lease = ""
		claim.CheckedAt = now()
		claim.State = "ready"
		run.Error = ""
		for _, check := range checks {
			if check.State != "pass" {
				claim.State = "blocked"
				run.Error = "Preflight: " + check.Name + ": " + check.Detail
				break
			}
		}
		claim.NextAt = time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
		writes := []Write{}
		for _, check := range checks {
			if check.Name == "Definition" && check.State != "pass" {
				writes = append(writes, e.escalationWrites(&run, "Invalid preflight declaration: "+check.Detail+". Correct it using adc_repair_step; retries cannot change the declaration.")...)
			}
		}
		writes = append(writes, Write{"preflight", r.Org, r.Task, claim.State, claim.ID, claim}, Write{"run", run.Org, run.Task, run.State, run.ID, run})
		if err := s.Batch(writes...); err == nil {
			s.Log(r.Org, r.Task, r.ID, "preflight", "Execution prerequisites: "+claim.State)
		}
	}()
	return false
}

func (e *Engine) probeReadiness(ctx context.Context, r Run, models []Run) []PreflightCheck {
	checks := []PreflightCheck{}
	add := func(name, detail string, err error) {
		c := PreflightCheck{Name: name, State: "pass", Detail: detail}
		if err != nil {
			c.State = "blocked"
			c.Detail = (Redactor{}).Text(clipped(err.Error(), 2000))
		}
		checks = append(checks, c)
	}
	if err := validatePreflight(r.Preflight); err != nil {
		add("Definition", "", err)
		return checks
	}
	for _, command := range r.Preflight.Commands {
		var err error
		if r.Execution == "protected" {
			x, xerr := runExecutor(r)
			err = xerr
			if err == nil {
				out, xerr := x.Execute(ctx, workspaceCommand{Command: "command -v " + shellQuote(command), TimeoutSeconds: 10})
				err = xerr
				if err == nil && out.ExitCode != 0 {
					err = fmt.Errorf("%s is unavailable in the protected worker environment", command)
				}
			}
		} else {
			_, err = exec.LookPath(command)
		}
		add("Runtime: "+command, "Executable available (version compatibility remains part of validation)", err)
	}
	for _, repo := range r.Preflight.Repositories {
		err := e.probeRepository(ctx, r, repo)
		add("Repository: "+repo.Owner+"/"+repo.Repository, "Metadata and reported token access verified; no write was performed", err)
	}
	if r.Preflight.Models {
		for _, candidate := range models {
			var task Assignment
			_ = e.Store.Get(r.Task, &task)
			account, err := e.Store.runAccount(task, candidate)
			if err == nil {
				var catalog []Model
				catalog, err = e.preflightModels(ctx, account)
				if err == nil {
					found := false
					for _, m := range catalog {
						if m.ID == candidate.Model {
							found = true
							break
						}
					}
					if !found {
						err = fmt.Errorf("selected model %s is unavailable in its funded account", candidate.Model)
					}
				}
				if err != nil {
					token, _ := e.Store.Unseal(account.Secret)
					err = fmt.Errorf("%s", (Redactor{Values: []string{token}}).Text(err.Error()))
				}
			}
			label := "Worker model"
			if candidate.ReviewOf != "" {
				label = "Independent review model"
			}
			add(label, candidate.Model+" is listed by the selected provider; this is not a capacity reservation", err)
		}
	}
	for _, c := range checks {
		if c.State != "pass" {
			return checks
		}
	}
	if len(r.Preflight.Directories) > 0 || len(r.Preflight.Ports) > 0 {
		err := e.prepareRunResources(ctx, r)
		add("Owned test resources", "Per-run directories (advisory mode is not a security boundary) and port suggestions recorded in adc_status. Ports are coordinated within ADC, not exclusively reserved against other host processes. Test data is retained with the workspace.", err)
	}
	return checks
}

func (e *Engine) probeRepository(ctx context.Context, r Run, repo RepositoryPreflight) error {
	s := e.Store
	raw, _ := json.Marshal(map[string]string{"Owner": repo.Owner, "Repository": repo.Repository})
	var tool GatewayTool
	admit := func() error {
		var current Run
		var agent Agent
		var conn Connection
		var policy ToolPolicy
		if s.Get(r.ID, &current) != nil || current.Superseded || current.State != "queued" || !Subset([]string{repo.Connection}, current.Tools) || s.Get(current.Agent, &agent) != nil || agent.Org != r.Org || !Subset([]string{repo.Connection}, agent.Tools) {
			return fmt.Errorf("repository connection is not granted to this queued worker")
		}
		if s.Get(repo.Connection, &conn) != nil || conn.Org != r.Org || conn.Transport != "github" {
			return fmt.Errorf("repository preflight needs a built-in GitHub connection")
		}
		var fresh GatewayTool
		if s.Get(digest(r.Org+":"+repo.Connection+":github_repository"), &fresh) != nil || s.Get("policy-"+fresh.ID, &policy) != nil || policy.Class != "read" {
			return fmt.Errorf("discover and classify repository metadata as read access")
		}
		if tool.ID != "" && tool.Fingerprint != fresh.Fingerprint {
			return fmt.Errorf("repository tool changed during preflight")
		}
		tool = fresh
		_, err := s.authorizeGatewayRun(current, tool, "", raw)
		return err
	}
	s.mu.Lock()
	err := admit()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	result, err := s.invokeGateway(ctx, tool, raw, func() error { s.mu.Lock(); defer s.mu.Unlock(); return admit() })
	if err != nil {
		return err
	}
	var response struct {
		Structured struct {
			Data struct {
				FullName    string `json:"full_name"`
				Permissions struct{ Pull, Push bool }
			}
		} `json:"structuredContent"`
	}
	if json.Unmarshal([]byte(result), &response) != nil || !strings.EqualFold(response.Structured.Data.FullName, repo.Owner+"/"+repo.Repository) {
		return fmt.Errorf("repository identity was not verified")
	}
	if repo.Write && !response.Structured.Data.Permissions.Push {
		return fmt.Errorf("connection does not report push access; supply an appropriately scoped connection")
	}
	return nil
}

func (e *Engine) prepareRunResources(ctx context.Context, r Run) error {
	if err := validatePreflight(r.Preflight); err != nil {
		return err
	}
	s := e.Store
	s.mu.Lock()
	var current Run
	var task Assignment
	if s.Get(r.ID, &current) != nil || current.State != "queued" || current.Superseded || s.Get(r.Task, &task) != nil || task.State == "paused" || task.State == "cancelled" || task.State == "ready" {
		s.mu.Unlock()
		return fmt.Errorf("run no longer eligible for test resources")
	}
	resources := s.runResources(r.ID)
	if resources.ID == "" {
		resources = RunResources{ID: "resources:" + r.ID, Org: r.Org, Task: r.Task, Run: r.ID, State: "owned", Created: now(), Directories: map[string]string{}, Ports: map[string]int{}}
	}
	previous := resources
	reacquire := resources.State == "released"
	if reacquire {
		resources.State = "owned"
		resources.Released = ""
		resources.Ports = map[string]int{}
	}
	used := map[int]bool{}
	for _, owned := range list[RunResources](s, "run-resources", "") {
		if owned.State == "owned" {
			for _, port := range owned.Ports {
				used[port] = true
			}
		}
	}
	var allocationErr error
	for _, name := range r.Preflight.Ports {
		if resources.Ports[name] != 0 {
			continue
		}
		for attempt := 0; attempt < 20; attempt++ {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				allocationErr = err
				break
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			if !used[port] {
				resources.Ports[name] = port
				used[port] = true
				break
			}
		}
		if resources.Ports[name] == 0 && allocationErr == nil {
			allocationErr = fmt.Errorf("no available ADC test port for %s", name)
		}
	}
	for _, name := range r.Preflight.Directories {
		resources.Directories[name] = "/workspace/.adc-test/" + name
		if r.Execution != "protected" {
			resources.Directories[name] = filepath.Join(r.Workspace, ".adc-test", name)
		}
	}
	if allocationErr == nil {
		writes := []Write{{"run-resources", r.Org, r.Task, resources.State, resources.ID, resources}}
		if reacquire {
			previous.ID = ID()
			writes = append(writes, Write{"run-resource-history", previous.Org, previous.Task, previous.State, previous.ID, previous})
		}
		allocationErr = s.Batch(writes...)
	}
	s.mu.Unlock()
	if allocationErr != nil {
		return allocationErr
	}
	if r.Execution != "protected" {
		// Root-relative operations cannot follow a worker symlink outside ADC data.
		root, err := os.OpenRoot(s.Dir)
		if err != nil {
			return err
		}
		defer root.Close()
		rel, err := filepath.Rel(s.Dir, r.Workspace)
		if err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("workspace must be inside ADC data")
		}
		if err := root.MkdirAll(rel, 0700); err != nil {
			return err
		}
		path := ""
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			path = filepath.Join(path, part)
			info, err := root.Lstat(path)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("workspace must not traverse a symlink")
			}
		}
		workspace, err := root.OpenRoot(rel)
		if err != nil {
			return err
		}
		defer workspace.Close()
		for _, name := range r.Preflight.Directories {
			if err := workspace.MkdirAll(filepath.Join(".adc-test", name), 0700); err != nil {
				return err
			}
		}
		return nil
	}
	x, err := runExecutor(r)
	if err != nil {
		return err
	}
	// mkdir occurs inside the worker boundary; hostile workspace symlinks never
	// cause host-side file operations. The data remains owned by this run.
	for _, path := range resources.Directories {
		out, err := x.Execute(ctx, workspaceCommand{Command: "mkdir -p -- " + shellQuote(path), TimeoutSeconds: 10})
		if err != nil {
			return err
		}
		if out.ExitCode != 0 {
			return fmt.Errorf("test directory unavailable: %s", path)
		}
	}
	return nil
}

// Release only ADC's port coordination records. Retain files and evidence for
// review/debugging; no path from an untrusted worker reaches host removal.
func (e *Engine) releaseRunResources() {
	s := e.Store
	for _, v := range list[RunResources](s, "run-resources", "") {
		if v.State != "owned" {
			continue
		}
		var task Assignment
		var run Run
		if s.Get(v.Task, &task) != nil || s.Get(v.Run, &run) != nil {
			continue
		}
		if task.State != "ready" && task.State != "cancelled" && !(run.Superseded || run.State == "cancelled") {
			continue
		}
		e.mu.Lock()
		active := e.active[run.ID] != nil || e.preflightActive[run.ID]
		e.mu.Unlock()
		if active {
			continue
		}
		v.State = "released"
		v.Released = now()
		_ = s.Put("run-resources", v.Org, v.Task, v.State, v.ID, v)
	}
}

func samePreflight(a, b *PreflightSpec) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func taskReadiness(s *Store, task string) []ExecutionReadiness {
	out := []ExecutionReadiness{}
	for _, v := range list[ExecutionReadiness](s, "preflight", "") {
		if v.Task == task {
			out = append(out, v)
		}
	}
	return out
}
func taskResources(s *Store, task string) []RunResources {
	out := []RunResources{}
	for _, v := range list[RunResources](s, "run-resources", "") {
		if v.Task == task {
			out = append(out, v)
		}
	}
	return out
}

func timeFuture(value string) bool {
	t, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && time.Now().Before(t)
}
