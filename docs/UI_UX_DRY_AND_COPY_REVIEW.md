# Werkbord UI, UX and copy review

Reviewed: 27 October 2026  
Source baseline: `9bd442bc55e90f83266f9765d3654ccda9e040d5`  
Status: implementation is tracked per finding below.

## 1. Purpose and scope

Make everyday work easier to understand by removing duplicate actions, controls that no longer fit the current stage, repeated explanations, awkward language and unnecessary technical detail.

This report combines **all 14 findings from the earlier DRY review** with **21 additional copy findings**: 9 interaction findings (`D01–D09`) and 26 copy/information findings (`C01–C26`). Stable IDs let each item become a separate Codex task. Related items are grouped into implementation batches in section 7.

The assessment follows current UI source and the handlers and state conditions behind it. It is not a completed usability study or a full live acceptance test. In the earlier inspection, Personal was unavailable at its default local address and Team opened at sign-in. Findings about behaviour therefore identify what the UI code offers; they do not claim that an unintended request successfully executes on the backend.

### Coverage

| Surface | Reviewed content |
| --- | --- |
| Desktop shell | Home, attention lists, calendar, analytics, saved workspaces, settings, Devices & Hosting, project moves, Team creation and native confirmation copy |
| Personal | Setup, tours, projects, board, task creation/details, run results, review, Git dialogs, planning and scheduling |
| Settings | Agents, defaults, runners, phone pairing, display, MCP services/clients, project memory, memory sharing and repository relationships |
| Salie | Opening suggestions, empty/unavailable states, proposals, confirmations, run sessions and recommendation explanations |
| Phone | Control Deck, onboarding, workflow configuration/monitoring, My Work, reviews and Team summary |
| Team | Workspace, board/ticket actions, private agent controls, scheduling, analytics, memory, invitations and hosting explanations |
| Help | Guide introduction, guide structure, terminology and tour text |

Excluded: marketing website, vendor licensing console, developer documentation unrelated to the UI, backend-only processes, user-authored content and arbitrary model/provider output. Backend-authored recommendation and health messages are included where the UI displays them.

## 2. Overall findings

The app frequently explains its own implementation and safeguards before the user has a question about either. The largest reading burden comes from several individually reasonable explanations appearing together: a page introduction, a section introduction, a field hint, a status message and a footer.

The primary recommendations are:

1. Give each visible stage one relevant action or clearly differentiated set of actions.
2. State the current outcome first, then the next useful action.
3. Explain a concept once in the current interaction; reveal uncommon detail on request.
4. Use familiar, specific language and consistent product terms.
5. Preserve facts needed to decide safely: destination, permissions, irreversible consequences, stale data and uncertain outcomes.

Do not apply a word-count target mechanically. A destructive confirmation can need more detail than an empty board. A visible label and its accessible name are not redundant copy to delete.

## 3. Recommended vocabulary and voice

These are proposed editorial defaults, not instructions to rename every matching string mechanically.

| Meaning | Recommended wording | Guidance |
| --- | --- | --- |
| Actionable questions, blockers and warnings | **Needs attention** | More natural than “Needs you.” Use “Needs your answer” for an actual unanswered question. |
| Conversational attention request | **What needs my attention?** | Best replacement for the existing Salie suggestion. Keep “today” only if that time scope is intended and supported. |
| Broad personal workload | **My work** / **What’s on my plate today?** | Suitable for a workload or daily-summary prompt; it has a broader meaning than an interruptions queue. |
| Product assistant | **Salie** | Use the name in user-facing controls that refer to Werkbord’s assistant. |
| Claude Code, Codex and similar tools | **Coding agent**, then **agent** | Do not call these “the assistant” in the same flow as Salie. Keep provider names intact. |
| Personal board item | **Task** | Do not introduce “ticket” as a synonym. |
| Team coordination record | **Ticket** | Explain the ticket-to-local-task handoff once at that boundary. |
| Work dates | **Planned dates**, **Start**, **End** | Preserve the distinction from an agent’s scheduled start. |
| Agent start timing | **Schedule**, **Scheduled start** | “Schedule used” or “Schedule another run” is clearer than “one-shot dispatched/rearm.” |
| One execution attempt | **Run** | Reserve “conversation” for retained chat context and “terminal” for an interactive terminal. |
| Inherited configuration | **Use project defaults** / **Use app defaults** | Name the actual source instead of “project / controller default.” |
| Unknown outcome | **Could not confirm…** | Do not shorten an uncertain save or start to “Failed” or “Nothing changed.” |
| Completion | **Run finished**, **Task done**, **Merged** | These are separate facts; do not collapse them into a generic success claim. |

Use British English, sentence case, concrete verbs and a calm tone. Prefer “Loading reviews…” to “Looking…”, “Unassign” to “Take it back”, and “This run needs a decision” to inferred motives such as “stopped rather than guess.” Keep Git terms such as branch, commit, merge and worktree where developers need them.

The proposed attention vocabulary changes the current convention recorded in [DESIGN.md](../DESIGN.md#L323). If adopted, update that convention together with visible labels, accessible names, tours, Help and label-sensitive tests. Preserve route IDs, stored keys, API names and search aliases unless a separate migration is justified. Do not rename a whole Workspace page to “Needs attention” if it also serves as a project/work overview.

## 4. Interaction and state findings

Priority: **P1** = misleading stage or next action; **P2** = avoidable confusion or duplicated work; **P3** = lower-impact clutter. Items are unimplemented unless a status says otherwise.

### D01 — Completed first-task setup still offers an agent start · P1

**Implementation status:** Completed in batch 2; the original observation and proposed behavior are retained below.

- **Source:** [HelloFlow.svelte:179](../web/src/lib/setup/HelloFlow.svelte#L179), [start handler:47](../web/src/lib/setup/HelloFlow.svelte#L47), [setup navigation:266](../web/src/routes/Onboarding.svelte#L266).
- **Observed:** Revisiting First task shows “Start the assistant” whenever the task exists. The control checks busy/title, not whether the example is running, ready for review or done.
- **Change:** Render the example’s current state and its relevant destination. Offer a start only when there is no run, or an explicitly appropriate retry.
- **Acceptance:** Revisit this step with no run, an active run, a pending question, a finished run and a done task. Each state has the correct action; the completed step does not invite another start.

### D02 — Paired phone setup continues to offer pairing · P1

**Implementation status:** Completed in batch 2; the original observation and proposed behavior are retained below.

- **Source:** [PhoneStepCard.svelte:31](../web/src/lib/setup/PhoneStepCard.svelte#L31), [PhoneEnroll.svelte:90](../web/src/lib/phones/PhoneEnroll.svelte#L90).
- **Observed:** “Phone connected” changes the card summary, but the scan instructions and nested “Add a phone” remain.
- **Change:** Replace the pairing controls with completion and continuation in onboarding. Keep adding additional phones available in Settings.
- **Acceptance:** Newly paired and previously paired states show one completed step and a next action; Settings still supports multiple phones.

### D03 — Agent setup has two “Check again” controls · P2

**Implementation status:** Completed in batch 2; the original observation and proposed behavior are retained below.

- **Source:** [AgentsCard.svelte:50](../web/src/lib/setup/AgentsCard.svelte#L50), [Onboarding.svelte:345](../web/src/routes/Onboarding.svelte#L345).
- **Observed:** The card and surrounding navigation both offer the same apparent refresh. Their handlers have different scopes, including model-option refresh in the card.
- **Change:** Keep one refresh that performs the checks needed by this stage.
- **Acceptance:** One visible refresh; readiness and any required model options both update. Background checks do not create additional user steps.

### D04 — Team retains the previous start action during policy review · P1

**Implementation status:** Completed in the Team actions batch; the original observation is retained below.

- **Source:** [desktop.js:507](../internal/team/console/static/desktop.js#L507).
- **Observed:** “Review and start agent” remains disabled while the policy preview adds “Start agent.” The previous-stage action stays in the current stage.
- **Change:** Replace the preparation action with the preview, a final “Start agent” and a clear way to change settings. Keep the existing approval boundary.
- **Acceptance:** Preparation, review, starting and running each have an appropriate action set. No duplicate start CTA; changing settings invalidates any approval that no longer matches.

### D05 — New schedules already offer “Allow one more run” · P1

**Implementation status:** Completed in the scheduling and everyday-work group; the original observation is retained below.

- **Source:** [ScheduleFields.svelte:257](../web/src/lib/ScheduleFields.svelte#L257), [ScheduleSheet.svelte:14](../web/src/lib/task/ScheduleSheet.svelte#L14).
- **Observed:** Rearming is always displayed next to saving, including before any scheduled attempt or when automatic running is off. “One more” presumes a previous attempt.
- **Change:** Show “Schedule another run” only when a used schedule needs an explicit new authorisation. Explain that it schedules an attempt rather than immediately starting work. Pass the original schedule state into the presentation if needed.
- **Acceptance:** Check new, disabled, armed, dispatched and used schedules. Saving configuration and authorising another attempt remain distinct operations with accurate labels.

### D06 — Saved-task recovery asks users to “Keep saved task” · P2

**Implementation status:** Completed in the recovery and saved-workspace group; the original observation is retained below.

- **Source:** [NewTaskDialog.svelte:159](../web/src/lib/NewTaskDialog.svelte#L159), [save/recovery logic:74](../web/src/lib/NewTaskDialog.svelte#L74).
- **Observed:** After a saved task’s launch fails, the dialog offers “View saved task”, “Close”, “Keep saved task” and possibly “Retry start.” “Keep” implies the saved task needs another persistence decision. Close and Keep also differ in recovery-draft cleanup.
- **Change:** Use a recovery view with “View task”, an eligible retry and one close action. Define draft cleanup deliberately.
- **Acceptance:** Closing preserves the saved task without suggesting another save is required. Reopening does not accidentally restore an obsolete creation draft. An uncertain start remains protected against unsafe retry.

### D07 — Workspace error states duplicate reload controls · P2

**Implementation status:** Completed in the recovery and saved-workspace group; the original observation is retained below.

- **Source:** [GeneratedWorkspace.svelte:55](../web/src/shell/GeneratedWorkspace.svelte#L55), [PersonalWorkspace.svelte:126](../web/src/routes/PersonalWorkspace.svelte#L126).
- **Observed:** A header reload remains present when a load error or rendering failure adds another reload/retry control nearby.
- **Change:** Give the error state one authoritative recovery action. Keep routine reload available in the normal state if useful.
- **Acceptance:** Inspect successful loading, failed loading and failed rendering; each error presents one appropriate recovery control. Preserve boundary-reset behaviour where required.

### D08 — Phone preview has two identical exit actions · P3

**Implementation status:** Completed in the phone workflows batch; the original observation is retained below.

- **Source:** [Deck.svelte:345](../web/src/lib/mobile/Deck.svelte#L345).
- **Observed:** “Stop preview” and “Return to Control Deck” run identical handlers and advance onboarding to the same step.
- **Change:** Keep “Return to Control Deck.” The preview label already establishes that no live work is running.
- **Acceptance:** One explicit exit; close and Escape still reach the intended onboarding state without executing a workflow.

### D09 — Finished tasks have overlapping next-run forms · P2

**Implementation status:** Completed in the next-run group; the original observation is retained below.

- **Source:** [RunHandoff.svelte:254](../web/src/lib/RunHandoff.svelte#L254), [TaskPanel.svelte:536](../web/src/routes/TaskPanel.svelte#L536).
- **Observed:** “Continue with another agent or model” and “Set up another run” both ask for execution choices and instructions. The second can also offer conversation continuation. These are different operations, but users must choose a form before their differences are clear.
- **Change:** Provide one next-run entry point with explicit choices for continuing existing work, resuming a supported conversation or starting fresh. Preserve the existing worktree/context semantics.
- **Acceptance:** The user can tell which branch, worktree, context and conversation the next run will use before starting. Consolidation must not silently turn one operation into another.

## 5. Copy and information findings

Each replacement below is proposed wording. Validate dynamic state, permissions and destination before implementing it. “Remove” means remove redundant visible prose, not useful accessible labels or consequential facts.

### C01 — Setup repeats its introduction and each step’s explanation · P2

**Implementation status:** Completed in batch 2; the original observation and proposed wording are retained below.

- **Source:** [Onboarding.svelte:231](../web/src/routes/Onboarding.svelte#L231), [step content:287](../web/src/routes/Onboarding.svelte#L287), [footer:352](../web/src/routes/Onboarding.svelte#L352), [GitHubCard.svelte:139](../web/src/lib/setup/GitHubCard.svelte#L139).
- **Problem:** Intro modal, welcome, step heading/paragraph, nested card and footer repeatedly explain setup, optional choices, local operation and saved progress. GitHub has several overlapping benefit statements.
- **Proposed:** Intro: “Connect your tools, then try your first task. Your progress is saved.” GitHub step: “Connect GitHub to find repositories and work with pull requests.” Keep “Work locally” as the explicit alternative. Remove duplicate surrounding benefits and the persistent completion reminder.
- **Acceptance:** One purpose statement per step, one progress-saving explanation in the flow, and local/later choices remain clear. Keep material capability and publishing consequences at the relevant decision.

### C02 — Personal setup invents a second name for tasks · P2

**Implementation status:** Completed in batch 1; the original observation and proposed wording are retained below.

- **Source:** [HelloFlow.svelte:180](../web/src/lib/setup/HelloFlow.svelte#L180), [tours.ts:33](../web/src/lib/setup/tours.ts#L33).
- **Problem:** First task becomes “Make a ticket”, followed by “A ticket is a task on your board.” Tours also call Personal items tickets. The explanation compensates for inconsistent naming.
- **Proposed:** “Create your first task”, “Create task and start agent”, “Open task”, “Press N for a new task.” Remove the synonym explanation. Preserve Ticket in Team.
- **Acceptance:** Personal setup, tours and Help use Task consistently; the Team-to-Personal handoff still explains why a Team ticket creates a local task.

### C03 — New-task settings repeat the permissions explanation · P2

**Implementation status:** Completed in the scheduling and everyday-work group; the original observation is retained below.

- **Source:** [NewTaskDialog.svelte:148](../web/src/lib/NewTaskDialog.svelte#L148), [ExecutionFields.svelte:182](../web/src/lib/ExecutionFields.svelte#L182), [policy.ts:41](../web/src/lib/policy.ts#L41).
- **Problem:** A permissions sentence appears above the settings disclosure, inside it, and again when an explicit interaction policy is selected.
- **Proposed:** Show “Permission requests still come to you” once beside Interaction. Retain the selected mode’s distinct explanation, including what autonomous investigation means.
- **Acceptance:** Default and explicitly selected policies each explain permissions once in the open dialog. No wording suggests autonomous mode grants broader execution permissions.

### C04 — Phone workflow guidance repeats setup mechanics · P2

**Implementation status:** Completed in the phone workflows batch; the original observation is retained below.

- **Source:** [WorkflowSettings.svelte:45](../web/src/lib/mobile/WorkflowSettings.svelte#L45), [footer:76](../web/src/lib/mobile/WorkflowSettings.svelte#L76), [DeckOnboarding.svelte:33](../web/src/lib/mobile/DeckOnboarding.svelte#L33).
- **Problem:** Saving versus running is explained at both ends of the configuration form; tap/hold/pencil/checkmark instructions repeat across onboarding and the final screen.
- **Proposed:** One configuration hint: “Save these defaults, then tap the workflow on your deck to run it.” Final onboarding: “Tap a workflow to run it. Hold to change its settings.” Put pencil/checkmark instructions in the editing step. Show keyboard guidance when useful without removing keyboard access.
- **Acceptance:** One instruction per current interaction. Saving remains clearly separate from executing, including when “Run once” is offered.

### C05 — Board cards restate their column’s default state · P3

**Implementation status:** Completed in the scheduling and everyday-work group; the original observation is retained below.

- **Source:** [board.ts:82](../web/src/lib/board.ts#L82), [TaskCard.svelte:153](../web/src/lib/board/TaskCard.svelte#L153), [Team card status:1401](../internal/team/console/static/console.js#L1401).
- **Problem:** Every neutral card adds “Not started”, “Available” or “Done” under a column that already communicates that state.
- **Proposed:** Omit plain duplicate state text within labelled columns. Keep exceptions, timestamps that matter, pending mutations, active runs, blockers, questions and stale-report qualifiers.
- **Acceptance:** A board remains understandable by column and accessible structure. Cards in search, lists or other standalone contexts retain explicit status.

### C06 — Attention language sounds unnatural and uses vague nouns · P2

**Implementation status:** Completed in batch 1; the original observation and proposed wording are retained below.

- **Source:** [Conversation.svelte:90](../web/src/lib/assistant/Conversation.svelte#L90), [Hub.svelte:33](../web/src/shell/Hub.svelte#L33), [Inbox.svelte:71](../web/src/shell/Inbox.svelte#L71), [deck.ts:43](../web/src/lib/mobile/deck.ts#L43), [DESIGN.md:323](../DESIGN.md#L323).
- **Problem:** “What needs me today?”, “Needs you”, “Nothing needs you” and “things wait for you” sound awkward. Phone accessible text can also produce “1 item need you.”
- **Proposed:** Prompt: “What needs my attention?” Attention heading: “Needs attention.” Empty state: “Nothing needs your attention.” Count: “1 item needs attention” / “3 items need attention.” A broader daily-work prompt can use “What’s on my plate today?”
- **Acceptance:** Grammar works at 0, 1 and many. Attention, workload and calendar scopes stay distinct. Update visible copy, accessibility text, Help and the design vocabulary together; retain old search aliases.

### C07 — “Assistant” ambiguously refers to Salie and coding agents · P2

**Implementation status:** Completed in batch 1; the original observation and proposed wording are retained below.

- **Source:** [HelloFlow.svelte:181](../web/src/lib/setup/HelloFlow.svelte#L181), [Conversation.svelte:177](../web/src/lib/assistant/Conversation.svelte#L177), [ConnectionsPanel.svelte:234](../web/src/lib/ConnectionsPanel.svelte#L234), [desktop/ui.go:98](../desktop/ui.go#L98).
- **Problem:** “Start the assistant”, “The assistant is turned off”, “Assistant” beside MCP permissions and “show the assistant” in file selection refer to different roles.
- **Proposed:** “Start agent” for task execution; “Salie is turned off” for the product assistant; “Salie” beside its MCP controls; “Choose files to share with Salie” for its picker.
- **Acceptance:** A user can distinguish Salie, the selected coding agent and a generic external MCP client. Do not rename provider output, protocol identifiers or internal APIs as part of copy cleanup.

### C08 — Salie decline and failure messages are awkward or ambiguous · P1

**Implementation status:** Completed in the recovery and saved-workspace group; the original observation is retained below.

- **Source:** [ConfirmCard.svelte:59](../web/src/lib/assistant/ConfirmCard.svelte#L59), [result states:74](../web/src/lib/assistant/ConfirmCard.svelte#L74), [Conversation.svelte:145](../web/src/lib/assistant/Conversation.svelte#L145).
- **Problem:** “Not now” performs a decline, while the result says “Not done. You said no.” Failure says “Nothing was changed by the attempt unless it says otherwise.” The former suggests deferral; the latter asks users to resolve contradictory possibilities.
- **Proposed:** Use “Decline” / “Decline all” for a terminal rejection; result “Declined.” Expiry: “This request expired. Ask Salie to propose it again.” Failure: lead with the operation and known outcome. Unknown outcome: “Could not confirm whether this was saved. Check [destination] before trying again.”
- **Acceptance:** Distinguish declined, expired, failed and uncertain states. Never promise “nothing changed” without evidence. Batch labels describe the decision applied to every proposal.

### C09 — Loading and generic empty labels do not name the operation · P3

**Implementation status:** Completed in the recovery and saved-workspace group; the original observation is retained below.

- **Source:** [Reviews.svelte:19](../web/src/lib/mobile/Reviews.svelte#L19), [Inbox.svelte:69](../web/src/shell/Inbox.svelte#L69), [Conversation.svelte:167](../web/src/lib/assistant/Conversation.svelte#L167), [Projects.svelte:91](../web/src/routes/Projects.svelte#L91).
- **Problem:** “Looking…”, “Getting ready…”, “Nothing here” and project status “quiet” make users infer what is being checked or why a list is empty.
- **Proposed:** “Loading reviews…”, “Loading workspace activity…”, “Preparing Salie…” and “No matching tasks.” Use a specific activity summary or omit “quiet” when it adds no information.
- **Acceptance:** Loading names a real operation. First use, filtered results, disconnected data and permission restrictions do not share a misleading generic empty state.

### C10 — Empty-state prose claims more than an empty list proves · P1

**Implementation status:** Completed in batch 1; the original observation and proposed wording are retained below.

- **Source:** [ProjectOverview.svelte:77](../web/src/routes/ProjectOverview.svelte#L77), [ControlCenter.svelte:171](../web/src/routes/ControlCenter.svelte#L171), [tours.ts:59](../web/src/lib/setup/tours.ts#L59), [MyWork.svelte:23](../web/src/lib/mobile/MyWork.svelte#L23).
- **Problem:** “Agents are working or waiting for work” is inferred from no attention items. The analytics tour says an empty chart means nothing has run; filters and missing measurements can also produce empty charts. “Yours” is a human/hybrid task subset, but its empty copy describes work “yours or shared.”
- **Proposed:** “Nothing needs your attention in this project.” Analytics: “Charts appear as activity is recorded. Change the filters to explore another period.” My Work: consider “For me to do” for the human/hybrid subset and “No unfinished tasks for you to work on.”
- **Acceptance:** Empty copy matches the actual query and known connection state. Preserve truthful active-agent counts where available. Do not infer execution, ownership or measurement completeness from absence.

### C11 — Analytics explains its accounting machinery in primary copy · P2

**Implementation status:** Completed in the analytics and recommendations batch; the original observation is retained below.

- **Source:** [Analytics.svelte:135](../web/src/routes/Analytics.svelte#L135), [completion section:201](../web/src/routes/Analytics.svelte#L201), [shell Analytics.svelte:130](../web/src/shell/Analytics.svelte#L130), [Team analytics:2250](../internal/team/console/static/console.js#L2250).
- **Problem:** “Bounded read”, “execution evidence”, “normal exit”, “completion signals” and “How far each signal reaches, compared on one count scale” require readers to translate internal measurement concepts. Several sections repeat the same limitations.
- **Proposed:** “Some history is unavailable; totals may be incomplete.” “Run finished does not mean task done.” Chart labels: “Created vs completed”, “Runs by agent”, “Review decisions” and “Recorded completion events” where those match the data. Put calculation methods and overlap details in Reporting details. For Team-only run views: “Agent usage is available in Personal. Choose Personal to view it.”
- **Acceptance:** Keep UTC/date scope, partial reporting, API cost versus estimates, missing values and different completion measures accurate. Do not add separate measures together or relabel a count as a rate.

### C12 — Settings repeatedly explain device scope and inherited defaults · P3

**Implementation status:** Completed in the settings, connections and project knowledge batch; the original observation is retained below.

- **Source:** [Settings.svelte:195](../web/src/routes/Settings.svelte#L195), [display sections:226](../web/src/routes/Settings.svelte#L226), [ProjectSettings.svelte:119](../web/src/routes/ProjectSettings.svelte#L119), [SettingsNative.svelte:28](../web/src/shell/SettingsNative.svelte#L28), [ExecutionFields.svelte:135](../web/src/lib/ExecutionFields.svelte#L135).
- **Problem:** “Follows your device unless you choose” and “Kept on this device” recur for related controls. “Same as project / defaults” and a paragraph about defaults saying otherwise make inheritance harder to parse.
- **Proposed:** State device scope once for the applicable settings group. Use “Use project defaults” or “Use app defaults”, with the effective value where it is useful. Project introduction: “Defaults for new runs in this project. Tasks can override them.”
- **Acceptance:** Do not imply all settings share storage scope. Browser-only and desktop-window choices remain accurate; saved explicit overrides are preserved.

### C13 — Agent settings surface implementation vocabulary before usefulness · P2

**Implementation status:** Completed in the settings, connections and project knowledge batch; the original observation is retained below.

- **Source:** [AgentSettings.svelte:31](../web/src/lib/AgentSettings.svelte#L31), [agent details:43](../web/src/lib/AgentSettings.svelte#L43), [Conversation.svelte:180](../web/src/lib/assistant/Conversation.svelte#L180).
- **Problem:** “Allow metered API fallback”, “Salie tool isolation not verified”, “Separate ACP adapter”, “Enable direct CLI availability” and “unknown access stays ineligible” expose implementation concepts in everyday choices. The no-agent explanation is also long and circular.
- **Proposed:** “Allow paid API usage when subscription usage is unavailable”, with one concise billing caveat. “Salie unavailable” with the reason in details. Billing option “Detect automatically.” Empty state: “Connect a coding agent to use Salie. Install and sign in to one below.” Keep technical adapter and verification details available where they affect setup.
- **Acceptance:** The user can tell what enabling a choice permits and whether it may cost money. No wording claims billing, sign-in or Salie compatibility has been verified when it has not.

### C14 — MCP setup explains the whole protocol before the immediate task · P2

**Implementation status:** Completed in the settings, connections and project knowledge batch; the original observation is retained below.

- **Source:** [ConnectionsPanel.svelte:145](../web/src/lib/ConnectionsPanel.svelte#L145), [form:164](../web/src/lib/ConnectionsPanel.svelte#L164), [MCPClientsPanel.svelte:146](../web/src/lib/MCPClientsPanel.svelte#L146), [credential setup:153](../web/src/lib/MCPClientsPanel.svelte#L153).
- **Problem:** Several introductory paragraphs repeat connection directions and setup instructions. “Transport”, “Environment references”, “loopback URLs” and “Create scoped client” are accurate technical terms but difficult primary instructions.
- **Proposed:** “Connection type” with Local process / Remote service; retain protocol names as secondary detail. “Environment variables” plus “Map the process variable to a variable already available to Werkbord.” CTA “Create client credential.” Show remote-address and stdio guidance only for that setup. Preserve the one-time credential warning.
- **Acceptance:** Both MCP directions remain explicit. Testing may launch a local server; concise copy must not imply tests are inert. Permissions, credential storage, environment-variable semantics and HTTPS requirements remain accessible before commitment.

### C15 — Memory screens use grants, publication and storage quotas as everyday nouns · P2

**Implementation status:** Completed in the settings, connections and project knowledge batch; the original observation is retained below.

- **Source:** [MemorySharing.svelte:45](../web/src/lib/MemorySharing.svelte#L45), [grant label:57](../web/src/lib/MemorySharing.svelte#L57), [ProjectMemory.svelte:116](../web/src/lib/ProjectMemory.svelte#L116), [Team memory:562](../internal/team/console/static/desktop.js#L562).
- **Problem:** “Workspace-bound grant”, “Memory publication”, “source observations per compacted record” and a leading explanation of 10,000 memory revisions make sharing and remembering sound like database administration.
- **Proposed:** “Team connection key” with “Use this key only for this Team workspace.” “Shared project notes” as explanatory copy for memory. Move quota mechanics into storage details; show capacity warnings when relevant. “Showing up to 64 saved sources” in source inspection if that accurately describes the bound.
- **Acceptance:** Preserve separate local approval, Team approval and publication states. Keep revocation acknowledgements and retained-copy limitations. Do not imply that turning sharing off deletes copies already read or backed up.

### C16 — Repository relationships ask users to understand identity plumbing · P2

**Implementation status:** Completed in the settings, connections and project knowledge batch; the original observation is retained below.

- **Source:** [ProjectRelationships.svelte:44](../web/src/lib/ProjectRelationships.svelte#L44).
- **Problem:** “Canonical remote identities”, “Source repository identity”, “Target repository identity” and “current review” explain a data contract instead of the dependency the person is recording.
- **Proposed:** “Link related repositories to suggest compatibility checks.” Use “Repository that depends on another” and “Repository it depends on” for directional dependency types. For a non-directional association, use type-appropriate labels. Explain the required `github.com/organisation/repository` format once beside the identity input.
- **Acceptance:** Preserve exact repository identity validation and direction. Manual links must not be described as proven breakages or dependencies unless the selected type says that.

### C17 — Team action labels and ticket explanations are unnecessarily conversational · P2

**Implementation status:** Completed in the Team actions batch; the original observation is retained below.

- **Source:** [console.js:1265](../internal/team/console/static/console.js#L1265), [ticket actions:1604](../internal/team/console/static/console.js#L1604), [agent explanation:1653](../internal/team/console/static/console.js#L1653), [dates:1709](../internal/team/console/static/console.js#L1709).
- **Problem:** “I merged it: record that”, “Take it back”, “Nobody yet”, “What it does, asks and writes stays private” and “The Calendar and the Timeline draw these dates” are wordy or imprecise.
- **Proposed:** “Record merge” with “Use after merging on your Git host”; “Unassign”; “Unassigned”; “Agent activity is private to [name]”; “Shown in Calendar and Timeline.” Explain Claim as “Assign this ticket to yourself” once where it is introduced.
- **Acceptance:** Record merge still records an externally performed merge. Unassign, Release, Claim and Request changes retain their separate effects and permissions.

### C18 — Team setup and handoffs repeatedly teach the trust architecture · P2

**Implementation status:** Completed in the Team actions batch; the original observation is retained below.

- **Source:** [AddTeam.svelte:128](../web/src/shell/AddTeam.svelte#L128), [desktop.js:371](../internal/team/console/static/desktop.js#L371), [private controls:501](../internal/team/console/static/desktop.js#L501), [native setup:201](../desktop/internal/shell/workspaces.go#L201), [runner consent:426](../desktop/internal/shell/workspaces.go#L426).
- **Problem:** Creation, native setup, runner consent, ticket handoff and private controls repeatedly explain whose computer, credentials and approvals are involved. “Effective policy”, “Untrusted task context” and “exact one-shot authorization” add technical vocabulary.
- **Proposed:** Keep installation/admin-password facts at installation, and runner permissions/revocation at runner consent. At a ticket: “Run this ticket on your computer using your agent.” Review labels: “Permissions for this run” and “Task instructions”, with a short note that task text does not grant permissions. “Allow this run once, within 30 days.”
- **Acceptance:** Keep explicit consent boundaries and per-device distinctions. Do not remove confirmation because a preceding screen explains it. The user must still know where work runs, which permissions apply and how to revoke access.

### C19 — Planning and scheduling repeat long conceptual explanations · P2

**Implementation status:** Completed in the scheduling and everyday-work group; the original observation is retained below.

- **Source:** [TaskPlanFields.svelte:35](../web/src/lib/TaskPlanFields.svelte#L35), [ScheduleFields.svelte:131](../web/src/lib/ScheduleFields.svelte#L131), [TaskPanel.svelte:637](../web/src/routes/TaskPanel.svelte#L637), [scheduling.ts:3](../web/src/lib/scheduling.ts#L3), [Team schedule:1959](../internal/team/console/static/console.js#L1959).
- **Problem:** Each form reteaches Timeline versus Calendar. “One-shot schedule dispatched”, “rearm”, “Waiting for Capacity”, “Potential Conflict”, and “Only the holder’s local approval can authorize execution” make routine timing controls sound operational.
- **Proposed:** Planned dates: “Plan the work. These dates do not start an agent.” Scheduled start: “Start the agent once, when the requirements are met.” Statuses: “Waiting for a runner” only where runner capacity is the actual constraint; “Waiting for capacity” for project limits; “May change the same files” for detected overlap. Use D05’s explicit schedule-another-run action after dispatch.
- **Acceptance:** Do not conflate project and runner capacity, dependencies and priority, planning and execution, or a requested Team schedule and local approval. Ensure the task’s rearming instructions match the actual control.

### C20 — Run and Git copy repeatedly explains safeguards and internal evidence · P2

**Implementation status:** Completed in the scheduling and everyday-work group; the original observation is retained below.

- **Source:** [RunHandoff.svelte:148](../web/src/lib/RunHandoff.svelte#L148), [continuation copy:291](../web/src/lib/RunHandoff.svelte#L291), [RunReviewSummary.svelte:39](../web/src/lib/RunReviewSummary.svelte#L39), [RunUsage.svelte:9](../web/src/lib/RunUsage.svelte#L9), [PushSheet.svelte:54](../web/src/lib/git/PushSheet.svelte#L54), [PRSheet.svelte:58](../web/src/lib/git/PRSheet.svelte#L58), [CleanSheet.svelte:49](../web/src/lib/git/CleanSheet.svelte#L49).
- **Problem:** “Git evidence”, “bounded diffs”, “execution ownership retained”, repeated promises that agents never merge, and prose about reading GitHub back distract from the result. “Tests reported: N notes · not verified” reports note-count rather than test outcome.
- **Proposed:** “Update Git details”; “Starts a new conversation in this task’s worktree, using its saved summary and changes”; “Runner offline. This run still belongs to it.” “Agent-reported tests · not independently verified”, with notes below. Push hint: “Push without force. The push is refused if the remote has newer commits.” Worktree removal: “Removes this worktree folder. The branch and commits are kept.”
- **Acceptance:** Keep branch/target/SHA evidence and live blockers where needed. Preserve uncommitted-work protection, reported-versus-verified test facts and remote ownership. Move implementation explanations into optional details instead of removing decision-critical facts.

### C21 — Generated workspace copy exposes implementation and taxonomy · P2

**Implementation status:** Completed in the recovery and saved-workspace group; the original observation is retained below.

- **Source:** [WorkspaceManagement.svelte:76](../web/src/lib/personal/WorkspaceManagement.svelte#L76), [deletion/history:87](../web/src/lib/personal/WorkspaceManagement.svelte#L87), [PersonalWorkspace.svelte:140](../web/src/routes/PersonalWorkspace.svelte#L140), [PersonalLibrary.svelte:61](../web/src/lib/personal/PersonalLibrary.svelte#L61).
- **Problem:** Explaining Lucide’s icon matching does not help users choose an icon. “Definition history”, “exclusively owned personal data”, “workspace references” and keeping a workspace URL are implementation-oriented instructions.
- **Proposed:** “Choose another icon”; remove the matching-algorithm paragraph. “Layout history” with “Restore the interface; current records are kept.” “Used by N workspaces.” Deletion should name the actual collections and record counts, then distinguish shared/protected data. Where a saved workspace is discoverable, point to its real navigation or Data library instead of requiring URL memorisation.
- **Acceptance:** History copy must mention restored names/icons/locations where relevant. Preserve exact deletion scope, protected shared collections and confirmation. Verify a reliable reopening route before removing the URL fallback.

### C22 — Workflow monitor uses a recovery label even when no retry is needed · P2

**Implementation status:** Completed in the phone workflows batch; the original observation is retained below.

- **Source:** [WorkflowMonitor.svelte:42](../web/src/lib/mobile/WorkflowMonitor.svelte#L42), [footer:68](../web/src/lib/mobile/WorkflowMonitor.svelte#L68).
- **Problem:** “Open task / retry” always appears, including for active and completed runs, and only navigates. “Change Settings” leads to general app settings. Several stacked recovery paragraphs can repeat that output/results remain available.
- **Proposed:** “Open task” for the navigation action. Describe any retry inside the destination based on actual state. “Open app settings” only where relevant; do not imply it edits this workflow. Combine failure/stop guidance into one state-specific message. “No update reported yet” is clearer than “No execution event reported for this stage.”
- **Acceptance:** Running, completed, failed, stopped and stop-pending states show accurate next steps. Opening the task never promises or triggers a retry. Preserve uncertainty and partial-result information.

### C23 — Phone introductions overexplain capabilities and repeat read-only status · P3

**Implementation status:** Completed in the phone workflows batch; the original observation is retained below.

- **Source:** [PhonesPanel.svelte:99](../web/src/lib/phones/PhonesPanel.svelte#L99), [PhoneEnroll.svelte:82](../web/src/lib/phones/PhoneEnroll.svelte#L82), [Team phone index.html:16](../internal/team/phone/static/index.html#L16), [phone.js:5](../internal/team/phone/static/phone.js#L5).
- **Problem:** Phone settings list nearly every capability, then explain sign-ins and computer-only administration. Team first use repeats read-only status in both its header and a long tour paragraph. “Read-only, on purpose” sounds defensive.
- **Proposed:** “Add a phone to manage your work remotely. You can revoke its access here.” Keep the meaningful access scope available beside pairing. Team header: “Team summary · read-only.” Tour: “View Team updates here. To change tickets or run agents, open Werkbord on your computer.”
- **Acceptance:** Do not imply Team phone access can write or execute. Explain permissions before pairing without a full feature catalogue; preserve expiry, single-use and revocation facts.

### C24 — Help reads as several simultaneous layers of instruction · P2

**Implementation status:** Completed in the Help batch; the original observation is retained below.

- **Source:** [Help.svelte:23](../web/src/routes/Help.svelte#L23), [guide body:45](../web/src/routes/Help.svelte#L45), [help.ts:20](../web/src/lib/help.ts#L20).
- **Problem:** Every guide renders summary, example, numbered steps, recovery text and destination at once. The introduction promises “words and safeguards shown in the app,” which describes documentation methodology rather than helping the reader. The MCP guide combines both directions and several transports into long steps.
- **Proposed:** Intro: “Help with tasks, agents, Git and connections.” Show title and one-line purpose in the guide list; reveal steps on selection. Keep examples and recovery details available within the selected guide. Split MCP instructions by “Use a service” and “Connect a client.”
- **Acceptance:** Help remains searchable, keyboard accessible and linkable by article. Revised instructions match current labels and destinations; important recovery advice remains available when needed.

### C25 — Broad reassurance and tour claims need narrower factual wording · P1

**Implementation status:** Completed in batch 1; the original observation and proposed wording are retained below.

- **Source:** [Projects.svelte:111](../web/src/routes/Projects.svelte#L111), [tours.ts:28](../web/src/lib/setup/tours.ts#L28), [FilesView.svelte:134](../web/src/lib/personal/views/FilesView.svelte#L134), [Conversation.svelte:170](../web/src/lib/assistant/Conversation.svelte#L170).
- **Problem:** A work project says “Nothing is read from or written to your disk” despite being saved application data. The tour says every project is a repository, excluding work projects. File copy says “this computer” even though storage can be on the viewing device. An offline message speculates that a restart will usually resolve in seconds.
- **Proposed:** “A board and timeline for work without a Git repository.” Tour: “Each project has its own board.” File view: “Files added here are stored on this device”; scope any no-upload claim to that feature. Offline: “Werkbord is not responding. Check that it is running, then try again.”
- **Acceptance:** Reassurance is tied to a verified feature/state. No blanket guarantees about disk writes, all projects, all file sharing or recovery timing. Check desktop and phone storage semantics before finalising copy.

### C26 — Recommendation and health wording can sound more certain or judgmental than its evidence · P2

**Implementation status:** Completed in the analytics and recommendations batch; the original observation is retained below.

- **Source:** [recommend.ts:48](../web/src/lib/recommend/recommend.ts#L48), [RecommendationCard.svelte:77](../web/src/lib/recommend/RecommendationCard.svelte#L77), [health_rules.go:897](../internal/domain/health_rules.go#L897), [inactive branch finding:965](../internal/domain/health_rules.go#L965).
- **Problem:** “Everything here is verified and current” is a sweeping confidence statement. “The evidence is thin”, “What it rests on”, “looks abandoned” and “Keeping it only adds noise” are vague, informal or judgmental. The detailed explanation may qualify an inference after its title has already overstated it.
- **Proposed:** “Based on checked project data” where supported; “Limited supporting data”; “Sources.” Health title: “Inactive branch with unmerged commits”, with the measured inactivity in the detail. Merged branch detail: “All commits are in [target]. You can remove this branch if you no longer need it.”
- **Acceptance:** Keep deterministic facts, inferences and preferences distinguishable in the title and details. Preserve timestamps, unknown sources and user choice; recommendations do not acquire authority to execute anything.

## 6. Traceability to the previous report

| Earlier finding | This report |
| --- | --- |
| 1. First-task start after completion | D01 |
| 2. Pairing after phone connection | D02 |
| 3. Duplicate Check again | D03 |
| 4. Previous Team start control during review | D04 |
| 5. Rearm action on new schedules | D05 |
| 6. Keep saved task versus Close | D06 |
| 7. Duplicate workspace reload | D07 |
| 8. Duplicate preview exits | D08 |
| 9. Overlapping next-run forms | D09 |
| 10. Repeated onboarding explanations | C01 |
| 11. Task/ticket inconsistency | C02 |
| 12. Repeated permissions explanation | C03 |
| 13. Repeated workflow guidance | C04 |
| 14. Column/card status repetition | C05 |

## 7. Implementation batches

Use one batch per Codex task or small PR. Re-read the current code before editing: references are pinned to this report’s baseline and may move. Establish vocabulary first so later batches do not rewrite the same strings twice. D09 is the largest interaction change and should be handled separately.

| Order | Batch | Findings | Completion evidence |
| --- | --- | --- | --- |
| 1 | Vocabulary and factual accuracy | C02, C06, C07, C10, C25 | Consistent terms; singular/plural checks; accurate empty/filtered/offline states and scoped claims; design/Help references aligned |
| 2 | Setup stages and onboarding copy | D01, D02, D03, C01 | Before/after stage walkthrough, including revisits and pre-existing setup |
| 3 | Recovery and outcome language | D06, D07, C08, C09 | Saved/failed/uncertain paths and load/render errors verified; one appropriate recovery action |
| 4 | Scheduling and planning | D05, C19 | New/disabled/armed/used schedule states; approval and dependency behaviour preserved |
| 5 | Team start, handoff and ticket wording | D04, C17, C18 | Local/device consent intact; preparation replaced by policy review; Team actions correctly named |
| 6 | Phone workflows and summaries | D08, C04, C22, C23 | Phone viewport review of onboarding, preview, execution and read-only summary |
| 7 | Everyday task and Git copy | C03, C05, C20 | Context-specific board statuses; one permission explanation; accurate test/merge/push/worktree copy |
| 8 | Agent and app settings | C12, C13 | Storage scope, inheritance, billing and compatibility meanings retained |
| 9 | Connections and project knowledge | C14, C15, C16 | MCP directions, credentials, memory approvals/revocation and relationship direction understandable |
| 10 | Analytics and recommendations | C11, C26 | Accurate counts, units, missing-data warnings, confidence/inference wording and chart labels |
| 11 | Saved workspaces and Help | C21, C24 | Actual reopening routes, deletion scope and history effects clear; guides match implemented labels |
| 12 | Next-run interaction consolidation | D09 | Explicit worktree/context/conversation choices and appropriate regression coverage |

### Reusable Codex task prompt

```text
Implement batch [NUMBER / NAME] from docs/UI_UX_DRY_AND_COPY_REVIEW.md.
Scope: [FINDING IDS].

Read AGENTS.md and the relevant report entries. Re-check each observation against
the current source before editing. Treat proposed copy as editorial guidance;
preserve the actual state, destination, permissions and outcome semantics.

Keep the existing visual identity. Make only the UI, copy and directly required
state-presentation changes for this batch. Do not refactor unrelated backend
processes or alter execution, approval, credential or product trust boundaries.

For each finding:
- implement the smallest coherent improvement;
- keep task/agent/Salie terminology and accessible names consistent;
- update relevant Help, tours and label-sensitive assertions;
- verify the listed acceptance states, including errors and uncertainty;
- keep a fact visible when the user needs it to decide or recover.

Use focused tests for meaningful state or behavioural changes. Do not add tests
that merely repeat static copy. Run the relevant existing checks; follow AGENTS.md
for completion and commit policy. Do not bump the version, tag or push.

Report the finding IDs addressed, final wording/behaviour, validation performed,
and any unresolved item with its concrete reason. Update only those findings'
implementation status in the report, retaining their original observations.
```

### Completion checklist for each batch

- [ ] The relevant acceptance states were inspected or tested; limits are recorded.
- [ ] The same action has one clear label throughout that interaction.
- [ ] Completed or inapplicable stages no longer invite repetition.
- [ ] Each visible explanation adds information that changes understanding or action.
- [ ] Visible labels, tooltips, accessible names, loading/error announcements and Help agree.
- [ ] Dynamic copy works at zero, one and many, with long project/team names.
- [ ] A phone width and a desktop width remain readable without hiding essential consequences.
- [ ] No unknown outcome is described as a confirmed failure or success.
- [ ] Relevant checks pass and implementation changes are committed under AGENTS.md.

## 8. What to preserve

Useful repetition is contextual, not automatically a defect. Keep status on standalone cards; a task title when context would otherwise be lost; accessible control names; a local privacy/permission explanation at the point of consent; and a final confirmation for a consequential operation.

Retain the distinctions between agent completion, review assessment, board status and merge; saved versus delivered answers; disconnected versus stopped runs; local changes versus pushes; and planning dates versus execution schedules. Shortening these into a single reassuring sentence would make the app less clear.

The existing Git result dialogs and Salie proposal cards already remove their execution actions after completion. Use that state progression as a model when addressing the duplicated stage controls.
