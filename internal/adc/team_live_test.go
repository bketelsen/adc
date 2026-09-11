package adc

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveTeamProposalRequiresHumanApproval(t *testing.T) {
	if os.Getenv("ADC_LIVE_TEAM_TEST") != "1" {
		t.Skip("set ADC_LIVE_TEAM_TEST=1 for the subscription-backed team setup qualification")
	}
	s, e, original, root := fixture(t)
	original.State = "cancelled"
	root.State = "cancelled"
	must(t, s.Put("assignment", original.Org, "", original.State, original.ID, original))
	must(t, s.Put("run", root.Org, root.Task, root.State, root.ID, root))
	account := Account{ID: "account", User: "owner", Local: true, Limit: 1}
	must(t, s.Put("account", "", account.User, "", account.ID, account))
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture owner','owner','fixture'); INSERT INTO memberships VALUES('owner','org')`)
	must(t, err)
	page := Page{User: User{ID: "owner", Name: "Fixture owner"}, Org: Organization{ID: "org", Name: "Synthetic team qualification"}}
	w := NewWeb(s, e, false)
	form := url.Values{"account": {"account"}, "model": {"gpt-6-astra"}, "prompt": {"Synthetic fixture with two small code repositories. All facts are supplied; no external research or shell needed. Propose exactly three roles: CTO, category supervision, model gpt-6-astra, authority draft; Implementer, category implementation, model gpt-6-astra, authority draft, reporting to CTO; Reviewer, category review, model claude-opus-5, authority observe, reporting to CTO. Give each a useful responsibility description. No MCP tools are needed. Present the proposed agents for human approval; do not create them."}}
	req := httptest.NewRequest("POST", "http://fixture/team-proposals", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	must(t, req.ParseForm())
	must(t, w.action(req, page))
	var task Assignment
	for _, candidate := range list[Assignment](s, "assignment", "org") {
		if candidate.ID != original.ID {
			task = candidate
		}
	}
	if task.ID == "" {
		t.Fatal("team designer was not queued")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	e.Start(ctx)
	defer e.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var originalDecision string
	for {
		select {
		case <-ctx.Done():
			t.Fatal("team proposal timed out")
		case <-ticker.C:
			for _, decision := range taskDecisions(s, task.ID) {
				if decision.State != "pending" {
					continue
				}
				if len(decision.Proposal) != 3 {
					t.Fatalf("unexpected proposal: %s (%d roles)", decision.Question, len(decision.Proposal))
				}
				if len(list[Agent](s, "agent", "org")) != 3 {
					t.Fatal("permanent team changed before approval")
				}
				roles, err := PrepareTeam(s, "org", decision.Proposal)
				must(t, err)
				var developer, reviewer string
				for _, role := range roles {
					if role.Category == "implementation" {
						developer = role.Model
					}
					if role.Category == "review" {
						reviewer = role.Model
					}
				}
				must(t, CanReview(developer, reviewer))
				if originalDecision == "" {
					originalDecision = decision.ID
					steer := url.Values{"task": {task.ID}, "message": {"Change all proposed GPT roles and their category defaults from gpt-6-astra to gpt-5.6-sol. Keep claude-opus-5 for independent review. Revise the pending proposal and its packet; do not create the team yet."}}
					req = httptest.NewRequest("POST", "http://fixture/steer", strings.NewReader(steer.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					must(t, req.ParseForm())
					must(t, w.action(req, page))
					continue
				}
				if decision.ID == originalDecision {
					continue
				}
				if decision.Replaces != originalDecision || developer != "gpt-5.6-sol" || reviewer != "claude-opus-5" {
					t.Fatal("provider did not revise the original proposal with the requested model families")
				}
				for _, role := range roles {
					if Family(role.Model) == "openai-gpt" && role.Model != "gpt-5.6-sol" {
						t.Fatal("GPT role retained its old model")
					}
				}
				var packet Document
				must(t, s.Get(decision.Document, &packet))
				if packet.Revision < 2 {
					t.Fatal("proposal revision lost document history")
				}
				approve := url.Values{"task": {task.ID}, "decision": {decision.ID}, "answer": {"Approved for this isolated test fixture"}, "approve_team": {"on"}}
				req = httptest.NewRequest("POST", "http://fixture/decision", strings.NewReader(approve.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				must(t, req.ParseForm())
				must(t, w.action(req, page))
				must(t, s.Get(task.ID, &task))
				if task.State != "ready" || len(list[Agent](s, "agent", "org")) != 6 {
					t.Fatal("approved team not created atomically")
				}
				t.Log("Provider revised Astra to Sol after conversational steering while retaining Claude review and packet history; permanent agents appeared only after fresh fixture approval")
				return
			}
		}
	}
}
