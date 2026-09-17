# Aide de Camp

Aide de Camp (ADC) is a self-hosted workspace for a small team of persistent agents. You give an agent an outcome; it does the work, delegates when that helps, keeps what it learns, and comes back to you with concrete evidence and the decisions only you can make. It runs on your own machine against your own model subscriptions, with SQLite for state and a plain server-rendered web UI.

It is built for a small shop: one person or a handful, a homelab, a Linux distro, a few repositories. The defaults favour getting work done and remembering it over process.

## Stewards

A steward is a permanent agent that owns a domain: Storage, Packaging, Hosting. It keeps a **charter** (what you asked it to look after), a set of named **facts** with sources ("pool layout", "backup target", "disk 3 replaced 2026-08-02"), an append-only **journal** of what happened and what it found, and **routines** that run on a cadence. When something needs you, it raises a **signal** that sits on the home page until you acknowledge, snooze or resolve it: the NVMe pool is at 50%, last night's replication failed, a decision is coming up.

You do not fill in a form to create one. Describe what needs looking after; a designer asks what you left open, then proposes the agent, its charter, connections, repositories and routines as one decision. Approve it and everything exists. Work assigned to a steward carries its charter, facts and completion default, and a steward's memory travels with it on every run. See [stewards](docs/design/stewards.md).

## Assignments

An assignment is an outcome handed to an agent: title, what should be true when it is done, who handles it. The agent carries it through research, implementation and correction without asking permission to continue. It can delegate bounded pieces to other agents, message them, wait for them without holding a slot, and recover after an interruption. You watch a live transcript of any run, steer with a message, and answer decisions in plain language: "looks good" approves, "no" rejects, anything with a condition goes back as notes.

Completion is **routine** by default: the agent finishes on concrete evidence (registered commits, saved documents, observed results) and you review the outcome, as you would a colleague's pull request. Choose **reviewed** on an assignment or a steward when every artifact should get a mandatory review from a different model family. Draft PR delivery always gets one independent review of the exact commit first, under either policy. See [completion policy](docs/design/completion-policy.md).

Documents are versioned Markdown with source references; code artifacts are tied to clean commits and rechecked before anything is delivered. Every message, decision and tool call is recorded and visible to everyone in the organization.

## Execution plans

For multi-step work the accountable agent can save a plan: named steps with owners, dependencies, optional reviewers, prerequisite checks and external milestones such as a merged PR or a released artifact. ADC dispatches steps as their dependencies complete, re-runs a stale check instead of blocking on it, and redoes finished work only when a prerequisite's actual output changed. Read-only observations and timers wait durably without occupying an agent. See [execution plans](docs/design/execution-plans.md).

## Proposals and standing work

Agents suggest follow-up work as proposals rather than doing it unasked. You edit, discuss, accept or decline them; acceptance creates a scoped assignment. A proposal with a cadence becomes standing work: an interval, a daily time or a weekly time, running as its own assignment each time with no overlap and a pause switch. Steward routines are standing work owned by the steward. See [work proposals](docs/design/work-proposals.md) and [standing work](docs/design/standing-work.md).

## Providers and connections

Agents run on your personal subscriptions: GitHub Copilot, Codex (ChatGPT), Claude, or a self-hosted OpenAI-compatible endpoint. Each agent has an explicit provider and model; available models are discovered from the connected subscription, and reviews use a different model family from the work they review. MCP connections (stdio or HTTP) give agents tools such as a NAS API or a GitHub scope, granted per agent. See [providers](docs/design/providers.md).

Assignments run **advisory** by default, with the host's shell and the agent's granted connections. **Protected** execution runs commands in an isolated Linux workspace with ordinary outbound networking, reaches MCP tools through a credentialed gateway with human-classified tool policies, and bundles missing capabilities into one approval. See [permissions](docs/design/permissions.md) for the boundary and its Incus prerequisite.

## Usage

The Usage page reports input, output and cache tokens by model, subscription and assignment over a chosen window, with a per-run breakdown on each assignment. Unknown counts stay unknown. See [usage accounting](docs/design/usage.md).

## Getting started

Requirements: Go 1.26 or newer, Git, and an authenticated GitHub Copilot CLI. Claude connections additionally need Node and the pinned Agent SDK runtime (`npm ci --prefix runtime/claude --ignore-scripts`); Codex connections need the Codex CLI (`ADC_CODEX_CLI_PATH` if it is not on `PATH`). Build tools for the repositories your agents work on, and any MCP servers, are installed separately.

```sh
make run     # gofmt check, vet, race tests, build, then serve in the foreground
make serve   # serve an already-built binary
make start   # build, install and enable the persistent adc systemd user service
```

Foreground serving listens on `127.0.0.1:8789` by default; the installed user service listens on `0.0.0.0:8789`. Both keep data in `./.adc`. Override `ADC_ADDR`, `ADC_DATA`, `GO`, `GOFMT`, `COPILOT_CLI_PATH`, `ADC_CLAUDE_NODE` or `ADC_CLAUDE_RUNTIME_DIR` for foreground commands on the command line or in a Git-ignored `Makefile.local`. The user unit uses the qualified runtime paths in `deploy/adc.service`; `make status`, `make logs` and `make stop` manage it. After an update, use `make stop && make start`. Binding all interfaces exposes ADC to reachable networks, so restrict access at the host or network boundary.

Then, in the browser:

1. Create your account and organization.
2. Under **Connections**, connect a subscription. The initial owner can use the Copilot CLI identity already signed in on the machine.
3. Under **Team**, describe your world and approve the proposed agents, or under **Stewards**, ask for your first steward.
4. Assign an outcome from the home page and watch it run.

## Data and deployment

`.adc` holds the SQLite database, the credential encryption key, provider sign-in state and isolated run workspaces. Keep it private and back it up together; the key must travel with the database. Passwords are hashed and provider tokens and MCP secrets are encrypted at rest. Codex and Claude keep their own sign-in files under `providers/<provider>/<account-id>`, which ADC does not encrypt.

For a longer-lived installation on Incus with systemd, see [deployment notes](docs/deployment/incus.md). Federation, remote execution and a backup/export UI are not built.

## Development

```sh
make verify
```

`make verify` runs gofmt, vet, the race-enabled test suite and a build; it is the gate before handing off a change. Opt-in live and browser tests exist behind environment variables and use temporary fixture databases; see [qualification](docs/testing/qualification.md) for what has been exercised against real providers and what has not. Design notes live under [docs/design](docs/design).

## License

MIT, see [LICENSE](LICENSE). The embedded Datastar 1.0.2 keeps its own license under [docs/licenses](docs/licenses/datastar-LICENSE.md); Go dependencies are recorded in `go.mod` and `go.sum`.
