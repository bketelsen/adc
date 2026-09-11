package adc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveMCPInventory(t *testing.T) {
	node := os.Getenv("ADC_LIVE_MCP_NODE")
	if node == "" {
		t.Skip("set ADC_LIVE_MCP_NODE to qualify MCP through the signed-in Copilot subscription")
	}
	s, e, task, root := fixture(t)
	account := Account{ID: "account", User: "owner", Name: "MCP qualification", Local: true, Limit: 1}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	marker := ID()
	secret, err := s.Seal(marker)
	must(t, err)
	script, err := filepath.Abs("testdata/inventory.cjs")
	must(t, err)
	c := Connection{ID: "storage", Org: task.Org, Name: "inventory-fixture", Transport: "stdio", Command: node, Args: []string{script}, Env: map[string]string{"ADC_FIXTURE_MARKER": secret}}
	must(t, s.Put("connection", c.Org, "", "", c.ID, c))
	root.Prompt = "This is an isolated synthetic MCP qualification. Call the inventory tool from the assigned inventory-fixture MCP server. Save exactly the returned inventory, including its inventory_id, in adc_document. Then call adc_decision with Question 'Review fixture inventory'. Do not delegate, use shell, or contact real infrastructure. You cannot know the inventory_id without invoking the MCP tool."
	root.State = "queued"
	task.Prompt = root.Prompt
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	e.Start(ctx)
	defer e.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("MCP qualification timed out")
		case <-ticker.C:
			for _, d := range taskDecisions(s, task.ID) {
				if d.State == "pending" {
					for _, doc := range taskDocs(s, task.ID) {
						if strings.Contains(doc.Content, marker) {
							t.Log("MCP returned the fresh inventory marker through encrypted connection configuration and the provider saved it in a document")
							return
						}
					}
					t.Fatalf("inventory proof missing; decision: %s", d.Question)
				}
			}
		}
	}
}
