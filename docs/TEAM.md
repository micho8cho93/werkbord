# Werkbord Team

Werkbord Team is the shared workspace for a team that uses Werkbord. It records **who is on the team**, **which
projects exist**, and **who is on which project**. It is a separate product from the individual Werkbord; see
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
- **Role.** What a member may do. Today: **Owner** and **Member**.

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
| `PUT /projects/{id}/members/{memberId}` | `project_members.manage` | idempotent |
| `DELETE /projects/{id}/members/{memberId}` | `project_members.manage` | |

There is deliberately no route that deletes a project (archive it), creates a workspace, or acts on a member's computer.

## Security notes

- Tokens are 256 random bits (`wbt_` + 64 hex digits); only the SHA-256 is stored; the API never returns one except when
  it is issued. Because they are long and random there is no rate limit on failed sign-ins; add one at the proxy if you
  want it.
- Cross-site writes from a browser are refused (`Origin` must match `Host`), and the console builds the page with
  `textContent`, never HTML, under a strict Content-Security-Policy.
- The database refuses, by foreign key, to put a member of one workspace on another workspace's project.
- No licensing or payment exists yet; they will live in `internal/team`.

## Layout

`internal/team/{domain,store,service,api,console,config,server}` and `cmd/werkbord-team`; see
[PRODUCTS.md](PRODUCTS.md#where-team-specific-functionality-belongs).
