package adc

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func checkPreflight(t *testing.T, e *Engine, r Run) ExecutionReadiness {
	t.Helper()
	e.Store.mu.Lock()
	e.ensurePreflight(context.Background(), r)
	e.Store.mu.Unlock()
	e.wg.Wait()
	return e.Store.runReadiness(r.ID)
}
func preflightPlan(t *testing.T) (*Store, *Engine, Run, Run) {
	t.Helper()
	s, e, _, root := fixture(t)
	input := planFixtureInput()
	input.Steps = input.Steps[:1]
	input.Steps[0].Preflight = &PreflightSpec{Models: true}
	input.Start = true
	_, err := e.saveExecutionPlan(root, input)
	must(t, err)
	_, err = call(t, e, root, "adc_wait", map[string]any{})
	must(t, err)
	planDispatch(e)
	step := planStepByKey(t, s, "R1")
	var worker, reviewer Run
	must(t, s.Get(step.Run, &worker))
	must(t, s.Get(step.Review, &reviewer))
	return s, e, worker, reviewer
}
func TestPreflightHoldsWorkerForReviewerAndRecoversWithoutModelSlot(t *testing.T) {
	s, e, worker, reviewer := preflightPlan(t)
	var activations atomic.Int32
	e.runActivation = func(context.Context, Run, Assignment, Account) { activations.Add(1) }
	available := false
	e.preflightModels = func(context.Context, Account) ([]Model, error) {
		out := []Model{{ID: worker.Model}}
		if available {
			out = append(out, Model{ID: reviewer.Model})
		}
		return out, nil
	}
	e.tick(context.Background())
	e.wg.Wait()
	v := s.runReadiness(worker.ID)
	if v.State != "blocked" || activations.Load() != 0 {
		t.Fatal("missing review model consumed activation", v, activations.Load())
	}
	var current Run
	must(t, s.Get(worker.ID, &current))
	if current.Activations != 0 || !strings.Contains(current.Error, "Independent review model") {
		t.Fatal(current.Error)
	}
	// Reopen persisted state, expire the retry timer, and observe recovery.
	reopened, err := Open(s.Dir)
	must(t, err)
	defer reopened.Close()
	fresh := NewEngine(reopened)
	fresh.runActivation = e.runActivation
	fresh.preflightModels = e.preflightModels
	available = true
	v.NextAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	must(t, reopened.Put("preflight", v.Org, v.Task, v.State, v.ID, v))
	fresh.tick(context.Background())
	fresh.wg.Wait()
	if ready := reopened.runReadiness(worker.ID); ready.State != "ready" {
		t.Fatal(ready)
	}
	fresh.tick(context.Background())
	fresh.wg.Wait()
	if activations.Load() != 1 {
		t.Fatal("recovered worker not dispatched once", activations.Load())
	}
}
func TestPreflightConfigurationChangesDiscardInflightResult(t *testing.T) {
	s, e, worker, reviewer := preflightPlan(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	e.preflightModels = func(context.Context, Account) ([]Model, error) {
		if !once.Swap(true) {
			close(started)
			<-release
		}
		return []Model{{ID: worker.Model}, {ID: reviewer.Model}}, nil
	}
	s.mu.Lock()
	e.ensurePreflight(context.Background(), worker)
	s.mu.Unlock()
	<-started
	s.mu.Lock()
	var account Account
	must(t, s.Get(worker.Account, &account))
	account.Secret = "changed sealed configuration"
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	s.mu.Unlock()
	close(release)
	e.wg.Wait()
	if v := s.runReadiness(worker.ID); v.State != "stale" {
		t.Fatal("stale probe was accepted", v)
	}
	var current Run
	must(t, s.Get(worker.ID, &current))
	if current.Error == "Checking declared execution prerequisites" {
		t.Fatal("stale probe still advertised as running")
	}
}
func TestPreflightValidationRejectsCommandsAndInvalidResources(t *testing.T) {
	for _, spec := range []PreflightSpec{{Commands: []string{"git;echo secret"}}, {Commands: []string{"/host/bin/git"}}, {Directories: []string{"../other"}}, {Ports: []string{"db", "db"}}, {Repositories: []RepositoryPreflight{{Connection: "github", Owner: "../other", Repository: "repo"}}}} {
		if validatePreflight(&spec) == nil {
			t.Fatal("unsafe preflight accepted", spec)
		}
	}
}
func TestPreflightRecoveryLeaseDoesNotDuplicateProbe(t *testing.T) {
	s, e, worker, reviewer := preflightPlan(t)
	var calls atomic.Int32
	e.preflightModels = func(context.Context, Account) ([]Model, error) {
		calls.Add(1)
		return []Model{{ID: worker.Model}, {ID: reviewer.Model}}, nil
	}
	v := ExecutionReadiness{ID: "preflight:" + worker.ID, Org: worker.Org, Task: worker.Task, Run: worker.ID, Fingerprint: e.readinessFingerprint(worker), State: "checking", Generation: 4, Lease: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	must(t, s.Put("preflight", v.Org, v.Task, v.State, v.ID, v))
	checkPreflight(t, e, worker)
	if calls.Load() != 0 {
		t.Fatal("unexpired lease duplicated probe")
	}
	v.Lease = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	must(t, s.Put("preflight", v.Org, v.Task, v.State, v.ID, v))
	actual := checkPreflight(t, e, worker)
	if actual.State != "ready" || actual.Generation != 5 {
		t.Fatal(actual)
	}
}

func TestPreflightSiblingProgressDoesNotInvalidateObservation(t *testing.T) {
	s, e, worker, reviewer := preflightPlan(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	e.preflightModels = func(context.Context, Account) ([]Model, error) {
		if !once.Swap(true) {
			close(entered)
			<-release
		}
		return []Model{{ID: worker.Model}, {ID: reviewer.Model}}, nil
	}
	s.mu.Lock()
	e.ensurePreflight(context.Background(), worker)
	s.mu.Unlock()
	<-entered
	s.mu.Lock()
	var task Assignment
	must(t, s.Get(worker.Task, &task))
	task.State = "running"
	task.Output = "Sibling progress"
	task.Revision++
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	s.mu.Unlock()
	close(release)
	e.wg.Wait()
	if v := s.runReadiness(worker.ID); v.State != "ready" {
		t.Fatal("sibling progress invalidated preflight", v)
	}
}
func TestPreflightPauseStopsVisibleProbeAndResumeRetries(t *testing.T) {
	s, e, worker, reviewer := preflightPlan(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	e.preflightModels = func(context.Context, Account) ([]Model, error) {
		if !once.Swap(true) {
			close(entered)
			<-release
		}
		return []Model{{ID: worker.Model}, {ID: reviewer.Model}}, nil
	}
	s.mu.Lock()
	e.ensurePreflight(context.Background(), worker)
	s.mu.Unlock()
	<-entered
	s.mu.Lock()
	var task Assignment
	must(t, s.Get(worker.Task, &task))
	task.State = "paused"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	s.mu.Unlock()
	close(release)
	e.wg.Wait()
	var current Run
	must(t, s.Get(worker.ID, &current))
	if v := s.runReadiness(worker.ID); v.State != "stopped" || v.Lease != "" || current.Error != "" {
		t.Fatal("stopped probe still advertised as running", v, current.Error)
	}
	task.State = "queued"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	if v := checkPreflight(t, e, current); v.State != "ready" {
		t.Fatal("resumed preflight held by stale lease", v)
	}
}
func TestPreflightRejectsForeignDesignatedReviewer(t *testing.T) {
	s, e, worker, reviewer := preflightPlan(t)
	reviewer.Org = "foreign-org"
	must(t, s.Put("run", reviewer.Org, reviewer.Task, reviewer.State, reviewer.ID, reviewer))
	candidates := e.readinessModels(worker)
	if len(candidates) != 2 || candidates[1].Model != "missing designated reviewer" {
		t.Fatal("foreign reviewer selected", candidates)
	}
}

func TestPreflightConcurrencyCapDoesNotOccupyAgentSlots(t *testing.T) {
	s, e, _, root := fixture(t)
	entered, release := make(chan struct{}, 4), make(chan struct{})
	var calls atomic.Int32
	e.preflightModels = func(context.Context, Account) ([]Model, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return []Model{{ID: root.Model}}, nil
	}
	runs := []Run{}
	for i := 0; i < 5; i++ {
		r := root
		r.ID = ID()
		r.State = "queued"
		r.Preflight = &PreflightSpec{Models: true}
		must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
		runs = append(runs, r)
		s.mu.Lock()
		e.ensurePreflight(context.Background(), r)
		s.mu.Unlock()
	}
	for i := 0; i < 4; i++ {
		<-entered
	}
	if calls.Load() != 4 || s.runReadiness(runs[4].ID).ID != "" {
		t.Fatal("preflight concurrency cap bypassed")
	}
	e.mu.Lock()
	active := len(e.active)
	e.mu.Unlock()
	if active != 0 {
		t.Fatal("preflight occupied agent slots")
	}
	close(release)
	e.wg.Wait()
	if v := checkPreflight(t, e, runs[4]); v.State != "ready" || calls.Load() != 5 {
		t.Fatal("fifth probe did not resume", v)
	}
}
