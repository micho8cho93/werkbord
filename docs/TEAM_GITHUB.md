# Team GitHub: using a team's repository without leaving Werkbord

Status: **G1 (live read-only view), G2 (change alerts) and G3 (write actions) and G4 (admin actions) are built.** Decided: polling, not webhooks; admin
actions view-only at first; a team admin may turn the live view off (not built yet).

G2 as built: each connected client polls branch heads (`GET /api/github/repository/heads`) and tells the window; the window passes the
heads to the team's page, which reports them with `ReportBranches`. A branch that moves after the project's first report is recorded as
a `repository.branch_pushed` activity, served as `Summary.Pushes` (`GET /api/team/v1/pushes`), so members without GitHub are told too.
Not built: suppressing your own pushes, per-team mute, ETag polling, the admin switch.

## Goal

A team's Git page should feel like GitHub, inside Werkbord: see the real repository, branches, pull
requests and checks, take actions, see who can do what, and hear about it when someone pushes. Every
member sees the same repository. What each member may *do* is decided by GitHub, not by Werkbord.

A member without GitHub (not signed in, or no access to the repo) sees the **reported view** that
exists today (`repositoryTab` in `internal/team/console/static/console.js`): branches, pull requests
and "needs attention" as members' own Werkbords report them.

## What we start from

* `internal/github` never talks to GitHub itself. It runs the person's own `gh`, so Werkbord holds no
  GitHub credential and the Team server never sees one (`service/gitreport.go`). **This design keeps
  that rule.** The token stays on the member's computer.
* Personal Git (`docs/GIT.md`) already has the safety model to reuse: separate local/remote/GitHub
  sources, typed action results (`done`, `refused`, `unverified`…), read-back verification before
  reporting success, and confirmation dialogs the page cannot press (`Shell.Relay`).
* Personal Git works on a local checkout. A team repo may not be checked out on a member's computer,
  so the live view must work from GitHub's API alone (`gh api`), with a checkout optional.

## Architecture

```
member's Werkbord (desktop)                         Team server
  shell ── Team frame (console.js)  ◄── reported view, pulse feed ──  stores repo URL, pulses
     │
     └── Personal controller ── internal/github (gh api) ──► GitHub
              ▲ live view, actions, permissions
```

1. **Identity of the repo.** The Team project's `repository` URL, parsed with `github.ParseRemote`.
   It is data from teammates, so it is only ever used to build `gh api repos/<owner>/<name>/…` calls
   (never a shell string, never a path), and write actions name it in the confirmation dialog.
2. **Live view runs in the member's Personal controller**, not in the Team server. New read
   endpoints on the Personal API take `{host, owner, name}` and answer from `gh api`.
   The shell's Git page for a team project loads the Personal Git components with that repo; the
   Team frame's reported view is the fallback.
3. **Three states per member**, decided by one probe (`GET repos/<o>/<n>`):
   * *Live:* `gh` signed in and the repo answers. Show the live view.
   * *No access:* signed in but 404/403. Show the reported view and say why.
   * *Not connected:* `gh` missing or signed out. Show the reported view and a "Connect GitHub" step
     (the existing login flow in `internal/github/account.go`).
4. **Permissions come from GitHub.** The probe returns `permissions` (`admin`, `maintain`, `push`,
   `triage`, `pull`). The UI hides or disables controls the member lacks and says why ("needs write
   access"). The UI is a hint; GitHub's answer is final, and a refusal is shown as such.

## What the member can do (tiers)

| Tier | Examples | Confirmation |
| --- | --- | --- |
| **Read** | repository, branches and protection state, commits, files, compare, pull requests, reviews, checks and Actions runs, collaborators and their roles | none |
| **Write** | create branch, open/close/reopen pull request, comment, review (approve / request changes), merge (method the repo allows), update branch from base, re-run checks, delete merged branch | a dialog naming the repo and the exact effect, as with `connectRunner` |
| **Admin** | change a collaborator's role or invite, branch protection, enable Pages, repo settings | same dialog, plus: view-only in the first release; "Open on GitHub" link instead |

Every write returns the existing `GitActionResult` shape and is **read back from GitHub** before
success is reported ("a pull request is only reported as opened after it has been read back").

## Changes made by others: the repo pulse

Goal: when anyone pushes, every member of the team hears about it, including members without GitHub.

* **Detecting (per connected client, no webhook server needed).** While Werkbord is open, the
  Personal controller polls with conditional requests (`ETag` / `If-None-Match` on
  `repos/<o>/<n>/branches` and `/events`). A `304` costs nothing against GitHub's rate limit. About one
  check a minute, slower when the window is hidden. Webhooks are rejected for now: they need a public
  address that a team host on a private network usually lacks.
* **Sharing.** When a client sees a branch head move, it reports a small **pulse** to the Team
  server: `{branch, headSHA, previousSHA, pusher, at, commitCount}`. No code, no tokens. The server
  de-duplicates by `(branch, headSHA)`, so the first member to notice reports it and the rest add
  nothing. This is a new, narrow report beside `ReportGit`, permission-checked like it.
* **Showing.** The team's Overview gets a *Repository* card: "3 new commits on `main` by Ana, 5 min
  ago", open pull requests, failing checks. The window raises one notification per pulse (the
  existing `alerts` key mechanism, key = repo+branch+sha), never for the member's own push, and
  members can mute it per team.
* **Members without GitHub** see pulses too, because they come from the Team server, not GitHub.
  If nobody with GitHub has Werkbord open, no pulse is produced; the card says "as last reported".

## Where it plugs in

* Go: new `internal/github` methods (`Repository`, `Branches`, `Commits`, `Compare`, `PullRequest*`,
  `Checks`, `Collaborators`, write actions) built on `gh api`; Personal API routes under
  `internal/api/`; a `RepoPulse` report in `internal/team/service` + `store`.
* Shell: team Git page loads the live view, else the reported view; Overview card; alert.
* Console: pulse feed on the project Overview; no GitHub token handling.

## Phases

1. **G1, live read-only.** Probe + three states, repository/branches/commits/PRs/checks, collaborators
   (read). Reported view stays as fallback.
2. **G2, pulse.** Polling, `RepoPulse` report, Overview card, notification, mute.
3. **G3, write actions.** Branch, PR, comment, review, merge, update branch, re-run, delete branch,
   each with its confirmation and read-back.
4. **G4, admin.** Roles, invites, protection, Pages, with the same confirmation, once G3 has proven
   the safety model.

## Risks

* **Untrusted repo URL.** A teammate could point a project at another repo. The member's own `gh`
  then reads (and, after confirmation, writes) *that* repo. Mitigations: strict `owner/name`
  validation, the repo is named in every dialog, writes are blocked when the URL changed since the
  member last accepted it.
* **Rate limits and Enterprise hosts.** Conditional polling keeps usage low; Enterprise hosts work only
  when `gh` is signed in to them (`HostKnown`).
* **Scope creep.** "Everything GitHub does" is unbounded; the tiers above are the boundary.
* **Two sources of truth.** Live and reported views can disagree; the page always labels which it is.

## Decisions needed

1. Polling now, with webhooks only if teams ask later. OK?
2. Admin actions (roles, protection) view-only in the first release. OK?
3. May a team admin turn the live GitHub view off for the team?

## As built: G3 write actions

`POST /api/github/repository/actions` (`service.GitHubSetup.ActOnRepository`): create branch, delete branch, open pull request,
comment, review (approve / request changes / comment), merge (merge / squash / rebase) and update branch. The request must repeat the
repository's name as `confirmRepo`; the person's role is read from GitHub first; the default and protected branches are not deleted; a
merge carries the commit the person looked at, so GitHub refuses if it moved; results are read back (`done` / `refused` / `rejected` /
`unverified` / `failed`). The page asks in a panel that names the repository, and the first change to a repository also asks the person
to say they recognise it. Not built: re-running checks, admin actions (G4).

## As built: G4 admin actions

`GET /api/github/repository/admin` shows who has access, the review rule on the default branch and the Pages site, as far as the
person's role lets GitHub show them (a part GitHub withholds says so). The same `POST .../actions` endpoint does `set_role` (also
invites), `remove_collaborator`, `protect_branch` (1 to 6 approving reviews, administrators not bound), `unprotect_branch` and
`enable_pages` (github.com only). All need the **admin** role on the repository. Changing or removing a person also needs their
user name typed again, and is refused for the person themselves, so the last administrator cannot be removed from here. Every result is
read back from GitHub. Not built: re-running checks, repository settings beyond these, other GitHub hosts for Pages.
