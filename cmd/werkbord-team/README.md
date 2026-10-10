# Werkbord Team

Werkbord Team is the shared workspace for a team using Werkbord: who is on the team, which projects exist, and who
is on which project. It is a separate product from the individual Werkbord, with its own version, executable,
installer and releases.

**Team coordinates; it does not execute.** Every developer keeps using their own computer, their own Werkbord
runner, and their own Git, GitHub and agent credentials. Nothing in Team can run a command on anyone's machine.

## Quick start

```bash
werkbord-team workspace create --name "Acme" --owner "Ada"   # prints the owner's token, once
werkbord-team serve                                          # http://127.0.0.1:7430
```

Open the sign-in link that `workspace create` printed, then add members from the Workspace tab; each gets their own
token. Roles are Owner (everything, and the only one who appoints admins), Admin (administers members, projects and devices) and Member (sees the team and the projects they are on).

The console is organised as Workspace, Projects, Board, **My Work** (what you are doing and what waits for you),
**Reviews** (what needs a decision), Repository and Activity. Create a project, then **invite people to it** from its *People & invites* page: they open the link, pick a name, and
are on the project. The project's **board** (Backlog, Available, In Progress, Review, Done) is shared and updates for
everyone as it changes. A member **claims** an available ticket (only one person can), and **Open in my runner** hands
that ticket's context to *their own* Werkbord; it never connects to anyone else's. The branch for ticket `WB-142` is
`wb-142-<title>`. Team records who holds what and the branches and pull requests developers report; it never runs Git,
merges, or touches a computer.

A workspace can also have a **private network** of its own, made and run by its own machines with no account or service of
anyone else's (`workspace create --network`, then `network invite`, `device join`, `network status`; see
`docs/TEAM_NETWORK.md`). The macOS and Linux release archives carry the pinned network program, which Team supervises and
checks against its pin before every start; it is never downloaded when Team runs.

A new workspace keeps its data in a **cluster of Workspace Hosts**: each runs the pinned rqlite database, which Team
supervises and checks against its pin before every start, and holds a full copy. It starts as one host; three are recommended,
so that one can fail without the workspace becoming unavailable. If a quorum is lost the workspace is read-only until it is back;
nothing is merged. `werkbord-team storage status` says where things stand, `werkbord-team host promote` adds a host,
`werkbord-team storage migrate` moves a workspace that is kept in one file (`team.db`) into a cluster, and
`WERKBORD_TEAM_BACKUP_DIR` makes it take verified backups: replication is not a backup (`docs/TEAM_STORAGE.md`). The Linux archives
carry the database program; on macOS build it with `make rqlite`, or keep the workspace in one file with `--storage single-file`.

Settings: `--addr` / `WERKBORD_TEAM_ADDR` (default `127.0.0.1:7430`), `--data-dir` / `WERKBORD_TEAM_DATA_DIR`.
Team serves plain HTTP: to reach it from other computers put it behind HTTPS or a private network.

Full documentation: `docs/TEAM.md` and `docs/STRUCTURE.md` in the repository.
