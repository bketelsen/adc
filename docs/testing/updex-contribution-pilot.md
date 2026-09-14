# Updex contribution pilot — September 14, 2026

Status: real-source, real-provider pilot passed. Brian authorized publication and [draft PR #417](https://github.com/frostyard/updex/pull/417) is open. No merge or release occurred. The pilot used a separate ADC qualification database and a loopback HTTP listener; it did not manufacture production organization activity or expose the live server publicly.

## Useful deliverable

Public source: `https://github.com/frostyard/updex`, pinned main commit `373717fe158dcdb8c8411f1083484370f93bb165`.

The donor added only `version/example_test.go`, external package `version_test`, with four executable SDK examples: ParsePattern (including an error), ExtractVersion (match and non-match), BuildFilename, and newest-first Sort. Production source, dependencies, workflows and release configuration are unchanged.

Local commit: `6d435c24baac24776b7ca17c58592fcf0ef98ae5` (`docs(version): add executable SDK examples`). Branch: `adc/updex-contribution-examples`, worktree `/var/home/bjk/projects/adc/work/updex-pilot`. The packet contained unchanged pinned `go.mod`, `go.sum`, `version/pattern.go`, and `version/pattern_test.go`; its criteria carried the relevant scope and validation requirements. Full repository instructions were inspected separately for integration.

## Observed workflow

1. An approved-policy qualification queue offered the deliberately public package snapshot, with three internal evaluations available.
2. A real Sol donor received only the public packet and a submission tool. HTTP issued the claim receipt automatically; the donor had no ADC membership or private organization tools. The harness submitted the donor's actual file bytes through the public HTTP route.
3. A real Opus reviewer independently inspected and tested the candidate using ADC's two admission tools. It ran two offline validation commands, covering formatting, vet, all examples, and existing package tests. It admitted the first candidate. One evaluation was spent; no ADC tool calls failed.
4. ADC automatically queued the waiting accountable owner, and its protected import tool materialized the admitted source. Admission left the original outcome unfinished and publication unauthorized, as required.
5. The exact admitted file was copied into the clean, pinned Updex worktree. Full `make ci`, `node scripts/check-docs.mjs`, native `make build`, and `build/updex --help` passed. No candidate corrections were needed.

Live contribution/admission/wakeup/import qualification: **55.81 seconds**, `/tmp/adc-updex-pilot.log`. Durable source, candidate, findings and tool traces: `work/updex-contribution-pilot.json` (a local qualification artifact, not production history). Full repository gate log: `/tmp/adc-updex-ci.log`; docs checker verified 20 documents, 278 links and 9 symlinks. Temporary logs are corroborating evidence; this committed report records the outcome.

## Runtime and ADC changes

Basic admission lacked Go. The pilot now uses an installation-curated, content-pinned runtime: exact Go **1.26.7** and public `github.com/hashicorp/go-version v1.9.0`, including their licenses. Runtime digest: `34fa80c898c1db93e9151560d5cd8fe2e2299f6db78bf50bd5e9e7529f53fa4d`.

Each command copies and verifies the runtime before mounting it read-only. Candidate code retains no network, host credentials, private caches or persistent workspace. The Go profile has fixed aggregate limits; there is no network-enabled fallback. The baseline Updex package tests and vet passed cold in 12.61 seconds before provider spending.

Independent Claude code review found that withdrawing a runtime could still allow intake to spend review budget. The queue now checks installed runtime identity and directory availability before offers, claims, submissions and dispatch. Regression coverage proves no budget is spent after withdrawal. Final independent review **PASS**, 58.53 seconds, `/tmp/adc-updex-runtime-audit-final.log`. Full ADC `make verify` passed (48.499-second race suite, vet/build); final rollout checks are recorded in the ownership checkpoint.

Review limits: a changed runtime's contents are fully verified at command execution, not hashed on every anonymous request. Administrators must replace rather than mutate active curated runtimes. Runtime snapshots use bounded host temporary storage outside the candidate cgroup and can remain after an abrupt host/process crash; dedicated scratch storage and stale-snapshot cleanup are follow-through. Namespace isolation shares the host kernel.

The initial packet attempt included AGENTS.md, whose public `id-token: write` text triggered the conservative credential-assignment detector. No inference or review budget was spent. The final packet contains only the source needed for this bounded contribution, with explicit package-admission and full-integration criteria. Better source-aware diagnostics remain backlog work; secret screening was not disabled.

## What this proves, and what remains

This proves a useful real repository change can travel through anonymous HTTP contribution, cross-family internal admission, automatic owner resumption and protected import, followed by normal repository validation. It does not yet prove an unattended contributor on another machine, internet-facing deployment availability, or autonomous owner integration all the way to GitHub. The qualification harness coordinated the donor and drove review dispatch; local integration and full CI were performed by the development agent.

Publication completed after Brian authorized it: draft PR #417 targets Frostyard main from `bketelsen:adc/updex-contribution-examples`. A final rubric check identified the coverage-ratchet requirement: the parse-error example exercises one previously uncovered statement, so `.coverage-baseline` was raised from 86.2% to the observed 86.4%. This required integration metadata change is commit `ce95cb0`, following the independently reviewed example commit. Full `make ci` passed again with the higher baseline (`/tmp/adc-updex-pr-ci.log`); GitHub checks are tracked on the PR. A live public queue still needs its deployment address and deliberate production configuration; this pilot does not silently expose all Frostyard work. Queue editing/top-up and event-driven owner recovery remain on the existing roadmap.

## Live localhost continuation

Brian selected localhost for the next rollout. The actual Frostyard organization now has an Updex area owned by the existing Product Engineer and one queue, using Brian's existing signed-in Copilot account and the existing Opus reviewer. No permanent agents, MCP grants or recurring tasks were added.

- Area: `1c3645e94cb4cfcf5f2106da658846e2`
- Queue: `146ce674112a5ef2d20215bc00276aac`, three total live admission evaluations, pinned Go runtime
- Accountable assignment: `564e5271fe216456e0976cc23948b10f`
- Owner run: `62dd0ee4b7fd0d4372eaec8739b763bf`
- Packet: `548a1e1c11f54a7eaf69fadce231d16e`, “Add version pattern and comparison fuzz invariants”
- Admitted contribution: `8df4cd78ef6ef533cca49340066540f6`
- Live review: task `048232d54ac56019dbd8b7b3d25177b3`, Opus run `cb2d6492ddbef7ba63a2ca4df13d66e4`

The endpoint is `http://127.0.0.1:8789/public/queues/146ce674112a5ef2d20215bc00276aac`. The server remains bound to loopback; no LAN/public deployment was enabled. The UI was used to configure real responsibility, funding and scope, then start a protected owner assignment. Initial activation does not manufacture a task to fill a queue: its bounded investigation was instructed to stop if there was no useful nonduplicative work. It selected two fuzz targets outside PR #417's scope.

A separate opt-in donor process (`TestLiveQueueAddressContributor`) started from only that HTTP queue address, with no connection to the production database, private organization context or internal scheduler. It waited for work without inference, used its own Sol subscription session to read/claim/solve/submit, and retained the automatic receipt privately. Two donor-side offline checks ran on donor compute. The production ADC scheduler independently dispatched Opus; the donor observed the receipt-bound admitted result. This took **246.81 seconds** including waiting and review, `/tmp/adc-live-address-donor.log`, artifact `work/live-queue-donor.json`.

Opus ran ordinary package tests and vet, then 25 seconds of fuzzing for each target (about 1.26 million and 432,000 executions). It admitted the first submission, using one of the three live evaluations. This is actual production admission evidence, not the earlier fixture. Full integration and repository checks remain the owner's obligation.

### Friction and repairs

The owner initially dumped roughly 47 KiB of source in one response. Copilot offloaded it to a provider-private temporary file which the protected workspace could not read. Missing `rg` and another oversized read compounded the problem. One developer steering message explained distribution `grep`/`sed` and bounded reads. The product correction caps encoded results at 12 KiB with explicit incomplete-evidence guidance. Workspace exit codes remain intact; truncation cannot newly satisfy the admission validation requirement. Generic oversized results remain valid JSON previews. Independent review caught a quadratic invalid-UTF-8 prefix scan; that was replaced with linear normalization and a regression.

The steering turn also exposed a wait bug. After the first `adc_wait_contribution` registered a deadline, a reactivated owner could not rejoin it and escalated a routine wait as a blocker. The corrected tool rejoins the same original deadline without extending it; expired waits and terminal results return actionable continuation, clear the wait registration, and never cancel an already submitted candidate. Provider handoff only yields when the tool actually suspends the run. A focused regression covers each transition.

The pre-fix live blocker was decision `61d64731cfe4ff095c95f3b8e5f41c05`. Its repair must be recorded as superseding obsolete runtime state, not as a new human approval. The live owner resumes with the admitted candidate and its existing authority, counters and funding. This pilot therefore does **not** claim zero intervention: it exposed and corrected two concrete orchestration defects. Final rollout and integration evidence are recorded in the checkpoint.

Deployment recovery retained all 202 tracked IDs and parents. Only the existing owner/assignment and its obsolete runtime decision changed state; the latter was superseded rather than answered as a fabricated approval. The owner then passed full pinned CI on integrated commit `85967a7ca7c4d53945d7edcd3fb2bcd32e096723`, including the mandated coverage ratchet to 86.5%. An attempted direct supervisor review/finish hit the existing role guards; the owner delegated a finalizing worker automatically and arranged Opus review of that worker's same exact commit. This extra handoff is recorded as recovered friction.

GitHub inspection on September 14 confirms the earlier examples PR #417 was merged at 18:52:23 UTC, producing main `8fcfe089e4420d8e515003f9d746fb31b07ed5c7`. ADC's new public packet used that base and does not duplicate its examples. This observation is not a merge performed by this development session.

Final localhost outcome: assignment `564e5271fe216456e0976cc23948b10f` is ready; original owner, finalizing worker and final Opus reviewer are complete. The supervisor guard defect was fixed, independently reviewed and deployed, then ADC's normal restart recovery resumed the same run without a second database repair. The owner also passed `make fmt` (no tracked changes) and the separate docs-integrity gate under official checksum-verified Node v22.23.2 (20 docs, 278 links, 9 symlinks). Final Opus review ran full pinned CI, independently confirmed base/head coverage at 86.4/86.5, and ran 11,770,752 additional fuzz executions. The final two-file commit is `85967a7ca7c4d53945d7edcd3fb2bcd32e096723`; publication remains unperformed. Initial output steering, the first runtime state repair and the later reminder about omitted docs checks are explicit operator interventions. The queue remains limited to localhost and one of three evaluations spent.
