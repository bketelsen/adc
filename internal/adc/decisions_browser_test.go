package adc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserDecisionBrief(t *testing.T) {
	node := os.Getenv("ADC_BROWSER_NODE")
	if node == "" {
		t.Skip("set ADC_BROWSER_NODE for decision qualification")
	}
	s, e, r, d := acceptanceFixture(t, false)
	w := NewWeb(s, e, false)
	handler := w.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/fixture-update" {
			s.mu.Lock()
			defer s.mu.Unlock()
			other := Decision{ID: "fixture-other-decision", Org: d.Org, Task: d.Task, Run: d.Run, State: "pending", Brief: "Choose a different support window.", Question: "Fixture supporting details only."}
			must(t, s.Put("decision", d.Org, d.Task, other.State, other.ID, other))
			rw.WriteHeader(204)
			return
		}
		handler.ServeHTTP(rw, req)
	}))
	defer server.Close()
	script, err := filepath.Abs("testdata/browser/decision-brief.cjs")
	must(t, err)
	out, err := exec.Command(node, script, server.URL).CombinedOutput()
	t.Log(string(out))
	must(t, err)
	must(t, s.Get(d.ID, &d))
	v := s.milestoneEvidence(r.ID, "gate")
	if d.Outcome != "approve" || v.Actor != "human:owner" || v.Revision != 1 {
		t.Fatal("browser approval did not record acceptance")
	}
	var other Decision
	must(t, s.Get("fixture-other-decision", &other))
	if other.Outcome != "refine" {
		t.Fatal("browser refinement not saved")
	}
}
