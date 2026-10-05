# Repository health

The question this answers is: **"I have had several coding agents working all day. What Git state now needs
my attention?"** It is a list of *findings*, each saying what is wrong, why, the evidence, and the next
useful step, and whether Werkbord can take that step. It is not a score. A score exists, but it is a
footnote: a number cannot say what is wrong.

Code: `internal/domain/health.go` (the finding entity, severities, thresholds, lifecycle, the rule registry),
`internal/domain/health_rules.go` (the rules: pure functions), `internal/service/githealth.go` (gathers facts,
keeps findings' lives, watches for events), `internal/api/githealth.go` (HTTP),
`internal/store/sqlite/health.go` and migration `0007` (persistence), `web/src/lib/health.ts` and
`web/src/lib/git/HealthPanel.svelte`, `FindingCard.svelte` (the screen), and the Control Center
(`internal/service/control.go`, `web/src/routes/ControlCenter.svelte`).

---

## 1. Principles

1. **Findings, not a score.** The Git screen leads with `Healthy`, or with what is wrong: for example
   `1 risk · 3 items need attention`. A score (0-100) is in the report and shown small; it never ranks or
   hides anything.
2. **Be conservative.** A rule fires on what its evidence supports and no more. "These branches change the
   same files and may conflict" is what file lists support. "These branches conflict" is said only when
   Git's own in-memory merge says so. A test fails any heuristic finding that says "will conflict".
3. **Say how sure.** Every finding has a `basis`: **deterministic** (a fact Git or Werkbord's records state
   outright) or **heuristic** (a reasoned guess, worded as a possibility). The screen shows which
   ("Git says so" / "A guess"). Section 3 lists every rule's basis.
4. **Do not become noise.** Quiet is the default. See §7, which lists what is deliberately *not* reported.
5. **Cheap, and no model.** Health uses Git metadata and Werkbord's own records. It never calls Codex or
   Claude, never touches the network, and never changes the repository. See §5.
6. **Never act.** A finding recommends a step. Nothing is executed by the health system. A button opens the
   same confirmation sheet a person would reach from the branch list, and that operation checks everything
   again when confirmed. Destructive steps (delete a branch, clean a worktree) are flagged as such.

## 2. A finding

| Field | Meaning |
| --- | --- |
| `id` | Stable: derived from the project, the rule, and what it is about (a branch, a worktree path, a pair of branches). The same problem is the same finding on every recalculation. |
| `projectId` (`projectName` outside its project) | The project. |
| `type`, `category` | The rule (section 3) and its group: `uncommitted`, `unsynced`, `branch`, `worktree`, `orchestration`, `operation`. |
| `severity` | `info` (housekeeping), `attention`, `risk`, `critical`. See below. |
| `basis` | `deterministic` or `heuristic`. |
| `title`, `explanation` | A concise sentence, and why it matters. |
| `subject` | The branch, other branches involved, worktree (id, path), task and run it concerns, where they apply. |
| `evidence` | Label/value facts the finding rests on. A finding without evidence is an opinion, and a test requires evidence. |
| `action` | The recommended next step (section 6), with `canPerform` and, if false, `reason`. |
| `state` | `open`, `resolved`, `dismissed` (section 4). |
| `detectedAt`, `updatedAt`, `resolvedAt`, `dismissedAt` | When first seen (in this life), last seen, resolved, dismissed. |

**Severities.** `info` is housekeeping: listed, folded away by default, never counted as needing attention.
`attention` is something to look at today. `risk` means work could be lost or stranded, or Git is blocked.
`critical` means the repository is stuck in a state that blocks everything (conflicts in your own checkout).
The report is `healthy` when nothing is above `info`. Its headline lists the worst first: critical, risks,
then items needing attention.

## 3. The rules, and which signals are deterministic and which are heuristic

*Deterministic* means the condition is computed from facts (the same input always gives the same answer, with
no guess about anyone's intent). *Heuristic* means it infers intent or likely behaviour from a pattern, so it
can be wrong, and the finding says "may" or "looks like". Thresholds are in section 8.

Scope: **Werkbord's own branches** (created by Werkbord: a `devboard/` name *and* a worktree record naming
exactly that branch) are what the branch rules are about. Beyond them, health looks at the target branch and
the branch your own checkout is on, and at your checkout's working state. Your other branches are yours.

| Type | Category | Basis | What it looks at | Severity | Recommends |
| --- | --- | --- | --- | --- | --- |
| `uncommitted_work` | uncommitted | deterministic | Staged, modified or (at least the threshold) untracked files in a checkout no agent is working in | info–risk | review_changes |
| `unpushed_commits` | unsynced | deterministic | A branch has commits its upstream (as of the last fetch) lacks | info–risk | push_branch |
| `branch_not_pushed` | unsynced | deterministic | A branch with its own commits has no upstream and no remote has those commits | attention–risk | push_branch |
| `remote_ahead` | unsynced | deterministic | The upstream has commits the local branch lacks (as of the last fetch) | info–attention | sync_branch |
| `upstream_diverged` | unsynced | deterministic | The local branch and its upstream each have commits the other lacks | attention–risk | sync_branch |
| `remote_branch_deleted` | unsynced | deterministic | The upstream branch was deleted while the local branch still has commits not in the target | attention | review_changes |
| `remote_state_stale` | unsynced | deterministic | The repository never fetched, or not for a week, so every remote comparison is out of date | info | fetch |
| `merged_branch_present` | branch | deterministic | A Werkbord branch whose commits are all in the target (ancestry) is still present | info | delete_branch |
| `branch_content_on_target` | branch | deterministic | Git's in-memory merge of the branch into the target changes nothing: its content is already there (squash or rebase merge, or superseded) | info | delete_branch |
| `abandoned_branch` | branch | heuristic | A Werkbord branch with no live run, no task waiting on its review, and no activity for days | info–attention | review_changes |
| `stale_branch` | branch | deterministic | Unmerged work untouched for two weeks while the target moved on | attention | review_changes |
| `branch_far_behind` | branch | deterministic | An unmerged branch lacks many commits the target has | attention–risk | review_changes |
| `task_done_unmerged` | orchestration | deterministic | A task is Done but its branch has commits that are not in the target and would change it | attention–risk | merge_branch |
| `finished_work_unmerged` | orchestration | heuristic | A run finished a day or more ago and its commits are not in the target, and nothing says they are unwanted | attention | merge_branch |
| `missing_branch_for_task` | orchestration | deterministic | An active task's worktree record names a branch Git does not have | risk | ask_agent |
| `branch_overlap` | orchestration | heuristic | Two in-flight Werkbord branches change some of the same files | attention | create_task |
| `branch_conflict` | orchestration | deterministic | Git's in-memory merge of two in-flight branches reports conflicts | risk | create_task |
| `orphaned_worktree` | worktree | deterministic | Git lists a worktree inside Werkbord's worktree directory that no Werkbord record owns | attention | inspect |
| `worktree_without_run` | worktree | deterministic | A Werkbord worktree record exists and no run ever used it | attention | clean_worktree |
| `worktree_retained_after_done` | worktree | deterministic | A clean worktree is kept for a task that is Done | info | clean_worktree |
| `worktree_metadata_mismatch` | worktree | deterministic | A worktree record disagrees with Git: unknown to it, on another branch or detached, directory gone, or a removal never finished | attention | clean_worktree (directory gone) or ask_agent |
| `operation_interrupted` | operation | deterministic | A merge, rebase, cherry-pick or revert is unfinished in a checkout no agent is using | attention–risk | finish_operation |
| `unresolved_conflicts` | operation | deterministic | Files are in conflict in a checkout no agent is using | risk–critical | finish_operation |
| `automation_blocked` | operation | deterministic | Something about the repository prevents Werkbord's own operations: no usable target, a detached HEAD, a stale lock | info–attention | inspect |
| `repository_unreadable` | operation | deterministic | Werkbord cannot read the repository at all | risk | inspect |

Notes on how a few of these are established:

* **`branch_conflict` is the only finding that says "conflict" about two branches**, and it is
  deterministic: it runs `git merge-tree --write-tree` on the two branches' commits, in memory, touching no
  ref, index or file, and reports what Git reports. It needs Git 2.38 or later; without it, only the
  heuristic `branch_overlap` is possible. Uncommitted work cannot be merge-tested, so an overlap that
  involves uncommitted files stays a heuristic.
* **`branch_overlap` compares file lists**: what each branch changed since it left the target, plus what is
  uncommitted in its worktree. Lock files and generated output (`go.sum`, `package-lock.json`, `dist/` and
  similar) are ignored, a pair where one branch contains the other is skipped (what they share is inherited),
  and a pair whose commits Git merges cleanly in memory is not reported unless uncommitted work is also
  involved. Only the ten most recently active branches are compared.
* **`branch_content_on_target` is how squash and rebase merges are recognised without GitHub.** By history such
  a branch is unmerged. Merging it in memory and getting exactly the target's tree proves it adds nothing.
  That is deterministic ("merging it would change nothing"), but it is not proof of *why*, and Werkbord still
  only deletes a branch it can show is merged by history or by GitHub's word, so the finding says when it cannot.
* **`task_done_unmerged` stays quiet about squash merges** (it defers to `branch_content_on_target`), and
  downgrades from risk to attention when the remote branch was deleted, which usually follows a merged pull
  request. Without GitHub, Werkbord cannot see a pull request merge, and says so in the finding.
* **`abandoned_branch` and `finished_work_unmerged` are guesses.** They infer from inactivity. They are worded
  as possibilities ("may be waiting on you") and are attention at most.
* **Remote facts are as of the last fetch.** `unpushed_commits`, `remote_ahead` and `remote_branch_deleted` read
  remote-tracking refs. Health never fetches by itself: `remote_state_stale` says when that knowledge is old,
  and its action is Fetch.

## 4. A finding's life: open, resolved, dismissed

Findings are recomputed from the repository each time, then reconciled with what was stored
(`domain.ReconcileHealth`, pure and tested without a database):

* A finding seen for the first time **opens**, `detectedAt` now.
* One that is still true **keeps its `detectedAt`**, however many recalculations it survives, so "first seen 3 days
  ago" is true.
* One that stops being true **resolves** (nobody marks it resolved: the fix is the resolution). It stays on the
  report for a day as "Fixed in the last day", so a fix is not silent, and is forgotten after 14 days. If it
  comes back later it opens again as a new finding.
* **Dismissing** ("I know") hides an open finding. It stays hidden while it is no worse than when it was
  dismissed, comes back if it gets worse (its severity rises), and resolves if it goes away. It changes nothing
  in the repository. "Show again" undoes it.
* **If the repository cannot be read**, that is one finding (`repository_unreadable`). Every other finding is left
  exactly as it was: what could not be checked is not reported as fixed.

Findings are stored in `health_findings` (one row each: identity, severity and state in columns, the wording
and evidence as JSON) and `health_checks` (when a project was last checked, how long it took, and the error if it
failed). The database refuses a finding whose state and timestamps disagree.

## 5. Cost: how and when it is recalculated

A recalculation reads Git metadata (refs, worktree list, status of Werkbord's worktrees, ancestry counts) and
Werkbord's records, runs the pure rules, and stores the result. On a real repository it takes tens of
milliseconds. The expensive-looking parts are bounded: at most 20 in-memory merges to recognise squash merges,
and at most 10 branches / 45 pairs for overlaps, each examined only if the file lists intersect.

It is **not polled.** It runs:

1. **When something that can change the answer happens.** A run changes state, a task moves, a worktree is created or
   removed, a project is inspected, or a Git action finishes (fetch, push, merge, delete, clean, pull request).
   A watcher waits three seconds after the last such event and recalculates once, however many arrived.
   Agent output does not count.
2. **When a person asks.** *Check now* (and Refresh on the Git screen) is an explicit recalculation.
3. **When a screen asks for a report and the last calculation is more than two minutes old.** This is how
   changes made in a terminal, which emit no event, are noticed. A fresh calculation is simply read.
4. **Once at start**, for every project.

A recalculation publishes `git.health_changed` only when the set of open findings changed (one opened, resolved,
or changed severity). Recalculating and finding the same thing is silent. The web app refetches the report on that
event, and the Control Center refreshes.

Health never changes the repository. It writes only the unreferenced objects of the in-memory merges, as the merge
check does, and a test compares refs, status, worktrees and stashes before and after. It never uses the network (a
test points the remote at an unreachable host and asserts nothing is contacted), and it never calls a model.

## 6. Actions: the next useful step

Every finding has an action: `kind`, `label`, `detail`, and **`canPerform`**: does Werkbord have a guarded
operation for this, and is what it needs true now? `canPerform` never means Werkbord will do it. When it is
false, `reason` says why and the card says what to do yourself.

| Kind | Opens | Werkbord can | When it cannot |
| --- | --- | --- | --- |
| `review_changes` | The branch, or the uncommitted changes of a worktree or your checkout | Always (it is navigation) | |
| `push_branch` | Push sheet (never forces) | If the branch name is usable, there is a remote (origin, or the only one), and the branch tracks a branch of the same name and is not behind | Diverged or behind: Werkbord never forces, pulls or rebases |
| `merge_branch` | Merge sheet (checks for conflicts with Git first) | If the target exists locally, is checked out, has no unfinished operation or tracked changes, the branch has something to merge, and no agent is on it | Otherwise the card falls back to Review and says why |
| `delete_branch` | Delete sheet (refuses unless nothing is lost) | A Werkbord branch, not protected, no agent, merged by history, checked out nowhere | Not merged by history (a squash merge): delete it yourself once sure |
| `clean_worktree` | Clean sheet (refuses unless nothing is uncommitted) | A Werkbord worktree, clean, not locked, no live run, still on its recorded branch. A directory already gone can be cleaned: only the record is retired | Uncommitted work, locked, or no longer matching its record |
| `sync_branch` | | Never | Werkbord does not pull, rebase or force-push |
| `finish_operation` | | Never | Werkbord never resolves conflicts or aborts an operation in a checkout |
| `fetch` | Fetch | Always | |
| `create_task` | An editable task form prefilled with the finding, repository context and evidence | Always: it adds a card to the board and starts nothing | |
| `ask_agent` | The same editable form, with an agent and interaction-policy choice | Always. It starts the chosen agent after you submit; the finding does not grant extra permissions | |
| `inspect` | | Never | Says what to check |

`merge_branch` and `push_branch` use only what the overview knows. The operation itself looks again and refuses
if anything has moved, so an offered step is never a promise.

## 7. What is deliberately not reported

A health view that cries wolf is one people learn to ignore. These are choices, each pinned by a test:

* **A branch an agent has a live session on.** It is work in motion: unpushed commits, uncommitted files and a
  stale-looking tip are normal. This includes a run waiting on a question or for the next message.
* **Finished, pushed work waiting for review** (for less than a day). That is the normal state of a day of agents.
* **A few untracked files** (fewer than five). A scratch file is not "work".
* **Your own checkout's edits**, except as housekeeping, or as attention when they block a merge that is ready.
* **Your own other branches**, and your branch's unpushed commits beyond housekeeping: committing before pushing
  is how people work.
* **Two branches editing the same file when Git merges their commits cleanly**; lock files and generated output;
  a branch that merely contains another.
* **A squash- or rebase-merged branch** is not called unmerged work.
* **A worktree record created minutes ago** (its run has not started yet), or a removal begun a moment ago.
* **A branch merely a little behind** the target (fewer than 25 commits).
* **A target that is not checked out, a detached HEAD**, unless there is something to merge.
* **A fresh `index.lock`**: Git may be working right now.
* **Old fetches** when there is no agent work to be wrong about.
* **Info findings** never count toward "needs attention", never appear in the Control Center, and are folded
  away on the Git screen.

## 8. Thresholds

All in `domain.HealthThresholds` (`DefaultHealthThresholds`), so none is buried in a rule and tests can set their
own.

| Name | Default | Used by |
| --- | --- | --- |
| `SignificantUntracked` | 5 files | `uncommitted_work`: fewer untracked files are not work |
| `IdleDirtyRisk` | 24 hours | `uncommitted_work`: finished run left work uncommitted this long becomes a risk |
| `FarBehind` / `VeryFarBehind` | 25 / 100 commits | `branch_far_behind`: attention / risk |
| `AbandonedAfter` | 3 days | `abandoned_branch`, and empty branches |
| `FinishedUnmergedAfter` | 24 hours | `finished_work_unmerged` |
| `WorktreeGrace` | 15 minutes | `worktree_without_run` |
| `StuckRemovalAfter` | 10 minutes | `worktree_metadata_mismatch` (a removal begun and never finished) |
| `LockStaleAfter` | 10 minutes | `automation_blocked` (index.lock) |
| `FetchStaleAfter` | 7 days | `remote_state_stale` |
| `MaxOverlapBranches` | 10 | the pairwise overlap check |
| (`StaleAfter`) | 14 days | `stale_branch`, the existing Git Control Center staleness |

## 9. On the screens

**Git screen.** The first card is *Repository health*: the headline, when it was checked, a footnote score,
the findings that need you (cards with severity, "Git says so" / "A guess", title, why, collapsible evidence, the
next step, "First seen", "I know"), then folds for housekeeping, what you have dismissed, what was fixed in the
last day, and "How this is worked out". *Check now* recalculates.

**Control Center.** It is for exceptions. In order: *Needs input*, *Blocked*, *Failed* (a task's latest run
failed and the task is in Doing or Review; moving the task on, or running it again, clears it), *Ready for
review* (agents that finished a turn, and tasks in Review with nothing running), and *Repository risk* (open
findings that are a risk or critical, from the stored report, so no Git is run to draw the screen; each opens the
project's Git screen). When nothing needs you it says *All clear*. Successful background activity ("Working now")
and the agent list are folded away below. A project's strip shows what it asks, for example `1 repository risk ·
2 Git items`. The badge counts questions, blocked and failed runs, runs waiting for a message, and repositories
at risk; not finished work, housekeeping or ordinary findings.

## 10. HTTP API

Under `/api/projects/{pid}/git`, token required, scoped to the project (another project's IDs are 404).

| Method and path | Purpose |
| --- | --- |
| `GET …/health` | The report: `summary`, open `findings`, `dismissed`, recently `resolved`, `check`. A database read; recalculates only if never checked, an event marked it dirty, or the last calculation is over two minutes old |
| `POST …/health/refresh` | Recalculate now |
| `POST …/health/findings/{id}/dismiss` · `…/reopen` | Say you know / undo. Dismissing a finding that is not open is 409 |

`GET /api/control-center` also returns `failed`, `review` and `repository`, and per project `failed`, `review`,
`repoAttention`, `repoRisk`.

## 11. Tests

* `internal/domain/health_test.go`: a table of cases, each building a repository state by hand. **Every rule has
  at least one case that fires it** (a test fails if a rule has none) and **many cases where it must stay silent
  despite a nearby condition** (a test fails if there are fewer than 20). Every finding any case produces must
  be well formed: documented rule, matching category and basis, concise title, explanation, evidence, a next
  action, and a stated reason when Werkbord cannot do it; a heuristic never says "will conflict". Also: which
  signals are heuristic, actions offered only when they could run, a busy day with nothing wrong is Healthy,
  finding ids, summary and headline wording, sort order, and the whole lifecycle (`ReconcileHealth`).
* `internal/service/githealth_test.go`: the same rules on **real temporary repositories**, a real bare remote
  and real worktrees: uncommitted work found then resolved, scratch files ignored, a live agent never flagged,
  unpushed and never-pushed work, a diverged branch, the remote ahead, a Done task with an unmerged branch, a
  **squash-merged branch recognised by content**, conflicts **proven** by Git versus overlap guessed, clean merges
  and lock files not reported, a worktree no record owns, a record with no run, a worktree switched away, a deleted
  directory, interrupted merges and conflicts in a worktree and in your checkout (critical), a stale
  `index.lock`, an unreadable repository leaving other findings alone, dismissal and reopening, **a refresh
  changes nothing in the repository**, **never uses the network**, is not repeated on every read, publishes an
  event only on change, and the watcher (debounce, ignoring agent output, shutting down without hanging).
* `internal/service/control_health_test.go`, `internal/api/githealth_test.go`: the Control Center's failed,
  review and repository-risk lists, that a quiet successful day produces no exceptions, and the HTTP routes
  (scope, token, dismissal, a critical finding reaching the Control Center).
* `internal/store/sqlite/health_test.go`: the round trip, the lifecycle constraints, ordering, pruning, cascade.
* `internal/domain/health_docs_test.go`: this document lists every rule with its category and basis.
* `web/src/lib/health.test.ts`: severity and basis wording, what each action does (and that nothing that
  cannot be done becomes a button), and the badge.

## 12. Known limits

* Remote knowledge is as of the last fetch, and GitHub is not asked: a squash-merged pull request is recognised
  by content, not by GitHub's record.
* The overlap check sees changes since the branch left the target, so two branches cut from a shared commit that
  is not on the target may share files that are inherited. A branch that contains another is skipped.
* Overlap and conflict detection needs agents' work to be visible: committed, or uncommitted in their worktrees.
* A user's own branches and other people's remote branches are not examined.
* The rules know Werkbord's records for up to the 2,000 most recent runs of a project.
* There is no notification when a finding opens (push notifications are deferred; `git.health_changed` is their source).
