# Owner coordination seam

P3 extends the existing run messenger with requests addressed to permanent agents in the same organization. A request retains its lead, answerable supervisor, source task/run, question, acceptance criteria, evidence reference and eventual response. Completion of the source run does not discard the request. Source cancellation suppresses it; no request can resurrect cancelled work.

Initial delivery uses a receiving owner's existing authorized active work context. Receipt changes neither its account, tools, authority, approval state nor assignment objective. The receiving owner assesses whether it can answer from existing knowledge or perform in-scope work. A request requiring additional authority remains a visible dependency; it does not borrow the sender's credentials or automatically start an unfunded run. Responses are attributed evidence, not independent verification or human approval.

Delivery is persisted and deduplicated. A claim associates a request with one existing receiving run; a completed, cancelled or unavailable receiver without an answer releases the claim for another valid context. Source completion and engine restart preserve routing. Request/response context is bounded and separates agent correspondence from human steering. No automatic response-to-response conversation is created.

The first queue has a stable caller key, per-source creation limits and per-owner outstanding limits. Reassignment retains request identity, history and spent execution effort. Exhausted or unauthorized work remains visible while independent work continues. Background activation without an existing valid receiving context requires a deliberately funded attention policy; discovering an area does not silently establish that policy.

Cross-area delivery keeps one lead and the source assignment's accountable supervisor. A lead transfer changes coordination responsibility, never permissions. Organization humans can inspect and cancel requests with a reason. Keeping the request visible after an originating task completes supplies a concrete handoff into the later supervisor briefing and bounded attention cycle.
