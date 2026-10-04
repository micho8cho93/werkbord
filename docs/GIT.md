# The Git Control Center and its safety model

The Git section of a project exists for one person: someone orchestrating many coding agents who
needs to understand, and control, the Git state those agents produce without switching to GitHub or a
terminal. It is **not** a reimplementation of GitHub, and it does not reimplement Git: every
operation runs the installed `git` executable (and, for GitHub, the user's own `gh`).

**Safety is more important than convenience.** Where a safe answer cannot be established, the
controller refuses and says why. It does not try to be clever.

Code: `internal/gitrepo` (the Git engine), `internal/github` (the `gh` wrapper),
`internal/service/gitcontrol.go` and `gitactions.go` (what to show, and every safety check),
`internal/api/git.go` (HTTP), `internal/domain/gitstate.go` (the types and the pure rules),
`web/src/lib/git/` and `web/src/routes/Git.svelte` (the phone UI).

---

## 1. Three sources of truth, never mixed

| Source | What it is | How fresh |
| --- | --- | --- |
| **Local** | The repository on this computer: branches, commits, the checkout, worktrees | Read now, on every request |
| **Remote-tracking refs** | What this repository last learned about a remote (`origin/main` …) | As of the last **fetch**, shown as "fetched 3h ago" or "never fetched" |
| **GitHub** | Pull requests, reviews, checks, mergeability, from `gh` | Asked separately, live, when the pull request section loads |

The overview JSON keeps them apart (`local`, `remote`, and GitHub from its own endpoint), and so does
the screen: *On this computer* and *On the remote* are separate blocks. Everything the overview says
about a remote ("2 commits behind origin/main") is derived from remote-tracking refs, is labelled as
being as of the last fetch, and is only updated by an explicit **Fetch**.

**A local change is never presented as something that happened on a remote.** Every action returns a
`GitActionResult` whose `local` and `remote` effects are separate:

* a **merge** is local only. The result says "Nothing was pushed", has no remote effect, and a pull
  request on GitHub is unaffected until the target is pushed and GitHub says otherwise;
* a **push** reports what changed locally (the remote-tracking ref) and then *asks the remote
  itself* (`git ls-remote`) where the branch is. `remote.verified` is true only if the remote
  answered and agreed. If Git said "success" but the remote could not be asked, or disagrees, the
  outcome is `unverified`, not `done`, and no audit event is written;
* a **pull request** is only reported as opened after it has been read back from GitHub as an open
  pull request for that branch;
* a **remote branch deletion** is confirmed gone by asking the remote again.

`ok` is true only for outcome `done`. The other outcomes are `refused` (a safety check stopped it
before anything changed), `noop`, `conflict` (a merge stopped on conflicts and was undone),
`rejected` (the remote refused), `unavailable` (remote or tool unreachable), `auth_failed`,
`failed` and `unverified`. They are HTTP 200 answers: a refusal is an expected result, not an error.

## 2. What the overview shows

`GET /api/projects/{pid}/git` returns, for the project:

* **Repository**: name, the **target** branch and how it was found, current **HEAD** (branch or
  detached, commit, subject), **remotes** (credentials in URLs are redacted), **recent commits**,
  **working-tree status** (staged / unstaged / untracked / conflicted, and any unfinished
  merge/rebase/cherry-pick/revert/bisect), **local commits not pushed**, **remote commits not
  pulled**, and **ahead/behind** against the upstream.
* **Branches**, local first (Dev Board's own on top), then remote branches that no local branch
  tracks. For each: name, scope (local/remote), HEAD commit, latest-commit age, upstream and its
  state, ahead/behind the target, ahead/behind the remote, merged or not, worktree, **task**,
  **run**, whether Dev Board created it, and why it needs attention.
* **Worktrees**: path, branch, locked/missing, whether Dev Board made it, what is uncommitted in it,
  and the run using it.
* A summary and a list of **notes** about how complete the picture is (never fetched, detached HEAD,
  no remote, branches not measured because there are too many).

### The target branch

Found in this order of trust: what `origin/HEAD` says the remote's default is; the user's
`init.defaultBranch`; `main`, `master`, `trunk`, `develop`; the branch that is checked out. The
result says which rule applied (`source`). If the target exists only as `origin/<name>` locally
(`localExists: false`) branches are still compared with it but **nothing can be merged** until it
exists locally.

### Branch status, and what each word means

* **Relation to the target**, from commit counts: `same` (the same commit: nothing on the branch),
  `merged` (everything on it is in the target, which has moved on), `behind` (the same ancestry as
  `merged`, but the branch's reflog shows it never had a commit of its own: still where it was cut,
  with the target moved on, so nothing was merged because nothing was done), `ahead`, `diverged`
  (each has commits the other lacks). `merged`, `behind` and `same` all mean no commit would be lost
  by deleting the branch.
* **Fully merged is ancestry**: every commit of the branch is reachable from the target. A branch that
  was **squash- or rebase-merged on GitHub is not merged in this sense**, because its commits are not
  in the target. It shows as ahead, and the pull request section shows the merge. (Deletion has one
  extra source of evidence for this case, see §5.)
* **Relation to the upstream**: `in_sync`, `ahead` (unpushed commits), `behind`, `diverged`, `none`
  (no upstream configured) or `gone` (configured, but the remote branch was deleted). As of the last
  fetch. For a branch with no upstream, "unpushed" means commits that no remote-tracking ref has.
* **Stale**: the branch is ahead of or diverged from the target, has had no commit for 14 days, *and*
  the target has moved on. A branch that is merged is finished, not stale.
* **Attention** (what "needs you" is built from): `review` (finished Dev Board work to look at),
  `unpushed`, `no_remote`, `gone`, `behind`, `diverged` (for branches that are not Dev Board's),
  `dirty` (uncommitted work in its worktree), `operation` (an unfinished merge/rebase there),
  `missing` (its worktree directory is gone), `stale`, `cleanup` (merged: delete it, or clean its
  worktree first). Attention is advice for ordering a phone screen; it never blocks an action.

An agent that finished a turn but **did not commit** leaves its work in the worktree, not on the
branch: the branch then shows "no commits" and an "N uncommitted" chip, and the work is reviewed from
*Working changes*. Dev Board does not commit for the user.

### Dev Board association

`devboard/*` branches are the ones Dev Board makes. For each local branch the overview joins Dev
Board's own records: the **worktree record(s)** for that exact branch, the **runs** that used them,
the **task** of the latest run, and the task's phase (`active`: in Doing or a run is live; `review`;
`completed`: Done; `idle`: Backlog). A branch is **created by Dev Board** only if *both* its name is
under `devboard/` *and* a worktree record of this project names exactly that branch. A name proves
nothing, because anyone can type `devboard/` into a branch name, so a branch that merely has the
name is shown as "has Dev Board's name, but Dev Board has no record of making it" and is treated as
the user's.

### Unusual branch names

A branch whose name could be taken for something else (`-x`, `--upload-pack=…`, `HEAD`, anything Git's
ref-name rules or Dev Board's stricter ones refuse) is listed, flagged `unusual`, and **every action on
it is refused**. Measurements never put a branch name on a command line: they use commit IDs. Names
that are ordinary but non-ASCII work.

## 3. Comparison and drill-down

Branch → changed files → file diff, each a request of its own so nothing huge is drawn at once.

* `GET …/git/compare?branch=&scope=&target=&offset=&limit=` returns the **comparison target**, the
  **commits unique to the branch**, the **commits missing from it**, the **changed files** (a page of
  them), and the totals of **additions and deletions over all files**. The diff is the one a pull
  request shows: from the merge-base to the branch tip, so changes made on the target since do not
  appear. The response carries the commit IDs it used.
* `GET …/git/diff?from=&to=&path=&oldPath=&offset=&lines=` is a **window** onto one file's unified
  diff between two commit IDs from the comparison, or, with no path, of everything. A diff is pinned
  to those IDs, so it is all one diff even if the branch moves while it is being read.
* `GET …/git/changes?worktree=` and `…/changes/diff?…` do the same for uncommitted work in the
  project's checkout or one of Dev Board's worktrees (staged, unstaged, untracked).

Limits: files are listed 50 at a time (at most 200), at most 5,000 files are read per comparison
(`truncated` says so); a diff window is 400 lines by default and at most 2,000; Git's diff output is
read up to 4 MiB and then cut (`truncated`, with a message that the rest cannot be shown here); an
untracked file is read up to 512 KiB; commit lists show 50 (unique) and 20 (missing); the status
listing keeps 300 files per category but counts all of them; at most 300 local branches are measured
and 100 remote-only ones shown (a note says so).

## 4. GitHub

* Uses the user's own **GitHub CLI**. Dev Board has no GitHub account, token or credential of its
  own, stores none, and never passes one on. `gh` runs in the user's environment, with prompts off.
* Optional. `config.json` → `"github": {"command": "gh", "disabled": false}`. With `gh` missing, not
  signed in, offline, or disabled, **everything local still works**, and the pull request section
  says why it is empty (`gh_missing`, `unauthenticated`, `no_remote`, `not_github`, `error`).
* Pull requests (`GET …/git/pull-requests`): number, title, source and target branch, open/closed/
  merged (and draft), review decision where there is one, **checks** summarised from the status
  rollup, and **mergeability**. GitHub computes mergeability lazily and often has not; **an unknown
  is shown as nothing, never as a yes**. Each pull request is joined to its task and run when its head
  is a Dev Board branch (a fork's pull request is never joined to a local branch of the same name).
* github.com repositories are recognised from the remote URL; any other host is accepted only if
  `gh auth status --hostname` says `gh` is signed in to it.
* Merging a pull request on GitHub is **not** implemented. "Merge safely" is the local merge below.

## 5. The actions, and what guards each

Every action follows the same four steps under **one lock per project** (so two taps cannot
interleave):

1. **Look again**, fresh, at the repository. Nothing from an earlier screen is trusted. The
   registered path must still be the registered repository (a directory deleted and recreated inside
   another repository is refused).
2. **Compare with what the user was looking at.** The request carries the commit IDs the user
   reviewed (`expectedSha` for a push or a pull request, `branchSha` and `targetSha` for a merge,
   `branchSha` for a deletion); if a branch moved between the inspection and the tap, the action is
   refused (`branch_moved`, `target_moved`). A request that does not say what was reviewed is
   refused. (For a worktree, `headSha` is checked when given; removing a worktree never loses a
   commit, because the branch stays, and uncommitted work is checked directly.)
3. **Check every condition**, and refuse with the reasons if any fails.
4. **Do exactly one guarded operation** and report it, locally and remotely, having asked the remote
   when it matters. Only something that actually happened is written to the event log.

The merge, delete and clean checks are also available as a **plan** (`…/merge/plan`,
`…/branches/delete-plan`, `…/worktrees/clean-plan`) that changes nothing: the phone shows the same
verdict, with its reasons, before asking for confirmation. The action then plans again itself.

| Action | Does | Refuses when |
| --- | --- | --- |
| **Refresh** | Looks again (no network). | — |
| **Fetch** | `git fetch --prune` of each remote. Updates remote-tracking refs only; never touches a branch of yours, your checkout or a worktree. | The remote cannot be reached → `unavailable` with the reason. |
| **Push branch** | A plain push of the reviewed commit to `refs/heads/<same name>` on one remote, **never forced**. Then asks the remote where the branch is. Sets the upstream if there was none. | Branch moved; name unusual; no remote; several remotes and none is `origin`; the upstream has a different name. A remote that has moved on makes Git refuse (`rejected`): the message says Dev Board never forces and the remote is untouched. |
| **Merge safely** | `git merge` of the reviewed commit into the project's **target branch**, in the worktree where it is checked out, as a merge commit or fast-forward only. Local only. Verified afterwards (target moved, branch is an ancestor). | See below. |
| **Delete branch** | Deletes a **local** branch, conditional on it still being at the reviewed commit. Optionally also the remote branch. | See below. |
| **Clean Dev Board worktree** | Removes the directory of a worktree Dev Board made. The branch is kept. | See below. |
| **Open pull request** | `gh pr create` from a branch already on the remote, into the target. Read back from GitHub. | Not on the remote, or at a different commit there (it never pushes for you); one is already open; nothing to propose; no title; `gh` unavailable. |
| **Open task / run** | Links. | — |

### Merge

Before anything, it re-checks, in this order, and refuses on any of: the branch exists and has not
moved; the target is the project's target, exists locally and has not moved; the branch is not the
target; the branch has commits the target lacks (**ancestry**: `already_merged` otherwise); the
histories are related; (for fast-forward only) the target has not moved on; no run on the branch is
starting or running or waiting on a question (an idle session only warns); **the target is checked
out in some worktree** (Git can only merge into a checked-out branch, and Dev Board will not switch
your checkout); and in *that* checkout:

* no merge/rebase/cherry-pick/revert/bisect is unfinished, nothing is in conflict, and **there are no
  staged or modified tracked files**, so a merge can neither mix with nor discard uncommitted work;
* untracked files are left alone, **unless** the merge would add a file of the same name (including
  inside an untracked directory), which is refused;
* no run is active in it if it is one of Dev Board's.

**Conflicts are predicted, never resolved.** With Git ≥ 2.38 the check is `git merge-tree
--write-tree`: Git's own merge, run in memory without touching any ref, index or file. A conflict
found that way is a blocker, and the files are named. It is reported as "Git merged them in memory",
and is true of the commits as they are *now*. Without it, only an **overlap heuristic** is possible
(files changed on both sides); the plan then says `method: overlap`, may say `possible` but never
`clean`, only warns, and its note says it is a guess. If a merge then conflicts anyway it is
**aborted** (`git merge --abort`) and the result states whether the checkout was restored exactly.

Dev Board does not pull or fast-forward the target. If the local target is behind or diverged from
its remote the plan **warns** (a merge made now is not on the remote until pushed, and the remote has
commits you lack).

### Delete

A local branch is deleted only if **all** hold: it is **created by Dev Board** (see §2: name *and*
record); it is not **protected** (the target, the branch checked out in the project's checkout, or
`main`, `master`, `trunk`, `develop`, `development`, `dev`, `release`, `production`, `prod`,
`staging`, `release/*`, `hotfix/*`); it is checked out **nowhere** (a Dev Board worktree must be
cleaned first); no run on it has a session open; it is still at the reviewed commit; and **no work
would be lost**: it is **fully merged by ancestry**, *or* GitHub says a pull request **from this
repository, from this branch, with exactly this tip commit** was merged (the squash/rebase-merge
case). If GitHub cannot be asked, the answer is no.

The deletion itself is `git update-ref -d <ref> <expected commit>`, which Git refuses if the branch
is not exactly there; so a commit pushed to it a millisecond ago is never deleted. Its settings are
removed with it. The result says how to bring it back (`git branch <name> <commit>`); the commits are
still in the object database.

The remote branch is deleted **only if asked** and only if its tip is the local branch's tip or is
already in the target. It is asked live and deleted with `--force-with-lease=<ref>:<commit>`: the
remote refuses unless it is still there. This is the only use of a lease anywhere, it deletes a
branch, and it is conditional; it is not a force push. If the remote step fails the result says
loudly that the remote branch was **not** deleted, and the local deletion (which was safe on its own)
is reported separately.

### Clean a Dev Board worktree

Only a directory that: Dev Board's own record names; lies **strictly inside Dev Board's worktree
directory** and passes through no symlink; **Git itself lists as a worktree of this repository** (a
directory Git does not know is never deleted on the strength of a record); is not locked; has no run
using it; is still on the branch and commit recorded and viewed; and has **nothing uncommitted: no
staged, modified, untracked or conflicted file, no unfinished operation**. It is removed with `git
worktree remove` **without `--force`**, so Git re-checks at the moment of removal. A worktree
directory that is already gone only has its record retired. The removal follows the worktree
records' crash-safe order (begin, delete, finish). **The branch is never touched.**

## 6. What the engine does so that it cannot do more than it says

Every Git command goes through one function (`gitrepo.CLI.run`). It:

* never runs repository **hooks** for what Dev Board writes (merge, push, worktree changes use
  `core.hooksPath=/dev/null`). The consequence: a repository's `pre-push` or merge hooks do not run
  for pushes and merges made here; if you rely on them, push and merge from your terminal;
* never lets a repository's configuration run a program for a **read**: `core.fsmonitor` is off,
  `diff.external` and textconv filters are switched off (`--no-ext-diff --no-textconv`), pathspecs
  are literal, lazy fetching and every transport are off, and optional locks are not taken;
* for the few commands that talk to a remote (fetch, push, `ls-remote`): allows only `file`, `git`,
  `ssh`, `http` and `https` transports (**not `ext::`, which runs a command named in the URL**),
  never prompts, runs in its own session so `ssh` cannot wait on a terminal nobody is at, and kills
  the whole process group on a timeout;
* passes **revisions only as full ref names (`refs/heads/…`) or full commit IDs**, never a bare name,
  and validates paths (relative, no `..`, no NUL, no leading `-`), remote names (they must be
  configured remotes: never a URL) and branch names;
* has **no force, no reset, no checkout, no clean, no rebase, no automatic conflict resolution, and no
  `+` refspec**; deletion is conditional on the expected commit;
* bounds every output and every run time, queues behind a fixed number of git processes, and
  removes credentials embedded in URLs from the error output of commands that talk to a remote.

**An agent finishing a run cannot merge.** The runner is given `gitrepo.Worktrees`, an interface that
physically has no merge or push (`TestRunnerCannotMerge` pins its method set to exactly add-worktree,
remove-worktree and delete-the-branch-you-just-made), and holds no `GitControl`. Completing a run,
moving a task to Review or Done, or keeping a worktree changes nothing in Git
(`TestFinishingARunDoesNotMergeAnything`, `TestFinishingARunNeverMergesIntoTheTargetBranch`).
Merging happens only when a person taps Merge and confirms.

**Audit.** `git.fetched`, `git.pushed`, `git.merged`, `git.branch_deleted`, `git.worktree_cleaned`
and `git.pull_request_created` are appended to the event log (with the task and run they concern)
in the same way as every other event, and only when the action happened.

## 7. Known limits

* **Window between check and removal.** A worktree is checked clean, its removal is begun in the
  records (which proves no run uses it and stops one starting), and Git then removes it without
  `--force`. Something writing into it in those few milliseconds makes Git refuse; the record then
  stays "removing" and the removal can be retried. (If a new run is started on that task before the
  retry, the runner's existing recovery for an unfinished removal force-removes a worktree: an
  existing behaviour of the runner, not of the Git Control Center.)
* **Hooks are not run** (above).
* **Squash/rebase merges** are recognised only through GitHub's pull request, not by content.
* **Merging needs the target checked out** in some worktree, usually your own checkout, and clean.
* **Identity.** A merge commit needs `user.name`/`user.email` configured for Git; if they are not,
  Git's error is shown and nothing changes.
* **GitHub Enterprise** is recognised only if `gh` is signed in to the host.
* **Untracked files** are listed with directories collapsed; a merge's clash check is exact for
  paths on disk, and refuses if there are too many files to check.
* **No commit action.** Dev Board does not commit, stash or discard for you. Review uncommitted work
  in *Working changes*, and commit it from the agent or a terminal.
* Not built: pulling or fast-forwarding the target, merging a pull request on GitHub, rebasing,
  conflict resolution, a force push of any kind, deleting remote branches that Dev Board did not
  create, tags, submodule awareness.

## 8. HTTP API

Everything is under `/api/projects/{pid}/git` and requires the API token, like the rest of the API.
A project can reach only its own repository; another project's IDs are 404.

| Method and path | Purpose |
| --- | --- |
| `GET …/git` | The overview (local; no network) |
| `GET …/git/pull-requests` | GitHub's pull requests, or why there are none |
| `GET …/git/commits?branch=&scope=&skip=&limit=` | A branch's history, paged |
| `GET …/git/compare?branch=&scope=&target=&offset=&limit=` | Commits both ways, changed files (paged), totals |
| `GET …/git/diff?from=&to=&path=&oldPath=&offset=&lines=` | A window of a diff between two commits |
| `GET …/git/changes?worktree=` · `…/changes/diff?worktree=&kind=&path=&oldPath=` | Uncommitted work and one file of it |
| `POST …/git/fetch` | Fetch every remote |
| `POST …/git/push` `{branch, expectedSha}` | Push |
| `POST …/git/merge/plan` · `…/git/merge` `{branch, branchSha, target, targetSha, strategy}` | Check · merge |
| `POST …/git/branches/delete-plan` · `…/git/branches/delete` `{branch, branchSha, deleteRemote}` | Check · delete |
| `POST …/git/worktrees/clean-plan` · `…/git/worktrees/clean` `{worktreeId, headSha}` | Check · remove |
| `POST …/git/pull-requests` `{branch, expectedSha, title, body, draft}` | Open a pull request |

Request bodies with unknown fields are refused, so there is no flag to send that Dev Board does not
define (there is no `force`). A malformed input is 400, an unknown project, branch or worktree 404, a
failing `git` 502 `git_failed` with Git's message, a `git` that takes too long 504.

## 9. Tests

All of these use **real temporary Git repositories** (with real bare repositories as remotes and real
linked worktrees), the real `git`, and, for GitHub, a stand-in `gh` script; no mocks of Git.

* `internal/gitrepo`: status parsing (staged/unstaged/untracked/conflicted/renamed/detached/unborn/
  mid-merge); refs and upstream tracking; target detection; worktree listing; commit paging; file
  lists and diff windows (binary, renamed, huge); merge simulation; push (new, fast-forward,
  rejected, never forced, unavailable remote, unknown remotes, `ext::` refused, **hooks not run**);
  remote-branch deletion with a lease; merge (commit, fast-forward, **conflict undone**, Git refusing
  over local changes); conditional branch deletion (moved branch, checked-out branch);
  clean-only worktree removal (untracked/modified/staged refuse, vanished directory); **Git command
  failure**; configured diff/textconv/fsmonitor programs are not run by reads; credential redaction.
* `internal/github`: normalising pull requests, checks, review, unknown mergeability; not installed /
  not signed in / offline / garbage / timeout; credentials removed; creation read back; remote URL
  parsing.
* `internal/service`: the overview (staged/unstaged/untracked, local ahead/behind/diverged, upstream
  none/gone, multiple worktrees, multiple Dev Board branches, merged vs unmerged, task phases,
  stale, remote-only, **detached HEAD**, **no remote**, **malformed branch names**, Git failure);
  comparison and paging; and every action's refusals: **branch changing between inspection and
  action**, merge into a dirty checkout, untracked clashes, target not checked out, **merge
  conflicts** (simulated, heuristic, and a real one undone), active runs, **branch deletion safety**
  (not owned, impostor name, protected, unmerged, moved, checked out, merged by pull request,
  remote with unseen commits, remote moved), **worktree deletion safety** (every kind of
  uncommitted work, active run, symlink path, locked, unknown to Git, switched branch, outside the
  root, already gone), **push failure / rejection / unavailable remote / unconfirmed push**, pull
  request refusals and failures, **actions serialised**, and that nothing merges on its own.
* `internal/api`: routing, error mapping, project scoping and the token on every Git route.
* `internal/runner`: a finished run does not merge.
* `web/src/lib`: wording, chips, which actions are offered, and the drill-down routes.

Run everything with `make check`.
