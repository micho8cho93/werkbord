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

Open the sign-in link that `workspace create` printed, then add members from the Members tab; each gets their own
token. Roles are Owner (everything) and Member (sees the team and the projects they are on).

Create a project, then **invite people to it** from its *People & invites* tab: they open the link, pick a name, and
are on the project. The project's **board** (Backlog, Available, In Progress, Review, Done) is shared and updates for
everyone as it changes. A member **claims** an available ticket (only one person can), and **Open in my runner** hands
that ticket's context to *their own* Werkbord; it never connects to anyone else's. The branch for ticket `WB-142` is
`wb-142-<title>`. Team records who holds what and the branches and pull requests developers report; it never runs Git,
merges, or touches a computer.

Settings: `--addr` / `WERKBORD_TEAM_ADDR` (default `127.0.0.1:7430`), `--data-dir` / `WERKBORD_TEAM_DATA_DIR`.
Team serves plain HTTP: to reach it from other computers put it behind HTTPS or a private network.

Full documentation: `docs/TEAM.md` and `docs/PRODUCTS.md` in the repository.
