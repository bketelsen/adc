package adc

import (
	"bytes"
	"net/http"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

func pendingProposals(s *Store, org string) int {
	n := 0
	for _, p := range list[WorkProposal](s, "proposal", org) {
		if p.State == "pending" {
			n++
		}
	}
	return n
}
func (w *Web) proposalPage(p *Page) {
	p.View = "proposal"
	p.Title = p.Proposal.Title
	p.ProposalRevision = p.Proposal.Revision
	p.ProposalNotes = proposalNotes(w.Store, p.Proposal)
	p.ProposalHistory = proposalHistory(w.Store, p.Proposal)
	p.RelatedWork = relatedProposals(w.Store, p.Proposal)
	p.Agents = list[Agent](w.Store, "agent", p.Org.ID)
	p.ProposalSupervisor = p.Proposal.Owner
	seen := map[string]bool{}
	for !seen[p.ProposalSupervisor] {
		seen[p.ProposalSupervisor] = true
		var agent Agent
		if w.Store.Get(p.ProposalSupervisor, &agent) != nil || agent.Org != p.Org.ID || agent.ReportsTo == "" {
			break
		}
		var boss Agent
		if w.Store.Get(agent.ReportsTo, &boss) != nil || boss.Org != p.Org.ID {
			break
		}
		p.ProposalSupervisor = boss.ID
	}
	p.Accounts = nil
	for _, a := range list[Account](w.Store, "account", "") {
		if a.User == p.User.ID {
			p.Accounts = append(p.Accounts, a)
		}
	}
}
func (w *Web) liveProposal(rw http.ResponseWriter, r *http.Request, p Page) {
	sse := datastar.NewSSE(rw, r)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	last := ""
	// Keep the browser's initial revision in the stream so form edits survive,
	// with a visible reload notice when another human or agent changes the packet.
	for {
		if !w.member(p.User.ID, p.Org.ID) {
			return
		}
		if w.Store.Get(p.Proposal.ID, &p.Proposal) != nil || p.Proposal.Org != p.Org.ID {
			return
		}
		p.ProposalNotes = proposalNotes(w.Store, p.Proposal)
		var b bytes.Buffer
		if w.templates.ExecuteTemplate(&b, "proposal-updates", p) != nil {
			return
		}
		current := b.String()
		if current != last {
			if sse.PatchElements(current) != nil {
				return
			}
			last = current
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func documentCatalog(s *Store, org string) []Document {
	docs := list[Document](s, "document", org)
	for i := range docs {
		docs[i].Content = ""
	}
	return docs
}

func (w *Web) populateProposals(p *Page) {
	all := []WorkProposal{}
	for _, proposal := range list[WorkProposal](w.Store, "proposal", p.Org.ID) {
		if (proposal.State == "pending") != p.ProposalArchive {
			all = append(all, proposal)
		}
	}
	start := p.ProposalPage * 12
	if start < 0 || start > len(all) {
		start = 0
		p.ProposalPage = 0
	}
	end := start + 12
	p.ProposalNext = 0
	if end < len(all) {
		p.ProposalNext = p.ProposalPage + 1
	} else {
		end = len(all)
	}
	p.Proposals = all[start:end]
}
func (w *Web) liveProposalQueue(rw http.ResponseWriter, r *http.Request, p Page) {
	sse := datastar.NewSSE(rw, r)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	last := ""
	for {
		if !w.member(p.User.ID, p.Org.ID) {
			return
		}
		w.populateProposals(&p)
		var b bytes.Buffer
		if w.templates.ExecuteTemplate(&b, "proposal-queue-list", p) != nil {
			return
		}
		current := b.String()
		if current != last {
			if sse.PatchElements(current) != nil {
				return
			}
			last = current
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
