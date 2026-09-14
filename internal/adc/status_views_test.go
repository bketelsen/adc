package adc

import (
	"strings"
	"testing"
)

func TestFocusedStatusAvoidsHistoricalPayloadAndRejectsForeignRuns(t *testing.T) {
	s, e, task, root := fixture(t)
	child := Run{ID: "child-status", Org: task.Org, Task: task.ID, Parent: root.ID, State: "complete", Title: "Evidence owner", Prompt: strings.Repeat("long historical prompt ", 3000), Result: strings.Repeat("long observed result ", 3000)}
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	raw, err := call(t, e, root, "adc_status", statusInput{View: "summary"})
	must(t, err)
	if len(raw) > 4000 || strings.Contains(raw, "long historical prompt") {
		t.Fatal("summary carried historical context", len(raw))
	}
	exact, err := call(t, e, root, "adc_status", statusInput{View: "run", ID: child.ID})
	must(t, err)
	if !strings.Contains(exact, "long historical prompt") {
		t.Fatal("targeted read lost exact context")
	}
	child.Task = "elsewhere"
	must(t, s.Put("run", child.Org, child.Task, child.State, child.ID, child))
	if _, err = call(t, e, root, "adc_status", statusInput{View: "run", ID: child.ID}); err == nil {
		t.Fatal("focused run read crossed assignment")
	}
}
