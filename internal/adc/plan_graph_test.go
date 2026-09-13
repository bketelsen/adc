package adc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanGraphPreservesEdgesAndRanksWithoutChangingPlan(t *testing.T) {
	p := ExecutionPlan{State: "active", Steps: []PlanStep{
		{PlanStepSpec: PlanStepSpec{Key: "D", DependsOn: []string{"B", "C"}}, State: "waiting"},
		{PlanStepSpec: PlanStepSpec{Key: "A"}, State: "complete"},
		{PlanStepSpec: PlanStepSpec{Key: "B", DependsOn: []string{"A"}}, State: "running"},
		{PlanStepSpec: PlanStepSpec{Key: "C", DependsOn: []string{"A"}}, State: "blocked"},
	}}
	before, _ := json.Marshal(p)
	g := planGraph(p)
	after, _ := json.Marshal(p)
	if !bytes.Equal(before, after) || len(g.Nodes) != 4 || len(g.Edges) != 4 || g.Complete != 1 || g.Active != 1 || g.Blocked != 1 || g.Waiting != 1 {
		t.Fatal("projection lost graph or changed plan")
	}
	nodes := map[string]PlanGraphNode{}
	for _, n := range g.Nodes {
		nodes[n.Key] = n
	}
	for _, e := range g.Edges {
		if nodes[e.From].X >= nodes[e.To].X {
			t.Fatal("edge does not point forward", e)
		}
	}
	if nodes["B"].Y == nodes["C"].Y {
		t.Fatal("parallel cards overlap")
	}
	p.State = "draft"
	for _, n := range planGraph(p).Nodes {
		if n.State != "draft" {
			t.Fatal("draft presented as executed")
		}
	}
}

func TestPlanGraphEscapesSourceContent(t *testing.T) {
	s, e, task, _ := fixture(t)
	w := NewWeb(s, e, false)
	p := Page{Org: Organization{ID: "org"}, Task: task, Plan: ExecutionPlan{ID: "plan", State: "draft", Title: "Fixture", Steps: []PlanStep{{PlanStepSpec: PlanStepSpec{Key: "A", Title: `<script>alert("fixture")</script>`}, Worker: Agent{Name: `<img onerror="fixture">`}}}}}
	var out bytes.Buffer
	must(t, w.templates.ExecuteTemplate(&out, "plan-graph", p))
	if strings.Contains(out.String(), "<script>") || strings.Contains(out.String(), "<img ") || !strings.Contains(out.String(), "&lt;script&gt;") {
		t.Fatal("untrusted graph label not escaped")
	}
}

func TestBrowserPlanGraph(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for visual plan qualification")
	}
	s, e, task, root := fixture(t)
	transcriptLogin(t, s)
	p := ExecutionPlan{ID: "plan:" + task.ID, Org: task.Org, Task: task.ID, Supervisor: root.ID, Title: "Synthetic multi-repository rollout", Source: "Synthetic graph fixture; no real work or external actions.", State: "active", Revision: 1}
	for i := 0; i < 33; i++ {
		key := fmt.Sprintf("N%02d", i+1)
		deps := []string{}
		if i >= 5 {
			deps = append(deps, fmt.Sprintf("N%02d", i-4))
			if i%5 > 0 {
				deps = append(deps, fmt.Sprintf("N%02d", i-5))
			}
		}
		step := PlanStep{PlanStepSpec: PlanStepSpec{Key: key, Title: []string{"Verify recovery baseline", "Validate package inputs", "Review release contract", "Prepare consumer migration", "Record support evidence"}[i%5], Prompt: "Bounded synthetic work for graph inspection.", Criteria: "Preserve exact fixture evidence.", DependsOn: deps}, Worker: Agent{Name: "Fixture Engineer"}, Verifier: Agent{Name: "Independent Reviewer"}, State: "waiting"}
		if i == 0 {
			step.State = "blocked"
			step.Reason = "Fixture human evidence is missing; other branches can proceed."
		}
		if i == 1 {
			r := root
			r.ID = "graph-worker"
			r.State = "running"
			must(t, s.Put("run", r.Org, r.Task, r.State, r.ID, r))
			step.Run = r.ID
		}
		p.Steps = append(p.Steps, step)
	}
	must(t, s.Put("execution-plan", p.Org, p.Task, p.State, p.ID, p))
	mux := http.NewServeMux()
	mux.Handle("/", NewWeb(s, e, false).Handler())
	mux.HandleFunc("/fixture-graph-update", func(w http.ResponseWriter, r *http.Request) {
		var run Run
		must(t, s.Get("graph-worker", &run))
		run.State = "blocked"
		run.Error = "Changed fixture blocker"
		must(t, s.Put("run", run.Org, run.Task, run.State, run.ID, run))
		w.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/plan-graph.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
}
