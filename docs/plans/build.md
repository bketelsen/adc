# Aide de Camp — proposed build plan

Status: BUILD APPROVED by Brian, with a comfortable, clean, deliberately designed UI and explicitly no generic ivory-and-green styling. The filesystem permission blocker has been resolved. Initial application code is implemented in `/var/home/bjk/projects/adc`, and the development service is responding at http://127.0.0.1:8789/. Implementation and qualification remain in progress; the roadmap below is not a claim that every phase is complete. See `adc-build-handoff.md` in the original task outputs for the current checkpoint.

## Purpose and first proof

Aide de Camp (ADC) is a self-hosted service for organizations of persistent agents. Agents own responsibilities, collaborate, delegate, preserve history, and carry authorized assignments through validation to completion. The human should not need to repeatedly tell a supervisor to continue ordinary corrective work.

Build primarily for Brian's installations, with open-source code and practical self-hosting documentation. There is no commercial product or marketing objective. Frostyard engineering and Brian's Homelab Team are equally valid uses; GitHub repositories and PRs are domain-specific integrations, not the application's fundamental data model.

The first real acceptance assignment is to update `frostyard/frostyard-org` to match what is shipping and used today. It must involve a supervisor, specialists, independent review using a different model family, and correction of review findings without repeated human prompting. The final deliverable is a draft PR ready for human approval, with applicable organizational and repository gates satisfied. It does not include autonomous merge or deployment.

## Agreed product behavior

### Organizations, teams, and people

- Support multiple organizations and reporting layers. An agent has a title, responsibility description, reporting relationship, explicit provider/model selection, autonomy settings, and assigned tools.
- Reporting relationships establish accountability. Within an organization, agents can request expertise across reporting branches. The supervisor gathers disagreements, resolves what it can within the assignment, and presents human decisions with competing views and tradeoffs.
- Create a team conversationally: “I have these things; propose me a team.” The human reviews the proposed roles and configuration before creation. Forms support later edits.
- Supervisors may create temporary workers and recommend permanent additions. Humans approve permanent additions. Temporary-worker findings remain attached to assignments; responsible permanent agents curate lasting knowledge.
- One permanent agent can have multiple active instances. Each has its own assignment context, access to shared durable knowledge, and visibility into related work. Code changes use separate branches/worktrees; actual conflicts are coordinated.
- Frostyard's three humans have equal authority and visibility into all organization work. Conflicting human instructions are surfaced for resolution, without silently choosing the latest instruction. Only affected work pauses. The Homelab installation is single-user.

### Ownership and completion

- Every assignment has an accountable supervisor, requested output, scope, authority, dependencies, and completion requirements.
- Research, implementation, review, correction, and resubmission are part of the authorized assignment. The supervisor may seek more expertise or reassign work without asking the human to restart the process.
- Delegate based on agent descriptions and relevant capabilities. A specialist can request review from another specialist; ownership of the overall outcome remains clear.
- Unaffected work continues while a decision or dependency is pending.
- A draft PR means ready for human approval, including applicable `AGENTS.md` requirements, organizational checks, and independent review. Merely creating a PR is insufficient.
- Preserve assignments through service restarts, disconnected workers, and temporary provider limits. Resume automatically when possible, first checking the outcome of actions whose completion is uncertain.
- Notify humans for ready reviews, decisions, and blockers the team cannot resolve. Keep routine activity inspectable without routine notifications.

### Workspaces and evidence

- Provide agent, team, and assignment workspaces for documents, working notes, conversations, and evidence that do not belong in a repository.
- Preserve historical information; discover current ground truth at authoritative sources. For Frostyard, discover `core` and then individual repositories' implementations of its standards. Do not create a competing copy of canonical policy in memory.
- Link documents and revisions back to their originating assignment, evidence, and feedback.
- Review proposals in the workspace first. Selecting a paragraph or line starts a contextual conversation with the supervisor. Questions invite explanation; clear change requests authorize revisions. Keep revisions recoverable and discussions anchored to the version reviewed.
- “Prepare the PR” is a subsequent publication step. Publication of a proposal and authorization of execution are distinct. A cross-repository proposal might become an ADR plus a linked plan in `core`; an existing ADR should be referenced rather than duplicated.
- Show work at several levels: organization overview, assignment/delegation structure, agent runs, conversations, tools, documents, and review findings. Permit direct human steering of workers, with that intervention visible to the supervising agent.

### Models, accounts, and tools

- Copilot first, with Codex and direct Claude integrations on the roadmap. Use existing subscriptions where supported.
- Each human connects their own accounts. An assignment and its cross-family review stay within that human's subscription portfolio. Dedicated organization subscriptions can be added later.
- Permanent agents have explicit model assignments; temporary workers use defaults by work category, such as implementation, review/QA, research, and planning.
- Implementation and review must use different model families. Provider identity alone does not establish independence. If the required review family is unavailable, wait and resume; an exception requires explicit human approval.
- Bound usage initially by concurrent active model runs per subscription. Count supervisors, specialists, reviewers, and temporary workers. Waiting for an approval, dependency, or retry should release the ADC execution slot. Provider-side session accounting remains provider-specific. Keep queued work visible and avoid starving supervision/review. Direct requests normally outrank maintenance when capacity is constrained.
- Shell, installed CLIs, and MCP connections are required in the first release. Configure an MCP connection once per installation and assign access to agents. A Storage agent can delegate its TrueNAS MCP access to an appropriate worker. Do not grant access beyond what the delegating agent holds.
- Keep credentials separate from workspace documents and notes.
- Record available usage events from the beginning. Token-count displays arrive soon after the first release; unavailable metrics remain explicitly unknown, not zero. Token counts and subscription billing units are distinct.

### Authority and operational work

- Use structured autonomy settings, with draft-PR-level authority as the default software workflow. Delegation cannot expand the authority of an assignment even when a recipient normally has greater authority.
- Initial limits are advisory. Broad shell and credential access means ADC must not claim it technically prevents out-of-policy actions. Strong models, clear intent, and visible history are the initial operating posture.
- Infrastructure agents investigate, suggest, and alert unless a concrete action is approved. Humans generally want agents to execute the approved action themselves.
- An operational proposal includes the target, action, expected effect, validation, and any rollback. Approval covers that bounded sequence; further changes come back to the human.
- Mid-term, introduce enforced permissions and credential mediation so cheaper models can handle daily work with narrower capabilities.

### Scheduled work and federation

- Agents propose concrete standing work. Approval creates a scheduled task with a cadence, scope, expected output, autonomy policy, and designated subscription from a human portfolio. Review can use the required other family within that same portfolio.
- Scheduled runs act within that approval, update existing outstanding work rather than duplicate it, and continue unrelated maintenance.
- Frostyard and Homelab will likely be separate ADC installations. Explicitly connected supervisors may request information or work from one another. Workers cannot contact workers in the other organization/installation.
- Approval and execution remain with the receiving installation. The requester sees an external dependency stub with status, blockers, and returned results, not the remote team's internal conversations.

## Proposed technical direction

Use a Go service with embedded web assets, server-rendered HTML, Datastar for reactive interactions and streamed updates, SQLite for application state, and ordinary files for documents and worktrees. Run one ADC service per installation under systemd in an Incus instance. Serve it behind the installation's HTTPS reverse proxy.

The additional approved UI constraint is a comfortable, clean interface with intentional visual design, explicitly avoiding a generic ivory-and-green palette. Proposed execution: cool neutral surfaces, graphite text, a restrained blue accent, clear typography, readable document measures, and deliberate spacing. Use meaningful work views, not a marketing dashboard or fabricated activity. Include accessible focus and contrast, clear loading/error/empty states, and usable phone layouts. These proposed visual details can evolve during implementation while preserving the user's constraint.

This targets a single ADC application binary, not a machine with no other dependencies: provider runtimes, repository build tools, `git`, and MCP server processes may still need installation. The official [Copilot SDK](https://github.com/github/copilot-sdk) has Go bindings and communicates with its CLI runtime. [Datastar](https://data-star.dev/) supports server-rendered HTML and server-sent events. SQLite is a reasonable starting point for this small, single-service deployment, with serialized short writes; reassess if write concurrency or multi-server requirements change. See [SQLite's deployment guidance](https://www.sqlite.org/whentouse.html).

ADC should own durable assignments, delegation, review requirements, queueing, approvals, and completion. Provider adapters supply agent runs, tools, events, and session continuation. A provider's conversational session should not be the only place that knows an assignment exists or what remains to finish.

Keep these concepts distinct in storage: installation, organization, human membership, subscription connection, agent definition, run, assignment, delegation, dependency, artifact revision, review finding, approval, tool connection, and scheduled task. Provider sessions map to runs, not permanent agent identities.

Use a persisted work queue and event history. Worker completion, review findings, dependency resolution, and provider recovery should schedule the next appropriate supervisor action. A response saying “I can fix that” does not finish an assignment. ADC should identify an idle but incomplete assignment and return control to its supervisor with the outstanding requirement.

Repeated failures should trigger assessment and a change of approach, not unlimited blind retrying. Record which findings are open, what was attempted, and what evidence changed. Make pause/cancel available to humans. Detect loops and bound repeated identical recovery attempts; exact defaults should be tuned against the acceptance assignment.

For revisions and code reviews, bind validation evidence to the artifact revision or commit reviewed. Subsequent material changes require the applicable checks and review again. On restart, reconcile actions such as remote PR creation before retrying them; promise recoverable execution, not magical exactly-once external side effects.

Use individual application logins and organization membership, with separate provider credentials. Shared visibility does not mean silently using another member's subscription. A minimal invitation/login flow is sufficient initially; enterprise identity features are outside the first milestone.

## Delivery sequence

### 0. Prove the provider boundary after build approval

Use a small development fixture to verify Copilot authentication with the intended account, actual model discovery, two distinct model families, tools/MCP, streaming events, cancellation, and session recovery. Pin a tested SDK/runtime combination. This is the first implementation work, not something performed during this interview.

Start with GPT-family execution and Claude-family review through Copilot if the connected account exposes suitable models. Discover exact IDs and access rather than hard-coding a presumed catalog. This proves cross-family review without requiring two provider integrations on day one.

[Copilot authentication](https://docs.github.com/en/copilot/how-tos/copilot-sdk/auth/authenticate) supports user-backed subscription usage. The [Codex app server](https://learn.chatgpt.com/docs/app-server) documents managed ChatGPT browser/device authentication, making it the proposed second provider adapter. Direct Claude subscription integration remains an explicit dependency to resolve: the [Claude Agent SDK documentation](https://code.claude.com/docs/en/agent-sdk/overview) restricts third-party use of claude.ai login/rate limits absent approval. Do not promise direct subscription parity before verifying an applicable supported route. This does not prevent using Claude-family models offered through Copilot.

Exit: a real cross-family correction/review cycle works with the intended subscription, and unsupported capabilities are documented.

### 1. Build the durable organization and execution foundation

Implement organizations and equal-authority memberships; provider portfolios; permanent agents and temporary runs; work-category defaults; MCP assignment; workspaces; event history; queued execution; subscription concurrency limits; structured advisory authority; and assignment/dependency state.

Implement agent-discovery and delegation tools, an explicit owner for each assignment, supervisor wakeups, independent review routing, finding resolution, and restart reconciliation. Wire approval/decision waits without blocking unrelated work.

Exit: a supervisor delegates, receives results, routes independent review, has findings corrected, and reaches a defined output without human continuation prompts. A restart does not lose the assignment.

### 2. Make the workflow usable on desktop and phone

Implement conversational team proposals plus editable configuration forms, a practical org chart/tree, shared activity and decision inbox, assignment detail with delegation drill-down, streaming conversations and tool activity, visible worker steering, artifact views with selected-passage chat, revision history, and workspace review followed by explicit publication.

Use responsive views from the outset. On a phone, details can be stacked screens with breadcrumbs rather than squeezing a desktop graph or split-pane editor into a narrow viewport. Test viewing a result, selecting text, steering, and approving an action on a touch-sized layout. Provide in-app notifications initially; unattended phone push is a separate delivery decision if needed.

Exit: the human can propose a team, assign work, inspect its evidence, discuss a passage, and make a decision from either device size.

### 3. Qualify the first usable release with the Frostyard website

Proposed team: a supervisor, portfolio/repository research specialists, a website implementer, and an independent reviewer from a different model family. Roles remain configurable; these are not built-in Frostyard-specific agent types.

The assignment proceeds as follows:

1. Discover shared standards in `core`, site-local instructions, the accepted design-system ADR, and relevant repository/release evidence. Check for existing work before duplicating it.
2. Inventory website claims and omissions against what ships and is used today. Record sources and unresolved ambiguities. Accepted future plans and unshipped default-branch changes are not current product truth.
3. Assemble a workspace packet proposing content and structural changes. Preserve the existing visual design system. Let the human discuss selected passages and review the proposed scope.
4. Implement the approved direction in a separate branch/worktree. Keep documents and changes connected to their originating evidence.
5. Run the applicable repository/organization checks. Obtain independent factual and implementation review from the required other model family, covering source support, stale claims, content completeness, and adherence to design constraints.
6. Correct findings and repeat the necessary checks/review automatically. Escalate only real decisions, authority changes, or exhausted approaches.
7. On the human's publication instruction, prepare a draft PR. Confirm the final PR revision satisfies all applicable gates, including remote checks that only run after publication. Do not mark the assignment complete while those are pending or failing.

Acceptance: a source-backed draft PR ready for human approval, no unexplained findings, complete traceability, and no repeated “OK, go do that” prompts. Demonstrate a review correction, a worker/service interruption with recovery, and a temporarily unavailable review model without silently substituting its family.

Separately exercise MCP with Brian's TrueNAS connection on a read-only inventory assignment. This verifies the first release's integration requirement without inventing infrastructure mutations for a test.

### 4. Add recurring operations and early usage visibility

Backlog refinement approved after the first real delivery: first add `adc_propose_work` and a shared human Accept/Edit/Decline/Discuss queue for evidence-linked one-off work proposals. Include suggested ownership, scope, dependencies and completion criteria, and detect related proposals/assignments. Acceptance creates work under the accepting human’s selected portfolio and authority; proposing alone grants no execution authority. Extend this proposal model to standing work when scheduling is available.

Add proposed-and-approved scheduled tasks, subscription designation, duplicate avoidance across runs, task priorities, and operational proposals with action/validation/rollback approval. Add token usage views by assignment, model, and account where provider telemetry supports them. Extend provider coverage with Codex; resolve direct Claude integration before promising support. Keep reviewer-family requirements intact across every adapter.

### 5. Add enforced boundaries and independent-installation collaboration

Introduce credential-backed operations scoped to resource and action, enforceable tool grants, execution isolation, and approvals bound to the concrete operation. Test attempted permission expansion through delegation and unmediated credential access before describing these boundaries as enforced. Use this foundation to evaluate cheaper models for routine work.

Add explicit installation pairing and supervisor-only work requests. Track external dependency stubs, correlate retries to avoid duplicate requests, and keep the receiving installation's approvals, execution, and internal history local.

### Later conveniences

Remote execution, Docker/Podman-backed workers and other execution environments; user-facing backup/export/restore; dedicated organization subscriptions; richer external notification delivery as needed. Persist data cleanly from the beginning so deferring a backup UI does not mean treating history as disposable.

## Validation priorities

The important tests are behavioral: completion after review findings; no authority expansion through delegation in ADC's routing; required model-family separation; correct subscription attribution; wakeup after dependency resolution; recovery without duplicate publication; no starvation of review under concurrency limits; two humans issuing conflicting directions; selected-text feedback retaining its revision reference; and shared visibility on desktop and phone.

Initial tests of advisory authority demonstrate configuration and routing behavior only. They must not be presented as proof that a broadly credentialed shell cannot bypass policy. Later enforcement tests must exercise the actual credential and execution boundary.

## Decisions deliberately left for implementation

Exact model IDs and reasoning settings depend on discovered account capabilities. Concurrency and no-progress defaults need tuning against real work. Exact authentication packaging, frontend helper libraries, SQLite driver, and MCP transport compatibility should be selected during the relevant implementation phase. None requires another product-discovery round unless a verified limitation changes the agreed experience.

Build authorization has been granted. Do not repeat the product interview or ask for build permission again. The original filesystem blocker is resolved. Continue implementation and qualification from the current code and handoff checkpoint; do not restart phase 0 or treat the project as empty.

## Execution-plan refinement

Following review of core PR #135, Brian prioritized [the execution-plan backlog](backlog.md): durable reviewed-work dependencies first, then explicit milestones, external/timed waits, repository integration evidence, artifact-bound operational approvals and mediated GitHub delivery. The first slice is described in [execution plans](../design/execution-plans.md). Its scope is ADC functionality; the referenced migration still needs its own implementation and operational authorization.

The EP2 typed milestone slice distinguishes registered reviewed code, merged changes, releases, canary observations and human evidence. Evidence is tied to its step attempt and review revision; changes invalidate downstream gates. External packets are independently reviewed reports, not automatic external verification. Durable observation/polling and timed conditions are the next EP3 slice; structured check results remain in the validation refinements backlog.

EP3 now adds generic read-only MCP observation and timer waits tied to frozen milestone requirements. Persisted leases, version/event predicates, observation periods and deadlines release model slots and recover through fresh reads. Independent review remains required. The next execution-plan priority is EP4, repository and integration provenance; observations are sampled and do not claim uninterrupted health.


### Current continuation — September 11 overnight work

Brian requested completion of EP4–EP6, followed by configurable self-hosted OpenAI-compatible providers and real qualification against the Lemonade server at `http://10.0.1.200:13305/` with `Qwen3.8-27B-MTP-Coding`. Discover its actual API base (`/v1` or `/api/v1`) and catalog rather than assuming them. This is authorized ADC development and connection qualification, not execution of the Frostyard migration. Preserve current real assignment state across restarts. The persistent goal remains active until both parts are complete.

EP4 is implemented and independently reviewed, with passing local/browser checks and a real Codex → Claude workflow. EP5 is implemented, independently reviewed, qualified and deployed. It reuses the access-request bundle and gateway journal, binding a concrete proposed operation to its current plan step/attempt, exact artifact plus validation revision, target/environment, expected effect, validation and rollback. Check the binding both when a human approves and immediately before gateway execution; stale proposals remain inspectable and cannot silently gain a broader scope. The exact operation ID and canonical arguments distinguish one canary from a second publication or credential cutover. Routine authorized checks/corrections should not acquire a new approval ceremony. Reuse the existing UI and membership/CSRF boundary.

EP6 then needs a narrow trusted GitHub integration for authenticated private fetch, exact reviewed-commit push/draft PR and read-only upstream state. Credentials stay outside worker/provider workspaces; operation results require durable reconciliation to prevent duplicate publication. Preflight should expose missing runtime/tool/repository/reviewer prerequisites before spending a model slot. Keep general assignment semantics provider-neutral and label limits honestly.


EP6 implementation (qualified): keep a built-in GitHub adapter behind the same connection catalog, policy/grant admission, redaction and durable operation journal. A small gateway session interface can support both existing MCP sessions and a trusted in-process adapter, with the actual calling run supplied by ADC context rather than model arguments. Seal the connection token; never supply it to worker/provider workspaces. Authenticate repository fetch/push only from an ADC-owned temporary Git repository, using object bundles to cross the protected workspace boundary, and generated ADC-owned branch names. Preserve the reviewed commit object exactly. Refuse dirty/changed or unreviewed source artifacts, unrelated organizations/assignments, unauthorized publication, arbitrary remote URLs and conflicting remote heads. Reconcile existing branches/draft PRs after interruption before attempting publication again. Use fixture Git/HTTP servers for mutation qualification; this work does not authorize publishing fixture PRs to a real GitHub repository.

The official GitHub [pull-request API](https://docs.github.com/en/rest/pulls/pulls) supports a draft flag with explicit head/base. The [Git reference API](https://docs.github.com/en/enterprise-cloud%40latest/rest/git/refs?apiVersion=2026-03-10) documents non-forcing updates. Keep authenticated delivery bounded to reviewed draft proposals; do not expose merge, release publication or deployment through that adapter. Model/tool/repository preflight must be optional declarations for known prerequisites, not a requirement to predict every permission or tool before work can begin. Missing prerequisites should surface as recoverable blockers before dispatch, without a model polling loop.


EP6's protected GitHub backend and optional asynchronous execution preflight are implemented. Fixture delivery/recovery/scope/revocation tests and preflight lifecycle/resource tests pass; the real Codex → Claude readiness workflow passed in 133.81 seconds, and the desktop/phone readiness/GitHub connection browser qualification passed in 3.15 seconds. Independent GitHub review confirmed the credential/authority boundary and identified minor corrections, which are implemented. Final independent preflight/corrective review and full verification are still in progress; the live server still runs the prior EP4/EP5 build. Self-hosted provider work begins after EP6 qualification/deployment.


Final focused independent Claude review returned PASS (185.99 seconds) for the four preflight fixes and the GitHub delivery corrections. The full final verification passed vet/race/build (32.590-second race suite); the additional four-probe concurrency regression passed with the race detector. EP6 is ready for an idle-service restart. No real GitHub publication occurred. Next: discover and qualify the requested Lemonade endpoint and add the configurable self-hosted provider.


EP6 deployed after the final full `make verify` (32.563-second race suite). The service had zero unfinished real runs, stopped gracefully and restarted through `make serve`. Login responds, the running executable matches the verified binary, and no GitHub/preflight/resource fixture records exist in the live database. Self-hosted provider integration is now the remaining active goal.

Self-hosted implementation checkpoint: both requested Lemonade API bases respond, `/v1` selected; Qwen function-call probe passed in 5.6 seconds. Added personal endpoint/key/capacity configuration, catalog/family discovery, a native Go Chat Completions tool loop, existing protected tools, durable outcomes/traces/usage and cancellation/steering boundaries. Initial unit and browser qualification passes. A full local supervisor prompt timed out at three minutes without ADC effects; requests now allow ten minutes, and the running isolated qualification pairs a Claude coordinator/reviewer with Qwen workers. Independent Claude code review is running. The live service remains the verified EP6 build; self-hosted changes are not deployed yet.


### Completed continuation — September 12

EP1–EP6 and configurable self-hosted provider support are implemented, independently reviewed, verified and deployed. The corrected real Lemonade Qwen → independent Claude two-step document workflow completed with no failed tool calls or human continuation; all expected readiness and usage checks passed. The saved qualification report survived the desktop interruption. Full `make verify` passed (33.221-second race suite), corrective independent review returned PASS, and the final connection browser workflow passed.

The idle service restarted through `make serve` as PID 584807; its executable matches the verified build and authentication smoke checks pass. Brian's personal Lemonade connection now uses `http://10.0.1.200:13305/v1`, exposes `Qwen3.8-27B-MTP-Coding`, and allows one concurrent run. Permanent roles are unchanged. The overnight objective is complete. Brian subsequently authorized committing and pushing this implementation. The two-account funding limit and broader local-model workload qualification remain documented follow-ups, not unfinished parts of this slice.
