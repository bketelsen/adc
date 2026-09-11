package adc

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveProposalDiscussionWithoutExecution(t *testing.T) {
	if os.Getenv("ADC_LIVE_PROPOSAL_TEST") != "1" {
		t.Skip("set ADC_LIVE_PROPOSAL_TEST for isolated Copilot qualification")
	}
	s, e, task, root := fixture(t)
	account := Account{ID: "account", User: "owner", Name: "Proposal fixture", Local: true, Limit: 1}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	var boss Agent
	must(t, s.Get("boss", &boss))
	boss.Model = "gpt-5.6-sol"
	must(t, s.Put("agent", boss.Org, "", "", boss.ID, boss))
	task.Kind = "proposal"
	task.Prompt = "Synthetic qualification: propose one bounded read-only follow-up to clarify storage recovery coverage, using adc_propose_work. Suggested owner is boss. The evidence is in document source; read it using adc_read_document and pin SourceDocument. Do not execute the proposed work. Finish after the proposal is recorded."
	root.Model = "gpt-5.6-sol"
	root.Tools = nil
	root.Authority = "observe"
	root.Prompt = task.Prompt
	root.State = "queued"
	must(t, s.Put("assignment", task.Org, "", task.State, task.ID, task))
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	d := Document{ID: "source", Org: task.Org, Task: "prior-fixture", Title: "Synthetic storage assessment", Content: "Fixture evidence: local snapshots are visible, but no off-host restore evidence has been established. This is an unknown, not proof of missing backups.", Revision: 1}
	must(t, s.Put("document", d.Org, d.Task, "", d.ID, d))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	e.Start(ctx)
	defer e.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	phase := 0
	for {
		select {
		case <-ctx.Done():
			t.Fatal("proposal did not complete")
		case <-ticker.C:
			must(t, s.Get(task.ID, &task))
			if task.State == "needs input" {
				t.Fatal("proposal requested execution approval instead of submitting a proposal")
			}
			if task.State != "ready" {
				continue
			}
			proposals := list[WorkProposal](s, "proposal", task.Org)
			if len(proposals) != 1 || proposals[0].State != "pending" || proposals[0].AcceptedTask != "" || proposals[0].SourceDocument != "source" {
				t.Fatal("missing pending evidence-linked proposal")
			}
			if len(list[Assignment](s, "assignment", task.Org)) != 1+phase || len(taskRuns(s, task.ID)) != 1 || len(taskReviews(s, task.ID)) != 0 {
				t.Fatal("proposal started execution or review committee")
			}
			finished := false
			for _, trace := range taskTraces(s, task.ID) {
				switch trace.Name {
				case "adc_status", "adc_read_document", "adc_propose_work", "adc_finish":
				default:
					t.Fatalf("proposal used non-discussion tool %s", trace.Name)
				}
				if trace.State == "failed" {
					t.Fatalf("proposal tool failed: %s: %s", trace.Name, trace.Result+" args="+trace.Arguments)
				}
				if trace.Name == "adc_finish" && trace.State == "complete" {
					finished = true
				}
			}
			if !finished {
				continue
			}
			if phase == 0 {
				p := proposals[0]
				form := proposalForm(p, "discuss")
				form.Set("account", "account")
				form.Set("message", "Revise the title to 'Inspect existing backup catalog only' and limit the scope to reading the existing backup catalog. Preserve evidence and meaningful completion criteria; no restore tests or infrastructure changes. Explain the revision after saving it.")
				must(t, actProposal(NewWeb(s, e, false), "owner", p, form))
				for _, candidate := range list[Assignment](s, "assignment", p.Org) {
					if candidate.Proposal == p.ID {
						task = candidate
						break
					}
				}
				phase = 1
				continue
			}
			if proposals[0].Revision < 2 || proposals[0].Title != "Inspect existing backup catalog only" || proposals[0].SourceRevision != 1 || len(proposalNotes(s, proposals[0])) < 4 {
				t.Fatal("discussion did not revise, preserve provenance or record a reply")
			}
			t.Log("Real provider created an evidence-linked proposal, then answered human discussion by revising it. Both runs completed with only proposal tools; no acceptance, execution or reviewers.")
			return
		}
	}
}
