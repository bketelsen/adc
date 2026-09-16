# Continuing ownership: first slice

Permanent agents can own explicit areas with human-maintained intent and versioned agent understanding. Areas are opt-in; creating one does not start work or grant access. The Areas page displays intent, understanding and links to originating assignments. All organization members share visibility and can edit intent.

`adc_owner` supplies bounded current context or inspects a named area in the same organization. `adc_remember` updates only the calling permanent owner's understanding and source at an explicit revision. It cannot change human intent. Previous area revisions are retained. Observation text and source references are evidence, not additional authority.

Permanent-owner correspondence and maintained knowledge are available. Follow-up obligations, bounded assessments and public contribution queues were removed on 2026-09-15; standing work covers recurring checks.

Area records are additive. Existing task IDs, approvals, schedules, plans and review pins are unchanged. A full knowledge export UI and general schema migration framework are not included in this slice.

## Maintained knowledge and discovery

Human intent, observed/inferred owner understanding, and discussion are separate. Human notes retain their selected passage and original area revision. A stale contribution remains a conflict for reconciliation. `adc_remember` returns a retained conflict with current context instead of overwriting a newer summary; callers must inspect that result. Historical summaries remain retrievable through `adc_owner` with a revision. Current context prioritizes unresolved notes across all owned areas and includes their full count while limiting injected text.

The Areas page supports passage selection, discussion, earlier revisions and discovery with an explicitly selected personal subscription portfolio. Discovery reuses existing specialists and independent review, narrows protected capabilities to read grants, and has a shared 24-activation limit. It cannot activate suggested standing work. The final budgeted activation can finish; no next claim is allowed at capacity. Exhaustion produces a visible paused task, and resume cannot pretend to replenish the budget. A new investigation requires cancelling/re-scoping the exhausted one. This is an initial bounded mechanism; autonomous supervisor reassessment belongs to P3/P4.

Only the permanent owner can update understanding or answer notes, and answers require the current note and area revisions. An answer records the owner's interpretation; it never changes a human's original statement, supplies approval, or settles a human disagreement by fiat. Pending notes are available on the next authorized activation; this slice does not itself fund or wake a separate owner session.

Public intent is a separate human-selected field. Its preview contains exactly that text, without automatically appending internal summaries, notes, names, or transcripts. Producing and publishing a document still uses the existing independent review and authorized delivery path. A human can link its source reference. After reading that source using existing access, the owner can report its content through `adc_check_public_intent`; ADC retains fingerprints and a neutral drift note, not the external prose. This is explicitly an agent-reported observation, not independent proof of a live source. Repeated identical drift is deduplicated, and open drift notes are bounded.

Qualification uses isolated databases, real Sol execution and Opus review. A dormant-experiment fixture retained a correction and concluded no maintenance was warranted. Two source pilots used actual local README excerpts from TrueNAS MCP and Snosi; both distinguished documented claims from unknown live deployment state. They are source-based onboarding demonstrations, not evidence that a NAS is healthy or a current image is shipping. No production areas or standing schedules were created by these tests.


## Completion policies

Human-selected area policies and immutable work snapshots now distinguish reviewed work from routine observed-evidence completion. See [completion policy](completion-policy.md). Historical work remains reviewed. Routine is the default for new work; an owner performs and verifies its responsibility directly, and the human reviews the result.
