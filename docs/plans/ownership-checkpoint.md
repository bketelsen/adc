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

## Next bounded action

Continue P2 maintained knowledge/onboarding and then P3 owner-addressed communication. Keep the qualified P1 service running while developing and testing later slices separately. Do not restart the product interview or replace the execution engine. Do not turn the existing runtime prerequisite wait into a completed result.
