# Approved standing work

Agents propose recurring work with `adc_propose_work`. Humans edit, discuss and approve it through the existing shared proposal queue. A recurring proposal contains an outcome, bounded scope, completion criteria, evidence, dependencies, suggested owner and structured cadence. Creating or discussing it never activates recurrence.

## Approval and funding

Approval selects the accountable agent, a subscription belonging to the approving human, explicit scope and advisory authority. Recurring operational actions also require validation and rollback/stop plans. The accepted packet and its evidence remain linked to the schedule and every occurrence. Approval creates a schedule with a future first occurrence; it does not immediately execute the work. Repeated acceptance returns the same schedule.

Every occurrence becomes an ordinary assignment using the saved human/account and scope. It follows the existing supervisor, delegation, correction and independent model-family review lifecycle. It shares the account's concurrency limit with other work. Within each existing queue priority class, direct work precedes scheduled work; reviewer/supervisor priority remains in place to let work finish.

The schedule snapshots the accountable agent's MCP connection ceiling. Later role grants do not automatically expand that ceiling. Missing funding membership, account, owner or approved connection access pauses the schedule with a visible reason. Permanent role descriptions and model choices remain live configuration. The connection ceiling and authority are routing controls, not a sandbox for shell access or host credentials.

## Cadence and downtime

- Intervals range from 15 minutes to one year and advance from the dispatch time. They are not aligned wall-clock cron expressions.
- Daily and weekly schedules specify `HH:MM` and an IANA timezone. Weekly schedules also specify weekday, with Sunday represented as 0.
- Calendar times follow their timezone across daylight-saving changes. A nonexistent spring-forward time is skipped; a repeated fall-back time runs once on that local calendar date.
- Missed occurrences during downtime coalesce into one due assignment. ADC does not replay a backlog of every missed interval.
- An unresolved earlier assignment, including one waiting for human input or paused, prevents another occurrence. That occurrence is skipped and the next time advances; the existing assignment remains the place to resolve outstanding work.

The queue write and next-occurrence update commit together. An occurrence has a deterministic schedule/due-time identifier. This prevents duplicate ADC assignments on restart within the single owning service; it does not guarantee exactly-once effects in external systems.

## Human controls

**Schedules** shows cadence, state, next occurrence, funding human, accountable agent, approved scope and recent assignment history. Each resulting task links back to its schedule. Live updates preserve expanded details, and the view works on a phone.

Pause stops future occurrences; existing assignments have their own pause/cancel controls. Resume revalidates access and chooses a future time without replaying paused occurrences. Any organization member may pause/resume shared work, but the funding account stays the one already approved. Concurrent stale control submissions are rejected.

Changing approved scope, cadence, authority or funding uses a fresh proposal. Pause the old schedule when replacing it; there is no automatic replacement transaction or semantic deduplication between independently approved schedules. Dependencies remain explicit planning text, not an automated dependency graph. Approved work must handle missing prerequisites before dependent actions.

## Qualification

Behavioral tests cover cadence validation, DST, overlap, downtime, duplicate avoidance, access revocation, control races, account priority and concurrent acceptance. The browser fixture exercises approval, no immediate run, pause/resume, a simulated due occurrence, history and phone layout. `TestLiveScheduledOccurrenceGetsIndependentReview` uses real Copilot Sol/Claude activations on synthetic content in an isolated database, with the due instant simulated. It verifies the normal execution/review lifecycle, not a real infrastructure action or weeks of wall-clock operation.

No real TrueNAS schedule is authorized by implementing or qualifying this feature. A concrete proposal must still receive human approval.
