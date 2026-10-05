# Werkbord Team

Werkbord Team is the shared workspace for a team that uses Werkbord. It records **who is on the team**, **which
projects exist**, **who is on which project**, and the **shared board** the team works from: tickets, who holds each one,
and the branches and pull requests that come out of them. It is a separate product from the individual Werkbord; see
[PRODUCTS.md](PRODUCTS.md) for how the two relate and where the code lives.

> **Team coordinates. It does not execute.** Every member keeps their own computer, their own Werkbord runner, and their
> own Git, GitHub and agent credentials. Team holds none of them and has no way to run anything on any member's machine.
> There is no remote-runner access, no shell, no file access. The server has no code path that starts a process, and a
> test (`internal/archtest`) keeps it that way.

## Concepts

- **Workspace.** A team's shared space. It has a name and exactly one **owner**, the person who created it.
- **Member.** A person in a workspace, with a name, an optional email and a **role**. A member signs in with their own
  **token**. Names are unique within a workspace (ignoring case).
- **Project.** Something the team works on: a name, a description, and the repository's address if there is one. A Team
  project is *not* a Werkbord project: it is a record the team shares, not a checkout. Each member's own Werkbord decides
  where, and whether, they have the code on their computer. A repository address must not contain a password or a token:
  each member signs in to Git with their own credentials.
- **Project membership.** A member is on zero or more projects. The person who creates a project is on it.
- **Role.** What a member may do in the workspace. Today: **Owner** and **Member**.
- **Project role.** What a member may do inside one project: **owner**, **reviewer** or **member**. A workspace owner is
  an owner of every project. See [Project roles](#project-roles).
- **Ticket.** A unit of work on a project's board, numbered across the workspace (`WB-142`). See [Tickets](#tickets).
- **Invite.** A link or code that lets someone join one project. See [Invites](#invites).
- **Activity.** The project's history of coordination events. See [Activity](#activity).

### Roles and permissions

Authorization asks *"may this member do X?"*, never *"is this member an Owner?"*. Each role is a list of permissions in
`internal/team/domain/roles.go`:

| Permission | Owner | Member |
| --- | :---: | :---: |
| `workspace.view` — see the workspace | ✓ | ✓ |
| `workspace.manage` — rename it | ✓ | |
| `members.view` — see the members | ✓ | ✓ |
| `members.manage` — add and remove members, reissue their tokens | ✓ | |
| `projects.view_all` — see every project (without it: only those you are on) | ✓ | |
| `projects.create` | ✓ | |
| `projects.manage` — edit or archive any project | ✓ | |
| `project_members.manage` — change who is on a project | ✓ | |

A thing a member may not see (a project they are not on) is reported as *not found*, exactly like one that does not
exist, so it cannot be probed for. A thing they can see but not change is *forbidden*. Anyone may reissue **their own**
token.

To add a role later (a "Maintainer" who can create projects, say): add a constant and an entry in `rolePermissions`. No
schema change (a role is stored as text), no handler change, and the roles endpoint lists it. Add a test beside
`TestOwnerCanDoEverythingAndAMemberAlmostNothing`.

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

`POST /projects/{id}/tickets/{tid}/handoff` returns it, **only to the member who holds the ticket** (in progress or in
review): not to other members, and not to an owner either. It contains no path, no environment variable, no credential and
no token, and a test walks every field name of everything Team shares to keep it that way.

From the console, *Open in my runner* shows the task text and the handoff to copy or download. For a one-step version,
run the command on your own computer:

```bash
export WERKBORD_TEAM_TOKEN=wbt_…        # your Team token
export DEVBOARD_TOKEN=…                 # your local Werkbord's API token (the file named token in its data directory)
werkbord-team handoff --server https://team.example.com --ticket WB-142 --runner http://127.0.0.1:7420
```

`handoff` fetches the ticket from Team, finds the project in **your** Werkbord whose Git remote is the Team project's
repository (`--local-project` overrides), and creates a task there; you start the run from your Werkbord as usual. It
refuses a `--runner` that is not on this computer (`localhost`, `127.0.0.1`, `::1`): a handoff only ever goes to your own
Werkbord. Your Team token goes only to the Team server and your Werkbord token only to your Werkbord; redirects are not
followed. Without `--runner` it prints the handoff (`--out FILE`, mode 0600, or `--prompt` for the text alone).

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
(`wbi_` + 128 random bits) and the console shows the link `https://team.example.com/#invite=<code>` once. Only the code's
hash is stored; listings never show it. An invite can be revoked.

- Someone without an account opens the link, picks a name, and `POST /invites/redeem` (the one unauthenticated call
  besides `/health`) creates their workspace membership, their own token, and their place on the project, in one step.
  If anything fails (the name is taken, say) the use is not counted.
- Someone who already has an account in the workspace uses `POST /invites/join` while signed in.
- A code that never existed, expired, was revoked or is used up all answer the same `404`, so codes cannot be probed.
  The check and the use are one `UPDATE`, so a single-use invite cannot be spent twice.

### Activity

A lightweight history of coordination events for each project, newest first (`GET /projects/{id}/activity`):
`ticket.created`, `ticket.claimed`, `ticket.released`, `ticket.reassigned`, `ticket.work_submitted`,
`ticket.pull_request_created`, `ticket.review_requested`, `ticket.changes_requested`, `ticket.completed`,
`ticket.reopened`, `ticket.moved`, `ticket.handed_off`, `ticket.pull_request_merged`, `project.member_joined`. There is no
chat and no comments: Team is a coordination tool, not a messenger.

### Staying in step

Every change to a project's board or reported repository state bumps the project's `revision`, in the same transaction.
`GET /projects/{id}/sync?since=<revision>&wait=20` holds the request open until the revision moves past `since` (or about
20 seconds pass) and answers `{"revision": N, "changed": true|false}`. The console keeps one such request open, so one
member's claim appears on the others' boards within moments, without polling, and reloads without losing what someone is
typing. The server wakes waiters in memory; because a waiter re-reads the revision from the database, a missed wake-up
costs at most one poll interval.

## Running it

```bash
make build-team                                              # or install a release: scripts/install-team.sh
werkbord-team workspace create --name "Acme" --owner "Ada"   # creates the workspace; prints the owner's token, ONCE
werkbord-team serve                                          # http://127.0.0.1:7430
```

`workspace create` prints a sign-in link (`http://127.0.0.1:7430/#token=…`): open it and the console signs you in (the
token is in the URL's fragment, which the browser never sends to a server, and the console removes it from the address
bar). From the **Members** tab, add everyone else: each gets their own token, shown once. Send it to them privately.
Only the token's SHA-256 is stored, so a lost token cannot be recovered, only **reissued** (which signs out the old one).

| Command | |
| --- | --- |
| `werkbord-team workspace create --name N --owner O [--email E]` | Start a workspace. Run on the computer that hosts the server. This is the only way a workspace is created: it is not reachable over HTTP. |
| `werkbord-team serve [--addr A] [--data-dir D] [--log-level L] [--log-format F]` | Run the server in the foreground. |
| `werkbord-team migrate` | Apply database migrations and exit. |
| `werkbord-team handoff --ticket KEY [--runner URL] ...` | On a **member's own computer**: open a ticket you hold in your own local Werkbord (see [above](#opening-a-ticket-in-my-runner)). |
| `werkbord-team version` | Print the version. |

Settings (flags win over environment): `WERKBORD_TEAM_ADDR` (default `127.0.0.1:7430`), `WERKBORD_TEAM_DATA_DIR`
(default `werkbord-team` in your user config directory; the database is `team.db` there), `WERKBORD_TEAM_LOG_LEVEL`,
`WERKBORD_TEAM_LOG_FORMAT`. These are separate from the individual product's `DEVBOARD_*`, so both can run side by side.

### Reaching it from other computers

By default Team listens on this computer only. To let teammates reach it, give `--addr` a wider address (for example
`0.0.0.0:7430`) **and put it behind HTTPS** (a reverse proxy such as Caddy or nginx) or on a private network such as a
VPN or tailnet. Team serves plain HTTP and says so in its log when it is not on loopback: tokens cross the network in the
clear otherwise. A token is required for every API request, on loopback too.

### Backups

The database is `<data dir>/team.db` (SQLite, WAL). Before a migration changes it, a consistent copy is written to
`<data dir>/backups/team-v<version>-<time>.db` (the newest five are kept). To back up a running server, use
`sqlite3 team.db ".backup copy.db"`; copying the file alone while it is being written is not safe.

## API

JSON under `/api/team/v1`. Every route except `/health` needs `Authorization: Bearer <token>`. The token identifies
the member, and so the workspace: no URL names one. Errors are `{"error": {"code": "...", "message": "..."}}` with
`unauthorized` (401), `forbidden` (403), `not_found` (404), `conflict` (409), `invalid` (400).

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
| `GET /projects` | any member | all (`projects.view_all`) or only yours |
| `POST /projects` `{name, description?, repository?}` | `projects.create` | the creator is put on it |
| `GET /projects/{id}` | on the project, or `projects.view_all` | |
| `PATCH /projects/{id}` `{name?, description?, repository?, archived?}` | `projects.manage` | |
| `GET /projects/{id}/members` | as `GET /projects/{id}` | |
| `PUT /projects/{id}/members/{memberId}` `{role?}` | project `members.manage` | idempotent; `role` (owner, reviewer, member) sets or changes their project role |
| `DELETE /projects/{id}/members/{memberId}` | project `members.manage` | their tickets in progress go back on the board |
| `GET /projects/{id}/people` | on the project | members with their project roles |
| `GET /projects/{id}/board` | on the project | project, `revision`, columns, every ticket, people, your project role and what it allows |
| `GET /projects/{id}/sync?since=N&wait=S` | on the project | long poll: `{revision, changed}` |
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

There is deliberately no route that deletes a project or a ticket (archive the project; move the ticket), creates a workspace, runs anything, reads a file, or acts on a member's computer.

## Security notes

- Tokens are 256 random bits (`wbt_` + 64 hex digits); only the SHA-256 is stored; the API never returns one except when
  it is issued. Because they are long and random there is no rate limit on failed sign-ins; add one at the proxy if you
  want it.
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
- No licensing or payment exists yet; they will live in `internal/team`.
- Because Team never reaches a member's machine, **a handoff is a pull, not a push**: the member chooses to bring a ticket
  into their own runner. A Team server cannot start, stop, or feed anything to any runner.

## Layout

`internal/team/{domain,store,service,api,console,config,server}` and `cmd/werkbord-team`; see
[PRODUCTS.md](PRODUCTS.md#where-team-specific-functionality-belongs).
