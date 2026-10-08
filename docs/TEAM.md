# Werkbord Team

Werkbord Team is the shared workspace for a team that uses Werkbord. It records **who is on the team**, **which
projects exist**, **who is on which project**, and the **shared board** the team works from: tickets, who holds each one,
and the branches and pull requests that come out of them. It is a separate product from the individual Werkbord; see
[PRODUCTS.md](PRODUCTS.md) for how the two relate and where the code lives.

> **Team coordinates. It does not execute.** Every member keeps their own computer, their own Werkbord runner, and their
> own Git, GitHub and agent credentials. Team holds none of them and has no way to run anything on any member's machine.
> There is no remote-runner access, no shell, no file access. The server has no code path that starts a process, and a
> test (`internal/archtest`) keeps it that way.

## How it fits together

```
  Ada's computer                          Team server                          Bo's computer
 ┌───────────────────┐   coordination   ┌──────────────────────┐ coordination ┌───────────────────┐
 │ browser (console) │ ◀──────────────▶ │ workspace · projects │ ◀──────────▶ │ browser (console) │
 │ her Werkbord      │   metadata only  │ board · tickets ·    │ metadata only│ his Werkbord      │
 │  · her agents     │                  │ reported Git facts · │              │  · his agents     │
 │  · her Git/GitHub │                  │ activity · sync      │              │  · his Git/GitHub │
 │  · her API keys   │                  └──────────────────────┘              │  · his API keys   │
 └───────────────────┘   nothing flows between the two Werkbords through Team   └───────────────────┘
```

The work a ticket goes through, and who owns each fact:

```
 Workspace → Project → Board → Ticket ──claim──▶ Local runner ──▶ Branch / PR ──▶ Review ──▶ Done
 (Team)      (Team)    (Team)  (Team)            (the member's    (the member's    (Team      (Team)
                                                  own Werkbord)    Git host)        records)
```

- **Team is authoritative for collaborative metadata:** who is on the team, which tickets exist, who holds each, their
  status, and the branch, commits and pull request *as reported*. Every client re-reads these from the server.
- **Each member's own Werkbord is authoritative for local execution:** whether an agent is running, what it did, the
  checkout, the credentials. Team never sees or controls any of it.
- **Git hosts are authoritative for code:** reviewing and merging happen there. Team records the outcome that a person
  reports and refuses to call a ticket Done while its pull request is open.

### The console

The console (the page Team serves at its address) is organised around one question each:

| Tab | Answers | What is on it |
| --- | --- | --- |
| **Workspace** | What is available? What is everyone doing? | Five tiles (available to claim, yours, everyone else, to review, repository state), what the team is working on right now, each project's counts, and the members. |
| **Projects** | Which projects are there, and who is on them? | The projects, creating one, each project's **People & invites**. |
| **Board** | What work is there in this project? | The shared board: Backlog, Available, In Progress, Review, Done, and a ticket's details and actions. A project switcher keeps the Board, Repository and Activity tabs on one project. |
| **My Work** | What am I doing, and what waits for me? | What needs your attention (changes requested, a pull request with conflicts, behind its base, ready to submit, stale), your tickets in progress and in review with their Git links, your open pull requests, and the state of your own branches. **Open in my runner** is on every ticket you hold. |
| **Reviews** | What needs a decision? | Tickets in review, those asked of you first: author, branch, pull request, commits, mergeability, and links to the pull request, branch and comparison on the Git host. Record that you merged, mark done, or request changes. Team does not reproduce a code-review interface; the diff is on the Git host. |
| **Repository** | What is the Git state? | What members' Werkbords reported: branches, pull requests, and what needs attention. |
| **Activity** | What happened? | The project's history. |

The tab badges show what waits (available tickets, your tickets, reviews for you, repository problems). The console
shows a banner when its live connection drops and refreshes everything by itself when it returns (see
[Staying in step](#staying-in-step)).

Workspace offers separate **Working now**, **Projects** and **Members** sections; Repository separates attention,
pull requests and branches. Small windows show one selected board column, and **New ticket** opens its form only
when needed. Dragging a ticket between columns performs the same permitted action as claim, assign, submit or
complete; it does not bypass ownership or review rules.

**Clear Done** moves completed tickets into the board's searchable **Archive**. **Close ticket** can also archive
unfinished work when the member has permission to manage it. Details, commits, pull requests and activity are
retained; **Restore** returns the ticket to its previous status. Archived tickets leave active work counts and
attention lists. Closing in Team changes coordination metadata only: stop an agent in the member's own Werkbord.

## Concepts

- **Workspace.** A team's shared space. It has a name and exactly one **owner**, the person who created it.
- **Member.** A person in a workspace, with a name, an optional email and a **role**. A member signs in with their own
  **token**. Names are unique within a workspace (ignoring case).
- **Project.** Something the team works on: a name, a description, and the repository's address if there is one. A Team
  project is *not* a Werkbord project: it is a record the team shares, not a checkout. Each member's own Werkbord decides
  where, and whether, they have the code on their computer. A repository address must not contain a password or a token:
  each member signs in to Git with their own credentials.
- **Project membership.** A member is on zero or more projects. The person who creates a project is on it.
- **Role.** What a *person* may do in the workspace: **Owner**, **Admin** or **Member**. Roles are about people; what a
  *device* does for the workspace is a [capability](#devices), and is independent of any role.
- **Project role.** What a member may do inside one project: **owner**, **reviewer** or **member**. A workspace owner is
  an owner of every project. See [Project roles](#project-roles).
- **Ticket.** A unit of work on a project's board, numbered across the workspace (`WB-142`). See [Tickets](#tickets).
- **Invite.** A link or code that lets someone join one project. See [Invites](#invites).
- **Activity.** The project's history of coordination events. See [Activity](#activity).

### Roles and permissions

Authorization asks *"may this member do X?"*, never *"is this member an Owner?"*. Each role is a list of permissions in
`internal/team/domain/roles.go`:

| Permission | Owner | Admin | Member |
| --- | :---: | :---: | :---: |
| `workspace.view` — see the workspace | ✓ | ✓ | ✓ |
| `workspace.manage` — rename it | ✓ | ✓ | |
| `members.view` — see the members | ✓ | ✓ | ✓ |
| `members.manage` — add and remove members, reissue their tokens (not an admin's or the owner's, see below) | ✓ | ✓ | |
| `projects.view_all` — see every project (without it: only those you are on) | ✓ | ✓ | |
| `projects.create` | ✓ | ✓ | |
| `projects.manage` — edit or archive any project | ✓ | ✓ | |
| `project_members.manage` — change who is on a project | ✓ | ✓ | |
| `admins.manage` — appoint admins, and act on their accounts | ✓ | | |
| `workspace.ownership` — the workspace's ownership authority (its licence, in time) | ✓ | | |
| `devices.own` — register, rename and revoke your own devices | ✓ | ✓ | ✓ |
| `devices.view_all` — see every device in the workspace | ✓ | ✓ | |
| `devices.manage` — give a device a host capability; revoke any device | ✓ | ✓ | |

`members.manage` is not a way up: `Role.CanManage(target)` lets an admin add, remove and reissue the token of a **member**,
never of another admin (that is `admins.manage`, the owner's) and never of the owner, whose token would be the workspace.

**One owner, any number of admins, many members.** The owner appoints and removes admins (`PUT /members/{id}/role`
with `{"role": "admin"}` or `{"role": "member"}`); there is no limit on how many, and each is checked by the same permission
table, so a tenth admin changes nothing about how the first is. The change takes effect on the person's very next request,
their devices included. The owner's role never changes and a workspace never has a second owner: both are refused by the
service and again by the write itself. An admin can manage people (add, remove, reissue a member's token), projects, invitations,
approvals of devices that ask to join, and the workspace's hosts (give a device the Workspace Host or Connectivity Host
capability, add or remove a host), and can see the workspace's health and backups; what an admin cannot do is appoint
another admin, act on one, or touch the owner. **Admins do not become hosts, and hosts do not become admins:** a host is a
*device* that someone who manages devices gave a capability, and its owner is as much, or as little, as they were before.

A thing a member may not see (a project they are not on) is reported as *not found*, exactly like one that does not
exist, so it cannot be probed for. A thing they can see but not change is *forbidden*. Anyone may reissue **their own**
token.

To add a role later (a "Maintainer" who can create projects, say): add a constant and an entry in `rolePermissions`. No
schema change (a role is stored as text), no handler change, and the roles endpoint lists it. Add a test beside
`TestEachRoleHasExactlyItsPermissions`, which writes every role's table out in full.

### Devices

A **device** is a machine registered in the workspace by the member who owns it. The workspace records only what other
members and devices may know: the device's ID, its owner, its name, its **public** signing key, what it does for the
workspace, and when it was last seen. It never holds a private key, a Git, API or model credential, a path or an
environment variable: there is no column for one, and a test lists the columns to keep it so.

What a device does is its **capability**, independent of its owner's role:

| Capability | Meaning | Who may grant it |
| --- | --- | --- |
| `runner` | executes its owner's own work, on their machine | the device's owner, or `devices.manage` |
| `workspace_host` | runs Team for the workspace and holds its signing keys; will hold a replica of its data (the next phase) | `devices.manage` |
| `connectivity_host` | a customer-owned, publicly reachable machine that helps devices find each other and relays for them | `devices.manage` |

An Admin does not own a host because they are an Admin, and a member whose machine is a host does not become anything more
than a member. A host's role has a status (`none`, `joining`, `active`, `unavailable`). A device is **online** if it was
seen in the last two minutes (derived, so one that stops reporting goes offline by itself). **Revoking** a device is
permanent: it holds no capability, is not a host, and any envelope it signs is refused. Registration includes a signature
by the device's own key over the workspace, member and name, so a key can only be registered by whoever holds it.

From Team 2.5 a workspace can have its own **private network**, and devices **join** it with an invitation: they are
registered, given an address and a certificate, and (for a member's own device) a credential for the API that is checked on
every request, so revoking the device ends it at once. `GET /devices`, `POST /devices/{id}/revoke` and the routes under
`/network`, `/enrollment-invitations` and `/enrollments` are the API for it; `werkbord-team network|device|host …` is the
command line. Team still connects to no device and starts nothing on one. See [TEAM_NETWORK.md](TEAM_NETWORK.md) and
[the architecture decisions](adr/0001-team-production-architecture.md) ([0002](adr/0002-customer-owned-network.md)).

## The collaborative workflow

Everything below is **coordination**. Team stores *who is doing what and what they say they produced*; the work itself
(editing files, running agents, committing, pushing, opening pull requests) happens in each developer's own Werkbord
on their own computer. Nothing in this section gives one member any reach into another's machine.

```
        ticket created
              │
         ┌────▼────┐   promote   ┌───────────┐  claim / assign  ┌─────────────┐  submit  ┌────────┐ complete ┌──────┐
         │ Backlog │ ──────────▶ │ Available │ ───────────────▶ │ In Progress │ ───────▶ │ Review │ ───────▶ │ Done │
         └─────────┘ ◀────────── └───────────┘ ◀─────────────── └─────────────┘ ◀─────── └────────┘          └──────┘
                       put back                    release                      request changes                  │
                                                                                                      reopen ◀───┘ (back to Available)
```

The recommended Git flow around it (nothing forces a developer through it, and **Team never merges**):

```
claim ──▶ branch named wb-142-authentication-error ──▶ work and commit locally ──▶ push ──▶ open a pull request
      ──▶ submit for review (ticket enters Review) ──▶ reviewer reviews and merges on the Git host ──▶ ticket Done
```

### Project roles

| Permission | Owner | Reviewer | Member |
| --- | :---: | :---: | :---: |
| `tickets.view`, `tickets.create`, `tickets.claim` | ✓ | ✓ | ✓ |
| `git.report` — report your own branches, commits and pull requests | ✓ | ✓ | ✓ |
| `handoff.own` — open a ticket **you hold** in your own runner | ✓ | ✓ | ✓ |
| `activity.view`, `repository.view` | ✓ | ✓ | ✓ |
| `tickets.review` — send work back, mark it done | ✓ | ✓ | |
| `git.report_any` — report Git facts for a ticket someone else holds (to record a merge) | ✓ | ✓ | |
| `tickets.edit` — edit any ticket, promote or demote it | ✓ | | |
| `tickets.assign` — assign, reassign, or release a ticket someone else holds | ✓ | | |
| `tickets.reopen` | ✓ | | |
| `invites.manage` — create, list and revoke invites | ✓ | | |
| `members.manage` — add and remove people, change project roles | ✓ | | |

As with workspace roles, code asks `Role.Can(permission)`, never "is this an owner"; a new project role is one entry in
`internal/team/domain/project_roles.go`. Whoever creates a project is its owner, and a workspace member whose role carries
`projects.manage` (the workspace owner) is an owner of **every** project, even one they are not on. They can see and manage
it, but only a member of the project can *hold* its tickets. A project a member is not on does not exist for them (404).

### Tickets

A ticket has a **title**, **description**, **requirements/context**, **status**, **assignee** (the active owner while it
is held), **creator**, **timestamps** (created, updated, claimed, submitted, completed), and Git facts: the related
**branch**, **commits** and **pull request**. Numbers (`WB-142`) run across the whole workspace, so two projects that
share a repository never produce the same branch name.

The legal moves are written once, in `domain.TicketStatus.CanTransitionTo`:

| From | To | How | Who |
| --- | --- | --- | --- |
| Backlog | Available | `move` | creator, or `tickets.edit` |
| Available | Backlog | `move` | creator, or `tickets.edit` |
| Backlog / Available | In Progress | `claim` (yourself) or `assign` (a member of the project) | any member / `tickets.assign` |
| In Progress | Available | `release` | the holder, or `tickets.assign` |
| In Progress | In Progress | `assign` to someone else (**reassign**: same branch, new owner) | `tickets.assign` |
| In Progress | Review | `submit` (optionally with a pull request and a named reviewer) | the holder, or `tickets.assign` |
| Review | In Progress | `request-changes` (with a note) | `tickets.review` |
| Review | Done | `complete` | `tickets.review` |
| Done | Available | `move` (**reopen**; clears the owner, branch and pull request) | `tickets.reopen` |

Anything else is refused with a `409` that says what the ticket is and what it can do. A few rules worth knowing:

- **Claiming is atomic.** `claim` is one guarded `UPDATE ... WHERE status = 'available' AND assignee_id IS NULL`, so of any
  number of simultaneous claims exactly one succeeds and the rest get `409 "WB-1 was already claimed by Bo"`. The database
  also refuses, by `CHECK`, a ticket that is in progress with nobody on it or available with somebody on it.
- **Edits are version-checked.** Every write bumps `version`; `PATCH` with a stale `version` is a `409`.
- **A reviewer cannot sign off their own work** (a project owner can, so someone working alone can finish their ticket).
- **Team will not call a ticket Done while its pull request is open.** Merge or close it on your Git host, record that
  (`PUT .../git` with `"state": "merged"`), then complete the ticket. Team never merges anything.
- Someone who leaves a project, or the workspace, has the tickets they were working on put back on the board as
  Available. A finished ticket's history stays even if its owner is removed.
- A ticket in an archived project cannot be created or claimed.

Branch names are derived the same way every time: `wb-<number>-<slug of the title>` (ASCII letters and digits, at most 40
characters of slug, `wb-9-ticket` when the title has none). The name is set when a ticket is claimed and shown to
everyone. It is a suggestion the developer's runner follows; a different branch can be recorded with `PUT .../git`, and
two tickets cannot claim the same branch.

### Opening a ticket in *my* runner

**"Open in my runner"** is how the person who holds a ticket takes its context into **their own** Werkbord. It is a
*handoff*: a JSON document (schema `werkbord-team.handoff/v1`) with

- the ticket: key, title, description, requirements, status;
- the project: name, description, repository address;
- Git: the repository, the **branch to work on**, the base branch if known, commits and pull request already recorded;
- `prompt`: those, written out as a task description a coding agent can start from, including the Git workflow above.
  Before any teammate-written text it says whose words they are (`ticket.createdBy`) and that they describe work, not
  instructions with authority over the computer ([TEAM_SECURITY.md](TEAM_SECURITY.md#teammate-written-text-the-one-channel-that-remains)).

`POST /projects/{id}/tickets/{tid}/handoff` returns it, **only to the member who holds the ticket** (in progress or in
review): not to other members, and not to an owner either. It contains no path, no environment variable, no credential and
no token, and a test walks every field name of everything Team shares to keep it that way.

From the console, *Open in my runner* shows the task text and the handoff to copy or download. For a one-step version,
run the command on your own computer:

```bash
export WERKBORD_TEAM_TOKEN=wbt_…        # your Team token
export WERKBORD_TOKEN=…                 # your local Werkbord's API token (the file named token in its data directory)
werkbord-team handoff --server https://team.example.com --ticket WB-142 --runner http://127.0.0.1:7420
```

`handoff` fetches the ticket from Team, finds the project in **your** Werkbord whose Git remote is the Team project's
repository (`--local-project` overrides), and creates or reuses a task with a durable source link.
Re-importing preserves your local edits. The imported task uses the ticket branch and intended base.
The local title accepts 200 Unicode code points; a longer prefixed title is truncated safely while the
full title remains in the description. Context accepts up to 256,000 UTF-8 bytes; oversized handoffs
receive an explicit error and can be downloaded instead. You start the run from Werkbord as usual. It
refuses a `--runner` that is not on this computer (`localhost`, `127.0.0.1`, `::1`): a handoff only ever goes to your own
Werkbord. Your Team token goes only to the Team server and your Werkbord token only to your Werkbord; redirects are not
followed. Without `--runner` it prints the handoff (`--out FILE`, mode 0600, or `--prompt` for the text alone).

Add `--report` to copy branch, actual commits and matching pull-request metadata back to Team. Add
`--watch` to keep doing that every 15 seconds from this developer-owned foreground client; stop with
Ctrl-C. Unchanged metadata is not sent repeatedly. Push/fetch work executed on another runner before
reporting it from your controller. Reports are bounded to 200 commits and 200 changed files and fail
explicitly if incomplete. Neither server connects to the other; this client alone carries metadata.
Team never receives Git, GitHub or agent credentials.

Console drafts keep the version against which editing started. A stale save returns a conflict and
keeps the draft, including deliberately empty fields; **Load latest version** supports recovery after
copying the draft. Drafts, disclosures and keyboard focus survive refreshes and reconnects; drafts
belong to their project and ticket. Board and Reviews use the same completion gates. Board ticket
links use `?tab=board&project=ID&ticket=ID` and support browser back/forward. Renewing your own token
adopts and persists the replacement before refreshing; the previous token stops authenticating.

### Git metadata, reported not discovered

Team holds no Git or GitHub credential and never opens a repository, so it cannot *look* at one. Instead, the developer's
own Werkbord (which has their credentials) **reports** facts and Team records them:

- `PUT /projects/{id}/tickets/{tid}/git` — for a ticket in progress or in review: `branch`, `commits`
  (`sha`, `subject`, `author`, `committedAt`), `pullRequest` (`url`, `number`, `state` open/merged/closed, `draft`,
  `mergeable` unknown/mergeable/conflicting, `baseBranch`, `behind`, `ahead`) and `state` (the branch's `headSha`,
  `baseBranch`, `ahead`, `behind`, `lastCommitAt`, and `files` changed). Only the holder (or a reviewer/owner) may report.
- `POST /projects/{id}/repository/branches` — any member of the project reports the branches they see, and names the ones
  that are `gone`.

Everything is validated: branch names by Git's ref rules, commit hashes as hex, pull request addresses must be `https`
with no credentials, query or fragment, file paths must be repository-relative. Only metadata is accepted; file contents
and diffs are not, and there is no field that could carry a path on the reporter's machine.

### Conflict awareness

`GET /projects/{id}/repository` (and the console's **Repository** tab) assembles what has been reported:

- **Branches**: reported branches, and the branch of every ticket in progress or in review, each linked to its ticket and
  holder, with commits ahead/behind and last activity. Base branches (anything others are based on) are marked.
- **Pull requests** with their state, draft flag, mergeability and how far behind the base they are.
- **Needs attention**, worst first:
  - `conflict` — an open pull request GitHub says has merge conflicts;
  - `behind` — a developer's branch is behind its base branch;
  - `overlap` — two active branches change the same files, so whoever merges second may have conflicts;
  - `stale` — no reported activity for 14 days on a branch, or on a ticket in progress;
  - `closed` — a pull request closed without merging while its ticket is still in review;
  - `merged` — a pull request is merged and the ticket can be marked Done;
  - `leftover` — a finished ticket's branch that can probably be deleted;
  - `orphan` — an active branch no ticket is linked to.

This is **awareness**. Every line is "as last reported by someone". Team does not attempt to solve a conflict, rebase,
merge, or touch a repository; that stays in each developer's checkout and on the Git host.

### Invites

A project owner creates an **invite** (`POST /projects/{id}/invites`): the person joins as a `member` or a `reviewer`
(never an owner), for up to 100 uses and 30 days (defaults: one use, seven days). The response carries the **code**
(`wbi_` + 128 random bits), which the console shows once, **as a code and not as a link**: a page's own address is the address
of the computer it is open on (the Team app's window is a page of a service on `127.0.0.1`), so a link would mean nothing to
anyone else. Only the code's hash is stored; listings never show it. An invite can be revoked.

- Someone who is already in the workspace uses `POST /invites/join` from their enrolled device: in the console, **Projects >
  Join a project with a code**.
- A person who is new to the workspace first joins it with an invitation from **Members**, which also sets up their computer;
  after that the code works. `POST /invites/redeem` (a new member and token from a bare code) is answered only to the
  computer the host runs on: from another computer it is refused with `403 device_required` and an explanation.
  Where it is allowed, if anything fails (the name is taken, say) the use is not counted.
- A member token used from another computer is refused with `401 device_required` (it is local administration only), and a
  device whose clock differs from the host's by more than the proof's 30 seconds of skew with `401 clock_skew`. Both say so
  in their message; the console shows it and does not sign the person out.
- A code that never existed, expired, was revoked or is used up all answer the same `404`, so codes cannot be probed.
  The check and the use are one `UPDATE`, so a single-use invite cannot be spent twice.

### Activity

A lightweight history of coordination events for each project, newest first (`GET /projects/{id}/activity`):
`ticket.created`, `ticket.claimed`, `ticket.released`, `ticket.reassigned`, `ticket.work_submitted`,
`ticket.pull_request_created`, `ticket.review_requested`, `ticket.changes_requested`, `ticket.completed`,
`ticket.reopened`, `ticket.moved`, `ticket.handed_off`, `ticket.pull_request_merged`, `project.member_joined`. There is no
chat and no comments: Team is a coordination tool, not a messenger.

### My Work, Reviews and the overview

Three views span every project the member can see. They only read; each is built from what is already stored and is
scoped to the caller (a project you are not on contributes nothing).

- `GET /my-work` — **inProgress** (tickets you hold, with who assigned them if it was not you), **submitted** (yours,
  in review), **pullRequests** (yours, still open), **needsAction**, **reviewsWaiting**, and **repositories** (the
  repository state that concerns your own branches). Each item carries the project, the ticket with its commits, the
  pull request's merge state (`none`, `mergeable`, `conflicting`, `unknown`, `merged`, `closed`) and links.
- `GET /reviews` — tickets in review that you may review or that you submitted. Each has `mine`, `requestedOfMe`,
  `canReview`, `canComplete` and, when it cannot be completed yet, a plain `blocker` ("the pull request is still open:
  merge it on your Git host, then record the merge"). The blocker uses the same rules `complete` enforces.
- `GET /overview` — each project's counts by status, your tickets and reviews in it, repository problems and warnings,
  everyone's tickets in progress or in review (`working`), and how many tickets are `available`.

Links (`links.pullRequest`, `branch`, `compare`, `repository`, `commitPrefix`) are built from the project's repository
address for GitHub and GitLab and from the reported pull-request address; Team never contacts the host.

### Staying in step

Every change to a project's board or reported repository state bumps the project's `revision` in the same transaction,
and database triggers move a **workspace revision** whenever anything members can see changes (a ticket, the people,
a role, a project, the workspace's name), so no new kind of write can forget to.

`GET /sync?since=<revision>&after=<event id>&wait=20` holds the request open until the workspace revision is not
`since` (or about 20 seconds pass) and answers:

```json
{ "revision": 41, "changed": true, "cursor": 118,
  "events": [ {"id": 118, "kind": "ticket.claimed", "ticketKey": "WB-4", "actorName": "Bo", "projectName": "Shop", …} ],
  "projects": [ {"id": "tpj_…", "revision": 17} ] }
```

- **Fast.** The server wakes waiters in memory the moment a write commits: a claim reaches another member's open console
  in well under a second (asserted in tests).
- **Events are hints, state is authority.** They exist so a client can say "Bo claimed WB-4"; what it shows is always
  re-read from the board, My Work or Reviews. `events` cover only projects the caller can see.
- **Starting.** Call without `after` (or `wait=0`): you get the current `revision` and `cursor` and no events.
- **Reconnecting.** A client that was offline calls again with the revision and cursor it last had. If anything changed
  it is told so, with the events it missed. Then it reloads.
- **When it cannot catch up.** `truncated: true` means more than 100 events were missed; `reset: true` means the
  server's revision is *lower* than the client's (a restored backup). Either way the client reloads everything and
  carries on from the new `revision` and `cursor`. A server restart needs neither: revisions are stored.
- **Bounded.** A member may hold 8 open syncs; more get `429` with `Retry-After`. Shutdown ends them at once.

`GET /projects/{id}/sync` is the older per-project form of the same long poll and still works.

The console keeps one such request open. It shows a banner while the connection is down, retries with a growing pause
(up to 30 seconds), and on reconnecting reloads what it shows; coming back to a hidden tab or the network returning
reloads at once. What someone is typing is kept across reloads.

### Concurrency

Collaborative operations are written so that two people acting at once cannot corrupt the board:

| Operation | What guarantees it |
| --- | --- |
| **Claiming** | One guarded `UPDATE … WHERE status = 'available' AND assignee_id IS NULL`: of any number of simultaneous claims exactly one succeeds, the rest get `409` naming the holder. |
| **Every other transition** (release, submit, assign/reassign, request changes, complete, move, reopen) | Run inside the one write transaction, which first re-reads the ticket, checks the move against the single transition table, and saves with a version check (`WHERE version = ?`). Racing moves have one winner; the loser gets `409` describing what the ticket is now. |
| **Project membership changes** | Membership, the role check and the ticket write are in the same transaction as the removal, so a member removed while claiming either fails to claim or has the ticket put straight back; a ticket is never left held by someone who is not on the project. |
| **Text edits** | `PATCH` with the `version` you loaded is refused with `409` if someone else changed the ticket since. |
| **Reported Git facts** | Reports are facts, last write wins, except that a **merged pull request cannot be reported back to open** (a late report from a slow client is refused), and two tickets cannot claim one branch. |
| **Invites** | Check and use are one `UPDATE`: a single-use invite cannot be spent twice. |
| **Ticket numbers, history** | Numbers are unique per workspace in the database; every successful write leaves its history entry in the same transaction. |

Writes are serialised by SQLite (one writer, `BEGIN IMMEDIATE`), and every guard above is also in the SQL. Tests:
`TestConcurrentClaimsGiveTheTicketToExactlyOneMember`, `TestRacingTransitionsHaveExactlyOneWinner`,
`TestRemovingAMemberWhileTheyClaimNeverLeavesAGhostHolder`, `TestAStormOfOperationsLeavesAConsistentBoard` (every kind of
operation from eight goroutines; afterwards every ticket is consistent, the versions add up to the successful writes and
the history is complete), `TestAMergedPullRequestCannotBeTurnedBackIntoAnOpenOne`.

## Workspace resilience

`GET /resilience` (and the Team app's **Workspace** screen) says how resilient the workspace is, in the words an
administrator needs, from facts the workspace already has: the registry, what each device says about itself, the database's own
report, the network's checks.

```
Workspace resilience
  Hosts: 3/3 online          Quorum: healthy          Database: healthy
  Connectivity Hosts: 2      Remote access: available Backups: current
```

Every weakness comes with what to do about it, and a button the screen can offer. The words are fixed so every screen says the
same thing: *"Only one Workspace Host is configured."* · *"Two hosts are offline. Workspace is read-only until quorum returns."* ·
*"No Connectivity Host is reachable from outside your network. Local use works, but remote access cannot be guaranteed."* ·
*"This device sleeps automatically. It is not ideal as a Workspace Host."* The judgement is one pure function
(`domain.AssessResilience`), tested on its own.

Each device reports, about itself and with its own credential, that it is there and what kind of machine it is (`POST
/device/heartbeat`: a platform, a kind (desktop, laptop, server), whether it has been seen to go to sleep, a count, a version). That
is how a laptop that sleeps is called out *before* it is made a host, and how the workspace knows a host is online. It holds no
path, hostname, address or credential, and no column could.

## Requests between your own devices

A person can ask one of their own runners to do something from another of their own devices (their phone, another computer):
*open this ticket*, *start that approved task*, *cancel this run*, *answer this question*, *what are you doing?*. These are the
five actions in `internal/envelope/actions.go`, each with a payload of identifiers; there is no command, path or script in any of them.

1. The asking device **signs** the request with its own application key: who is asking, in which workspace, for which device,
   what, when it expires, a nonce, and the payload's hash.
2. A Workspace Host **stores and routes** it (`POST /messages`), exactly as signed. It checks the signature, the expiry, that
   the signer is the device whose credential sent it, and that the target is a runner **of the same person**. It cannot make one:
   it holds no sender's key.
3. The target device **collects** it (`GET /device/messages`) and verifies it again itself, against the sender's registered public key and
   its own replay cache, before it does anything; then it passes the semantic action to that person's *own* Werkbord, which applies its
   normal execution policies and approvals, and says what happened (`ack`).

So a host that routes a request cannot forge one, change one (every field is signed), redirect one (the target is signed), keep one
for later (it expires in minutes), or ask someone else's device (both devices must be the signer's). See
[TEAM_SECURITY.md](TEAM_SECURITY.md#requests-between-a-persons-own-devices-team-27).

## Running it

Team 3.0 requires a vendor-signed offline license and authenticated installation artifacts. Read [TEAM_INSTALL.md](TEAM_INSTALL.md) and [TEAM_LICENSE.md](TEAM_LICENSE.md) first. Werkbord operates no customer runtime infrastructure; you operate the workspace/connectivity hosts, backups and local runners. The formal boundaries and gate evidence are in [TEAM_SECURITY.md](TEAM_SECURITY.md) and [TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md).

```bash
make build-team                                              # or install a release: scripts/install-team.sh
export WERKBORD_TEAM_LICENSE_FILE=/customer/licenses/team.json
werkbord-team workspace create --name "Acme" --owner "Ada"   # creates the workspace; prints the owner's token, ONCE
werkbord-team serve                                          # http://127.0.0.1:7430
```

`workspace create` prints a sign-in link (`http://127.0.0.1:7430/#token=…`): open it and the console signs you in (the
token is in the URL's fragment, which the browser never sends to a server, and the console removes it from the address
bar). This bearer is for local administration. Other devices join through a signed invitation and administrator approval, obtain their own credentials, and sign remote API requests using their own device keys. The current Team app/daemon supplies that signing client.
Only the token's SHA-256 is stored, so a lost token cannot be recovered, only **reissued** (which signs out the old one).

| Command | |
| --- | --- |
| `werkbord-team workspace create --name N --owner O [--email E]` | Start a workspace. Run on the computer that hosts the server. This is the only way a workspace is created: it is not reachable over HTTP. |
| `werkbord-team serve [--addr A] [--data-dir D] [--log-level L] [--log-format F]` | Run the server in the foreground. |
| `werkbord-team migrate` | Apply database migrations and exit (for a cluster, once, for the whole cluster). |
| `werkbord-team storage status\|backup\|backups\|verify-backup\|restore\|migrate\|rollback` | Where the workspace's data is kept, and what to do about it: [TEAM_STORAGE.md](TEAM_STORAGE.md). |
| `werkbord-team host promote\|collect\|remove` | Add a Workspace Host (its keys, and a copy of the data) and take one out. |
| `werkbord-team handoff --ticket KEY [--runner URL] ...` | On a **member's own computer**: open a ticket you hold in your own local Werkbord (see [above](#opening-a-ticket-in-my-runner)). |
| `werkbord-team version` | Print the version. |

Settings (flags win over environment): `WERKBORD_TEAM_ADDR` (default `127.0.0.1:7430`), `WERKBORD_TEAM_DATA_DIR`
(default `werkbord-team` in your user config directory; the database is `team.db` there), `WERKBORD_TEAM_LOG_LEVEL`,
`WERKBORD_TEAM_LOG_FORMAT`. These are separate from the individual product's `WERKBORD_*` (formerly `DEVBOARD_*`), so both can run side by side.

### The private network (Team 2.5)

`werkbord-team workspace create --endpoint <a name or address that reaches this machine from outside>` gives the
workspace a private network of its own (no account, no service of anyone else's), and makes the machine its first
Workspace Host. Invitations, joining, approval, more than one host, revoking a device, what the network allows and what it
cannot promise about the Internet are in [TEAM_NETWORK.md](TEAM_NETWORK.md). The settings are
`WERKBORD_TEAM_ENDPOINTS`, `WERKBORD_TEAM_BOOTSTRAP_ADDR`, `WERKBORD_TEAM_NETWORK_PORT`, `WERKBORD_TEAM_NETWORK_NODE`,
`WERKBORD_TEAM_NEBULA_DIR`, `WERKBORD_TEAM_KEY_STORAGE` and `WERKBORD_TEAM_PKI_PASSPHRASE_FILE`. Nebula and administrator approval are defaults; `--network=false --storage=single-file` is explicit loopback evaluation only.

### Reaching it from other computers

The local API listens on loopback; production rejects a wider `--addr`. Other devices reach the separately bound Nebula API, with a device bearer plus signed proof for every request and durable nonces for mutations. No Tailscale account is required. Do not expose the local bearer administration API through a reverse proxy. Older remote bearer clients are refused and must migrate to enrolled device clients; there is no silent dual-network mode.

### Where the data is kept (Team 2.6)

A new workspace keeps its data in a **cluster of Workspace Hosts**: each runs the pinned rqlite (SQLite replicated with Raft,
shipped with Team and supervised by it) and holds a full copy. It starts as a cluster of one host; three are recommended, and
the loss of one then leaves the workspace working. If a quorum is lost, writes and remote authentication stop (`503 read_only`), cached local diagnostic reads remain available, and
nothing is merged later. `werkbord-team storage status` says exactly where things stand. Everything, including moving an existing
`team.db` into a cluster and back, adding and removing hosts, and recovering from a lost quorum, is in
[TEAM_STORAGE.md](TEAM_STORAGE.md). `--storage single-file` keeps a workspace in one SQLite file, as before.

### Backups

Replication is not a backup. Set `WERKBORD_TEAM_BACKUP_DIR` (a directory you own) and a Workspace Host takes a verified backup of the
whole workspace database on a schedule, with a retention policy; `werkbord-team storage backup | backups | verify-backup | restore`
([TEAM_STORAGE.md](TEAM_STORAGE.md#backups)).

For a workspace kept in one file: the database is `<data dir>/team.db` (SQLite, WAL). Before a migration changes it, a consistent
copy is written to `<data dir>/backups/team-v<version>-<time>.db` (the newest five are kept). To back up a running server, use
`sqlite3 team.db ".backup copy.db"`; copying the file alone while it is being written is not safe.

A workspace also has sealed `<data dir>/pki/` material. macOS production uses Keychain with separate device/authority wrapping keys; other supported hosts use an external owner-only passphrase file. Back up the authority ciphertext and recoverable unlock material separately from the database. Database backups are plaintext and require customer-managed encryption. See [backup/recovery](TEAM_BACKUP_RECOVERY.md), [host replacement](TEAM_HOST_REPLACEMENT.md), and [disaster recovery](TEAM_DISASTER_RECOVERY.md).

## API

Remote protocol gate: device bearer credentials require `Werkbord-Device-Proof` v1, bound to the full request and identity. Mutations consume a replicated nonce. Member tokens are local-loopback compatibility only. The owner can import a signed renewal with `PUT /api/team/v1/license`, including after runtime expiry. Unsupported proof/envelope versions and unknown network-description fields fail closed; do not assume an older remote client works against this major version.

JSON under `/api/team/v1`. Every route except `/health` needs `Authorization: Bearer <token>`. The token identifies
the member, and so the workspace: no URL names one. Errors are `{"error": {"code": "...", "message": "..."}}` with
`unauthorized` (401), `forbidden` (403), `not_found` (404), `conflict` (409), `invalid` (400), `busy` (429).

| Method and path | Who | |
| --- | --- | --- |
| `GET /health` | anyone | status, product, version |
| `GET /me` | any member | the member, the workspace, and the permissions their role gives |
| `GET /roles` | any member | each role and its permissions |
| `GET /workspace`, `PATCH /workspace` `{name}` | any / `workspace.manage` | |
| `GET /members` | `members.view` | owner first |
| `POST /members` `{name, email?, role?}` | `members.manage` | creates a member; the response carries their token, once. `role` defaults to `member`; `owner` is refused. |
| `DELETE /members/{id}` | `members.manage` | removes them from the workspace and every project. The owner cannot be removed. |
| `POST /members/{id}/token` | yourself, or `members.manage` | issues a new token and invalidates the old |
| `PUT /members/{id}/role` `{role}` | `admins.manage` (the owner) | `admin` or `member`. The owner's role never changes and there is no second owner |
| `GET /resilience` | `devices.view_all` | how resilient the workspace is, in words, with what to do about each weakness: [Workspace resilience](#workspace-resilience) |
| `POST /device/heartbeat` `{platform, form, sleeps, sleepEvents, version}` | **a device, with its own credential** | the device says it is here and what kind of machine it is; the time is the workspace's |
| `POST /messages` (a signed envelope) | a device, with its own credential | stores a request the device signed, for another runner of the same person: [Requests between your own devices](#requests-between-your-own-devices) |
| `GET /messages`, `GET /messages/{id}` | the person who asked | what happened to the requests they made |
| `GET /device/messages?wait=N` | a device, with its own credential | the signed requests waiting for it (it checks them itself) |
| `POST /device/messages/{id}/ack` `{state, result}` | the device the request is for | `done` or `refused`, once, with a short JSON answer |
| `GET /projects` | any member | all (`projects.view_all`) or only yours |
| `POST /projects` `{name, description?, repository?}` | `projects.create` | the creator is put on it |
| `GET /projects/{id}` | on the project, or `projects.view_all` | |
| `PATCH /projects/{id}` `{name?, description?, repository?, archived?}` | `projects.manage` | |
| `GET /projects/{id}/members` | as `GET /projects/{id}` | |
| `PUT /projects/{id}/members/{memberId}` `{role?}` | project `members.manage` | idempotent; `role` (owner, reviewer, member) sets or changes their project role |
| `DELETE /projects/{id}/members/{memberId}` | project `members.manage` | their tickets in progress go back on the board |
| `GET /projects/{id}/people` | on the project | members with their project roles |
| `GET /projects/{id}/board` | on the project | project, `revision`, columns, active `tickets`, `archived` tickets, people, your project role and what it allows |
| `GET /overview` | any member | the projects you can see with their counts, everyone's work in progress, and what is available |
| `GET /my-work` | any member | your tickets, pull requests, what needs your attention, and your branches' state |
| `GET /reviews` | any member | tickets in review you may review or submitted, with mergeability and blockers |
| `GET /sync?since=N&after=E&wait=S` | any member | workspace long poll with events; see [Staying in step](#staying-in-step). `429` when you hold too many |
| `GET /projects/{id}/sync?since=N&wait=S` | on the project | the same for one project: `{revision, changed}` |
| `POST /projects/{id}/tickets` `{title, description?, requirements?, status?}` | `tickets.create` | `status` is `backlog` (default) or `available` |
| `GET /projects/{id}/tickets/{tid}` | `tickets.view` | with commits |
| `PATCH /projects/{id}/tickets/{tid}` `{title?, description?, requirements?, version?}` | creator, or `tickets.edit` | stale `version` → 409 |
| `POST …/tickets/{tid}/move` `{status}` | creator or `tickets.edit`; `tickets.reopen` | backlog ⇄ available; done → available |
| `POST …/tickets/{tid}/claim` | `tickets.claim`, on the project | atomic; 409 names who has it |
| `POST …/tickets/{tid}/release` | the holder, or `tickets.assign` | |
| `POST …/tickets/{tid}/assign` `{memberId}` | `tickets.assign` | assign or reassign |
| `POST …/tickets/{tid}/submit` `{reviewerId?, pullRequest?}` | the holder, or `tickets.assign` | → Review |
| `POST …/tickets/{tid}/request-changes` `{note?}` | `tickets.review` | → In Progress |
| `POST …/tickets/{tid}/complete` | `tickets.review` | → Done; refused while the pull request is open |
| `PUT …/tickets/{tid}/git` `{branch?, commits?, pullRequest?, state?}` | the holder; or `git.report_any` | reported Git metadata |
| `POST …/tickets/{tid}/handoff` | the holder only | the handoff for their own runner |
| `GET /projects/{id}/repository` | `repository.view` | branches, pull requests, what needs attention |
| `POST /projects/{id}/repository/branches` `{branches:[{name, headSha?, baseBranch?, ahead?, behind?, lastCommitAt?, files?}], gone?:[]}` | a member of the project | |
| `GET /projects/{id}/activity?before=&limit=` | `activity.view` | |
| `GET`/`POST /projects/{id}/invites`, `DELETE …/invites/{inviteId}` | `invites.manage` | the code is in the `POST` response only |
| `POST /invites/redeem` `{code, name, email?}` | **anyone with a valid code** | creates the member and their token, once |
| `POST /invites/join` `{code}` | any member | join a project with an invite |
| `GET /devices` | own devices, or `devices.view_all` | the registry |
| `POST /devices/{id}/revoke` | the owner of the device, or `devices.manage` | ends the device in the application and on the network at once |
| `PUT /devices/{id}/network` `{bootstrapEndpoints?, networkEndpoints?, discovery?, relay?}` | the device itself (endpoints), or `devices.manage` | where a host can be reached and what it does for the network |
| `POST /devices/{id}/provision` | `devices.manage` | seals the workspace's signing keys to a Workspace Host, for it to collect |
| `GET /network` | `devices.view_all` | the network's hosts, reachability, warnings and this host's own node |
| `PUT /network/approval` `{approval}` | `devices.manage` | `auto` or `admin`: whether a joining device waits for approval |
| `GET /network/config`, `POST /network/certificate` | **a device, with its own credential** | the device's configuration; a renewed certificate for its own key |
| `POST /network/checks` `{deviceId, endpoint, ok}` | a device, or `devices.manage` | the outcome of trying to reach a host |
| `GET /network/provision`, `POST /network/provision/ack` | a device, with its own credential | collect (ciphertext only) and acknowledge the keys sealed to it |
| `GET`/`POST /enrollment-invitations`, `DELETE …/{id}` | `members.manage` (`devices.manage` for a host) | the link is in the `POST` response only |
| `GET /enrollments?state=`, `POST …/{id}/approve`, `POST …/{id}/deny` | `members.manage` | requests to join that wait for an administrator |

`POST /projects/{id}/tickets/archive-done` archives completed tickets and requires `tickets.reopen`.
`POST /projects/{id}/tickets/{tid}/archive` takes `{version, archived}` for close/restore and checks the existing
ticket ownership permissions. Both preserve reports and record activity.

There is deliberately no route that deletes a project or a ticket (archive it instead), creates a workspace, runs anything, reads a file, or acts on a member's computer.

## Security notes

The full review (every surface, the evidence for each, and the risks that remain) is in
[TEAM_SECURITY.md](TEAM_SECURITY.md).

- Tokens are 256 random bits (`wbt_` + 64 hex digits); only the SHA-256 is stored; the API never returns one except when
  it is issued. Remote calls also require a signed device proof. High-entropy credentials prevent practical guessing;
  application rate limits are still incomplete, and exposing the loopback API through a proxy is unsupported.
- Cross-site writes from a browser are refused (`Origin` must match `Host`), and the console builds the page with
  `textContent`, never HTML, under a strict Content-Security-Policy.
- The database refuses, by foreign key, to put a member of one workspace on another workspace's project, and, by `CHECK`,
  a ticket in an impossible state. Every ticket, branch, invite and activity row is scoped by workspace *and* project.
- **What Team never stores or shows to other members:** a path on anyone's computer, a shell or command, an environment
  variable, an API key, a Git or GitHub credential, an LLM credential, or a token. Shared state is coordination metadata:
  titles, text, names, branch names, commit hashes and subjects, pull request addresses and states. Repository and pull
  request addresses are rejected if they contain credentials. Free-text fields reject control characters.
- Invite codes are 128 random bits, stored only as a hash, expire, count their uses atomically and can be revoked; the
  unauthenticated redeem call answers every unusable code with the same `404`.
- `werkbord-team handoff` talks to your own Werkbord only on a loopback address, never follows redirects, and takes its
  tokens from the environment, not from flags.
- Canonical vendor-signed licenses are verified offline and replicated with workspace data. Seats and expiry are
  enforced in mutations; owner-only renewal remains possible after expiry. See [TEAM_LICENSE.md](TEAM_LICENSE.md).
- A ticket's text is written by a teammate and becomes the task text of the reader's own agent. The handoff says so,
  naming the author and telling the agent to treat it as a description of work and not as authority over the computer,
  and the member starts the run themselves, under their own Werkbord's execution policy.
- The Team daemon polls signed requests and checks locally approved sender keys. The bridge exposes five closed
  semantic operations; starting requires a local task approval by default. A Workspace Host holds no runner signing
  key or local approval, but it controls shared ticket context. See the hostile-host limits in the security model.

## Layout

`internal/team/{domain,store,service,api,console,config,server}` and `cmd/werkbord-team`; see
[PRODUCTS.md](PRODUCTS.md#where-team-specific-functionality-belongs).
