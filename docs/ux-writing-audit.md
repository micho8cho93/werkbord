# Werkbord UX writing audit

12 October 2026 · source snapshot `a8b864f` · build `4.19.3-preview.1`

## Decision

Implement a targeted copy and message-state pass. Werkbord already has a useful voice: calm, direct, concise and careful about consequences. A wholesale rewrite would erase good distinctions between planning, running, reviewing, merging and archiving.

The greatest risk is inaccurate certainty: some messages claim a cause or outcome that the underlying state does not prove. Address those before spelling and sentence length. Then improve action labels, enforce a small vocabulary, and explain advanced controls at the point of use.

This report recommends changes; it does not implement application changes.

## Method and evidence limits

Executed the requested command:

```sh
npx skills use "https://github.com/content-designer/ux-writing-skill" --skill "ux-writing"
```

Read its complete output from `/tmp/werkbord-ux-writing-skill.txt`. Resolved the checklist, accessibility guide and voice-chart template from the supporting directory it supplied: `/var/folders/x7/m55l6mw53dx8y1ndhdfgb_yr0000gn/T/skills-use-LR2Em8/ux-writing`.

Applied its four standards: purposeful, concise, conversational and clear. Reviewed Claude’s pasted findings against current source, DESIGN.md and the product boundaries in docs/STRUCTURE.md. Inspected the Individual UI, onboarding, sign-in, Help, assistant failure handling, scheduling, Git confirmations, desktop workspace shell, selected native messages, Team console and relevant Go/provider/answer-delivery paths.

This is a source-based expert audit, not an exhaustive string inventory or a live usability study. No browser walkthrough, VoiceOver session, visual layout checks or user tests were performed. Accessibility observations identify source-level issues and validation work; they do not establish WCAG conformance. The skill’s sentence and character targets are editing heuristics, not hard limits or guaranteed comprehension percentages. Important consequences take precedence over an arbitrary word count.

## Keep these strengths

- New-task recovery distinguishes a saved task from an uncertain agent start and tells users to check Runs before starting again (`web/src/lib/NewTaskDialog.svelte:94`).
- Question recovery keeps the same-answer guarantee and delivery uncertainty visible (`web/src/lib/QuestionCard.svelte:173`).
- Review help explains that Accept work / Reject work record a verdict, while Merge changes the repository (`web/src/lib/help.ts:38`).
- Onboarding names three local requirements, offers planning separately, and keeps optional connections secondary (`web/src/routes/Onboarding.svelte:80`).
- Merge confirmations explain the local scope and the absence of a push or pull-request change (`web/src/lib/git/MergeSheet.svelte:140`).
- Team reports label source times and stale runner availability rather than silently treating old reports as current (`internal/team/console/static/console.js:1124`).

## Priority 1: make failure and outcome messages truthful

### 1. Separate saved answers from delivered answers

`internal/appops/operations.go:717` says “the agent was given” an answer whenever AnswerQuestion returns successfully. Remote answers can be accepted and queued before delivery: `internal/runner/manager.go:445` delegates remote runs to `internal/service/runner_events.go:374`; delivery is confirmed separately. `internal/domain/question.go:129` already exposes DeliveredAt on the domain record.

**Change:** build the operation result from delivery state. Before delivery: “Answer saved. Waiting for the agent to receive it.” After confirmed delivery: “Answer delivered to the agent.” Use the answer text where useful. Ensure the model-facing result and any human-facing outcome agree; otherwise the assistant can repeat an inaccurate success claim.

**Acceptance:** a remote queued answer never claims receipt. Confirmed delivery changes the wording. Same-answer retries retain their existing semantics.

### 2. Do not describe every protocol error as a safety stop

`web/src/lib/assistant/words.ts:42` groups policy and protocol under “The assistant stopped for safety.” The provider defines protocol as a response this version of Werkbord cannot understand (`internal/assistant/provider/provider.go:194`). A compatibility failure is a different problem from a prohibited tool attempt.

**Change:** keep policy safety wording; give protocol its own title, such as “Werkbord could not read the assistant’s response.” Provide version/diagnostic guidance when the failure evidence supports it. Do not invent an update requirement or offer a retry the engine does not support.

### 3. Do not equate every rate limit with exhausted allowance

`web/src/lib/assistant/words.ts:37` says “Your plan has no allowance left for now.” Codex’s classifier maps HTTP 429, rate limits, too many requests and quota failures to the same kind (`internal/assistant/provider/codex/run.go:506`), with the same allowance claim in its detail.

**Change:** use “The assistant’s service is limiting requests” as the generic case. State exhausted usage only when the provider confirms it; show a reset time only when supplied. Keep provider detail as secondary evidence. Review the other provider’s equivalent mapping in the same pass.

### 4. Preserve uncertainty about local availability

`web/src/shell/WorkspaceProblem.svelte:9` always headlines “Werkbord is not running on this computer.” The source can also report “Werkbord is not answering” (`desktop/internal/workspaces/individual.go:58`). An unreachable service may still be running.

**Change:** use “Cannot reach Werkbord on this computer” for unknown availability. Keep “not running” only for positively established stopped state. “Check connection” or “Reconnect to Werkbord” accurately describes the existing refresh action; do not imply it starts a stopped service.

### 5. Give generic failures a useful object and supported recovery

| Location | Finding | Recommended direction |
| --- | --- | --- |
| `web/src/lib/assistant/words.ts:14,48` | “That did not work” obscures what failed. | For a failed reply, “The assistant could not finish this reply.” Offer “Retry reply” only when retryable. Transport/request failures need caller context rather than one universal retry instruction. |
| `web/src/lib/assistant/ReplyView.svelte:35` | “a board action” hides the failed operation. | Map `step.name` to a human action, e.g. “Could not update the task.” Keep detailed cause below it. |
| `web/src/routes/TaskPanelHost.svelte:52` | “Something went wrong while drawing it” is vague. | “The task panel could not load. Your task and runs are unaffected. Refresh the task or return to the board.” Button: “Refresh task”. |
| `web/src/lib/assistant/dictation.ts:97,278` | Several failure cases have no alternative. | For unknown start failure: “Dictation could not start. Type your message instead.” For missing microphone: connect a microphone or type. Preserve known permission/settings guidance. |
| `web/src/lib/api.ts:119,146`; `web/src/lib/assistant/api.ts:39` | “Cannot reach the controller.” | “Cannot reach Werkbord.” Add the owning computer/workspace where known. |
| `web/src/lib/api.ts:123`; Team `console.js:206` | HTTP/status fallback can reach the user. | Add an action-specific explanation and safe recovery. Put HTTP status and raw details in diagnostics. |

Check `words.ts:9–12` too: automatic reconnection and the meaning of 401/503 must match each calling flow. A status alone does not always prove device rejection or a disabled assistant.

Do not append “try again” to all shared errors. A failed read can be refreshed; an unconfirmed save/start needs reconciliation first. Do not promise “nothing changed” unless the relevant operation boundary proves it.

## Priority 2: labels must describe the actual action

| Location | Current | Proposed |
| --- | --- | --- |
| `web/src/routes/Board.svelte:298` | Clear Done / Clearing… | Archive Done tasks / Archiving… |
| `web/src/routes/Board.svelte:300` | Clear all | Reset filters |
| `web/src/lib/board/Archive.svelte:25` | Clear Done in empty-state instructions | Use the new archive action name. |
| Team `console.js:937,962` | Clear Done; Clear Done or close a ticket | Archive Done tickets; archive a ticket |
| Team `console.js:1085–1086` | Close ticket; Closing archives… | Archive ticket; “Archiving moves this ticket out of the active board. Stop any running agent in your own Werkbord.” |
| `web/src/lib/attention/AttentionCard.svelte:90` | Look at it | Open task |
| `web/src/lib/setup/NetworkCard.svelte:94` | Turn off | Turn off phone access |
| `web/src/lib/git/FindingCard.svelte:182` | I know | Hide finding |
| `web/src/shell/Workspaces.svelte:44,46`; `AddTeam.svelte:29` | Start Team / Stop Team | Start Team service / Stop Team service |
| `web/src/lib/ScheduleFields.svelte:195` | Rearm for another execution | Allow one more run |

For “Allow one more run”, retain confirmation and explain that saving edits alone does not request another attempt. Show the same wording in the helper text and confirmation. The action permits another scheduling attempt; it must not imply immediate execution.

“Hide finding” needs its existing return conditions visible or accessibly described: the finding can return when it worsens or disappears and recurs. It changes no repository content. A hover-only tooltip is insufficient as the only explanation.

Keep short labels when context is sufficient. Generic “Try again” is acceptable in a single clearly labelled recovery region. Repeated controls or isolated screen-reader labels should name their action. If an aria-label supplements a visible label, include the visible wording, for example “Try again to load GitHub repositories”.

## Priority 3: enforce a small, accurate vocabulary

| Concept | Preferred language | Apply at |
| --- | --- | --- |
| The locally running product | Werkbord; the computer running Werkbord when location matters | TokenPrompt, API failures, QuestionCard, Help, HealthPanel, RunnersPanel, Settings diagnostics |
| One board item | Task in Individual; ticket in Team for now, with an explicit handoff explanation | Fix Individual’s assistant operation results/summaries, which currently call its tasks tickets (`internal/appops/operations.go:555,565,673,697`). Do not rename APIs or domain types. |
| Adding either kind of project | Add project | `web/src/lib/shell/Rail.svelte:58` currently says Add a repository but links to the mixed project page. |
| Model effort | Reasoning level | ModelPicker accessible label; ExecutionFields and RoutingRules labels; TaskPanel summary. Preserve provider-specific option names. |
| Agent configuration | How it runs / Run settings | ProjectSettings “Execution capacity” → “Concurrent runs”; NewTaskDialog “Execution summary” → “Run settings”, “Change execution settings” → “Change run settings”. |
| Schedule precedence | Run order | ScheduleFields; explain the actual ordering rule after checking scheduler semantics. |
| Planning problems | Planning issues | Timeline heading and aria-label currently say Needs a look. The warnings can cover invalid planning as well as conflicts; “Conflicts” would be too narrow. |
| Interaction choice | Agent decisions | TaskPanel “Asks you” is misleading when the value is autonomous. Keep the existing concrete option descriptions and permissions note. |

“Needs you” should continue to identify something the current person can act on. Team execution’s “Needs input” (`console.js:1125`) may concern a different member’s agent. Prefer “Waiting for an answer” in a shared report unless it actually needs the viewer; reserve “Needs you” for the viewer’s own actionable item. One name per concept does not justify collapsing different responsibilities.

“Waiting for owner” (`web/src/shell/aggregate.ts:60`) needs a state-specific definition: does it mean the ticket assignee, run owner, workspace owner or a missing repository? Identify the source before choosing replacement copy. Show the responsible person and next action when available; do not replace it with another unsupported guess.

Adopt British English for newly written prose, consistent with colour/recognise in existing copy: organisation, authorised, synchronise, cancelled, acknowledgement. Document this explicitly rather than inferring a binding rule from spelling in DESIGN.md. Keep brands, commands, provider output, identifiers and user-entered text unchanged.

## Priority 4: explain advanced controls and make instructions accessible

- **Sign-in:** `web/src/lib/TokenPrompt.svelte:18–23`: “Connect to Werkbord.” Use short steps: on the computer running Werkbord, run `werkbord token`; paste the token here; or run `werkbord token --url` for a sign-in link. Show a persistent visible Access token label; it is currently visually hidden with a placeholder serving as the visible label.
- **Routing:** `RoutingRules.svelte:28`: “Use the selected agent” with help explaining that the rule leaves the agent choice unchanged. At line 31 retain the hardware distinction: “Minimum logical CPU cores”, with “Only use runners reporting at least this many logical cores.” Avoid reducing this to physical CPU count.
- **Lost runners:** `RunnersPanel.svelte:106`: replace “fence window” with an explanation of the safety waiting period after revocation. Give the actual duration/status or link to the relevant recovery procedure if the product can supply it. “Wait for the safety period” alone remains a dead end.
- **Clock changes:** `ScheduleFields.svelte:128`: “When this time occurs twice”, with “Earlier occurrence” / “Later occurrence” and a short explanation that clocks moving back can repeat a local time. Explain how the selected timezone applies.
- **Missed schedules:** line 144: “Wait before marking missed (minutes)”. Define when the wait starts and what happens after it ends. Preserve the distinction from “Run when ready”.
- **Git:** `MergeSheet.svelte:161` already explains fast-forward as “no new commit”. Retain the familiar Git term. Validate its “refused if target moved” explanation against actual checks; both strategies may have reviewed-commit checks. Add precise help only where the existing explanation leaves a comprehension gap.
- **Dictation:** split the long permission instructions into problem, settings path and alternative input. Do not remove either Microphone or Speech Recognition when both are required. Do not promise a particular keyboard shortcut on every Mac.
- **Help:** simplify controller/acknowledgment/mutation language while retaining saved-versus-delivered and planned-versus-scheduled distinctions. Split multi-action recovery paragraphs into steps, especially `help.ts:46,91,111`.

For example, “Answer saved. Waiting for the agent to receive it” can replace “Awaiting acknowledgment” where space allows, without erasing the distinction between saved and delivered.

## Assessment of Claude’s findings

**Agree:** targeted pass; failure specificity; project/repository mismatch; misleading Clear Done; vague Look at it / I know / Turn off; undefined fence terminology; inconsistent model effort labels; overloaded instructions.

**Refine:**

1. TaskPanelHost already names the failing view, preserves task/run safety and offers recovery. Replace its vague sentence; do not characterize the whole screen as a dead end.
2. ReplyView already shows step details and retry/recheck controls. Improve the operation title and fallback rather than duplicating every recovery instruction.
3. Thinking versus Reasoning is mostly an accessible-label consistency issue in ModelPicker, not two visible screen headings.
4. NewTaskDialog’s Execution summary is an aria-label. Fix it alongside its visible “Change execution settings” text.
5. Needs a look covers planning warnings, not only schedule conflicts. “Planning issues” is more accurate than “Conflicts”.
6. Fast-forward only has an explanation already. Developer vocabulary can remain when useful and explained.
7. Negative refusal wording is useful trust copy. It will not merge / be deleted / be removed is lower priority than clarity about the reason and remedy.
8. Do not mechanically enforce a 20-word total instruction limit. Separate actions into short sentences and preserve essential consequences.
9. The earlier note about uncommitted TaskPanel/BranchView edits is stale: the working tree was clean at the start of this audit. Recheck before implementation.

**Additional findings:** remote answer delivery overclaim; protocol/safety conflation; rate-limit/allowance conflation; unreachable/not-running conflation; sign-in’s hidden visible label; Team archive/close drift; rearm jargon; Individual’s server-produced ticket terminology.

## Implementation sequence and acceptance

1. **Truthful states and failures.** Address answer delivery, protocol classification, rate limits, availability headlines and safe fallbacks. Trace both Go and UI text so one layer does not contradict the other. Add focused tests for queued versus delivered answers, protocol versus policy, and unavailable services. Existing uncertain-save/start and same-answer tests must remain green.
2. **Action names and vocabulary.** Update archive labels, related empty states and Help together; apply project, reasoning and run terminology. Keep Team edits inside its console and shell lifecycle actions inside their existing product-neutral surface. Avoid shared code importing either product’s behavior.
3. **Advanced controls and language guidance.** Add concise contextual help; update sign-in labels; define British English and a small voice/terminology section in DESIGN.md. Centralise recurring state explanations where it reduces drift, while keeping caller-specific actions specific.
4. **Verify the complete experiences.** Run `make check` and applicable desktop checks. Walk onboarding → project → new task → answer → review → merge → archive, plus Team claim → local handoff → reported progress → review → archive. Exercise disconnection after saves/starts, remote answer delivery, failed provider replies and dictation refusal. Test narrow screens, keyboard navigation and VoiceOver for updated labels and messages.
5. **Test comprehension with developers.** Use `docs/ux-formative-test-guide.md` as a starting point. Ask users to explain what Archive does, whether a saved answer reached the agent, whether a Timeline date starts work, and what Allow one more run permits. Observe completion and unintended retries, not just copy preferences.

Suggested acceptance: no outcome claim exceeds the observed state; archive never implies deletion or agent termination; recovery identifies an executable next step; repeated action labels remain meaningful out of visual context; no new distinction requires learning an unexplained synonym.

Each implementation commit must include the appropriate unified VERSION update, then `make tag` and `make verify-tag`, per AGENTS.md. Inspect the current version and tags at implementation time; do not hard-code this audit’s snapshot as the next release base. Documentation-only audit work does not require a product release tag. Do not push without the user’s request.
