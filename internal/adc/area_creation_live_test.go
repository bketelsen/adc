package adc

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveAreaCreationProposal(t *testing.T) {
	if os.Getenv("ADC_LIVE_AREA_CREATION") != "1" {
		t.Skip("set ADC_LIVE_AREA_CREATION")
	}
	s, e, task, root := areaConversationFixture(t)
	account := Account{ID: "account", User: "owner", Name: "Isolated area fixture", Local: true, Limit: 1}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	task.Prompt = "Synthetic ADC qualification only. Define one area named Backup readiness, with permanent owner dev, reviewed completion, and intent to understand restore coverage across storage and hosting and propose bounded verification work. No infrastructure actions or standing schedule are authorized. You have enough detail; propose the area for human approval with adc_propose_area now. Do not create a work proposal or start discovery."
	root.Model = "gpt-5.6-sol"
	root.Prompt = task.Prompt
	root.State = "queued"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	e.Start(ctx)
	defer e.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("no area proposal before timeout")
		case <-ticker.C:
			decisions := taskDecisions(s, task.ID)
			for _, d := range decisions {
				if d.ProposedArea != nil {
					if d.State != "pending" || d.ProposedArea.Owner != "dev" || d.ProposedArea.Name != "Backup readiness" || len(list[Area](s, "area", task.Org)) != 0 {
						t.Fatal("incorrect live proposal", d)
					}
					t.Log("Real Sol area designer submitted a pending structured area proposal without creating the area or starting execution.")
					return
				}
			}
		}
	}
}
