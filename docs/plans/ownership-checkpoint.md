# Ownership evolution checkpoint

## Current state

- Approved direction: [plan](ownership-evolution.md), [desired state](../design/desired-state.md).
- Baseline: `7a3e951`; existing identities and approval states preserved.
- P0 complete: reuse permanent Agent identity; add explicit areas; do not reinterpret Assignment.Owner or existing schedule overlap rules; preserve approval and review semantics.
- P1 implemented locally: areas, versioned understanding, narrow timed verification obligations, constrained funding/grants, reviewed observations, restart-safe dispatch, minimal Areas UI and cancellation.
- P1 deployed through `make serve` on September 14. No real area, schedule, obligation or infrastructure action has been created by qualification. All 191 baseline assignment/run/decision/plan records retained identical identities, parents and states after startup; login responds. The pre-existing queued durability run remains in cheap preflight wait because `dig` is unavailable; it has not started inference. This is not an ownership regression.

## Evidence so far

Deterministic ownership tests passed, including SQLite close/reopen, duplicate registration/dispatch, knowledge across runs, stale/missing reviews, failed verification, cancellation, mid-run funding revocation, bounded creation and grant narrowing. The final `make verify` passed (39.955-second race suite; vet/build passed).

Desktop/phone browser qualification passed: area view, intent editing, cancellation, CSRF enforcement and no horizontal overflow. Screenshots: ignored `work/ui-ownership-desktop.png` and `work/ui-ownership-mobile.png`; mobile inspected.

Synthetic real-provider follow-up passed with Sol execution and Opus review. The first run recovered two tool errors; restricting observation recording to workers and making the obligation ID implicit removed that friction. The second run completed in 64.04 seconds with zero failed ADC tools and no human continuation. It used corrected owner context after the source task finished. This is an accelerated fixture, not a weeks-long real-world claim.

Independent Claude review passed after corrections. Findings fixed: credential screening on shared knowledge, superseded observation handling, completion at the activation limit, inherited old output, review discovery for operations-category observations, and funding removal during active verification. The reviewer withdrew its stale-attempt concern after inspecting in-place reassignment. Advisory native tools remain explicitly advisory, as authorized. Final report: ignored `work/ownership-independent-review.json`; logs `/tmp/adc-ownership-audit-corrections.log`, `/tmp/adc-ownership-live-final.log`, `/tmp/adc-ownership-verify-corrections.log`.

Consistent pre-rollout backup: `.adc/backups/before-ownership-20260914T021750Z.db` (0600); baseline metadata: ignored `work/ownership-before-deploy.json`. The new records are additive, but older binaries cannot safely execute active follow-up assignments without reconciliation.

## P2 qualification and deployment

P2 maintained knowledge, selected-revision discussion/correction, explicit history retrieval, observed/inferred understanding, and bounded discovery are deployed. Public intent preview includes only human-selected text. A linked public source can be checked by the owner; the stored agent-reported fingerprint exposes drift without importing external prose into intent. Public publication remains on the existing reviewed/authorized path; cadence-based refresh is later work.

`make verify` passed (40.974-second race suite, vet/build). Browser qualification passed with selected-passage correction and phone layout. The dormant-experiment live fixture passed in 72.03 seconds with Sol/Opus, retained the correction and proposed no maintenance. Real-source onboarding pilots for TrueNAS MCP and Snosi passed in isolated databases (188.07 seconds total). They used real README snapshots and explicitly retained unknown live state. The NAS pilot recovered one benign `adc_wait` race; the product pilot had no failed tools. Logs are `/tmp/adc-knowledge-source-pilots.log`, `/tmp/adc-knowledge-live.log`, `/tmp/adc-knowledge-browser-release.log`, `/tmp/adc-knowledge-verify-deploy.log`.

Independent Claude review passed after fixing exhausted-budget resume, late capacity checking, pending-note prioritization and count, and the human-brief newline. Final review of public-source drift passed; its low-severity suggestions were also addressed with regressions: retain a deferred drift-notification flag when the note queue is full, and label observations stale after the human changes expected public text/source. Report: ignored `work/knowledge-independent-review.json`; log `/tmp/adc-knowledge-audit-final.log`.

Pre-rollout consistent backup: `.adc/backups/before-knowledge-20260914T024947Z.db`; baseline in `work/knowledge-before-deploy.json`. There were no active provider runs. All 191 baseline tracked records retained IDs/parents/states after rollout; login responds. No production areas or schedules were populated with qualification data. Existing missing-`dig` preflight remains unchanged.

## P3 first coordination queue — deployed

Permanent-owner requests now retain lead/supervisor accountability, responses and history across assignments/restart. Cross-task delivery attaches only to already running/queued contexts; it does not bypass waits or create unfunded work. Three failed receiving attempts retain a blocker, and a later authorized owner run can answer a prior blocker with new evidence. Lead changes preserve spent attempts and original supervision. Shared Coordination UI provides bounded inspection and cancellation.

`make verify` passed (41.956-second race suite, vet/build). Browser qualification passed on desktop/phone with collapsed evidence and cancellation. Independent Claude re-review passed after fixing waiting-run wakeups, blocked stale claims, recipient-as-lead transfer and repeated routing scans. Report: `work/owner-coordination-independent-review.json`. A harmless empty-result retry edge noted by review was tightened before final verification.

Real Sol/Opus coordination passed after source completion (50.03 seconds). A second live fixture recovered a retained prior blocker through a later owner context and independent evidence review (76.03 seconds), without human continuation. The second recovered three ordinary tool errors, recorded in the backlog/P5 follow-through; it was not zero-friction. Logs: `/tmp/adc-owner-coordination-audit-corrections.log`, `/tmp/adc-owner-coordination-recovery-live.log`, `/tmp/adc-owner-coordination-browser.log`, `/tmp/adc-owner-coordination-verify-release.log`.

Deployment: `.adc/backups/before-coordination-20260914T031538Z.db` and `work/coordination-before-deploy.json` preserve the consistent baseline. No provider run was active. All 191 tracked records retained IDs/parents/states after restart; login responds. Existing Monday 09:00 America/New_York TrueNAS schedule remains active and unchanged. No real owner requests or areas were invented for qualification.

The queue's deliberately unfunded wait is not the completed background-attention experience. P4 must provide explicit standing scope/funding and a supervisor briefing, while preserving existing schedule semantics unless a human approves migration. Broader recovery accounting will evolve with that attention layer; the present counters cover requests and owner follow-up/discovery tasks.

## Next bounded action

Implement P4 bounded periodic assessment and the supervisor briefing using approved proposal/schedule machinery. Preserve the real TrueNAS weekly schedule until any concrete replacement is explicitly approved. Continue toward a reviewable migration option, not an automatic reinterpretation of prior approval. Existing P3 service stays running during development.
