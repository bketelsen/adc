package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// prove exercises actual model calls with an intentionally incorrect fixture.
// Tools are narrowly declared so the proof cannot change a real repository.
func prove() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	c := copilot.NewClient(&copilot.ClientOptions{LogLevel: "error"})
	if err := c.Start(ctx); err != nil {
		return err
	}
	defer c.Stop()
	models, err := c.ListModels(ctx)
	if err != nil {
		return err
	}
	enabled := map[string]bool{}
	for _, m := range models {
		enabled[m.ID] = m.Policy == nil || m.Policy.State == "enabled"
	}
	for _, id := range []string{"gpt-6-astra", "claude-opus-5"} {
		if !enabled[id] {
			return fmt.Errorf("required proof model unavailable: %s", id)
		}
	}
	var mu sync.Mutex
	artifact := "Current: Snow, NBC. Upcoming: Floe."
	reports := []map[string]any{}
	run := func(model, prompt string, write bool) (string, error) {
		tools := []copilot.Tool{copilot.DefineTool("read_fixture", "Read the authoritative shipping inventory and candidate website copy.", func(_ struct{}, _ copilot.ToolInvocation) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			return "AUTHORITATIVE FIXTURE: Snow is shipping. Floe is shipping. NBC is retired. Forky support is planned, not shipping.\nCANDIDATE: " + artifact, nil
		})}
		if write {
			tools = append(tools, copilot.DefineTool("write_candidate", "Replace the candidate copy with corrected copy.", func(p struct {
				Content string `json:"content"`
			}, _ copilot.ToolInvocation) (string, error) {
				mu.Lock()
				defer mu.Unlock()
				artifact = p.Content
				return "Saved", nil
			}))
		}
		s, err := c.CreateSession(ctx, &copilot.SessionConfig{Model: model, Tools: tools, AvailableTools: func() []string {
			n := []string{}
			for _, t := range tools {
				n = append(n, t.Name)
			}
			return n
		}(), OnPermissionRequest: copilot.PermissionHandler.ApproveAll, SystemMessage: &copilot.SystemMessageConfig{Content: "This is a synthetic ADC integration fixture. Use only the supplied tools. Do not inspect any other data. Return a concise result."}})
		if err != nil {
			return "", err
		}
		defer s.Disconnect()
		e, err := s.SendAndWait(ctx, copilot.MessageOptions{Prompt: prompt})
		if err != nil {
			return "", err
		}
		if e == nil {
			return "", fmt.Errorf("no result")
		}
		d, ok := e.Data.(*copilot.AssistantMessageData)
		if !ok {
			return "", fmt.Errorf("unexpected result %T", e.Data)
		}
		reports = append(reports, map[string]any{"model": model, "prompt": prompt, "result": d.Content})
		fmt.Printf("%s: %s\n", model, d.Content)
		return d.Content, nil
	}
	findings, err := run("claude-opus-5", "Read the fixture and identify factual errors in the candidate. Do not fix the candidate. State whether it passes or needs changes.", false)
	if err != nil {
		return err
	}
	_, err = run("gpt-6-astra", "Read the fixture and fix every factual error using write_candidate. Review findings: "+findings, true)
	if err != nil {
		return err
	}
	_, err = run("claude-opus-5", "Independently read the current fixture and candidate. Verify that only Snow and Floe are described as current, NBC as retired if mentioned, and Forky as planned if mentioned. Return PASS or FAIL with reasons.", false)
	if err != nil {
		return err
	}
	if err = os.MkdirAll("work", 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(map[string]any{"fixture": true, "at": time.Now().UTC(), "candidate": artifact, "runs": reports}, "", "  ")
	return os.WriteFile(filepath.Join("work", "provider-proof.json"), data, 0600)
}
