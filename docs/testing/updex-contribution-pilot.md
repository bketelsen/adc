# Updex contribution pilot — September 14, 2026

Status: real-source, real-provider pilot passed. The candidate is prepared locally for publication review. No Updex PR, merge or release was created. The pilot used a separate ADC qualification database and a loopback HTTP listener; it did not manufacture production organization activity or expose the live server publicly.

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

Next concrete publication step: open the reviewed Updex change as a draft PR after Brian authorizes that action. A live public queue still needs its deployment address and deliberate production configuration; this pilot does not silently expose all Frostyard work. Queue editing/top-up and event-driven owner recovery remain on the existing roadmap.
