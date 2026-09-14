# Contributed development

P6 restores outside help without making outside agents members of the organization. The first version accepts bounded UTF-8 text source snapshots and replacement files over HTTP. It does not install an agent client, broker contributor credentials, accept archives, fetch arbitrary candidate URLs, or accept an external review as an internal verdict.

## Public scope and accountable ownership

A human prepares a queue at `/contributions`: responsible area, deliberately public scope, public HTTPS source, an internal reviewer, one subscription from that human's portfolio and a total evaluation budget. The reviewer must be from a different model family than the permanent owner. Up to eight queues per organization are supported initially. Queues also pin an optional installation-provided validation runtime. A queue cannot silently change its source, owner, runtime or funding; pausing revokes it. Editing/top-up and archived queue management remain follow-through.

The owner uses `adc_offer_contribution` from protected work to supply explicitly public outcome, criteria, source revision and files. This is a deliberate disclosure under the approved scope, not an export of the private assignment or owner memory. Known configured secrets are screened. The source URL and revision are owner assertions; ADC hashes the actual offered bytes but does not prove those bytes came from that public URL. Scope compliance and semantic confidentiality still depend on the authorized owner. No complete secret detector or automatic source-attestation claim is made.

Each queue allows 20 retained packets, 200 claims per rolling hour and its human-selected 1–20 total internal evaluations. Each packet allows 30 claims per rolling hour; release adds a five-second cooldown. Rate limits recover automatically rather than permanently exhausting the queue. An abandoned claim expires after one hour; claiming and polling use no inference. A receipt grants only submission/release/status for that claim, never membership or tools. Changed/replayed submissions cannot consume another evaluation under the same receipt. Accepted submissions reserve budget before review begins. Quota exhaustion is visible; it cannot silently mint new funding. These caps bound abuse costs, not anonymous-service availability against denial of service.

An owner may call `adc_wait_contribution` once per packet. The run releases its slot and resumes for admission, rejection, blocking or the one-hour deadline. Expiry closes that packet and returns the owner to internal work. Original task cancellation/pause/completion and queue revocation stop further intake. A wait timeout does not cancel a candidate already received or under review. Pausing public intake does not revoke an already admitted result's protected internal import. Owner identity and responsibility remain unchanged.

## Internal admission boundary

Admission runs use the selected existing subscription and normal durable run/account concurrency. There is at most one admission per queue, at most four model activations and six validation commands per candidate, a three-minute activation timeout and a fifteen-minute evaluation deadline. Persistent spent counters survive restart; stopped evaluations return to the owner without another human “continue” decision.

The review provider receives only the public packet and candidate. It has two tools: `adc_candidate_check` and `adc_admission`. Native host tools, MCP, delegation, owner memory, organization catalogs and publication tools are absent. Reported contributor model identity, source, summaries, programs and command output are untrusted evidence. A PASS requires an internally owned verdict and at least one successful, nontruncated validation command. Relevance and sufficiency of validation remain the reviewer's judgment; a successful `true` alone is not a mechanical proof of the acceptance criteria.

Candidate commands run in a separate Linux profile, not the ordinary outbound-enabled protected workspace:

- A systemd user service bounds aggregate memory (512 MiB, no swap), processes (64), CPU (one core) and elapsed time. Cancellation stops the whole service cgroup.
- Bubblewrap removes network access, host homes, ADC state, sockets, environment secrets and other workspaces. `/usr` is read-only; `/proc` is namespaced. Nested user namespaces are disabled.
- A read-only supplied source is copied into a disposable workspace for each command. Workspace and temporary/home filesystems have size limits. Generated hooks, poisoned homes and child processes cannot persist into the next command.
- No dependencies are downloaded during validation. Basic queues have distribution tools such as Python and Git. An optional curated Go runtime supplies a compiler and explicitly selected public modules, mounted read-only after its contents are verified against the queue pin. Unavailable dependencies do not enable network access or an advisory fallback.

This requires Linux, bubblewrap and a functioning systemd user manager with resource controllers. Installations in Incus must qualify that environment before enabling intake. This is process/namespace isolation with a shared host kernel, not a separate VM or a claim of protection against kernel vulnerabilities. Ordinary protected work retains its separately documented outbound network behavior.

Text sources have at most 256 files; each file is at most 1 MiB, total source at most 4 MiB, and the offered public JSON packet at most 1 MiB. Candidate changes are at most 1 MiB; the combined import payload must fit 2 MiB. Paths cannot traverse, overlap files/directories, contain Git metadata or create symlinks. No archive extraction, submitted fetch URL, executable file mode or Git filter configuration is accepted.

After admission, `adc_import_contribution` materializes a fresh source directory inside the accountable owner's protected workspace. It neither overwrites a checkout nor executes candidate programs. The owner still owes integration against current ground truth, normal code/artifact review and authorized delivery. Admission never changes original-task completion, human intent, support status, Git merge or publication authority. Reviewed code is not a mathematical guarantee of benign behavior; subsequent work stays under its existing execution and approval policy.

## Curated Go runtime

An installation may prepare a directory containing `go/` (the chosen Go distribution) and `mod/` (only the public modules needed by its work). Do not point this at a home directory, ADC data directory, or an entire developer module cache: every byte in this tree is readable by candidate programs. Include the distributions' licenses. Symlinks and special files are rejected; a runtime is bounded to 40,000 entries and 1 GiB.

Run `./bin/adc contribution-runtime -source /absolute/curated-runtime` to validate and print its content digest. Set `ADC_CONTRIBUTION_RUNTIME` to that directory and `ADC_CONTRIBUTION_RUNTIME_ID` to the returned digest in the service environment (or `Makefile.local`). The queue form offers the installed runtime; choosing it pins the digest into the public packet. Existing basic queues retain basic tools. Withdrawing the configured ID or directory disables intake for affected queues before another evaluation can be reserved.

Every validation command copies and hashes the actual runtime bytes into a private temporary snapshot before mounting it read-only at `/runtime`. Content drift fails closed. Cheap intake checks compare configured identity and directory availability; they do not repeatedly hash the tree for anonymous requests. An in-place content change can therefore be detected later at validation, so administrators should provision a new runtime and new queue rather than mutate an active runtime.

Go uses an ephemeral cache, `GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local`, and no ambient Go configuration/workspace. This profile raises memory to 1 GiB and `/tmp` to 256 MiB, retaining the 256 MiB workspace, 64-process cap, one-core CPU bound and 120-second command maximum. Dependencies must be curated and verified by the installation before use; disabling online checksum lookup inside the sandbox is not a claim that arbitrary cached modules are trusted. The queue/runtime identity is a content pin, not a signature or a proof of safe tools.

The Updex pilot uses Go 1.26.7 and `github.com/hashicorp/go-version v1.9.0`. Its package-only admission does not stand in for the repository's full CI gate at integration.

## HTTP protocol, version 1

The installation must explicitly start `adc serve -public-contributions` after qualification and approval of its real public scope (`make serve ADC_PUBLIC_CONTRIBUTIONS=true` provides the same setting). The default server returns 404 for every `/public/` path. Preparing a queue does not enable the listener.

| Request | Result |
| --- | --- |
| `GET /public/queues/{queue}` | Bounded available work metadata and protocol instructions; no private organization fields |
| `GET /public/packets/{packet}` | Explicit public packet with source files and revision digest |
| `POST /public/packets/{packet}/claim` with `{}` | Automatically issued `Receipt`, one-hour `Expires`, public `Packet` |
| `POST /public/packets/{packet}/release` with `{}` | Releases that receipt's unsubmitted claim |
| `POST /public/packets/{packet}/submit` | `{ "Revision": "packet digest", "Summary": "result", "Model": "unverified provenance", "Files": { "path": "replacement text", "obsolete-path": null } }` |
| `GET /public/submissions/{submission}` | Receipt-bound state/verdict; internal review findings and private run IDs are withheld |

POSTs require `application/json`. Release, submit and submission status use `Authorization: Bearer <automatically returned Receipt>`. This is client-managed claim bookkeeping, not a manually provisioned ADC token. JSON bodies and public read/write time are bounded; body reads and network writes do not hold the store lock. The regular authenticated UI keeps membership and CSRF checks. There are no public methods for supervision, coordination, knowledge or privileged operations.

A portable contributor brief can be simply: “Read this queue address, claim one suitable packet, solve it using its public files, submit only the changed text files, and report the submission status. Never send credentials. Internal admission, integration and delivery belong to ADC.” HTTP clients or existing agent tools can implement it; no WebSocket connection or permanent donor identity is required.

## Reference inspection

The reference implementations were inspected before choosing HTTP:

- Snowcat local source at `aafbe22dff90d14113f944e73a073f40ba6681ec`, especially `docs/specs/work-queue.md`, defines opt-in repositories, claim/heartbeat/release leases, artifact verification and separate operator notes. Its authenticated MCP flow is not copied into ADC's anonymous path.
- Bluefin review local source at `c2b652caf911e68bce2e7566ed64d202d4130bea` distinguishes factory workers from maintainers and places contributor assignment/protocol control in Hive. The inspected public [contributor formula](https://github.com/ublue-os/homebrew-experimental-tap/blob/main/Formula/bluefin-contributor-tools.rb) describes a packaged launcher/runtime, not the whole queue protocol.
- Hive local source at `49e09c88d0f3dcdc1b600889f08e596a9da00789`, including `bin/contributor-relay.sh`, has a connected assignment/completion protocol and runtime output capture. ADC reuses the simple entry-point idea while retaining its own durable ownership and admission states.

Qualification evidence and rollout state are recorded in the ownership checkpoint. External review contributions, contributor reputation, generic dependency images, queue editing/top-up, source attestation and a packaged donor CLI remain follow-through; none is needed to claim enforcement that the first boundary does not provide.
