# Reported model usage

The shared **Usage** view summarizes an organization's recorded model telemetry by model, personal subscription and assignment. Selecting an assignment opens its agent-run breakdown. The task page links to its all-history usage. All organization members see the same work usage, including work funded by other members; credentials and unrelated organization activity are never included.

## What the numbers mean

Input, output, cache-read and cache-write tokens remain separate provider-reported quantities. ADC does not add cache counts to input/output, estimate monetary charges, calculate premium-request billing units or infer subscription quota. Provider definitions and reporting availability can differ.

A reported zero is displayed as zero. An absent, negative, invalid or overflowing count is unknown. When some counts exist and others are absent, the displayed sum is marked partial. The coverage line counts usage events and unique runs with received usage events versus runs observed through start/usage events. A run with no usage report cannot be assigned to an actual model, so it affects overall/assignment/subscription completeness but is absent from the model breakdown. Receiving a usage event does not guarantee that every field was present, or that every call in the run was reported. Unreported calls can still be missing even when every observed run has at least one report.

The window uses ADC receipt times in UTC: rolling 24 hours, 7 days, 30 days, or all recorded history. Late reports count when received. Totals include the whole selected window, independent of assignment-table pagination. Tables have bounded scrolling, phone layouts use labeled values, and live updates every ten seconds preserve an edited time filter and expanded count explanations.

## Capture, provenance and history

ADC already logged Copilot `assistant.usage` input/output/cache-read counts. The view reads that full durable event history rather than the 250-event activity page. Historical records are left intact; attribution falls back to their saved assignment for the funding account and to the saved run for the role. Missing model identifiers remain unreported, rather than substituting a requested model as if it had served the call.

New events also capture optional cache-write counts, account/human/agent IDs, provider, session ID and SDK event ID. A small explicit allowlist is serialized; no full provider event, credentials, prompt or tool payload is copied into usage data. Session/event identifiers deduplicate replayed callbacks before receipt-window filtering. Identical legacy counts are not deduplicated because they might be separate real calls. Legacy cache-write counts and replay identifiers were not captured and cannot be reconstructed from the usage log.

The run breakdown labels its configured request model as requested; the model table uses the actual identifier in each usage report. Account names and role descriptions remain current display metadata, while new event IDs preserve original attribution. An unavailable account appears explicitly as an unavailable subscription.

## Scope and limits

This release has organization and assignment scopes, not an account-wide provider quota dashboard. It does not count direct CLI use or work in another installation. Telemetry collection supports Copilot reports and Codex cumulative-total deltas. The direct Claude adapter normalizes per-model SDK reports into the same delta collector; actual authenticated document and MCP/repository qualifications captured usage from both Codex and direct Claude. The Codex path has simulated transport coverage and passed real mixed-provider qualification with usage reports captured from both Codex and Copilot. Unreceived callbacks, provider omissions and ambiguous legacy duplicates remain possible; these are reported token totals, not an exact bill. Aggregation reads the organization's event history on refresh; a persistent aggregate/index strategy may become useful at larger scale.

Behavioral checks cover full history, zero versus unknown, partial counts, malformed reports, window boundaries, callback deduplication across sessions and windows, original account attribution, pagination, and organization access checks. Browser checks cover live totals, filter preservation, task drilldown and phone layouts in both themes. The real scheduled-work fixture verifies new usage capture during Sol execution and Claude review without modifying real infrastructure.
