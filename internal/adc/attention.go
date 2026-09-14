package adc

import (
	"encoding/json"
	"fmt"
	copilot "github.com/github/copilot-sdk/go"
	"sort"
	"strings"
	"time"
)

// Attention is an explicitly approved recurring assessment policy. Existing
// schedules without this policy keep their original overlap semantics.
type AttentionPolicy struct {
	Areas                                                          []string
	Owners                                                         map[string]string
	Scan, Investigation, Review, MaxProposals, Minutes, Concurrent int
	ReplacesSchedule                                               string
	ReplacesRevision                                               int
}

type AreaAssessment struct {
	ID, Org, Task, Area, Owner, Run, Summary, Reference, Disposition, Created string
	Revision                                                                  int
}

type SupervisorBrief struct {
	ID, Org, Task, Run, Summary, Reference, Created string
	Revision                                        int
}

func validateAttention(s *Store, org string, p *AttentionPolicy) error {
	if p == nil {
		return nil
	}
	if len(p.Areas) < 1 || len(p.Areas) > 6 {
		return fmt.Errorf("an attention cycle covers one to six explicit areas")
	}
	if p.Scan < 1 || p.Scan > 12 || p.Investigation < 2 || p.Investigation > 16 || p.Review < 1 || p.Review > 12 || p.MaxProposals < 0 || p.MaxProposals > 3 || p.Minutes < 5 || p.Minutes > 30 || p.Concurrent < 1 || p.Concurrent > 4 {
		return fmt.Errorf("choose bounded attention: scan 1–12, investigation 2–16, review 1–12 activations; 0–3 proposals; 5–30 minutes; 1–4 concurrent runs")
	}
	seen := map[string]bool{}
	for _, id := range p.Areas {
		var a Area
		if seen[id] || s.Get(id, &a) != nil || a.Org != org {
			return fmt.Errorf("select distinct areas in this organization")
		}
		seen[id] = true
		if p.Owners != nil && p.Owners[id] != "" && p.Owners[id] != a.Owner {
			return fmt.Errorf("an area owner changed; recheck the attention policy")
		}
	}
	if p.ReplacesSchedule != "" {
		var old StandingSchedule
		if s.Get(p.ReplacesSchedule, &old) != nil || old.Org != org {
			return fmt.Errorf("replacement schedule unavailable")
		}
		if p.ReplacesRevision != 0 && p.ReplacesRevision != old.Revision && old.State != "replaced" {
			return fmt.Errorf("replacement source changed; inspect the schedule before approving")
		}
		if p.ReplacesRevision == 0 {
			p.ReplacesRevision = old.Revision
		}
	}
	return nil
}

func (s *Store) pinAttention(org string, p *AttentionPolicy) (*AttentionPolicy, error) {
	if err := validateAttention(s, org, p); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	b, _ := json.Marshal(p)
	var out AttentionPolicy
	_ = json.Unmarshal(b, &out)
	out.Owners = map[string]string{}
	for _, id := range out.Areas {
		var a Area
		_ = s.Get(id, &a)
		out.Owners[id] = a.Owner
	}
	return &out, nil
}

func attentionInstructions() string {
	return "\nBOUNDED OWNER ASSESSMENT: This is an approved read-only attention cycle, not permission for repairs. Begin with brief area observations (or none where nothing changed). Delegate each area's scan to its permanent owner, using AttentionStage=scan on adc_delegate. Owners record adc_assess_area with evidence, observed changes/unknowns and a no-change/changed/unknown disposition; these are provisional observations, not human intent. Look for shared causes across areas before selecting at most the configured number of improvements. Use AttentionStage=investigate only for selected deeper questions, and retain existing/declined proposals and deferral reasons rather than proposing them again without new evidence. Every area need not propose work. Cross-family review applies to substantive deliverables. Have a specialist author a concise supervisor briefing via adc_brief and obtain independent review; the final briefing states decisions needed, what is being handled, meaningful changes, and remaining unknowns with source references. Do not send missing read-only facts through an endless blocker/review cycle: record UNKNOWN. Retain existing obligations; new checks and changes outside this occurrence remain human-reviewed proposals. Waiting consumes no slot; scope, stage/total activation budgets, wall-clock deadline and concurrency ceilings remain in force across delegation/reassignment. Finish this assessment independently of any longer-lived repair it identifies. No infrastructure or repository mutation, public publication, or automatic acceptance of proposals."
}

func (e *Engine) attentionStageRemaining(t Assignment, r Run) int {
	if t.Attention == nil {
		return 1
	}
	stage := runAttentionStage(r)
	limit := t.Attention.Investigation
	if stage == "scan" {
		limit = t.Attention.Scan
	}
	if stage == "review" {
		limit = t.Attention.Review
	}
	for _, other := range taskRuns(e.Store, t.ID) {
		if runAttentionStage(other) == stage {
			limit -= other.Activations
		}
	}
	return limit
}
func (e *Engine) attentionRunCapacity(t Assignment, r Run) bool {
	if !e.attentionCapacity(t) {
		return false
	}
	if t.Attention == nil {
		return true
	}
	if e.attentionStageRemaining(t, r) <= 0 {
		return false
	}
	active := 0
	for _, other := range taskRuns(e.Store, t.ID) {
		if other.State == "running" {
			active++
		}
	}
	return active < t.Attention.Concurrent
}

func (e *Engine) attentionExpired(t Assignment, at time.Time) bool {
	if t.Attention == nil || t.AttentionStarted == "" {
		return false
	}
	start, err := time.Parse(time.RFC3339Nano, t.AttentionStarted)
	return err != nil || !at.Before(start.Add(time.Duration(t.Attention.Minutes)*time.Minute))
}

func (e *Engine) boundAttentionCycles(at time.Time) {
	for _, t := range list[Assignment](e.Store, "assignment", "") {
		if t.Attention == nil || t.State == "ready" || t.State == "cancelled" || t.State == "paused" {
			continue
		}
		_, running := e.attentionSpent(t.ID)
		reason := ""
		if t.AttentionStarted == "" && e.attentionQueueExpired(t, at) {
			reason = "Assessment could not start within its bounded queue window"
		} else if e.attentionExpired(t, at) {
			reason = "Attention reached its approved time limit"
		} else if problem := e.Store.attentionRunProblem(t); problem != "" {
			reason = problem
		} else if !e.attentionCapacity(t) && !running {
			reason = "Attention reached its approved activation limit"
		}
		if reason == "" {
			reason = e.exhaustedAttentionStage(t)
		}
		if reason != "" {
			e.holdObligationTask(t.ID)
			e.Store.Log(t.Org, t.ID, "", "attention", reason+"; observations and open ownership remain available")
		}
	}
}

func (e *Engine) assessmentContext(t Assignment) map[string]any {
	if t.Attention == nil {
		return nil
	}
	areas := []AreaKnowledge{}
	for _, id := range t.Attention.Areas {
		var a Area
		if e.Store.Get(id, &a) == nil && a.Org == t.Org {
			k := e.Store.areaKnowledge(a)
			k.History = nil
			k.Notes = nil
			k.Revisions = nil
			areas = append(areas, k)
		}
	}
	reports := []AreaAssessment{}
	for _, v := range list[AreaAssessment](e.Store, "area-assessment", t.Org) {
		if v.Task == t.ID {
			reports = append(reports, v)
		}
	}
	briefs := []SupervisorBrief{}
	for _, b := range list[SupervisorBrief](e.Store, "supervisor-brief", t.Org) {
		if b.Task == t.ID {
			briefs = append(briefs, b)
		}
	}
	previous := []AreaAssessment{}
	if t.Schedule != "" {
		all := list[AreaAssessment](e.Store, "area-assessment", t.Org)
		sort.SliceStable(all, func(i, j int) bool { return all[i].Created > all[j].Created })
		for _, v := range all {
			var old Assignment
			if v.Task != t.ID && e.Store.Get(v.Task, &old) == nil && old.Schedule == t.Schedule {
				previous = append(previous, v)
				if len(previous) == 12 {
					break
				}
			}
		}
	}
	prior := []map[string]any{}
	for _, p := range list[WorkProposal](e.Store, "proposal", t.Org) {
		notes := proposalNotes(e.Store, p)
		if len(notes) > 3 {
			notes = notes[:3]
		}
		prior = append(prior, map[string]any{"proposal": p, "decisions_and_notes": notes})
		if len(prior) == 24 {
			break
		}
	}
	return map[string]any{"prior_work": prior, "briefs": briefs, "previous_observations": previous, "policy": t.Attention, "started": t.AttentionStarted, "areas": areas, "observations": reports}
}

func (s *Store) saveAssessment(r Run, area, summary, reference, disposition string, revision int) (AreaAssessment, error) {
	var t Assignment
	var a Area
	if s.Get(r.Task, &t) != nil || t.Attention == nil || s.Get(area, &a) != nil || a.Org != r.Org || a.Owner != r.Agent || t.Attention.Owners[area] != r.Agent || r.ReviewOf != "" || r.Parent == "" {
		return AreaAssessment{}, fmt.Errorf("only the approved area's permanent owner may record its assessment")
	}
	if disposition != "no-change" && disposition != "changed" && disposition != "unknown" {
		return AreaAssessment{}, fmt.Errorf("use no-change, changed or unknown")
	}
	if !boundedText(summary, 2400) || !evidenceReference(reference) {
		return AreaAssessment{}, fmt.Errorf("supply a concise observed assessment and source reference")
	}
	if err := s.checkOwnershipText(r.Org, summary, reference); err != nil {
		return AreaAssessment{}, err
	}
	id := "assessment:" + digest(r.Task + "\n" + area)[:32]
	var old AreaAssessment
	_ = s.Get(id, &old)
	if old.Revision != revision {
		return old, fmt.Errorf("assessment changed; inspect revision %d", old.Revision)
	}
	v := AreaAssessment{ID: id, Org: r.Org, Task: r.Task, Area: area, Owner: r.Agent, Run: r.ID, Summary: summary, Reference: reference, Disposition: disposition, Created: now(), Revision: revision + 1}
	writes := []Write{{"area-assessment", r.Org, r.Task, disposition, id, v}}
	if old.ID != "" {
		writes = append(writes, Write{"assessment-history", old.Org, old.ID, old.Disposition, ID(), old})
	}
	return v, s.Batch(writes...)
}

func (s *Store) saveSupervisorBrief(r Run, summary, reference string, revision int) (SupervisorBrief, error) {
	var t Assignment
	if s.Get(r.Task, &t) != nil || t.Attention == nil || r.Parent == "" || r.ReviewOf != "" {
		return SupervisorBrief{}, fmt.Errorf("a substantive specialist authors the reviewable briefing for this attention cycle")
	}
	if !boundedText(summary, 5000) || !evidenceReference(reference) {
		return SupervisorBrief{}, fmt.Errorf("supply a nonempty Summary of at most 5000 bytes and a short credential-free Reference")
	}
	if err := s.checkOwnershipText(r.Org, summary, reference); err != nil {
		return SupervisorBrief{}, err
	}
	var old SupervisorBrief
	id := "supervisor-brief:" + r.ID
	_ = s.Get(id, &old)
	if old.Revision != revision {
		return old, fmt.Errorf("brief changed; inspect revision %d", old.Revision)
	}
	v := SupervisorBrief{ID: id, Org: r.Org, Task: r.Task, Run: r.ID, Summary: summary, Reference: reference, Created: now(), Revision: revision + 1}
	writes := []Write{{"supervisor-brief", r.Org, r.Task, "", id, v}}
	if old.ID != "" {
		writes = append(writes, Write{"brief-history", old.Org, old.ID, "", ID(), old})
	}
	return v, s.Batch(writes...)
}

func (e *Engine) ownerDeliverableRevision(r Run) []byte {
	var v SupervisorBrief
	_ = e.Store.Get("supervisor-brief:"+r.ID, &v)
	observations := []AreaAssessment{}
	for _, a := range list[AreaAssessment](e.Store, "area-assessment", r.Org) {
		if a.Task == r.Task && (a.Run == r.ID || v.ID != "") {
			observations = append(observations, a)
		}
	}
	if v.ID == "" && len(observations) == 0 {
		return nil
	}
	b, _ := json.Marshal(struct {
		Brief        SupervisorBrief
		Observations []AreaAssessment
	}{v, observations})
	return b
}
func (e *Engine) hasOwnerDeliverable(r Run) bool { return len(e.ownerDeliverableRevision(r)) > 0 }

func (e *Engine) attentionComplete(t Assignment) error {
	if t.Attention == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, a := range list[AreaAssessment](e.Store, "area-assessment", t.Org) {
		var run Run
		if a.Task == t.ID && e.Store.Get(a.Run, &run) == nil && run.State == "complete" && !run.Superseded && e.hasCurrentReview(run, taskReviews(e.Store, t.ID)) {
			seen[a.Area] = true
		}
	}
	for _, area := range t.Attention.Areas {
		if !seen[area] {
			return fmt.Errorf("area assessment missing; record observed facts or UNKNOWN before completing")
		}
	}
	for _, brief := range list[SupervisorBrief](e.Store, "supervisor-brief", t.Org) {
		var r Run
		if brief.Task == t.ID && e.Store.Get(brief.Run, &r) == nil && r.State == "complete" && !r.Superseded && e.hasCurrentReview(r, taskReviews(e.Store, t.ID)) {
			return nil
		}
	}
	return fmt.Errorf("a current independently reviewed supervisor briefing is required")
}

func (e *Engine) attentionProposalLimit(t Assignment) error {
	if t.Attention == nil {
		return nil
	}
	count := 0
	for _, p := range list[WorkProposal](e.Store, "proposal", t.Org) {
		if p.Task == t.ID {
			count++
		}
	}
	if count >= t.Attention.MaxProposals {
		return fmt.Errorf("this assessment's approved proposal limit is reached; prioritize existing proposals or finish with none")
	}
	return nil
}

func attentionStage(value string) bool {
	return value == "" || value == "scan" || value == "investigate" || value == "review"
}
func attentionSummary(p *AttentionPolicy) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d areas · scan %d / investigate %d / review %d activations · up to %d proposals · %d minutes · %d concurrent runs", len(p.Areas), p.Scan, p.Investigation, p.Review, p.MaxProposals, p.Minutes, p.Concurrent)
}

func sameAttention(a, b *AttentionPolicy) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (e *Engine) prepareAttention(t Assignment) (Assignment, error) {
	p, err := e.Store.pinAttention(t.Org, t.Attention)
	if err != nil {
		return t, err
	}
	if t.Authority != "observe" || t.Publication || t.Obligation != "" || t.Kind == "proposal" {
		return t, fmt.Errorf("bounded assessments require observe authority and no publication or repair authorization")
	}
	t.Attention, t.Kind = p, "owner-assessment"
	if !t.ConstrainTools {
		var owner Agent
		if e.Store.Get(t.Owner, &owner) != nil || owner.Org != t.Org {
			return t, fmt.Errorf("assessment supervisor unavailable")
		}
		t.Tools = append([]string{}, owner.Tools...)
		t.ConstrainTools = true
	}
	grants := t.Capabilities
	if !t.ConstrainCapabilities {
		grants = e.Store.initialCapabilities(t.Org, t.Tools)
	}
	t.Capabilities = nil
	for _, g := range grants {
		if g.Class == "read" && g.Operation == "" && g.Binding == nil {
			t.Capabilities = append(t.Capabilities, g)
		}
	}
	t.ConstrainCapabilities = true
	if !strings.Contains(t.Prompt, "BOUNDED OWNER ASSESSMENT:") {
		t.Prompt += attentionInstructions()
	}
	return t, nil
}

// Stage exhaustion is a retained, deterministic stop, not a queued run which
// can never acquire capacity. Active last turns are allowed to finish.
func (e *Engine) exhaustedAttentionStage(t Assignment) string {
	if t.Attention == nil {
		return ""
	}
	runs := taskRuns(e.Store, t.ID)
	for _, r := range runs {
		if r.State == "running" {
			return ""
		}
	}
	for _, r := range runs {
		if r.State == "queued" && !e.attentionRunCapacity(t, r) {
			return "Attention exhausted the " + runAttentionStage(r) + " activation budget"
		}
	}
	return ""
}
func runAttentionStage(r Run) string {
	if r.ReviewOf != "" {
		return "review"
	}
	if r.AttentionStage == "" {
		return "investigate"
	}
	return r.AttentionStage
}

func (s *Store) attentionRunProblem(t Assignment) string {
	if t.Attention == nil {
		return ""
	}
	if err := validateAttention(s, t.Org, t.Attention); err != nil {
		return err.Error()
	}
	if t.AttentionStarted != "" {
		at, err := time.Parse(time.RFC3339Nano, t.AttentionStarted)
		if err != nil || !time.Now().Before(at.Add(time.Duration(t.Attention.Minutes)*time.Minute)) {
			return "Assessment reached its approved time limit"
		}
	}
	var member int
	if s.db.QueryRow(`SELECT count(*) FROM memberships WHERE user_id=? AND org=?`, t.Creator, t.Org).Scan(&member) != nil || member != 1 {
		return "Assessment funding human is no longer an organization member"
	}
	return ""
}

func (e *Engine) attentionTools(original Run) []copilot.Tool {
	s := e.Store
	var task Assignment
	if s.Get(original.Task, &task) != nil || task.Attention == nil {
		return nil
	}
	active := func() (Run, error) {
		var r Run
		var t Assignment
		if s.Get(original.ID, &r) != nil || r.State != "running" || r.Superseded || s.Get(r.Task, &t) != nil || t.Attention == nil || t.State == "paused" || t.State == "cancelled" || t.State == "ready" {
			return r, fmt.Errorf("no active bounded assessment")
		}
		if reason := s.attentionRunProblem(t); reason != "" {
			return r, fmt.Errorf("%s", reason)
		}
		return r, nil
	}
	return []copilot.Tool{
		copilot.DefineTool("adc_assess_area", "Record your permanent area's provisional observation for this approved assessment. Disposition is no-change, changed, or unknown. Include a concise Summary and evidence Reference; missing facts are UNKNOWN, not a request for new authority. Revision starts at 0; inspect assessment in adc_status before updating. Does not change human intent or close obligations.", func(p struct {
			Area, Summary, Reference, Disposition string
			Revision                              int
		}, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r, err := active()
			if err != nil {
				return nil, err
			}
			return s.saveAssessment(r, p.Area, p.Summary, p.Reference, p.Disposition, p.Revision)
		}),
		copilot.DefineTool("adc_brief", "Author a concise reviewable supervisor briefing as a substantive child worker. Summary covers decisions, what is being handled, meaningful changes, related areas/proposals and unknowns. Include source Reference; Revision starts at 0. This is evidence, not approval. Finish and obtain independent cross-family review before the supervisor completes the assessment.", func(p struct {
			Summary, Reference string
			Revision           int
		}, _ copilot.ToolInvocation) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			r, err := active()
			if err != nil {
				return nil, err
			}
			return s.saveSupervisorBrief(r, p.Summary, p.Reference, p.Revision)
		}),
	}
}

func (e *Engine) attentionWorkerComplete(t Assignment, r Run) error {
	if t.Attention == nil || r.Parent == "" || r.ReviewOf != "" || r.AttentionStage != "scan" {
		return nil
	}
	for _, id := range t.Attention.Areas {
		if t.Attention.Owners[id] != r.Agent {
			continue
		}
		var v AreaAssessment
		if e.Store.Get("assessment:"+digest(t.ID + "\n" + id)[:32], &v) != nil || v.Run != r.ID {
			return fmt.Errorf("record your area %s using adc_assess_area before finishing; use UNKNOWN for missing evidence", id)
		}
	}
	return nil
}

// Queue waiting is not billed work, but a permanently unstartable occurrence must
// release its cadence. Allow twice the approved execution window for contention.
func (e *Engine) attentionQueueExpired(t Assignment, at time.Time) bool {
	if t.Attention == nil || t.AttentionStarted != "" || t.Created == "" {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, t.Created)
	return err != nil || !at.Before(created.Add(2*time.Duration(t.Attention.Minutes)*time.Minute))
}

const attentionScanInstructions = "\nBefore adc_finish, record your permanent area observation using adc_assess_area with current Revision (0 when none), source Reference and no-change/changed/unknown Disposition. An ordinary message or document does not persist the area assessment. Missing facts are UNKNOWN; do not invent evidence."
