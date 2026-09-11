# ADC priorities after the first real delivery

The Frostyard website assignment delivered two reviewed draft PRs, subsequently merged by Brian. The orchestration cleanup discovered during that run is deployed. The completion report retains the separate website dependency/security and visual-review follow-ups.

The real read-only TrueNAS qualification is also complete: a GPT storage worker collected NAS observations and a Claude reviewer independently verified them through MCP. The resulting provisioning and blocked-outcome fixes are deployed. Infrastructure follow-ups in that report remain proposals requiring approval.

The one-off proposal tool and shared human queue are implemented, including edit/discuss/decline/accept, history, related-work hints and atomic acceptance under the human’s portfolio and scope. The first real qualification produced five pending proposals from the reviewed TrueNAS report. Brian subsequently accepted the update-readiness research, which completed with independent review and proposed one further evidence-gathering task. No update was authorized.

Approved standing work and scheduling are implemented: cadence/timezone, a designated funding account, concurrency limits, persisted occurrences, no overlapping unresolved assignments, action/validation/rollback approval and pause/resume. Real recurring infrastructure work still requires approval of its specific proposal.

Usage visibility is implemented: reported input/output/cache tokens by model, human subscription and assignment/run, live time-window views, full historical telemetry and explicit unknown/partial counts. Tokens are not subscription billing or quota.

Direct Claude is now connected and qualified: actual Codex execution → direct Claude review completed for both documents and stdio MCP/local Git work, with independent checks and usage attribution. The model-context suffix fix is deployed. HTTP MCP, external integrations, long-running recovery and exhausted-account behavior remain qualification gaps.

## Next

1. **Enforced permissions and credential boundaries.** The first protected Linux profile, provider tool controls, generic MCP gateway, scoped grants and bundled approval UI are implemented. See [the operating guide](../design/permissions.md) and [plan](permissions.md). Complete reviewed rollout and remaining authenticated repository publication/recovery qualifications before treating every workflow as protected. Existing organizations and approved schedules keep their prior settings; advisory host/shell access remains explicitly advisory.
2. **Separate-installation collaboration.** Supervisor-to-supervisor requests, with approval and execution in the receiving installation and an external dependency stub for the requester.

Provider qualification gaps remain explicit follow-ups. Remote execution, backup/export/restore and richer notifications remain later conveniences. This is backlog sequencing, not authorization to execute proposed infrastructure work or install connections.
