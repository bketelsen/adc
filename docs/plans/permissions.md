# Permissions and credential boundaries

Status: implementation authorized and the first protected execution profile is implemented, 2026-09-11. Linux sandbox, provider tool controls, generic stdio/HTTP gateway, versioned grants and bundled permission decisions are implemented. Qualification and rollout details are in [the operating guide](../design/permissions.md) and [qualification](../testing/qualification.md). Existing organizations and approved schedules retain their settings; no automatic migration to protected execution has occurred. This extends phase 5 of `build.md`.

The architecture and gates below remain the target. Remaining work includes mediated private Git fetch/push/publication, expiry and per-role tool rules beyond existing connection ceilings, request-friction metrics, aggregate resource quotas, and cancellation/reconciliation of in-flight remote effects. Current revocation prevents new dispatches; an admitted call can finish. Protected mode never falls back to advisory. The local Git/review workflow and two real NAS observation tools have passed; this does not establish protected real GitHub publication or a complete protected NAS investigation.

## Intended result

An authorized assignment completes research, execution, independent review, corrections and validation without routine permission prompts. Workers can use shell commands and ordinary outbound networking, but cannot reach ADC's credentials, database, other humans' accounts or unrelated workspaces. Authenticated external operations pass through grants checked by ADC.

Adding an MCP server normally requires configuration, not integration code. Permissions need to be established before use, not exhaustively predicted before an assignment starts. Missing capabilities can be requested during execution. Supervisors bundle requests where possible; unaffected work continues. Human approvals may cover one operation, the assignment, or explicitly granted standing access. Delegation cannot enlarge authority. Approval and outcome records survive restarts and correction cycles.

This plan does not introduce host-maintenance coordination, deployment-topology rules, remote executors or network destination allowlists. Those are deferred. It does not authorize infrastructure mutations or change current role and schedule grants.

## Original advisory baseline

For advisory runs, `engine.go` decrypts connection environment variables and headers and supplies credentialed MCP configuration to provider runtimes. Copilot approves native permission requests. `codex_run.go` starts ordinary runs with full host access; the Claude bridge enables native shell and file tools. Provider authentication directories are separate per account, but share the host with worker execution. `connection_access.go` and delegation already enforce connection membership and authority in ADC routing; they do not prevent a shell bypass.

The new design separates trusted provider/connection processes from untrusted workspace execution. Merely hiding tools, filtering environment variables or placing the whole installation in a container is insufficient.

## Architecture

### 1. Trusted ADC control and connection services

ADC owns policy, approvals, provider authentication and MCP connection credentials. Provider SDK processes remain trusted components because they need subscription authentication. Model-controlled commands and file access must not execute in those processes' host environment.

The connection service owns stdio MCP subprocesses and authenticated HTTP sessions. A provider receives only a filtered tool catalog and ADC-controlled invocation handlers, not connection launch configuration or secrets. No privileged gateway endpoint or bearer token is placed in a workspace. Caller organization, account, assignment and run are bound by ADC's authenticated dispatch context, never accepted from model-supplied arguments.

MCP server binaries are trusted integrations selected by the installation administrator. Their credentials should be service-scoped where possible. Launch them with an explicit environment and only the required connection secrets. Redact known secret values from evidence. A generic gateway cannot guarantee that an authorized server never returns sensitive data; classification and returned data remain part of the integration's trust boundary.

The gateway checks every call, even when it was present in the catalog. It validates arguments against the discovered schema and applicable grant, then forwards the original structured arguments. Tool descriptions and annotations inform suggested classification; they do not grant access. Tool names, schemas and policy-relevant metadata are fingerprinted. New or materially changed tools require classification, without invalidating unrelated unchanged tools.

Start with tool invocation. Other MCP surfaces, including resources, prompts, sampling and elicitation, must not become accidental paths around grants: explicitly reject unsupported surfaces and advertise only implemented capabilities. Support stdio and Streamable HTTP with authentication, cancellation, bounded results and explicit connection failure handling. Legacy transports remain visibly unsupported until qualified.

### 2. Workspace executor

Provide ADC-owned command and file tools whose implementations enter the same isolated environment. Providers' native host shell, file modification/read, collaboration, plugin and config-discovery routes must be disabled or redirected. An unavailable provider control is a release blocker for protected execution, not a reason to silently retain host access.

First Linux candidate: Bubblewrap with an ADC-owned mount, process and environment policy. It is already installed on this development host. The initial proof must also run under an ordinary unprivileged Incus deployment before claiming that deployment supported. Bubblewrap provides mechanisms rather than a complete security policy; profile construction and adversarial qualification are ADC's responsibility. See the [upstream security and usage documentation](https://github.com/containers/bubblewrap).

Expose the current run's workspace, private temporary/home/cache directories, and explicitly selected read-only runtime/toolchain paths. Do not mount the host home, ADC state, provider homes, agent sockets, container-management sockets or unrelated workspaces. Use isolated process visibility, minimal devices, no inherited privileged descriptors, no elevation and a sanitized allowlisted environment. Resolve mounted toolchains and symlink targets deliberately; never expose an entire personal tool directory merely because one executable lives there.

Allow ordinary outbound network access and dependency downloads without per-command approval. Dependencies install into writable workspace-local locations rather than modifying host packages. Keep ADC's control endpoints authenticated even on loopback, and verify that shell HTTP calls cannot impersonate a human or trusted provider. Ordinary networking may reach LAN services; this phase does not enforce destination policy or prevent exfiltration of information legitimately available to a worker.

Repositories and their build scripts are untrusted execution inputs. Git hooks, repository config, evidence collection, archive extraction and artifact inspection must not execute repository-controlled commands in ADC's trusted environment. Use constrained file operations or the executor. Cross-run review receives a pinned artifact snapshot in the reviewer's own workspace, not writable access to the author's checkout. Use private or safely constrained caches to prevent cross-run contamination.

Long-running commands need tracked process handles, cancellation, output limits and cleanup of descendants. Bound resource use enough to avoid trivial runaway processes or disk output taking down ADC; document the remaining shared-kernel and availability limits. If the isolation prerequisite is absent, protected execution fails closed with a setup diagnostic.

### 3. Deterministic grants and approvals

Introduce versioned policy records without replacing the existing durable assignment model:

- A discovered tool record binds organization, connection identity/configuration revision, name, schema and metadata fingerprint, classification and human review history.
- A grant binds those identities to a role or assignment, allowed operation, optional argument constraints, delegation rights, expiry and originating approval. An explicit deny takes precedence. Unknown tools are not allowed by default.
- Effective access is the intersection of installation policy, assignment ceiling and delegated access. A recipient's broader standing authority cannot enlarge an incoming assignment. Temporary workers can inherit a permitted subset rather than requiring permanent role changes.
- A permission request contains purpose, proposed capability bundle, target constraints, duration, affected work and originating runs. Equivalent requests within the same assignment coalesce. A human edit creates a new revision; accepting a stale revision is rejected.
- An invocation record binds the effective grant revision, tool fingerprint, canonical arguments, result and outcome state. Single-operation approval includes a durable operation identity; assignment grants allow subsequent matching calls.

Initial optional argument restrictions are a small declarative vocabulary: exact values or finite allowed values at specified JSON paths. Missing constrained arguments fail validation. Complex semantic interpretation, arbitrary scripts and general API-request tools are broad capabilities and must be shown as such. Do not imply that argument matching proves a tool's real-world effect or makes arbitrary code safe. Custom semantic adapters remain optional future enhancements.

Approval changes persist atomically and wake affected work without changing its selected funding portfolio. Revocation prevents new dispatches immediately and cancels in-flight work where possible; it cannot undo completed operations. Standing schedule ceilings remain stable unless explicitly revised. Revoked or replaced connections do not inherit grants through a reused display name.

Persist an operation intent before a side-effecting call. After an uncertain result, reconcile through a supported status/idempotency mechanism or report uncertainty; never blindly replay a mutation. Approval persistence does not mean an approved mutation should execute twice. Read retries are allowed only under the configured classification, not a name-based guess.

### 4. Low-friction human experience

Connection setup discovers tools and lets ADC suggest groups such as observation, changes and broad execution. A human reviews and can approve a group once; new tool discovery does not require custom application code. Forms support later edits. Broad tools receive an honest description of their scope.

The supervisor consolidates unexpected needs into an existing shared decision queue, explaining what each bundle enables. Approval choices are this operation, this assignment or explicit standing access. No automatic approval follows from a timeout, another agent's message or repeated failure. Decisions are organization-visible and preserve the existing equal-human-authority and conflicting-instruction behavior.

Record requests per assignment, repeated equivalent requests, wait time, and changes that could eliminate repetition. Agents may propose standing access adjustments for human review, never broaden access automatically. Avoid treating ordinary dependency installation, review or correction as new authority.

## Delivery sequence and gates

1. **Prove execution and provider compatibility.** Add an isolated executor prototype and adversarial fixtures using synthetic secrets. Demonstrate useful Git/build/test commands and outbound access while denying host files, process environments, sibling workspaces and privileged sockets. For Copilot, Codex and Claude, demonstrate that only ADC-mediated execution/file tools are usable. Exercise ADC HTTP endpoints from the worker network. Qualify the profile in an unprivileged Incus instance when one is available. Stop and report a concrete compatibility constraint if any provider cannot meet the boundary.
2. **Build the generic connection gateway.** Move credentialed MCP launch/session ownership out of provider run configuration. Implement discovery, invocation validation, cancellation and revision-aware per-tool checks for stdio and HTTP. Use a synthetic second server to prove that connecting a new tool requires configuration only. Keep existing connections usable in explicitly advisory mode during development; do not advertise partial work as protected.
3. **Add grants and permission decisions.** Persist policy and invocation records, narrow delegation, coalesce requests, and implement one-operation/assignment/standing approvals plus revocation and recovery. Extend connection setup, role configuration and the shared decision queue. Retain current role/account/schedule records and preserve traceability to the human approval.
4. **Close the authenticated repository workflow.** Protected workers do not receive personal GitHub credentials. Use approved MCP tools for external repository operations. Where an existing server needs an unrestricted token or arbitrary API tool, expose that limitation; do not claim draft-only enforcement. Public cloning and local commits stay ordinary workspace operations. Private fetch/push must use a scoped mediated operation or remain unavailable in protected mode until supported; do not quietly copy a token into Git configuration. Validate with a local synthetic authenticated endpoint before requesting any new real publication.
5. **Qualify completion and roll out.** Re-run actual cross-family document and local Git/MCP qualification, then the existing read-only TrueNAS investigation under reviewed observation grants. Use a disposable external repository only if authorized. Prove review corrections and restarts need no extra approvals. Migrate existing connection classifications through a concrete human-reviewed configuration, not inferred permission expansion. Activate protected mode only for qualified providers/workflows, while showing remaining advisory runs explicitly. Never automatically fall back to advisory on failure. Retain an explicit rollback path for configuration; rollback must not silently downgrade active protected work.

## Acceptance evidence

Required negative tests cover cross-human and cross-organization access, delegated expansion, forged run identity, stale approval, tool schema/configuration changes, malformed/missing constrained arguments, duplicate requests, restart replay, revocation, secret exposure through filesystem/environment/processes, symlink/path traversal, repository hooks and direct gateway/control API calls. Test both stdio and HTTP credential handling and failure paths with synthetic secrets.

Required positive tests show ordinary network research, dependency installation, builds, independent artifact review and correction complete under one assignment approval. A missing capability produces one consolidated decision, survives restart and resumes exactly the affected work on approval. A second unfamiliar MCP server works without ADC source changes. New unclassified tools remain blocked while previously approved unchanged tools continue working.

Run `make verify` for implementation changes, browser checks for setup/approval/mobile flows, and real provider qualifications in isolated fixture state. Only then describe the tested capabilities as enforced. Keep unqualified providers, transports, deployment profiles and broad tools visible as limitations.

## Sources and decisions

Product scope comes from the permissions discussion in this task: no exhaustive permission inventory required before execution; configuration over per-server code; infrequent bundled approvals; ordinary outbound access; deployment-specific host maintenance deferred. Repository evidence is in the implementation files cited above and existing provider/standing-work design documents.

MCP discovery, schemas and tool annotations follow the [official tools specification](https://modelcontextprotocol.io/specification/2025-11-25/server/tools). ADC's grant vocabulary, approval lifecycle and executor/service split are proposed architecture, not capabilities supplied automatically by MCP or Bubblewrap.
