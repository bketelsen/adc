package adc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveInterruptedActionReconcilesWithoutRepeating(t *testing.T) {
	node := os.Getenv("ADC_LIVE_RECOVERY_NODE")
	if node == "" {
		t.Skip("set ADC_LIVE_RECOVERY_NODE to qualify interrupted provider work on a synthetic receipt fixture")
	}
	s, e, task, root := fixture(t)
	account := Account{ID: "account", User: "owner", Local: true, Limit: 1}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	file := filepath.Join(s.Dir, "fixture-receipt")
	sealed, err := s.Seal(file)
	must(t, err)
	script, err := filepath.Abs("testdata/recovery.cjs")
	must(t, err)
	conn := Connection{ID: "storage", Org: task.Org, Name: "recovery-fixture", Transport: "stdio", Command: node, Args: []string{script}, Env: map[string]string{"ADC_RECEIPT_FILE": sealed}}
	must(t, s.Put("connection", conn.Org, "", "", conn.ID, conn))
	root.State = "queued"
	root.Prompt = "Isolated ADC recovery qualification. Use only the recovery-fixture MCP tools and ADC tools. The intended outcome is exactly one durable synthetic receipt. Inspect the receipt count first; if zero, call append_receipt once. If one already exists, do not append again. When the count is exactly one, save a document recording the observation and call adc_decision with Question 'Review recovered fixture'. No shell, delegation or real infrastructure. Before repeating an interrupted action, inspect the durable receipt."
	task.Prompt = root.Prompt
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	e.Start(ctx)
	stopped := false
	defer func() {
		if !stopped {
			e.Stop()
		}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("fixture action never committed")
		case <-ticker.C:
			if b, _ := os.ReadFile(file); len(b) > 0 {
				goto committed
			}
		}
	}
committed:
	// Stop after the external action commits but while its provider response is withheld.
	e.Stop()
	stopped = true
	must(t, s.Get(root.ID, &root))
	if root.State != "queued" {
		t.Fatalf("interrupted run not recoverable: %s", root.State)
	}
	traces := taskTraces(s, task.ID)
	interrupted := false
	for _, trace := range traces {
		if strings.Contains(trace.Name, "append_receipt") && trace.State == "interrupted" {
			interrupted = true
		}
	}
	if !interrupted {
		t.Fatal("uncertain action not preserved in tool evidence")
	}
	// A fresh engine represents the service restart, retaining only persisted state.
	recovered := NewEngine(s)
	recovered.Start(ctx)
	defer recovered.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("recovered fixture timed out")
		case <-ticker.C:
			for _, decision := range taskDecisions(s, task.ID) {
				if decision.State == "pending" {
					b, err := os.ReadFile(file)
					must(t, err)
					if count := strings.Count(string(b), "fixture-action"); count != 1 {
						t.Fatalf("non-idempotent action ran %d times", count)
					}
					if len(taskDocs(s, task.ID)) == 0 {
						t.Fatal("recovery evidence missing")
					}
					t.Log("Fresh engine inspected the committed receipt, avoided repeating the non-idempotent action, and returned its evidence for review")
					return
				}
			}
		}
	}
}
