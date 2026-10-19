# Suggested next steps: how Werkbord decides what to suggest

Werkbord can suggest what to do next on a project: continue unfinished work, get a finished branch reviewed or merged, look at
repository findings, run Find Bugs or Refactor Codebase, or pick up the next task. At most **two** suggestions are shown,
each with the evidence it stands on and the rules that shaped it. A suggestion is **advice**. It opens a screen that already
exists and asks the person itself; it starts nothing, approves nothing, creates no branch, pushes no commit and merges no
pull request.

This is the second half of the assistant's memory work ([Phase 3A](ASSISTANT.md#phase-3a-salies-memory-context) gave Salie
controlled access to memory; this decides what to do with it). There is no second agent system: no new process, runner,
model or approval flow. The decision is a pure function of facts the application already has; a model, where one is
involved, only narrates the answer.

```
 facts (read now)                       rules (internal/domain/recommend_rules.go)            what is shown
 ───────────────                        ──────────────────────────────────────────            ─────────────
 board, runs, deck history  ─┐          1. tiers 1–3 decide what is ALLOWED                    ≤ 2 cards
 Git state, pull requests   ─┤          2. tier 4 (verified state) builds candidates           each with
 health findings            ─┼────────▶ 3. tier 5 (habits, feedback) ORDERS the survivors ───▶  evidence, the rules
 saved memory (policies,    ─┤             and picks between ways that are both allowed        that applied, and a
   decisions, preferences)  ─┤                                                                 button that OPENS
 who is asking, what they   ─┘                                                                 an existing screen
 may read, what is online
```

## The order that cannot be broken

| Tier | What it is | What it can do to a suggestion |
| --- | --- | --- |
| 1 | Security and application-enforced restrictions: what this viewer may read, whether a runner is online, whether the repository is usable | forbid |
| 2 | Explicit Team policies and repository protection (a Team link, a saved policy, GitHub asking for review) | forbid, require, explain |
| 3 | Explicit user decisions (saved decisions) | forbid more; **order** the ways that tiers 1 and 2 left |
| 4 | Current, verified Git and task evidence | create a candidate, rank it |
| 5 | Historical preferences, observed habits and earlier feedback | **order** candidates and choose between allowed variants |

Tiers 1 to 3 can only forbid, require or explain. Tiers 4 and 5 can only order what they left standing. The consequence is the
one the feature rests on: **a remembered habit can pick between two allowed ways of working, and it can never add a way that a
restriction removed.** Nothing observed is read as permission: there is no rule that says "they did it before, so it is
allowed". A way of working is available only because nothing in tiers 1 to 3 forbids it.

`TestHistoryCanOnlyOrderWhatTheRestrictionsLeftStanding` checks this over 400 random combinations of habits, saved
preferences and feedback against fixed restrictions: the set of forbidden routes never changes, and no forbidden route is ever
suggested.

## The decision, step by step

1. **Gather the facts**, bounded and read now (`internal/service/recommend.go`): the project and its tasks, runs and deck
   history; the repository as the Git Control Center reads it; pull requests as GitHub reports them (asked at most once a
   minute); stored health findings; saved memory through the same gate the assistant uses
   (`Knowledge.Retrieve`: current, private, non-duplicate records whose repository and run evidence still check out,
   policies and decisions first). A source that cannot be read is marked **unavailable** and reported. It is a gap, never proof
   of nothing, and never a reason to allow something.
2. **Derive the restrictions** (`deriveRules`): everything tiers 1 to 3 forbid, per intent and per variant, with a stable code,
   the tier, and the record that caused it.
3. **Build candidates from tier 4.** For each intent that is not forbidden, look for evidence; a forbidden one is recorded as
   *withheld* with its reason. For finished branch work, choose a variant (below).
4. **Order with tier 5.** Feedback and habits adjust scores by a bounded amount (±15) and choose between variants that are
   both allowed. A dismissed suggestion stays hidden until something about it changes; a kind found not useful three times
   more than useful goes quiet unless it is *required*.
5. **Select**: required suggestions first, then by score; at most two; one per intent; one per task or branch; at most one
   workflow start.

The same facts always give the same answer. The rules read no clock, no file, no network and no model
(`TestTheRulesForSuggestionsArePure`).

### Restrictions

| Tier | Code | Effect |
| --- | --- | --- |
| 1 | `no_repository` | a work project has no branches or repository workflows to suggest |
| 1 | `repository_not_permitted` | the viewer may not read repository state: none is suggested |
| 1 | `repository_unreadable` | the repository could not be read (moved, missing, slow). No path or raw error is shown |
| 1 | `unborn_repository` | no commits yet |
| 1 | `detached_head` | no direct merge, no refactor from a detached checkout |
| 1 | `operation_in_progress` | a merge or rebase is unfinished: no new Git work until it is; "finish it" comes first |
| 1 | `target_missing` | no local main branch to merge into: no direct merge |
| 1 | `no_runner` | no online runner can take a workflow |
| 1 | `workflow_running` | that workflow is already running |
| 1 | `tasks_not_permitted` | the viewer may not read tasks |
| 2 | `team_pull_request` | the project's tasks came from a Team workspace: the pull request route only, never a direct merge |
| 2 | `team_policy_unread` | Team policies could not be read here: the pull request route is assumed |
| 2 | `repo_requires_review` | GitHub shows a review requirement on this repository (a pull request has a review decision) |
| 2/3 | `policy_integration`, `decision_pull_request` | a saved policy or decision concerns how changes reach main: no direct merge |
| 2/3 | `policy_freeze`, `decision_freeze` | no direct merge, no refactor; a pull request is still prepared for review |
| 2/3 | `policy_no_refactor`, `decision_no_refactor` | no Refactor Codebase |
| 2 | `policy_no_agents` | no agent workflows |

### How saved policies and decisions are read

Policies and decisions are prose. Werkbord does not understand prose and does not try to. It looks for a small set of plain
phrases (`internal/domain/recommend_memory.go`), and what it finds can only **restrict** or **explain**:

* A saved **policy** that touches how changes reach the main line (merge, push, release, review, pull request, protected
  branch, CI, approval…) removes the direct-merge shortcut. This is deliberately over-cautious: the cost of being wrong the
  other way is a merge a policy forbade; the cost of caution is one extra pull request.
* A saved **decision** restricts only when it says so (it names pull requests or review, or says not to commit directly). A
  decision that says changes go straight to main orders the ways that are already allowed; it allows nothing.
* A policy marked *freshness uncertain* by Team is still honoured as a restriction (restrictions fail closed) and labelled so.
* A policy that says "direct pushes are allowed" does **not** lift anything. There is no phrase that permits: a restriction is
  lifted only by there being none.
* Preferences, verified facts and unverified suggestions never restrict and never permit. They only order.

### Team

An Individual project is Team-linked when its tasks were imported from a Team workspace (their provenance reference starts with
`werkbord-source:v1:`). Such a repository gets the pull request route, always. The controller holds no Team credential, so Team
policies reach the rules in one place only: the desktop assistant, which reads them through the reviewed `retrieve_memory`
bridge (Team rechecks membership, project and classification on every read) and applies the same project allow-list as
`retrieve_memory`. Everywhere else the Team memory status is `not_checked` and the rules say so and assume the pull request
route. Team memory can only add restrictions and explanations; it cannot loosen the Team-linked rule.

A project that lives only in a connected Team workspace has no repository on this computer. The assistant suggests from its
tickets and the workspace's policies (next task, what is in flight) and never reads a repository for it.

## What is suggested, and on what evidence

| Intent | When | Opens | Variants |
| --- | --- | --- | --- |
| Continue | an unfinished merge/rebase (required); an agent waiting for an answer (required); a stopped run on a task in Doing/Review; a task in Review; uncommitted work (on a protected main: "move your changes off main", required) | the changes, or the task | – |
| Branch or pull request | a finished Werkbord branch with commits the target lacks and nothing running on it; or an existing pull request that needs changes, has failing checks, conflicts, or is approved and ready | the branch screen, where push, pull request and merge are confirmed | direct merge, pull request |
| Repository issues | open health findings of attention level or above | the Git screen | – |
| Find Bugs | never scanned and history exists; last scan failed; 8+ commits since the last scan, or 3+ and the scan is 21+ days old | the Control Deck, at the workflow's key | – |
| Refactor Codebase | a refactor task in the backlog, or saved notes about duplication or debt, **and** a quiet repository (clean checkout, at most two open branches or pull requests, no overlapping branches) | the Control Deck | – |
| Next task | the first ready task in Backlog (dependencies finished), planned dates first; withheld at three tasks in Doing | the task | – |

### Choosing the way a branch goes in

When both a pull request and a direct merge are allowed, the choice is made by tiers 3 and 5 only, in this order: an explicit
decision for a direct merge (+100), the observed habit (+25 for a direct merge: 5+ of the last 30 changes on the target landed
without a pull request, two thirds or more, fewer than 3 merged pull requests; +15 for a pull request when most arrived through
one), a saved preference (+15 for a direct merge, +10 for a pull request), earlier feedback (±15). A pull request starts ahead by 10
(it keeps the change reviewable), so with nothing saved a project with a remote gets a pull request. The habit is reported
as such ("9 of the last 12 changes on main were committed directly") and the card says the suggestion is *from how you work*.

* **A personal project that is usually committed straight to main**, with no policy, no Team link and no review requirement:
  the direct merge is suggested. The button opens the branch screen; the merge there is planned and checked again, and asks for
  confirmation, exactly as without a suggestion.
* **A Team-linked project, or one with a saved policy about review and protected branches**: only the pull request route is
  suggested, whatever the habit, with the policy's requirements named ("Review and passing checks are required.") and the
  open pull requests ahead of anything new.

## What a suggestion can do

Every suggestion carries one action, and every action is an `open_…` kind: a task, a branch, the changes, the Git screen, the
Control Deck. `startsExecution` is `false` for every one of them and the tests hold it there. The button opens the screen; the
confirmation (merge plan, pull request sheet, the deck key and its approval sheet) is that screen's own, unchanged. A
workflow's key is **marked** on the Control Deck (a dashed ring) but never pressed. If it is not on the deck, the ordinary
Edit Deck library opens so the person can add it.

`internal/archtest/recommend_test.go` keeps the shape: the rules import no I/O; the code that gathers facts, the assistant
operation and the HTTP handlers call nothing that pushes, merges, creates a branch, pull request, task or run, answers a
question, starts a workflow or runs a process.

## Where it appears

* **Salie.** `recommend_next_actions` is a read (`memory:read`, no new permission). Evidence follows the principal's own
  grants: tasks need `tickets:read`, runs `runs:read`, repository and pull requests `projects:read`; what is not permitted is left
  out and reported as `not_permitted`. A session limited to some projects gets no personal memory. The result reaches the screen
  as cards built from the controller's own answer, so a model cannot add an action or reword a restriction. The model is
  told to say which suggestion matters most and why in a sentence or two, to offer only what the result contains, to
  mention a standing restriction when it matters, and to say when a source was a gap. When Salie opens with nothing yet said, it
  shows what the rules would suggest for the project last worked on.
* **The Control Deck** (phone and desktop app), under the keys: the suggestions for the current project, and the suggested
  workflow's key marked. The nine keys and their layout are unchanged.
* **Git, Overview**, at the top of the left column; silent when there is nothing.

A card says what kind of suggestion it is, why it was made (*needed first*, *from current state*, *from how you work*), what
it will do and who decides, and has **Why this**: what shaped it (restrictions, policies, decisions, strongest first), what it
rests on (each fact with its source and record, and whether it is verified), and how complete the evidence was.

## Feedback

*Useful*, *Not useful* and *Dismiss* are on every card. They adjust the order of later suggestions, nothing else.

* *Dismiss* and *Not useful* hide that exact suggestion for 14 days. Its id is a fingerprint of the intent, the variant and the
  facts under it, so it returns if something real changes (more commits, a new review, a different branch).
* Where the controller serves memory (the desktop, the owner's browser) the verdict is kept in **Individual memory** as
  one record per project, intent and variant: type `recommendation_feedback`, authority `inferred_preference`, private,
  unverified, **not eligible for sharing**, with counters (capped at 1,000) and at most eight hidden ids. Repeated taps update
  the one record. It is shown in the project's memory list like any other record and can be archived or deleted there to reset
  it. It never reaches the assistant's memory context (it is not current, verified knowledge), is never a policy or a decision,
  and cannot be published to Team except by the person going through the ordinary review with a statement of their own wording.
* A phone and a connected Team workspace's project keep the verdict **on the device only** (see below).
* A kind that has been found not useful three more times than useful goes quiet. A *required* suggestion (a half-finished
  merge, an agent waiting) is never silenced by feedback.

## What a connection that does not serve memory sees

The private network, used by an enrolled phone, does not serve Individual memory (`Options.Memory` is nil there), and a phone is
not a place memory is read or written wherever it connects from. Suggestions are served to it so the Control Deck can show them,
with the **words of saved memory taken out**: a constraint says that a
saved policy applied, which record, and what it did to the suggestion, not what it said. Feedback is refused there (the route
is not on the phone's list, and memory is not served), and the phone keeps its dismissals on the device. The list of routes a
phone may use gained exactly one: `GET /api/projects/{}/recommendations`.

## HTTP API

| Method and path | Purpose |
| --- | --- |
| `GET /api/projects/{pid}/recommendations?focus=` | The suggestions for a project: `items` (at most two), `standing` (the restrictions in force), `withheld` (what was considered and why it was not made), `sources` (what could be read), `notes`, `feedbackStoredIn` (`memory` or `device`) |
| `POST /api/projects/{pid}/recommendations/feedback` `{recommendationId, intent, variant, verdict}` | Record a verdict (`useful`, `not_useful`, `dismissed`). Local controller only; unknown fields, kinds and verdicts are refused |

`recommend_next_actions {projectId, focus?}` is the assistant operation. The model chooses the project and at most one kind of
suggestion. It cannot choose who the viewer is, what they may read, whether personal memory is included, or what counts as a
policy; unknown arguments are refused.

## Not in this stage

No suggestion starts anything, creates anything or approves anything, and none is made by a model. The rules understand a
small vocabulary and nothing more; a policy phrased in terms they do not recognise changes nothing, except that a policy which
touches how changes reach main (a broad test) removes the direct-merge shortcut. Branch protection that exists only on GitHub
and is not visible in a pull request's review state is not known. Team policies are read only by the desktop assistant. The
assistant's own "touch no Git" guarantee is unchanged: it can read this bounded projection (names and counts, no paths, no
diffs) and has no operation that changes a repository.

## Tests

| What | Where |
| --- | --- |
| The rules: the individual and the Team developer, habit versus policy, decision versus policy, every restriction, invalid Git states, empty and unavailable memory, ranking, bounds, feedback, determinism, text from other people, and the ordering guarantee | `internal/domain/recommend_test.go` |
| The facts, against real Git repositories: branches, pull requests (fake `gh`), tasks, deck history, Team provenance, memory gate, permissions, nothing changed anywhere, feedback and its bounds, redaction | `internal/service/recommend_test.go` |
| The operation: read-only, audited, scoped, permission-narrowed, no new arguments, Team policies through the desktop bridge, a connected workspace project | `internal/appops/recommend_test.go` |
| The assistant: the rules' own answer reaches the screen; third-party text cannot break out; nothing is proposed | `internal/assistant/recommend_test.go` |
| HTTP: token, redaction on a connection without memory, feedback validation and storage, the phone | `internal/api/recommend_test.go`, `internal/api/phone_test.go` |
| Source-level boundaries | `internal/archtest/recommend_test.go` |
| The screens in a real browser: Git overview (both themes), the phone's Control Deck (nine keys unchanged, key marked and never pressed, verdicts on the device, no overlap with the bottom bar), Salie (empty state and reply), feedback in memory, nothing started | `make test-recommendations-browser` (`scripts/browser-recommendations.cjs`) |
| The screen: destinations, labels, on-device hiding, the store, the transcript | `web/src/lib/recommend/*.test.ts`, `web/src/lib/assistant/transcript.test.ts` |
