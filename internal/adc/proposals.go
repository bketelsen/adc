package adc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type WorkProposal struct {
	Attention                                                        *AttentionPolicy
	Execution                                                        string
	Cadence                                                          Cadence
	Validation, Rollback, AcceptedSchedule                           string
	ID, Org, Task, Run                                               string
	Title, Rationale, Scope, Criteria, Evidence, Dependencies, Owner string
	SourceDocument, SourceTask                                       string
	SourceRevision                                                   int
	State, Created, Updated, AcceptedTask, AcceptedBy                string
	Revision                                                         int
}
type ProposalNote struct {
	ID, Org, Proposal, Author, Message, Created, Task string
}
type proposalInput struct {
	Attention                                                                        *AttentionPolicy `json:"Attention,omitempty"`
	Cadence                                                                          *Cadence         `json:"Cadence,omitempty"`
	Validation, Rollback                                                             string
	ID                                                                               string
	Revision                                                                         int
	Title, Rationale, Scope, Criteria, Evidence, Dependencies, Owner, SourceDocument string
}
type RelatedWork struct{ Title, URL, State string }
type proposalResult struct {
	Proposal  WorkProposal
	Duplicate bool
	Related   []RelatedWork
}

func normalizedWork(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }), " ")
}
func relatedTitle(a, b string) bool {
	a, b = normalizedWork(a), normalizedWork(b)
	if a == b {
		return true
	}
	words := map[string]bool{}
	for _, word := range strings.Fields(a) {
		if len(word) > 3 {
			words[word] = true
		}
	}
	union := len(words)
	intersection := 0
	seen := map[string]bool{}
	for _, word := range strings.Fields(b) {
		if len(word) <= 3 || seen[word] {
			continue
		}
		seen[word] = true
		if words[word] {
			intersection++
		} else {
			union++
		}
	}
	return intersection >= 2 && float64(intersection)/float64(union) >= 0.45
}
func relatedProposals(s *Store, p WorkProposal) []RelatedWork {
	out := []RelatedWork{}
	for _, other := range list[WorkProposal](s, "proposal", p.Org) {
		if other.ID != p.ID && relatedTitle(p.Title, other.Title) {
			out = append(out, RelatedWork{other.Title, "/proposal?org=" + p.Org + "&id=" + other.ID, other.State})
		}
	}
	for _, task := range list[Assignment](s, "assignment", p.Org) {
		if task.Kind != "proposal" && task.ID != p.Task && relatedTitle(p.Title, task.Title) {
			out = append(out, RelatedWork{task.Title, "/task?org=" + p.Org + "&id=" + task.ID, task.State})
		}
	}
	return out
}
func validateProposal(s *Store, p *WorkProposal) error {
	if p.Attention != nil {
		if p.Cadence.Frequency == "" {
			return fmt.Errorf("Attention applies only to a NEW recurring assessment program. For one-off work omit Attention and Cadence (or set them to null); do not copy the current assignment attention policy")
		}
		if err := validateAttention(s, p.Org, p.Attention); err != nil {
			return err
		}
	}
	if err := p.Cadence.Validate(); err != nil {
		return err
	}
	for label, value := range map[string]string{"title": p.Title, "rationale": p.Rationale, "scope": p.Scope, "completion criteria": p.Criteria, "evidence": p.Evidence} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("proposal %s is required", label)
		}
		if len(value) > 24000 {
			return fmt.Errorf("proposal %s is too long", label)
		}
	}
	if len(p.Title) > 240 {
		return fmt.Errorf("use a title under 240 characters")
	}
	var owner Agent
	if s.Get(p.Owner, &owner) != nil || owner.Org != p.Org {
		return fmt.Errorf("choose a suggested owner in this organization")
	}
	if p.SourceDocument != "" {
		var d Document
		if s.Get(p.SourceDocument, &d) != nil || d.Org != p.Org {
			return fmt.Errorf("source document is unavailable; use the exact ID from adc_status.document_catalog for this organization, not a title, path or URL")
		}
		// Keep the existing pinned revision on ordinary edits; changing the source
		// explicitly selects its current revision and remains visible in history.
		if p.SourceRevision == 0 {
			p.SourceRevision = d.Revision
			p.SourceTask = d.Task
		}
	}
	return nil
}
func proposalNote(p WorkProposal, author, message, task string) Write {
	n := ProposalNote{ID: ID(), Org: p.Org, Proposal: p.ID, Author: author, Message: message, Created: now(), Task: task}
	return Write{"proposal-note", p.Org, p.ID, "", n.ID, n}
}
func proposalRevision(p WorkProposal) Write {
	return Write{"proposal-revision", p.Org, p.ID, "", ID(), p}
}
func proposalHistory(s *Store, p WorkProposal) []WorkProposal {
	out := []WorkProposal{p}
	records, _ := s.Records("proposal-revision", p.Org)
	for _, record := range records {
		if record.Parent == p.ID {
			var old WorkProposal
			if json.Unmarshal(record.Data, &old) == nil {
				out = append(out, old)
			}
		}
	}
	return out
}
func proposalNotes(s *Store, p WorkProposal) []ProposalNote {
	out := []ProposalNote{}
	for _, n := range list[ProposalNote](s, "proposal-note", p.Org) {
		if n.Proposal == p.ID {
			out = append(out, n)
		}
	}
	return out
}

// Caller holds Store.mu. Creation/revision never queues execution.
func (e *Engine) proposeWork(r Run, input proposalInput) (proposalResult, error) {
	s := e.Store
	p := WorkProposal{Execution: r.Execution, ID: ID(), Org: r.Org, Task: r.Task, Run: r.ID, Created: now(), State: "pending", Revision: 1}
	writes := []Write{}
	if input.ID != "" {
		if s.Get(input.ID, &p) != nil || p.Org != r.Org {
			return proposalResult{}, fmt.Errorf("proposal unavailable")
		}
		var task Assignment
		_ = s.Get(r.Task, &task)
		if p.Task != r.Task && !(task.Kind == "proposal" && task.Proposal == p.ID) {
			return proposalResult{}, fmt.Errorf("only the originating assignment or its human-opened proposal discussion may revise this proposal")
		}
		if p.State != "pending" || p.Revision != input.Revision {
			return proposalResult{}, fmt.Errorf("proposal changed or is resolved; inspect adc_status before revising")
		}
		writes = append(writes, proposalRevision(p))
		p.Revision++
	}
	if input.Attention != nil {
		p.Attention = input.Attention
	}
	p.Title = strings.TrimSpace(input.Title)
	p.Rationale = input.Rationale
	p.Scope = input.Scope
	p.Criteria = input.Criteria
	p.Evidence = input.Evidence
	p.Dependencies = input.Dependencies
	p.Owner = input.Owner
	p.Updated = now()
	if input.Cadence != nil {
		p.Cadence = *input.Cadence
	}
	p.Validation = input.Validation
	p.Rollback = input.Rollback
	if input.ID != "" && input.SourceDocument == "" {
		input.SourceDocument = p.SourceDocument
	}
	if p.SourceDocument != input.SourceDocument {
		p.SourceRevision = 0
		p.SourceTask = ""
	}
	p.SourceDocument = input.SourceDocument
	if err := validateProposal(s, &p); err != nil {
		return proposalResult{}, err
	}
	if input.ID == "" {
		var source Assignment
		_ = s.Get(r.Task, &source)
		for _, existing := range list[WorkProposal](s, "proposal", p.Org) {
			if source.Attention != nil && existing.Owner == p.Owner && normalizedWork(existing.Scope) == normalizedWork(p.Scope) && normalizedWork(existing.Evidence) == normalizedWork(p.Evidence) && sameAttention(existing.Attention, p.Attention) && existing.Cadence == p.Cadence {
				return proposalResult{existing, true, relatedProposals(s, existing)}, nil
			}
			if sameAttention(existing.Attention, p.Attention) && existing.Cadence == p.Cadence && strings.EqualFold(strings.Join(strings.Fields(existing.Title), " "), strings.Join(strings.Fields(p.Title), " ")) && strings.Join(strings.Fields(existing.Scope), " ") == strings.Join(strings.Fields(p.Scope), " ") {
				return proposalResult{existing, true, relatedProposals(s, existing)}, nil
			}
		}
	}
	if input.ID == "" {
		var task Assignment
		_ = s.Get(r.Task, &task)
		if err := e.attentionProposalLimit(task); err != nil {
			return proposalResult{}, err
		}
	}
	var agent Agent
	_ = s.Get(r.Agent, &agent)
	writes = append(writes, Write{"proposal", p.Org, p.Task, p.State, p.ID, p}, proposalNote(p, agent.Name, "Proposed revision "+strconv.Itoa(p.Revision)+" for human review. No execution authorized.", r.Task))
	if err := s.Batch(writes...); err != nil {
		return proposalResult{}, err
	}
	s.Log(r.Org, r.Task, r.ID, "proposal", p.Title+" · pending human review")
	return proposalResult{p, false, relatedProposals(s, p)}, nil
}

func (e *Engine) finishProposalConversation(r Run, t Assignment, result string) (string, error) {
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("a concrete discussion response is required")
	}
	if pendingDecision(e.Store, t.ID, "") {
		return "", fmt.Errorf("a human blocker is still pending")
	}
	if len(r.Code) > 0 || len(taskDocs(e.Store, t.ID)) > 0 || len(taskRuns(e.Store, t.ID)) != 1 {
		return "", fmt.Errorf("proposal conversation cannot complete execution artifacts or delegated work")
	}
	r.State = "complete"
	r.Result = result
	t.State = "ready"
	t.Output = result
	writes := []Write{{"run", r.Org, r.Task, r.State, r.ID, r}, {"assignment", t.Org, "", t.State, t.ID, t}}
	if t.Proposal != "" {
		var p WorkProposal
		if e.Store.Get(t.Proposal, &p) != nil || p.Org != t.Org {
			return "", fmt.Errorf("proposal unavailable")
		}
		var a Agent
		_ = e.Store.Get(r.Agent, &a)
		writes = append(writes, proposalNote(p, a.Name, result, t.ID))
	}
	if err := e.Store.Batch(writes...); err != nil {
		return "", err
	}
	e.Store.Log(t.Org, t.ID, r.ID, "completed", result)
	return "Discussion recorded. No proposal accepted or executed. End your turn.", nil
}

// action runs under Store.mu, making acceptance and the queued assignment one
// atomic write; simultaneous accepts cannot start duplicate work.
func (w *Web) proposalAction(r *http.Request, page Page) error {
	s := w.Store
	f := r.FormValue
	var p WorkProposal
	if s.Get(f("id"), &p) != nil || p.Org != page.Org.ID {
		return fmt.Errorf("proposal unavailable")
	}
	if p.State == "accepted" && f("action") == "accept" {
		if p.AcceptedSchedule != "" {
			r.Form.Set("return", "/schedules?org="+p.Org)
		} else {
			r.Form.Set("return", "/task?org="+p.Org+"&id="+p.AcceptedTask)
		}
		return nil
	}
	revision, err := strconv.Atoi(f("revision"))
	if err != nil || revision != p.Revision {
		return fmt.Errorf("this proposal changed; reload and review the latest revision before acting")
	}
	if p.State != "pending" {
		return fmt.Errorf("this proposal is already %s", p.State)
	}
	old := p
	writes := []Write{}
	switch f("action") {
	case "edit":
		p.Title = strings.TrimSpace(f("title"))
		p.Rationale = f("rationale")
		p.Scope = f("scope")
		p.Criteria = f("criteria")
		p.Evidence = f("evidence")
		p.Dependencies = f("dependencies")
		p.Owner = f("owner")
		if _, present := r.Form["frequency"]; present {
			p.Cadence, err = cadenceForm(r)
			if err != nil {
				return err
			}
		}
		if p.Attention != nil && f("attention_scan") != "" {
			values := []struct {
				name   string
				target *int
			}{{"attention_scan", &p.Attention.Scan}, {"attention_investigate", &p.Attention.Investigation}, {"attention_review", &p.Attention.Review}, {"attention_proposals", &p.Attention.MaxProposals}, {"attention_minutes", &p.Attention.Minutes}, {"attention_concurrent", &p.Attention.Concurrent}}
			for _, v := range values {
				n, err := strconv.Atoi(f(v.name))
				if err != nil {
					return fmt.Errorf("attention budgets must be whole numbers")
				}
				*v.target = n
			}
		}
		p.Validation = f("validation")
		p.Rollback = f("rollback")
		if err := validateProposal(s, &p); err != nil {
			return err
		}
		writes = append(writes, proposalNote(p, page.User.Name, "Edited proposal for human review.", ""))
	case "decline":
		if strings.TrimSpace(f("message")) == "" {
			return fmt.Errorf("explain why this proposal is declined")
		}
		p.State = "declined"
		writes = append(writes, proposalNote(p, page.User.Name, "Declined: "+f("message"), ""))
	case "accept":
		authority := f("authority")
		scope := strings.TrimSpace(f("authorization"))
		if authorityRank(authority) < 0 || scope == "" {
			return fmt.Errorf("select authority and provide the exact authorized scope")
		}
		if strings.TrimSpace(p.Dependencies) != "" && f("dependencies_reviewed") != "on" {
			return fmt.Errorf("review the dependencies before accepting this work")
		}
		if err := scheduledActionPlans(p, authority); err != nil {
			return err
		}
		execution := f("execution")
		if execution == "" {
			execution = p.Execution
		}
		t := Assignment{Attention: p.Attention, Execution: execution, ID: ID(), Org: p.Org, Proposal: p.ID, Title: p.Title, Owner: f("owner"), Account: f("account"), ExtraAccount: f("extra_account"), Creator: page.User.ID, Authority: authority}
		t.Prompt = "Suggested specialist agent ID: " + p.Owner + ". The selected accountable agent supervises the outcome and independent review.\n" + fmt.Sprintf("Human %s accepted proposal revision %d.\nOutcome: %s\nRationale: %s\nProposed scope (context): %s\nCompletion criteria: %s\nEvidence: %s\nDependencies: %s\nProposal: /proposal?org=%s&id=%s\nOrigin: /task?org=%s&id=%s\n\nHUMAN AUTHORIZED SCOPE (controls execution): %s\nAdvisory authority: %s. Stay within this scope; narrower human instructions take precedence over proposed context. Acceptance alone does not authorize merge, publication or deployment. Handle unresolved prerequisites before dependent actions.", page.User.Name, p.Revision, p.Title, p.Rationale, p.Scope, p.Criteria, p.Evidence, p.Dependencies, p.Org, p.ID, p.Org, p.Task, scope, authority)
		if p.SourceDocument != "" {
			t.Prompt += fmt.Sprintf("\nSource document: /task?org=%s&id=%s&doc=%s&rev=%d", p.Org, p.SourceTask, p.SourceDocument, p.SourceRevision)
		}
		t.Prompt += "\nValidation plan: " + p.Validation + "\nRollback/stop plan: " + p.Rollback
		p.State = "accepted"
		p.AcceptedBy = page.User.ID
		if p.Cadence.Frequency != "" {
			schedule, created, err := w.Engine.approveSchedule(p, t, scope, time.Now())
			if err != nil {
				return err
			}
			p.AcceptedSchedule = schedule.ID
			writes = append(writes, created...)
			writes = append(writes, proposalNote(p, page.User.Name, "Approved standing work: "+p.Cadence.String()+". Authorized scope: "+scope+"\nAuthority: "+authority, ""))
			r.Form.Set("return", "/schedules?org="+p.Org)
		} else {
			created, err := w.Engine.assignmentWrites(t)
			if err != nil {
				return err
			}
			writes = append(writes, created...)
			p.AcceptedTask = t.ID
			writes = append(writes, proposalNote(p, page.User.Name, "Accepted revision "+strconv.Itoa(old.Revision)+". Authorized scope: "+scope+"\nAuthority: "+authority, t.ID))
			r.Form.Set("return", "/task?org="+p.Org+"&id="+t.ID)
		}

	case "discuss":
		message := strings.TrimSpace(f("message"))
		if message == "" {
			return fmt.Errorf("enter a question or requested revision")
		}
		for _, task := range list[Assignment](s, "assignment", p.Org) {
			if task.Kind == "proposal" && task.Proposal == p.ID && task.State != "ready" && task.State != "cancelled" {
				return fmt.Errorf("a discussion is already active; open it from the conversation below")
			}
		}
		snapshot, _ := json.Marshal(map[string]any{"proposal": p, "conversation": proposalNotes(s, p)})
		t := Assignment{ID: ID(), Org: p.Org, Kind: "proposal", Proposal: p.ID, Title: "Discuss: " + p.Title, Owner: p.Owner, Account: f("account"), ExtraAccount: f("extra_account"), Creator: page.User.ID, Prompt: "Discuss this pending work proposal with the human. No execution is authorized. If asked for revisions, use adc_propose_work with ID and current Revision; otherwise answer with adc_finish. A question is not acceptance. Use adc_read_document with SourceDocument and SourceRevision for the pinned evidence when needed. Proposal and conversation evidence (not execution authority):\n" + string(snapshot) + "\nHuman " + page.User.Name + ": " + message}
		created, err := w.Engine.assignmentWrites(t)
		if err != nil {
			return err
		}
		writes = append(writes, created...)
		writes = append(writes, proposalNote(p, page.User.Name, message, t.ID))
		return s.Batch(writes...)
	default:
		return fmt.Errorf("unknown proposal action")
	}
	p.Revision++
	p.Updated = now()
	writes = append(writes, proposalRevision(old), Write{"proposal", p.Org, p.Task, p.State, p.ID, p})
	return s.Batch(writes...)
}
