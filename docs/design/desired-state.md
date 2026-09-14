# Aide-de-Camp: Desired State

This document records the desired state established in the ownership, knowledge, and accountability discussion of September 13, 2026. It describes the intended product, not its current capabilities or a plan for implementation. The migration from today's ADC is a separate discussion.

## Purpose

ADC is a tool for creating a continuing team of agents responsible for areas of human concern. An installation can serve an open-source organization, a homelab, a family, personal finance, a personal assistant, or another knowledge domain.

The defining experience is being able to say **“own this area”** and receive informed attention, useful proposals, reliable follow-through, and a concise supervisor briefing. Humans should not have to remember every outstanding promise, prompt routine continuation, relay ordinary internal handoffs, or execute work they already authorized agents to perform.

ADC is primarily a tool for Brian's installations, available as open source for others. Its core must not encode Frostyard, the homelab, repositories, or software delivery as universal assumptions.

## The central shift

Permanent agents provide continuity. Assignments organize particular outcomes. Runs are temporary instances through which agents execute work.

An agent's responsibility survives the end of an assignment or run. It maintains understanding of its area, retains obligations, learns from outcomes, and notices when approved responsibilities need attention. It can have multiple concurrent instances within the applicable capacity and effort limits.

Work can move through queues and follow different workflows. Queues make work accessible and allocate capacity; they do not prescribe a universal planner → developer → QA → finisher sequence. Owners and supervisors arrange appropriate collaboration around the outcome. Durable task records and dependency graphs remain useful evidence and coordination mechanisms.

## Domain language

| Concept | Meaning in ADC | Examples |
| --- | --- | --- |
| Installation / instance | A shared team, knowledge, and visibility boundary | Frostyard, Brian's homelab, a family instance |
| Area | A continuing subject of responsibility; may span multiple resources | Storage, Snosi, shared engineering infrastructure |
| Owner | A permanent agent accountable for an area's understanding and obligations | NAS owner, Updex owner |
| Supervisor | The agent answerable to humans for overall priorities, coordination, recovery, and outcomes | CTO, homelab supervisor, personal aide-de-camp |
| Intent | Desired outcomes, audiences, priorities, constraints, and non-goals | Keep hosting costs proportionate to the user base |
| Knowledge | Maintained understanding with sources, uncertainty, and relevant history | Why a design was chosen; what remains unverified |
| Product promise | An outcome or compatibility expectation users should be able to rely on; relevant where the domain has products | ZFS data-pool support; Updex compatibility beyond Snosi |
| Standing responsibility | A continuing duty within approved scope and attention limits | Periodically assess backup health |
| Obligation | Something still owed, with an owner and a next action or wake condition | Verify the next overnight replication after a repair |
| Assignment | A bounded piece of work toward an outcome | Investigate hosting costs; improve container recoverability |
| Delivery lead | The named agent responsible for completing a particular outcome, including cross-area work | Hosting owner leading an Incus/NAS backup improvement |
| Run | A temporary execution instance of an agent | One instance investigating while another verifies a prior fix |
| Proposal | A concrete suggestion for work requiring human selection or authorization | Up to three prioritized maintenance improvements |
| Evidence | Observations and artifacts supporting a conclusion | Test results, a restore observation, a bill, a reviewed change |
| Attention | A bounded opportunity to inspect responsibilities and decide what needs action | Sunday assessment, an obligation becoming due, a surprise bill |
| Authority | The scope of actions an agent may take; distinct from knowledge and responsibility | Observe, prepare a draft PR, execute an approved change |
| Budget | Limits on capacity and effort | Concurrent workers, investigation effort, recovery effort |
| Contribution | Externally supplied execution or scrutiny submitted for internal evaluation | A code candidate or independent review findings |
| Federation | Deliberate supervisor-to-supervisor exchange between instances | Share availability or request infrastructure support |

Repositories, storage pools, household calendars, and financial accounts are resources within areas. They do not define the core ownership model.

## Continuing ownership and accountability

An owner remains responsible through delegation, correction, waiting, and verification. “I did my part” is not a sufficient outcome. A finished run does not erase a promise.

Owners may create bounded follow-up obligations and arrange their own later attention without another approval when the follow-up stays within existing authorization. For example, an approved backup repair includes checking that the next relevant backup succeeds. If verification fails, the owner pursues correction within scope or presents a concrete decision when scope or authority must change.

Adjacent improvements discovered during work become proposals. Following through on an approved outcome must not become a pretext for unlimited new work.

Waiting on a human preserves agent ownership. The owner explains the actual decision or missing observation, retains the context, and resumes after the answer. It does not return the whole coordination burden to the human.

Concurrent instances share the permanent owner's knowledge and obligations. ADC coordinates claims and updates so that two instances do not independently perform the same follow-up or silently overwrite conflicting conclusions. Concurrent code work uses separate branches/worktrees as already agreed.

## Supervision and collaboration

Humans have one supervisor they can hold to account. The supervisor may appoint a specialist as delivery lead, while remaining answerable for the complete outcome.

For cross-area work, one lead owes the result and coordinates contributing specialists. Specialists retain responsibility for their own areas. Being a lead does not grant additional permissions or authority over another agent's resources. Conflicting priorities or scope are resolved through the supervisor.

Agents can request expert collaboration directly within the organization. The supervisor need not relay every exchange or approve every intermediate step. Communication addresses continuing agents and responsibilities, rather than depending exclusively on the existence of a particular active run.

The supervisor watches for repeated failure or effort without new evidence. Within approved scope and budget, it changes tactics, obtains expertise, or reassigns delivery. It keeps unaffected work progressing. It involves humans for substantive choices or changes to bounds, not routine recovery.

Delegation never escalates authority. The initial practical authority model may be advisory, as previously agreed; ownership must not be presented as technical enforcement that does not exist. Stronger guardrails remain important as more daily work moves to cheaper models.

## Familiarity, knowledge, and intent

Familiarity means maintained understanding, not an indefinitely growing transcript placed into every prompt. Owners retain useful decisions and their reasons, relevant experiences, unresolved questions, deferred ideas, and links to evidence. Current ground truth remains discoverable and is rechecked when needed.

Knowledge distinguishes documented decisions, human-confirmed intent, observed facts, and agent hypotheses. Sources, freshness, and uncertainty remain visible. An old plan is not proof that something shipped; current implementation is not necessarily a promise to preserve every behavior.

Knowledge, obligations, and standing responsibilities have different lifetimes. Knowledge may need refreshing; obligations need resolution; standing responsibilities continue until changed. A note in a memory file alone is not sufficient tracking for a promise.

New owners perform bounded discovery, augmented by human briefs, documents, and interviews. They first inspect enough evidence to ask informed questions, then present their provisional understanding, important gaps, and proposed periodic checks. Exhaustive inventory is not a prerequisite for useful work.

Humans can inspect the owner's understanding, select a statement or paragraph, and discuss a correction. The owner preserves the reason for the correction and considers affected obligations and proposals. Learning from completed work is part of responsible follow-through; it should not create a quota of artificial lessons.

Specialists maintain detailed understanding while making relevant conclusions discoverable to colleagues. Shared decisions inform the whole team without forcing every agent to read every investigation.

For repositories, a short maintained public intent document states audiences, desired outcomes, commitments, and non-goals. It is the public-facing version of potentially deeper ADC knowledge. Accepted public intent is authoritative; internal notes cannot silently redefine it. Changes from either side must be reconciled, and private context is not automatically included in public documents.

## Bounded proactive attention

Owning an area includes periodic checks within an approved scope and time/effort budget. Attention can also be triggered by an obligation becoming due, relevant new evidence, or a human nudge. The system provides a durable mechanism to notice these conditions and resume the appropriate owner, including after restart.

The desired Sunday cycle is:

1. The supervisor requests a brief list of needed work from each area, including none where appropriate.
2. Specialists make bounded assessments informed by history and prior decisions.
3. The supervisor identifies patterns and clusters of related work across areas.
4. It requests deeper investigation only where warranted.
5. It presents a small, prioritized set of concrete proposals for humans to accept, reject, or refine. Humans may select zero, some, or all.

“Up to three” is a limit, not a quota. Declined and deferred proposals retain their reasons so subsequent assessments do not recreate the same conversation without new justification. Investigation and approved execution have separate bounds.

An unresolved repair must remain tracked without causing endless model polling or preventing unrelated useful assessment. A wakeup is an opportunity to exercise judgment within budget, not a command to generate activity.

## Human experience

The organization home begins with a supervisor briefing: what needs a human decision, what the team is handling, and what materially changed. It distinguishes a healthy wait for later verification from a stall with no useful next step.

Humans can drill into an outcome, its lead and contributors, and then an area's intent, understanding, obligations, upcoming checks, recent outcomes, and evidence. Execution graphs and live transcripts remain available as supporting detail. Agent identity precedes model identity.

Decision requests have a concise brief and Approve / Reject / Refine with notes controls. Evidence is available without overwhelming the request. Approving an action lets the agent execute, validate, record, and continue that action; it does not mean the human must copy hashes, merge local work, or repeatedly approve the same approval.

The UI must remain comfortable and usable on a phone. Ongoing internal activity should not drown the briefing in tool calls or require navigating an unbounded page.

## Domain examples that the model must support

### Homelab

Owners cover storage/NAS, hosting/virtualization, and networking. The supervisor can combine observations from NAS and Incus owners into a single proposal for better container recoverability, appoint one lead, and retain overall accountability.

A repair is verified by the relevant operational outcome, such as a successful backup and restore observation. Storage changes remain subject to the intended approval boundaries. Responsibility to notice a problem does not imply permission to make every possible infrastructure change.

### Frostyard

Ownership follows products and shared capabilities rather than repository count. Snosi is understood as a product family with desktop and server variants that have distinct goals. Separate variant promises must remain visible even if one permanent Snosi owner initially covers both. The exact team decomposition is not settled by this document.

Intuneme and Updex have substantial independent audiences and warrant attention to goals beyond Snosi. Chairlift primarily serves the Snow images but also has outside users whose expectations must not disappear. Owners represent those audiences when cross-project changes create conflicts.

One permanent owner is desired for shared engineering foundations: `core` standards, build and release infrastructure, publishing, and hosting. Product owners remain responsible for the effect of shared changes on their users.

All Frostyard humans have equal authority and visibility. Participation level and subject expertise guide whom agents ask, not whose permission counts. Brian supplies most current direction; Ben has particular context for Floe and Updex's use in other ecosystems. Kyle's limited availability should not become a routine dependency. Contradictory human direction is surfaced with context and trade-offs rather than exploited to obtain a preferred answer.

Concrete intent examples:

- **ZFS:** Snosi servers should support creating and using data pools outside the boot disk, with the requisite operating tools. ZFS boot disks are out of scope. The desired experience is an explicit opt-in, an extension, and a reboot away. A systemd-sysext is the current delivery preference; feasibility and licensing require investigation. Selfie potentially taking over TrueNAS hosting duties is motivating context, not authorization for a storage migration.
- **NVIDIA:** The desired desktop experience is a usable initial desktop and potentially an optional extension plus reboot for a preferred driver. Current first-boot behavior is unverified. Other users have hardware and requested support. An experimental candidate can be delivered before supported status, while the owner retains the hardware-validation obligation. Model review cannot substitute for missing hardware observations. Initially, maintainers relay test requests and results; direct external tester coordination requires separate design.
- **Hosting cost:** Keep distribution costs proportionate to a small user base without compromising installation and update reliability. A surprise bill is a nudge to investigate and prioritize, not automatic permission to change services. The reported Cloudflare bill grew from roughly one fifth of $31 to $31 in a month; artifacts include ISOs, retiring a/b root images, and sysexts. The owner should establish actual cost drivers, account for planned retirement, investigate suitable alternatives, and verify the effect of approved changes. Existing GHCR use is a candidate precedent, not proof that all artifact types can move there without investigation.

### Experiments and personal continuity

An experiments caretaker can provide low-effort ownership across repositories that do not deserve individual agents. It remembers purpose, why work stopped, whether it was superseded, and what might be useful later. Inactivity is not a defect. It does not manufacture maintenance campaigns; archiving, deletion, and shutting down resources remain proposals where approval is required.

A personal aide-de-camp can retain the trajectory across projects. The history supplied in this discussion is that Snowcat had rigid entry points and generated unbounded busywork, but contributed inference through its MCP queues was valuable. Bobsled reduced ceremony and roadblocks. Both lacked the supervisor that ADC introduced. The lesson is to preserve flexible participation, bound work, reduce ceremony, and keep someone responsible for the outcome.

### Other domains

Family, personal finance, and personal assistant instances use the same concepts with different resources, tools, intent, and policies. They do not inherit PR terminology or mandatory software-development stages. Review policies are configurable by the domain and work category. Independent cross-model review remains fundamental to the agreed engineering workflows.

## Contributed inference

ADC should recover Snowcat's useful ability to let others contribute execution through a simple queue address. A small client or an existing agent should be able to discover public work, take a bounded packet, and submit a candidate without ADC membership or manual token provisioning for the public contribution path.

The reference experience is Bluefin's contributor tooling, identified by `brew install ublue-os/experimental-tap/bluefin-contributor-tools`, and the simple contribute/review commands described in the discussion. This is an experience reference, not a claim that its implementation or security model has been audited.

Development work is suitable for contribution by default, with external review also a possibility. Supervision, continuing ownership, coordination, and control of internal knowledge remain internal responsibilities. Only deliberately exposed material belongs in public work packets; private knowledge and installation credentials do not accompany them. The precise selection policy for public work remains to be specified.

ADC retains responsibility for evaluating submissions, arranging corrections, recovering abandoned work, and controlling admission. An internally owned review remains required even when external reviews exist. It independently evaluates the candidate rather than merely accepting an external verdict.

External code, logs, claims, reviews, and proposed learnings are untrusted input. They cannot directly grant authority, change internal memory, or execute in a privileged environment. Cross-model review is a quality measure, not a substitute for the technical boundary around external material. Public participation must not enable unlimited internally funded review work.

Reputation-based contributor or reviewer privileges are a future consideration, not an initial requirement.

## Instance boundaries and federation

Everyone within an instance can see its shared work and knowledge. Private areas and compartmentalized visibility inside an instance are intentionally out of scope for now. Separate instances provide the boundary between different audiences.

Federation permits deliberate supervisor-to-supervisor exchange of information and work requests. Workers do not directly call workers in another installation. Each instance decides what it discloses, and the receiving installation retains approval and execution authority. External work appears as a tracked dependency stub rather than exposing internal execution details.

A personal assistant might share availability with the family supervisor without sharing the underlying appointment or conversation. Shared information retains its source and freshness. Federation is selective disclosure between distinct instances, not automatic merging of their knowledge.

## What success feels like

An owner can explain its area and intent, recall why past choices were made, admit uncertainty, and improve its understanding through work and human correction. It remembers what it owes and follows through across runs and restarts. The supervisor combines local observations into useful priorities and resolves ordinary friction without repeated human prompting.

Humans receive concrete outcomes and meaningful decisions. A contribution, a merged change, or a completed run is recognized as a milestone where appropriate, while the promised operational or product outcome remains owned until it is established or explicitly changed.

The same core can support an Incus backup follow-up, a cross-repository product change, or a family responsibility without domain-specific machinery leaking into every workflow.

## Deliberately deferred

This desired state does not choose the storage design for knowledge and obligations, prompt formats, wakeup transport, queue protocol, exact role topology, or migration sequence. Time/effort accounting and the detailed public contribution boundary need concrete design. Stronger runtime guardrails and federation implementation need their own treatment.

The next artifact should explain how to reach this state from existing ADC, identifying what can be reused, what assumptions must change, and how the behavior will be validated. This document does not authorize implementation.
