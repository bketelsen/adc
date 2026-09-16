# Stewards

A steward is a permanent agent that owns a domain. Naming follows the agent: "Storage" is the steward, and its charter, memory, routines and signals are simply its own. This replaces the earlier "area" record, which separated human intent from agent understanding behind revision pins, passage-anchored notes and public-intent drift tracking. That governance layer was removed on 2026-09-15; what a steward needs is memory, routine and a voice.

## What a steward holds

- **Charter.** What the human asked it to look after and where the boundaries are. Edited on the Stewards page, the one form that remains.
- **Facts.** Small, named, current statements: `pool-layout`, `backup-target`, `disk3-replaced`. Each has a value, a source and an optional observation time. Replacing a fact keeps the old value in history; an empty value retracts it. At most 64 per steward, so the set stays worth reading.
- **Journal.** Append-only, dated entries: what happened, what was found, what changed. Humans can add to it from the steward page ("Disk 3 was replaced today"). The last few entries travel with the steward; the rest are one search away.
- **Signals.** Something a human should know, keyed by condition so repeats update one row: severity, message, evidence and a suggestion. Signals appear on the home page until acknowledged, snoozed for a week, or resolved. A repeat of an acknowledged signal stays quiet unless it gets worse or says something new; a resolved condition that comes back reopens.
- **Resources.** Connections come from the agent's own tool grants; repositories are a short list of identities on the steward.
- **Completion default.** Routine unless the human chooses reviewed; work routed to the steward snapshots it.

## Tools

`adc_remember`, `adc_journal`, `adc_recall` and `adc_signal` are offered only to runs whose agent is a steward and that are not reviewing someone else's work. Every activation of a steward carries its charter, connections, repositories, facts, the last eight journal entries and open signals under the `steward` context key, plus a short standing instruction to record what stays true before finishing. A worker delegated inside an assignment routed to a steward receives that steward's charter and facts read-only as `context_steward`.

## Routines

A routine is a standing schedule whose template is owned by the steward: `Authority: observe`, routine completion, a brief ending with the instruction to journal findings and raise signals rather than write documents. Routines are proposed in the steward conversation and later through ordinary work proposals; the schedules page pauses and resumes them.

## Asking for a steward

Stewards are asked for, not filled in. One box on the Stewards page (or the home page link) takes a description of what needs looking after. The subscription is implied when the human has one; the designer model is the organization's supervision default on that subscription, otherwise the first model the subscription offers. A temporary designer agent asks what the description leaves open, in one or two short messages, then calls `adc_propose_steward` with the agent (new, or an existing permanent agent to attach), charter, connections, repositories, routines and completion default. The human approves, refines with notes, or rejects from the decision card. Approval creates the agent, a missing category default, the steward and every routine schedule in one transaction and lands on the steward's page. Free-text replies continue the conversation through ordinary steering.

## Learning a domain

"Ask it to learn its domain" starts one ordinary read-only assignment routed to the steward with its tools constrained to its own grants and routine completion, asking it to inspect its connections and repositories, record facts with sources, and write a journal entry with what remains unknown. One discovery runs at a time per steward.

## Migration

On start, area records still present are converted: one steward per owning agent with the area's intent as charter, the recorded understanding as an `understanding` fact, and a journal entry noting the migration. Assignments and proposals routed to an area are moved to the steward, and the retired area, note and public-intent records are deleted. The migration is idempotent.
