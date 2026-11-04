# Analytics and Feed: decisions left open

Reviewed on 4 November 2026 against `d7b0bd8:web/src/routes/Analytics.svelte` and the current shared pages. No Analytics view or Feed function was restored or removed in this verification change.

## Analytics

The shared page already has Work and Runs and usage, including attempts, retries, outcomes, tokens, costs and partial-report warnings. The loss is the separate views and the following detail, rather than every execution/usage metric.

| Old view or control | What it showed | Work to add to the shared page | What the window gains |
| --- | --- | --- | --- |
| Execution | Attempts, retries, failed/waiting runs, outcome distribution, normal-exit/retry reliability, current blocked/stopped tasks, attempts by provider and runner | Reuse existing aggregation and charts; add the reliability/current-state sections and separate navigation. About 1 day. | Explain failures and distinguish a machine/provider problem from task state. |
| Usage | Input/output/cached tokens, reported API cost versus estimated equivalent, limit notices, coverage and per-provider totals; usage details | Reuse `AnalyticsUsageDetails` and existing execution rows; add a measure selector and grouped charts. About 1 day. | Investigate consumption and incomplete reporting without leaving the window. |
| Completion | Accepted/rejected assessments, controller-verified merges, normal session ends, Done events and completion evidence; each explicitly a separate signal | Expose the report's completion fields through the shared adapter, then rebuild the evidence sections. About 1–2 days. | Inspect acceptance and verified outcomes without equating normal exit with completed work. |
| Provider and runner filters | Provider scope on non-Activity views; runner scope on Execution/Usage, including unrecorded runners | Extend the shared `read` contract and native Analytics relay (currently workspace/project/days); filter execution rows and keep filters per view. About 1 day, shared with the views above. | Narrow the same evidence across Personal projects while keeping Team's lack of execution data explicit. |
| Usage measure | Tokens versus API cost; reported and estimated figures kept separate | Shared state and chart labels; preserve unknown values and reporting coverage. Included in Usage above. | Choose the question the chart answers. |
| Breakdown | Project, runner, provider, configured model, attempts/retries, tokens/coverage, reported/estimated cost and limit notices | Shared accessible table with narrow-screen scrolling; map identifiers to labels on each host. About half a day. | Inspect the underlying evidence behind totals. |

These are rough implementation estimates, not commitments. Around 3–5 engineering days together, including responsive, behavior and parity checks; bridge validation and choosing which views to keep could change that. **User decision: leave Analytics unchanged pending review.** No restoration or removal is authorised by this verification pass.

## Feed

The shared Overview already provides waiting decisions, questions/notices, running work, Up next, plans and catch-up. `ControlCenter.svelte`, still reachable through Feed, additionally provides:

| Feed function | Current overlap | Moving it would require |
| --- | --- | --- |
| Attention-kind segments and project/runner/agent filters | Project selection is shared; runner/agent filtering and kind segments are not | Filter inputs on the shared attention/Overview components and runner/agent IDs in the workspace summary. Both hosts supply data and callbacks. |
| Inline browser-alert toggle and blocked-permission explanation | Alerts can also be changed in Settings → Alerts; the inline shortcut is Feed-specific | A host-supplied browser alert action/snippet, using existing notification code, without treating browser alerts as native push. Small change, roughly half a day. |
| Team tickets waiting for a repository | The shared Overview has no repository-linking flow for these pending imports | A shared pending-import section plus the existing `WaitingRepository` actions; project registration, linking/refusal and permissions need browser/window tests. Roughly 1–2 days. |
| Blocker choices, failed-run “Run again”, idle Reply, risk and schedule explanations | Shared notices link to the relevant task; questions can already be answered inline | Typed action callbacks and richer summary evidence; preserve existing start/reply authorization and stale-state safeguards. Roughly 1–2 days. |
| Expanded live run activity and “Next executions” readiness order | Running now and Calendar cover part of this; schedule readiness/reasons and expandable live output remain richer in Feed | Add activity and orchestration projections, distinguish planned dates from actual starts, reuse scheduling labels and freshness rules. Roughly 1 day. |

The filters and actions can share implementation work; do not add the estimates mechanically. The allow-list remains unchanged. **User decision: leave Feed unchanged pending review.** No move or removal is authorised by this verification pass.
