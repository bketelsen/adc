# ADC evolution plan: continuing ownership

**Status:** Approved by Brian; implementation in progress. P0 and P1 are complete; P1 is deployed and P2 is in progress. See [ownership checkpoint](ownership-checkpoint.md) for current evidence and rollout state.

**Product authority:** [ADC Desired State](../design/desired-state.md), produced from the September 13, 2026 ownership discussion and subsequently reviewed by Brian. That document defines the destination. This plan describes an incremental route from existing ADC without freezing storage schemas, tool names, transports, or prompts prematurely.

**Inspected baseline:** `/var/home/bjk/projects/adc`, `main`, commit `7a3e951` (`fix: recover approved delivery without repeated human handoffs`). The working tree was clean when inspected. Code and repository documentation were read for this plan; no new claim about live assignment health is made. Recheck the baseline before implementation because other work may have occurred.

## 1. Recommendation

Evolve the existing application rather than replace its execution engine. Make permanent agents the home of responsibility, while keeping assignments, runs, artifacts, decisions, and execution graphs as the machinery for doing and explaining particular work.

Deliver a small complete ownership loop first: an existing owner learns something about its area, records an authorized follow-up, finishes the originating run, survives a server restart, wakes later, verifies the outcome, and updates its understanding and supervisor briefing. Then deepen onboarding, cross-area coordination, periodic assessment, and domain-specific policies.

Public contributions and federation follow once internal ownership works. Their interfaces should be anticipated, but neither should hold the first useful release hostage. Public contribution admission additionally requires a demonstrated technical boundary; today's advisory execution mode is not that boundary.

The first release should answer three questions convincingly:

- Does an owner remember relevant intent and experience in a new assignment?
- Does it follow through without the human remembering to prompt it?
- Can the human understand what is owned, what is waiting, and what needs a decision without reading transcripts?

## 2. What exists and what changes

Paths below are relative to the inspected ADC repository. They are investigation entry points, not instructions to concentrate all new behavior in those files.

| Existing foundation | What to reuse | Gap or change |
| --- | --- | --- |
| `model.go`, `team.go` | Permanent `Agent` identity, reporting relationships, model/category choices, tools; separate `Assignment` and `Run` records | Areas, intent, durable obligations and knowledge are not first-class owner state. Do not confuse `Assignment.Owner` with the broader concept without inspecting its current supervisor semantics. |
| `store.go` | SQLite, record/history patterns, transactional batches, organization membership | Add versioned state and atomic coordination appropriate to owners, obligations, messages and wakeups; exact tables versus record kinds are a phase-local choice. |
| `engine.go`, `messages.go` | Delegation, provider execution, status, yielding, persisted messages | Messages currently target a run in the same assignment. Context primarily assembles the assignment, its runs and evidence, plus organization catalogs. Add bounded owner context and an addressable agent inbox. |
| `documents.go` and document UI | Revision history, source links, selected-text discussion patterns, organization document discovery | Add maintained area knowledge, uncertainty, correction and reconciliation. Stored transcripts/documents are source material, not already-curated memory. |
| `schedules.go`, `waits.go`, `recovery.go` | Cadences, durable observation, restart handling, account capacity, coalescing missed schedules | Schedules create assignments; waits are tied to planned milestones. Neither currently provides general attention to an owner's obligations after an assignment ends. |
| `execution_plans.go`, `review_status.go`, `review_handoff.go` | Dependencies, candidate/final review, correction, evidence pins, durable progress | Keep these for work that benefits from them. Remove the assumption that every domain needs the same engineering review workflow. Existing work retains its requirements. |
| `decision_actions.go`, `decision_acceptance.go`, `delivery_lifecycle.go` | Concrete approvals, executor wakeup, observed outcomes, correction before delivery | Connect outcomes to continuing obligations without duplicating approval or making a completed run the final proof of success. |
| `proposals.go`, `schedules_web.go`, `usage.go` | Shared proposal queue, recurring-work approval, funding and reported usage | Add bounded assessment cycles, reasons for deferral, owner-level effort accounting and supervisor synthesis. |
| `tool_permissions.go`, `mcp_gateway.go`, protected execution and GitHub delivery | Existing connection boundaries, controlled operations, artifact reconciliation | Preserve current protections. Public submissions require additional hostile-input and admission qualification; do not assume existing protected mode is sufficient. |
| `web.go`, templates, `plan_graph.go`, transcripts | Datastar updates, graph drilldown, decision briefs, phone support | Lead with a supervisor briefing and area pages, keeping existing work views reachable. |

Keep the present Go, SQLite, embedded assets and server-rendered HTML/Datastar stack. No evidence in this discussion justifies a framework rewrite, a separate queue service, a vector database, or permanently running model sessions.

## 3. Contracts to preserve across handoffs

These are behavioral contracts. Implementers may choose different names or representations if the contracts remain intact.

### Identity and responsibility

An area has a designated accountable permanent agent. One agent may own multiple areas; an area may involve many contributors. A cross-area outcome has one delivery lead and an answerable supervisor. Changing the execution instance or delivery lead preserves the history and never silently drops an obligation.

An obligation must identify the owed outcome, accountable owner, originating context, authorization basis, evidence needed to resolve it, current disposition, and its next action, wake condition, or explicit blocker. It may link several assignments and attempts over time. Ownership stays clear even when someone else must provide an observation.

Completing a run or assignment does not automatically resolve linked obligations. Conversely, an explicitly tracked future follow-up must not force the completed implementation activity to appear endlessly running. UI language must distinguish the completed milestone from the outstanding outcome.

Deferral, cancellation, ownership transfer and resolution retain reasons. Cancelling an assignment suppresses its execution and queued effects; it must neither erase continuing obligations nor silently allow those obligations to recreate the cancelled work. Surface affected obligations for disposition, and require a legitimate continuing authorization before any new execution. Removing or disabling an owner exposes unresolved responsibilities for transfer rather than deleting them.

### Knowledge and authority

Maintained knowledge identifies sources, revision, relevant observation time, uncertainty and whether intent is human-confirmed or inferred. Conflicting updates are preserved for reconciliation. A model summary cannot silently replace a human decision or accepted public promise.

Knowledge is context, not executable instruction or authority. A message, a discovered document, or an external submission cannot expand grants, switch funding, or create human approval. Tool/runtime checks enforce the properties ADC actually controls; advisory limitations remain explicitly labeled.

### Attention and funding

An owner can arrange follow-up within existing authorization without human reapproval. A follow-up is not automatically entitled to replay the original mutation: authority for a one-time action and authority for later observation must remain distinguishable.

Every activation has a source, purpose, bounded context, funding account from an explicitly selected human portfolio, and applicable effort limits. Background attention needs designated funding. Missing funding, revoked access or exhausted capacity creates a visible wait or blocker, not an implicit fallback to another human's subscriptions.

Concurrent runs do not multiply a shared investigation or recovery budget. Waiting consumes no model slot. Unknown token telemetry remains unknown; enforceable bounds can use elapsed execution, activation/attempt limits and concurrency without pretending token counts are exact bills. Choose tunable defaults during qualification.

### Recovery and concurrency

Durable claims prevent duplicate handling of an obligation or inbox item. Repeated due events and restart recovery reconcile to the same intended work. A lease expiring is not proof an external effect did not occur. Inspect durable operations and external outcomes before retrying a mutation.

Revision checks protect concurrent knowledge edits. No-progress history and effort accounting survive run replacement, reassignment and restart. Recovery may change tactics within the agreed bounds; it must not reset the budget merely by creating another worker.

## 4. Delivery sequence

The dependencies below are for implementing ADC. They are not a mandatory workflow that future ADC users must follow. Each phase should leave a usable increment, a handoff checkpoint, and evidence for its exit criteria. Routine technical decisions do not require another product interview.

### P0 — Establish the seam and migration baseline

**Outcome:** A small documented contract explains how owner state will coexist with current assignments, plans and approvals.

Inspect the execution and review state transitions, account selection, scheduling overlap rules, document context, and persistence/migration conventions at the current checkout. Reconcile the historical backlog against code and the latest deployment checkpoint; older “next” or “awaiting rollout” paragraphs are not authoritative on their own.

Choose the minimal relationships needed for P1: permanent owner ↔ area; obligation ↔ source/authorization and execution attempts; knowledge ↔ area and evidence; due attention ↔ obligation or standing responsibility. Record behavior for pause, cancellation, revocation, owner transfer and restart. Select a migration approach that keeps old IDs and evidence intact.

Create small fixtures representing an owner with a completed source run and an outstanding later check, plus a family-domain responsibility containing no software vocabulary. Define expected behavior before extending prompts.

**Exit:** A short implementation note and test scenarios settle the coexistence rules. No universal workflow language, full knowledge ontology, or mass reinterpretation of old tasks is required. P0 must remain a bounded preparation step.

**Depends on:** None. **Likely touchpoints:** model/store, engine, recovery, schedules, existing design documents.

### P1 — Prove one owner remembers and follows through

**Outcome:** The first complete ownership loop works across assignments and restart, with a minimal area view.

Add area association and a small versioned owner summary with source references. Existing agent IDs remain stable; do not infer rich confirmed intent from role descriptions. Add obligations, agent-initiated in-scope follow-up registration, and one durable timed wake path using existing execution capacity. Carry authorization and funding explicitly into the resulting execution.

Provide narrow tools for reading owner state, recording/updating an obligation, and reporting evidence or a blocker. Exact tool names remain open. Updates must be structured enough for the scheduler and UI to understand the owed outcome without repeatedly asking a model to parse the transcript.

On activation, supply a bounded owner summary, applicable obligations, current assignment context, and links for targeted retrieval. A fresh run can use knowledge from prior work; it does not require the same provider session or model instance.

Provide a minimal owner page showing understanding, outstanding obligations and next attention, with links to source work. Completing the source activity leaves a visible “verification due” obligation. Keep advanced onboarding and polished supervisor synthesis for later phases.

**Exit scenarios:**

- A fixture records “verify the next replication after this approved repair,” finishes the source run, restarts the service, and executes the due verification once without human continuation.
- The resulting evidence resolves the obligation or records a concrete unresolved condition; a model saying “done” without the required evidence does not close it.
- A new assignment retrieves a relevant earlier lesson without receiving the entire history.
- Two instances claiming the same follow-up do not perform duplicate work. A revoked grant or cancelled source does not get bypassed by wakeup.
- Old assignments, reviews and schedules still operate as before; no real storage mutation is used to prove the feature.

**Depends on:** P0. **First usable milestone:** stop and demonstrate this before broadening the model.

### P2 — Make familiarity maintainable by agents and humans

**Outcome:** Owners acquire useful understanding through discovery and retain corrections and experience over time.

Build bounded onboarding: inspect configured resources and supplied documents, produce a provisional area understanding, ask focused questions, and propose standing checks. Support a human brief before or during discovery. “Confirmed intent,” “observed,” and “inferred/unverified” must be recognizable without requiring humans to classify every sentence manually.

Extend knowledge beyond a single summary where actual use requires it. Keep underlying sources and historical revisions retrievable, stale observations distinguishable, and context loading bounded. Start with explicit references and simple retrieval; add semantic indexing only if measured retrieval failures justify it.

Support selected-text correction with the selected revision, discussion history and conflict handling. A correction identifies related obligations/proposals for reconsideration. Superseded knowledge remains historical evidence, not an instruction to continue an obsolete action. Equal-authority human disagreements are surfaced to the supervisor with both positions.

Add a deliberate way to promote useful lessons from outcomes without ingesting every transcript as truth. External content never directly writes confirmed knowledge. Public repository intent documents are proposed from the accepted understanding, reviewed, and published only through the existing authorized delivery path. Changes to the public document can be detected and reconciled; its filename and exact layout are not fixed here.

**Exit:** Onboard one homelab area and one product area using real read-only evidence where access is already authorized. A later task uses a corrected fact; a concurrent stale edit is handled without losing either contribution; private/contextual material is absent from a public-document preview. A dormant-experiment fixture can conclude “leave this alone” and retain why.

**Depends on:** P1. **Likely touchpoints:** documents, context assembly, team/area UI, proposal and publication flows.

### P3 — Add owner-addressed coordination and accountable recovery

**Outcome:** Permanent agents can coordinate across assignments without losing the lead or the supervisor's obligation.

Add a durable inbox addressed to a permanent agent, including originating evidence, the relevant obligation/outcome, and the requested response. Decide whether to deliver into an appropriate active run or create a bounded activation. Preserve today's run messaging for in-assignment conversation. An acknowledgment is not completion of the requested work.

Receiving a message does not import the sender's approvals, tools or account. Where cooperation requires work under another authorization context, the receiving owner uses its own valid scope or requests the missing decision. Deduplicate repeated messages and suppress response loops that produce no new evidence.

Represent a cross-area lead and contributors without making the lead a new permissions tier. The supervisor can inspect all obligations it remains answerable for, reassign execution, obtain expert help, and keep independent branches progressing.

Introduce durable no-progress and recovery accounting. Use observable signals—repeated errors, unchanged deliverables or blockers, consumed effort, and missing responses—alongside model judgment. Thresholds are tunable. Ordinary recovery stays autonomous within budget; exhaustion or a genuine authority/priority conflict produces one concise decision rather than repeated “continue” requests.

**Exit:** A hosting lead requests NAS evidence across work contexts; the source run finishes but the reply reaches the continuing owner. A failed approach changes within budget without human prompting. A blocked collaborator does not halt unrelated work. Changing leads preserves accountability, evidence and spent effort. A message cannot resurrect a cancelled action.

**Depends on:** P1; use P2 knowledge APIs as available. **Likely touchpoints:** messages, engine, recovery, blocked-work handling, owner views.

### P4 — Turn bounded attention into a supervisor briefing

**Outcome:** A small periodic assessment produces useful cross-area proposals, and the organization home explains what is happening.

Extend attention beyond one-time timers to approved periodic checks and relevant events. Reuse cadence and observation infrastructure. An inexpensive scheduler determines what is due; a model does not run continuously just to rediscover that nothing changed.

Separate the short assessment occurrence from the longer obligations it may identify. Existing schedules retain their current non-overlap behavior unless explicitly migrated. New assessment cycles must not be permanently suppressed by one earlier long-lived repair, nor duplicate that repair.

Implement the Sunday cycle: terse area observations or none, supervisor clustering, selective deeper investigation, and a small prioritized proposal set. Preserve deferral reasons and related work. Limit initial scanning, deeper investigation and internally funded review separately. Direct human requests, corrections and overdue authorized follow-through must not be starved by speculative maintenance.

Make the organization landing page a supervisor briefing with decisions first, work being handled, and material changes. Generate narrative on meaningful changes or bounded cadence; opening/refreshing the page must not trigger fresh model work. Anchor narrative to current records and show freshness. Deterministic status should expose a stall even if the last narrative sounds reassuring.

Area and outcome drilldowns retain agent identity, obligations, next checks, evidence, graphs and transcripts. Live updates preserve selections, expanded detail and notes on desktop and phone. Build on the current visual direction rather than replace it with a generic dashboard.

**Exit:** An assessment combines NAS and hosting observations into one recoverability proposal, or reasonably produces none. Repeating the cycle without new justification does not reproduce declined suggestions. A long-lived verification does not block the next bounded scan. Restart coalesces missed checks without an inference burst. The briefing distinguishes a healthy wait, a stalled obligation, and a human decision without transcript inspection.

**Depends on:** P2 and P3. **Milestone:** A coherent first release of continuing ownership.

### P5 — Validate domain independence and adapt workflow policies

**Outcome:** The same ownership core serves engineering and non-engineering responsibilities without forcing either into the wrong process.

Identify software-specific assumptions in tools, prompts, completion logic and UI. Move review requirements and delivery conventions into explicit domain/work-category policy while keeping artifact-bound validation reusable. Start with a small number of comprehensible policies rather than a workflow programming language.

Snapshot applicable policy for work when it starts; changing defaults must not silently weaken active work or its approvals. Frostyard engineering retains independent model-family review and repository gates. A routine family obligation can finish on appropriate evidence without an artificial code artifact or mandatory QA document. Learning notes and simple internal acknowledgments should not trigger recursive review chains.

Configure candidate areas, not hard-coded product types: Snosi with distinct variant intent; independent products such as Updex; shared engineering infrastructure; and a low-effort experiments caretaker. Let onboarding and experience inform permanent team size. No automatic large team creation is part of migration.

**Exit:** Run three end-to-end scenarios using the same ownership APIs: a homelab follow-up, a Frostyard cross-resource investigation, and a family responsibility with no repository/PR concepts. Include the uncertain NVIDIA-support scenario: an experimental milestone may finish while hardware validation remains owed; an external test report cannot declare supported status by itself. Configuration changes do not broaden existing scope.

**Depends on:** P4 for release qualification. Investigate policy seams in P0 so the family fixture can be used earlier. **Milestone:** Generalized internal ownership, before external participation.

### P6 — Restore simple contributed inference with controlled admission

**Outcome:** A contributor can use a public queue address and existing inference tools to return useful bounded work; internal owners retain control.

Inspect Bluefin's actual contributor package and Snowcat's prior flow as references before selecting the client/protocol. Do not assume their implementation from the command-line experience alone. Prefer a minimal usable HTTP interaction unless another transport has a demonstrated need; the endpoint and client packaging remain choices for this phase.

Define deliberately public work packets: outcome, criteria, public source revisions, allowed contribution type and a submission route. Development is eligible; external review is optional. Supervision, ownership, coordination and internal knowledge management are never donated roles. Owners select public eligibility under an approved organizational policy, avoiding a new human approval ceremony for each already-authorized public item.

Public contributors need no ADC membership or manually provisioned token. Automatic temporary claim receipts may associate a submission with work, but grant no internal access or authority. Claims expire safely, contributions bind to their work/source revision, and abandoned claims become available without losing owner accountability. Public work should be solvable without installation credentials or internal tools.

Implement intake before exposing the endpoint: bounded payloads and queue capacity, cheap structural checks, replay/deduplication handling, controlled artifact retrieval, and a budget for internal evaluation. Arbitrary submitted URLs, logs and commands are not trusted. Public review cannot satisfy the required internally owned admission review; reported model identity is not independently verified reviewer provenance.

Evaluate candidate code in an environment without installation secrets or privileged internal access, including during build/test hooks. Threat-model and qualify the actual route from submitted material to reviewer context, execution, retained knowledge and final admission. Use existing protected execution only where testing establishes that it supplies the necessary boundary. If that boundary is incomplete, keep the public endpoint disabled while internal ownership remains usable.

**Exit:** An unaffiliated client discovers work, claims it, submits a candidate, receives useful status, and can leave without stranding the outcome. A malicious prompt in a log, hostile test hook, duplicate submission, stale candidate and review-flood attempt do not gain authority, expose secrets, poison confirmed memory, or create unbounded internal spending. An internal reviewer catches an external false PASS. Existing human publication/merge approval remains in force.

**Depends on:** P5 and demonstrated admission isolation. Reputation and earned reviewer privileges are deferred; they must not complicate the first public flow.

### P7 — Add deliberate federation between instances

**Outcome:** Separate supervisors exchange selected information or work requests without sharing their entire knowledge or authority.

Define explicit pairing and revocation, a disclosed information/request packet, source/freshness, request correlation and a receiving-side response. Authentication between installations is separate from the anonymous public contribution path. Keep workers from directly addressing another installation's workers.

Receiving supervisors accept work under local authority and funding. The sender sees a tracked external dependency and the information deliberately returned, not internal runs, hidden context or credentials. A received request or fact is not a trusted local instruction. Timeouts, decline, cancellation and retries remain visible without duplicate execution or disclosure.

Start with one information exchange and one bounded work request. Do not implement private areas inside an instance or synchronized cross-instance memory.

**Exit:** A personal assistant shares an availability fact with a family instance without revealing its underlying appointment; a Frostyard supervisor requests homelab assistance whose execution remains locally approved. Test rejected pairing, revocation, duplicate delivery, delayed/stale evidence and remote unavailability.

**Depends on:** P5. Can proceed independently of P6 after the internal model stabilizes; recommended ordering favors restoring contributed inference first. Transport and deployment packaging are phase-local decisions.

## 5. Migration and rollout strategy

Introduce owner state additively and enable the new behavior for selected areas first. Existing organizations, agents, assignments, plans, approvals and schedules preserve their identities and semantics. Do not automatically reset finished work, clear deliberate blockers, reinterpret prose as permission, or convert every old task into a new obligation.

For an opted-in area, import current role descriptions and linked history as provisional source material. Let bounded discovery identify candidate obligations; distinguish inferred historical obligations from explicit current commitments. Reuse valid authorization where it demonstrably applies rather than requesting approval again, but never manufacture it from a past Result string.

Select pilot work using live state at rollout time. Do not assume the Repogen or wiki tasks discussed earlier are still in the same state. Existing business work can continue on the legacy execution path while new owner capabilities are qualified separately.

Before deployment: pass relevant checks and independent review, take a consistent database backup, record migration version and pre-restart active work, and define how to stop new attention without deleting its state. After deployment: verify unchanged task identities/approvals, resumed eligible work, no duplicate effects, and visible ownership obligations.

Rollback must name which binary/schema combinations are safe. An additive migration is not automatically downgrade-compatible. Prefer disabling new dispatch/feature behavior while retaining records; restoring a database after new external effects requires reconciliation and is not an automatic rollback technique.

Updating the in-repository build/backlog/design documents is part of adopting the plan, not yet done by writing this proposal. Repository-specific permission and deployment prerequisites must be checked when implementation starts. Currently these planning artifacts are in the writable conversation `outputs` directory; the source repository was inspected read-only.

## 6. Qualification and evidence

Use deterministic lifecycle fixtures for failure, concurrency, clocks, revocation and restart. Then run a small number of real provider-backed scenarios to establish that agents can use the tools and context correctly. Label fixtures and real evidence distinctly. Tests should challenge the behavior, not merely mirror new record fields.

For code changes, follow repository `AGENTS.md`: meaningful tests, `make verify`, and independent review from a different model family. Same-family execution under a different provider is not independent review. Do not use real infrastructure mutations or public publication as software tests without the corresponding explicit authorization.

The primary qualification story is:

1. Discover a small area with supplied intent and inspectable sources.
2. Have a human correct one material statement.
3. Complete a bounded authorized action or safe fixture equivalent and register a later verification obligation.
4. Finish the source activity, change the run/provider session, and restart ADC.
5. Wake the owner automatically with the corrected understanding and valid funding.
6. Encounter one controlled failure; recover within budget while unrelated work continues.
7. Record observed verification, resolve the obligation, and update the supervisor briefing.

Add a read-only Frostyard cost investigation to test judgment: identify actual missing evidence, consider retiring artifacts, and propose proportionate next steps without an unrequested hosting migration. Add the family scenario to detect domain coupling early. A real calendar-time follow-up supplements accelerated clock fixtures; do not call weeks of behavior proven by a simulated timer.

Track a small set of outcomes across these scenarios: avoidable human continuation prompts, unresolved obligations lacking a next step, duplicate actions, no-progress activations, context/usage size where measurable, and whether a correction affects later work. The controlled lifecycle should require zero routine continuation prompts and perform no duplicate action. Tune broader thresholds against pilots; do not promise dollar savings from incomplete provider telemetry.

## 7. Relationship to the existing backlog

- Preserve the deployed delivery/recovery repairs and their regression coverage. Treat a recurrence as a defect, not a reason to reintroduce manual bookkeeping.
- Bring relevant reviewer briefs, validation evidence and environment readiness into the phase that needs them; do not rebuild capabilities already present merely because old backlog prose calls them pending.
- Review-capacity limits and useful outcome metrics support P4 and P6. Detailed finding analytics can follow unless a pilot demonstrates a blocker.
- Broader account portfolios remain useful but are not a prerequisite for ownership. Use the currently supported explicit funding combinations for initial qualification; never silently add accounts to make a three-provider scenario work.
- Additional local-model coding qualification remains separate. Start ownership pilots with trusted, already-qualified model combinations so unfamiliar model behavior does not obscure lifecycle defects.
- Stronger isolation becomes a prerequisite for public candidate execution, not for delivering the internal ownership milestone. Remote execution, container-provider breadth, export UI and reputation remain later work unless a concrete phase requires them.

## 8. Handoff protocol

At the end of each implementation slice, leave a compact checkpoint in the adopted plan/backlog containing:

- Phase and behavioral outcome delivered; remaining exit criteria.
- Commit/branch and touched interfaces or record contracts.
- Tests and independent-review evidence, including failures and unresolved limitations.
- Migration/version compatibility, feature state, and whether deployed or only built.
- Live work affected and any required reconciliation; never include credentials.
- Decisions made with brief reasons, and the next bounded action.

The next agent reads the desired state, this plan, repository `AGENTS.md`, and the latest checkpoint; verifies the actual checkout and runtime state; then continues the next bounded action. It does not restart the interview, reset completed work, or treat historical approvals as permission for unrelated actions.

A phase needs human input when evidence forces a change to product behavior, authority, spending, disclosure, or an important trade-off. Storage layout, routine tool names and prompt tuning should be settled through implementation evidence and review. Keep such choices replaceable rather than encoding them as new product requirements.

## 9. Decisions intentionally left open

| Decision | When to settle it | Evidence that should drive it |
| --- | --- | --- |
| Exact owner/area/obligation schema and tools | P0–P1 | Existing persistence conventions, concurrency and migration tests |
| Knowledge granularity and retrieval | P1–P2, revisited as needed | Context cost, missed relevant facts, correction behavior |
| Attention and recovery limits | P1 defaults; tune in P3–P4 | No-progress behavior and real usage, without budget resets through delegation |
| Permanent role topology | Onboarding and pilot use | Actual responsibility and collaboration needs, not repository count |
| Minimal workflow/review policy representation | P0 seam; complete in P5 | Engineering and family scenarios, preserved active-work guarantees |
| Public queue protocol and client packaging | P6 | Bluefin/Snowcat inspection and a working low-friction contribution prototype |
| Federation transport and exchange format | P7 | Selective disclosure, revocation and recovery qualification |

The next step after approval is **P0 followed by the P1 ownership loop**, not a wholesale redesign or a new fleet of agents. The plan succeeds incrementally when ADC remembers an obligation and completes it without Brian having to remember it too.
