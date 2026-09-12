# Aide de Camp

A self-hosted workspace for teams of agents that carry an assignment through research, implementation, independent review, and correction. Humans review evidence and make decisions; routine continuation belongs to the supervisor.

ADC is being built for Frostyard engineering and a personal homelab. The [approved plan](docs/plans/build.md) describes the complete vision and delivery sequence. The current build is an initial working implementation, not the completed roadmap.

Protected execution is available for new Linux assignments: isolated commands with ordinary outbound networking, a credentialed MCP gateway, scoped grants and bundled human approvals. Configure it under **Connections → Permissions**. Existing work remains advisory until explicitly selected otherwise. See [installation and permission boundaries](docs/design/permissions.md), including the Incus nesting prerequisite and current authenticated Git publication limit.

## Run locally

Requirements: Go 1.26 or newer, Git, and an authenticated GitHub Copilot CLI. This build has been exercised with Go 1.27.1, Copilot Go SDK 1.0.8, and Copilot CLI 1.0.83. Direct Claude connections require Node and the pinned official Agent SDK runtime (`npm ci --prefix runtime/claude --ignore-scripts`); see [provider setup](docs/design/providers.md). Codex connections additionally require the Codex CLI; the adapter protocol was checked against version 0.153.0. Set `ADC_CODEX_CLI_PATH` if it is not on PATH. Repository-specific build tools and MCP server processes are additional installation dependencies.

```sh
make run     # Check formatting, vet, race-test, build, then start the server.
# Or start an already-built binary:
make serve
```

Both startup targets use the same provider runtime environment and default to `127.0.0.1:8789` with the project's `.adc` directory. Stop the foreground server with Ctrl-C. `make run` replaces the previous `go run` shortcut with the verified-binary workflow.

Override `GO`, `GOFMT`, `COPILOT_CLI_PATH`, `ADC_CLAUDE_NODE`, `ADC_CLAUDE_RUNTIME_DIR`, `ADC_ADDR` or `ADC_DATA` in an optional, Git-ignored `Makefile.local`, or on the command line—for example, `make serve ADC_ADDR=127.0.0.1:8790`. This development checkout's `Makefile.local` contains the same qualified executable paths used during development. Other installations default to executables on PATH and the bundled `runtime/claude` directory.

Open http://127.0.0.1:8789/. Create your application account and organization. In **Connections**, connect your personal Copilot subscription. The initial owner may explicitly use the CLI identity already signed in on the machine; other members connect their own access tokens. Available models are discovered from the connected subscription.

In **Team**, choose **Propose a team**, describe what needs looking after, and select a model. ADC creates a temporary team designer and presents a proposal under Work. Permanent roles are created when you approve the proposal. You can also create or amend agents with forms, choose category defaults for temporary workers, and assign MCP access.

Assign an outcome to an accountable agent. The team delegates work, saves documents, obtains cross-family review, and returns findings for correction. Select a document passage to discuss it; the revision selector preserves access to earlier drafts. Human steering and decisions are shared within the organization. External PR publication requires a separate explicit decision.

In **Who’s on it**, choose **View transcript** to follow one agent’s recorded messages and tool activity live. **Team** cards also link to each active run, including concurrent instances of the same agent. Older transcript history stays available.

For multi-step assignments, the supervisor can save an **Execution plan** with named steps, owners, independent reviewers and prerequisites. Steps can require reviewed code, merge/release/canary observations or human evidence. ADC starts eligible steps automatically after their dependencies meet those requirements and pass review; the assignment page shows progress and blocker reasons with transcript links. Read-only MCP observations and timers can wait durably without occupying agent slots, then resume for independent review. Draft plans can be discussed before starting. Activation uses the assignment’s existing authority and subscriptions. See [execution plans](docs/design/execution-plans.md) for the workflow and its limits.

Open **Proposals** to review agent-suggested follow-ups. Edit or discuss a proposal before accepting it; acceptance asks for your subscription, accountable agent, authority and exact scope. **Ask for proposals** uses existing document evidence without executing the suggestions. For recurring proposals, choose an interval, daily time or weekly time: approval activates the schedule without starting an immediate run. **Schedules** shows its next occurrence, funding human, scope and history, with pause/resume controls. See [work proposals](docs/design/work-proposals.md) and [standing work](docs/design/standing-work.md).

Open **Usage** for reported input/output and cache tokens by model, subscription and assignment. Choose a rolling time window or all history, then open an assignment for its agent-run breakdown. Each assignment also has a **Token usage** link. Missing counts stay unknown, and partial totals are identified. See [usage accounting](docs/design/usage.md).

To add Codex, open **Connections → + Codex**, create your personal connection, and complete ChatGPT device-code sign-in. Choose **Codex** on the roles that should use it. For work with Codex execution and Copilot/Claude review, select both subscriptions when creating or accepting the assignment. Both must belong to the same human. Existing roles remain on Copilot. Real Codex supervision/execution with independent Copilot/Claude review has passed; see [provider integration](docs/design/providers.md) for qualification status and remaining limits.

To add direct Claude, open **Connections → + Claude**, create your personal connection, and run the displayed native Claude Code sign-in command on the ADC host. Then choose **Check sign-in**. Real Codex execution with direct Claude review, stdio MCP and local Git validation has passed.

## Current behavior

- Go application with embedded web assets, server-rendered HTML, Datastar updates, and SQLite.
- Organizations, equal-authority members, personal subscription attribution, and per-account concurrency limits.
- Permanent agent definitions, temporary runs, reporting tree, category defaults, and conversational team proposals.
- Copilot execution, a Codex app-server adapter and a direct Claude Agent SDK adapter, provider-specific model discovery, GPT/Claude family separation, shell/CLI execution and stdio/HTTP MCP configuration. Codex protocol, simulated lifecycle checks and real Codex-to-Copilot/Claude completion pass, including stdio MCP and committed changes in an isolated local Git repository.
- Persisted assignments, delegation, review corrections, supervisor reassessment, pause/cancel, and recovery after an interrupted activation.
- ADC-native collaboration messages between active runs, inspectable assignment status, and provider turns that end after durable handoff. Provider-native agent tools are excluded to avoid confusing their IDs with ADC workers.
- Workers can schedule independent review before finishing; reviewers wait for completed evidence and remain accountable to the supervisor. Late collaboration reports completion without reopening work, while explicit human steering remains actionable. Assignment status lists missing reviews, including publication evidence.
- Versioned Markdown documents, source references, a live decision inbox/work overview, paginated activity, inspectable tool evidence, visible steering, and desktop/phone review flows.
- Personal connection editing, visible organization membership, and self-service password changes that revoke other sessions.
- Shared, versioned work proposals with evidence links, live discussion, related-work hints and atomic human acceptance into a scoped assignment.
- Approved standing work with explicit cadence/timezone, a designated personal subscription, persisted occurrences, overlap prevention and pause/resume.
- Organization-scoped usage views with historical telemetry, live updates, time windows, assignment/run drilldown and explicit unknown/partial counts.
- Registered code artifacts tied to clean commits and rechecked before review acceptance and task completion.
- Repository/integration ledgers with exact consumed commits, environment pins and required artifact-bound validation, including observed protected commands and stale-check gating.
- Concrete operational approvals for protected execution plans, bound to step/attempt and reviewed evidence; stale proposals recover without widening access.
- Optional preflight checks for runtimes, repository access, worker/reviewer models and isolated test resources, with automatic recovery before model dispatch.
- Scoped GitHub connections for credential-free private fetch into protected workers and exact reviewed-commit draft delivery, with interruption reconciliation; mutation qualification uses isolated Git/HTTP fixtures.
- Self-hosted OpenAI-compatible model connections with optional sealed API keys, catalog discovery, function calling, usage and protected workspace/MCP tools.

The first real Frostyard website assignment delivered two reviewed draft PRs, subsequently merged by Brian. The bounded read-only TrueNAS inventory also completed with independent verification. Those qualifications do not complete the remaining roadmap. See [qualification and limitations](docs/testing/qualification.md).

## Data and deployment

The default data directory is `.adc`. It contains SQLite state, a credential encryption key, provider state, and isolated run workspaces. Keep the directory private. Application passwords are hashed; provider tokens and configured MCP secret values are encrypted at rest. The encryption key must accompany the database when restoring an installation. Codex and Claude manage their own sign-in files under each account’s private `providers/<provider>/<account-id>` directory; these files are not encrypted by ADC’s credential key. Protect and back up that directory with the rest of the installation.

Each service owns its data directory exclusively. For an Incus/systemd installation, see [deployment notes](docs/deployment/incus.md). Advisory assignments retain broad host shell access. New assignments can instead select protected Linux execution with isolated commands and credential mediation; see [the permission guide](docs/design/permissions.md). The service unit alone does not establish that boundary.

## Development and checks

```sh
make verify
# Optional: uses the signed-in Copilot subscription on synthetic content.
ADC_LIVE_TEST=1 go test ./internal/adc -run TestLiveSupervisorCompletion -v
```

`make verify` formats-checks, vets, runs the race-enabled test suite, and builds the executable. The opt-in live test proves supervisor delegation and independent review through the actual provider. Browser checks use separate temporary fixture databases; they do not populate the working installation.

Federation, further permission controls, mediated private Git/publication, remote execution, and backup/export UI remain on the roadmap.

Embedded Datastar JavaScript is version 1.0.2; its license is preserved in [docs/licenses](docs/licenses/datastar-LICENSE.md). Go dependencies and versions are recorded in `go.mod` and `go.sum`.

## License

MIT — see [LICENSE](LICENSE). Third-party components retain their own licenses.
