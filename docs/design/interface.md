# Interface direction

Aide de Camp is a working environment for supervising assignments, reviewing evidence, and making decisions. Its interface should be comfortable for extended use and usable on a phone.

Use cool neutral surfaces, graphite text, clear sans-serif typography, and a restrained blue accent. Avoid ivory-and-green styling, ornamental gradients, generic oversized dashboard cards, and fabricated activity. Use deliberate spacing, readable line lengths, and compact but legible metadata. Color supplements explicit status labels.

The organization view leads to active assignments, people and agents, documents, and decisions. Each assignment makes its outcome, responsible agent, dependencies, open findings, and next action clear. Users can drill into worker activity and return without losing their place.

Document review pairs a readable document with conversation anchored to selected text and its revision. Phone layouts use stacked views and breadcrumbs rather than squeezing desktop split panes into a narrow viewport. Steering is recorded and visible to supervising agents.

Use semantic controls, visible focus, sufficient contrast, meaningful loading/error/empty states, and reduced-motion support. Regular text and touch controls must remain usable with enlarged text. Design team creation, assignment inspection, document review, and approvals as one coherent experience.

Assignment activity folds consecutive tool starts/results into collapsed groups, with visible failure counts. Empty provider messages and usage records do not split those groups. Live updates preserve expanded groups and worker details. The worker rail presents agent name, model, then the bounded assignment title. Activity, document views, decisions and individual evidence-rail sections use bounded scrolling regions so long assignments do not grow the page indefinitely; activity history remains explicitly paginated.

The right rail has no shared scrollbar. Workers, documents, reviews and tool evidence have separate bounded content regions with stationary headers and subtle thin scrollbars. Tool evidence starts collapsed; its expansion is preserved across live updates.

Each worker in “Who’s on it” links directly to its recorded transcript. The read-only run view shows its agent, current state, model, brief, messages and tool evidence, updating every two seconds as records arrive. Only that run’s evidence appears; history is paginated before sibling activity can crowd it out. Team cards list each concurrent running, queued, waiting or blocked instance on an active assignment, with links and live state updates. Finished runs remain accessible from their assignment. Historical transcript pages stay pinned until the human returns to live activity. These are persisted messages and tool events, not token-by-token provider output.

Execution plans appear above assignment activity, with a bounded step list, stable keys, owners/reviewers, prerequisite links, status and blocker reasons. Briefs and completion criteria expand independently; live updates preserve those disclosures and unsent steering. Each dispatched step links to worker and reviewer transcripts. Drafts clearly show that no work has started and can be started within the existing assignment scope.

Declared milestone requirements appear within each execution-plan step, with target, criteria, evidence provenance and observation time. Human attestations use an inline form; recorded evidence remains subject to independent review. Live updates preserve unsaved text and the revision it was written against, so another member’s update cannot silently rebase a stale submission. Replacement and withdrawal are explicit.

Automatic waits live inside their milestone disclosure, showing the source, deadline, last/next check, matching interval, failure reason and latest redacted observation. Polls update those details without erasing unsent steering or collapsing inspected evidence. A satisfied observation still shows the step awaiting independent review.

Integration evidence expands within each step, showing repository base/output commits, consumed prerequisite versions, environment pins and required checks. Check disclosures show missing/pass/failed/stale/inapplicable state, provenance, artifact pin and supporting output. These remain bounded inside the existing step list; live changes preserve inspected disclosures and unsent steering.

Execution readiness has a separate collapsible disclosure within each plan step. It shows worker/reviewer prerequisites and recoverable failures, plus ownership and limitations of allocated test resources. SSE updates preserve its expansion and unsent steering. Connections offers a dedicated GitHub form with explicit repository scopes and a masked token field; credentials are never echoed after saving.


Connections lists an Edit link for each organization tool, opening a dedicated desktop/phone page. Members can update the existing stdio command/arguments, HTTP URL, or GitHub repository scopes and token without changing the connection ID or reassigning agents. Saved environment/header values remain hidden: blank JSON fields preserve them, supplied strings replace individual values, and null explicitly removes a name. Invalid submissions reopen the stored configuration without echoing submitted secrets. Stale forms require reload. Transport changes use a new connection.


Execution plans now open with a visual dependency overview and progress counts. Sources and the long evidence list are collapsed on the task page. Open full plan uses the available page width, starts on an active/reviewing or blocked step, and shows one selected step's existing evidence, criteria and transcript links. SVG nodes are layered from actual prerequisite edges; arrows point toward dependent work. Selection emphasizes ancestors and descendants; Show all branches restores the complete view. Zoom, fit, scrolling and a Jump to step selector support large graphs and phone screens without making the page overflow. Live patches preserve viewport/selection, open disclosures and unsaved evidence; milestone submissions return to the same selected step and retain the full-plan route. The graph is a read-only projection and grants no execution authority.
