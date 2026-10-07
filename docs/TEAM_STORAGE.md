# Werkbord Team: where the workspace's data is kept

Since Team 2.6 a workspace's data lives in a **cluster of Workspace Hosts** rather than in one file on one computer. Each
Workspace Host runs [rqlite](https://rqlite.io) (SQLite replicated with Raft), which Werkbord ships and supervises, and holds
a full copy of the workspace: projects, tickets, members, activity, devices. Three hosts are recommended, so that one can fail
without the workspace becoming unavailable for writing.

Nothing here involves Werkbord's servers. There are none: the database runs on machines you own, over the private network the
workspace owns ([TEAM_NETWORK.md](TEAM_NETWORK.md)), and its backups go to a directory you own. The reasoning behind the design
is in [ADR 0003](adr/0003-replicated-workspace-storage.md).

## What you get, honestly

| Workspace Hosts that vote | Copies of the data | Writes need | The workspace keeps working when | In Team's words |
| --- | --- | --- | --- | --- |
| 1 | 1 (plus your backups) | that host | never: if it is down, the workspace is | Valid. **No high availability.** |
| 2 | 2 | **both** | neither fails | Two copies, but the loss of *either* leaves no safe write quorum: no more available than one host. |
| 3 | 3 | any 2 | **1 fails** | **Recommended.** |
| 4 | 4 | any 3 | 1 fails | No more tolerant than three. Use 3 or 5. |
| 5 | 5 | any 3 | **2 fail** | For teams that need it. Every write waits for three hosts. |

If a quorum is not available, **writes stop**: Team answers `503 read_only`, the console and `werkbord-team storage status` say so
and say why, and reading continues from the host's last copy of the data. Nothing is accepted on a minority to be merged
later, and Team does no merging: there is one history, the cluster's. When enough hosts are back, writes resume with nothing lost
and nothing invented.

Replication is **not a backup**: a change that deletes the wrong thing is on every host a moment later. Configure backups
([below](#backups)).

## Creating a workspace

```sh
werkbord-team workspace create --name Acme --owner "Ada Lovelace" --network --endpoint team.example.org
```

The workspace starts in a cluster of one host (this one): valid, and it says it has no high availability. Add two more hosts to
get three. A workspace can still be kept in one SQLite file with `--storage single-file`, which is valid for evaluation and has
no copy but its backups; `werkbord-team storage migrate` moves it into a cluster later.

The database program needs to be on the host: Team's release archives carry it (in `libexec/werkbord-team/`). From a source
tree, `make rqlite` fetches (Linux) or builds from the pinned source (macOS) into `.cache/rqlite`, and
`WERKBORD_TEAM_DATABASE_DIR` points Team at it. A workspace that is asked to be replicated and cannot find the program is **not
created**, and nothing is left behind.

## Looking at it

```sh
werkbord-team storage status
```

```
The workspace's data is replicated across 3 Workspace Host(s): writable

Three Workspace Hosts, the recommended arrangement: any two can serve, so the workspace keeps working, for reading and writing, when one host is lost.

  host (node)                    votes    answers    
  tdv_ab12…                      yes      yes        leader
  tdv_cd34…                      yes      yes        
  tdv_ef56…                      yes      yes        

This host's copy is at position 4182 (confirmed current 0.3s ago); schema version 6.

Backups: 14 in /srv/werkbord-backups; last 2026-10-07T02:00:11+02:00 (ok)
```

The same report is `GET /api/team/v1/storage` (administrators), and `GET /api/team/v1/health` says in one word whether the workspace
is `ok` or `read_only`. The position is how many writes the host's copy has applied; two hosts at the same position hold the same data.
A host that cannot reach the cluster says how long ago its copy was last confirmed current.

## Adding a Workspace Host

A Workspace Host is a **device**, independent of anyone's role: a member's always-on machine can be one without that member being
able to do anything more than before, and an Admin needs to own none. The host holds the workspace's signing key and the network
authority's key too, so it is a high-trust machine ([TEAM_NETWORK.md](TEAM_NETWORK.md)).

1. **Invite it** as a host (an administrator): `werkbord-team network invite --capability workspace_host`; on the new machine
   `werkbord-team device join <link>`, then `werkbord-team network node` to bring it onto the private network.
2. **Promote it** (an administrator): `werkbord-team host promote <device id>`. This verifies the device (unrevoked, registered,
   with the capability and a sealing key), checks the cluster can take a membership change (a quorum answers), makes the first host's
   database reachable on the private network if it only listened on loopback, and seals to the device, with the workspace's keys,
   the database's credentials and its node's name and addresses. Nothing is readable by anyone but that device.
3. **Collect and start** (on the new machine): `werkbord-team host collect`, then `werkbord-team serve`.

What `serve` then does on the new host, in order: brings the private network up; starts its database node **as a read-only
replica** that joins the cluster; waits until it has the whole database; checks its copy against the cluster's data, table by
table; makes it a voter; and only then records the host as **active**. Until that last step the registry and `storage status` say
the host is *joining*, and it counts for nothing in the quorum. If a step fails the next try starts where it left off.

Adding the second host leaves the workspace at two voters, which is not highly available (see the table): add the third promptly.

## Removing a Workspace Host

```sh
werkbord-team host remove <device id>
```

It is refused unless a quorum of the voting hosts answers now **and** a quorum of the voters that would remain will, so a removal
can never leave the workspace unable to write. A leader hands over leadership first. The change is rqlite's own membership
change; Team never deletes a voter and hopes Raft recovers. If the arrangement that remains is a weaker one, Team says so.
Afterwards the device is not revoked (it is still on the network) and the removed host stops its node and keeps its files, which are
yours to delete. A host that was removed does not start as a host again until an administrator adds it again.

Revoking a device that is a member of the cluster, or removing the member who owns one, is refused until the host is removed.

**Evicting a host for cause** (it is lost or compromised): remove it as above if a quorum is up, then revoke the device. It still
holds the database's credentials and a copy of the data. Rotating the credentials is not built yet; until it is, treat the workspace
as exposed to that host's holder and rebuild the cluster's credentials by moving to a new cluster (restore a backup into new hosts).

## Backups

Set a directory on storage you own (a disk, or a network share mounted on the host):

| Setting | Default | |
| --- | --- | --- |
| `WERKBORD_TEAM_BACKUP_DIR` | none | where this host puts backups. Without it nothing is backed up and `storage status` says so. |
| `WERKBORD_TEAM_BACKUP_EVERY` | `24h` | how often; `0` turns the schedule off (a manual backup still works) |
| `WERKBORD_TEAM_BACKUP_KEEP` | `14` | the newest this many are always kept |
| `WERKBORD_TEAM_BACKUP_KEEP_FOR` | `720h` | and any younger than this |

A backup is the leader's copy of the whole database (confirmed by a quorum; if there is no leader it is taken from this host's own node
and marked so). It is written whole-or-not-at-all, with a record beside it, and then **read back and verified the way a restore relies on
it**: its hash against the record, SQLite's integrity check, its position in the workspace's history, and a trial restore into a scratch
copy with this version's migrations applied. A backup that fails is removed and reported. Hosts that share a directory do not all back
up at once: the first to find the newest backup too old takes one.

```sh
werkbord-team storage backup                       # take one now (asks the running server; administrators)
werkbord-team storage backups --dir /srv/backups   # list
werkbord-team storage verify-backup workspace-20261007T020011Z-p00004182.sqlite --dir /srv/backups
```

An S3-compatible destination that **you** own can be added later (`replicated.Destination` is the interface); Werkbord hosts no bucket
and no service for this and never will.

### Restoring

A restore replaces the workspace's data with a backup: whatever was written after the backup is gone from the workspace.

```sh
# on a Workspace Host, with `werkbord-team serve` stopped on it (the other hosts keep running and take the restored data)
werkbord-team storage restore workspace-20261007T020011Z-p00004182.sqlite --dir /srv/backups --safety-dir /srv/before-restore --yes
```

It verifies the backup first; **takes and keeps a copy of what it replaces** in `--safety-dir` (which is itself a backup you can
restore); refuses a backup of another cluster's database unless you say `--another-workspace`; and loads through rqlite's restore.
Afterwards the history has a new identity, so every host takes a fresh copy of the restored data and none mistakes itself for merely
behind.

## Moving a workspace that is kept in one file (`team.db`)

Existing Werkbord Team installations keep their data in `team.db`. Nothing changes until you move it, and the move is not destructive.

```sh
werkbord-team serve  # stop it first: the move refuses a database that is in use
werkbord-team storage migrate --dry-run    # checks everything that can be checked, changes nothing
werkbord-team storage migrate
werkbord-team serve                         # as before
```

What it does, and what it checks, in order: finds and verifies the database program; checks `team.db` is not in use; makes a copy
(`VACUUM INTO`, so the original is only read), has SQLite check it, and **keeps it as the rollback copy** in `<data>/backups`; starts an
empty first node in a directory of its own; loads the copy with rqlite's own restore; **reads the cluster back and compares** the schema,
the number of rows of every table and a hash of every row with the original, and the schema version; opens the store, which applies any
migrations this version has that your file lacked (once); checks this host's copy against the cluster; and only then writes `storage.json`,
which is what makes the workspace be in the cluster. `team.db` is left exactly as it was.

If anything fails, what was made is moved aside (`storage.failed-<time>`, never deleted), `storage.json` does not exist, and the next
start uses `team.db` as before. Run it again when the cause is fixed.

To go back: `werkbord-team storage rollback --yes` (the server stopped, and the workspace held by one host) exports what the cluster
holds **now** into a new `team.db`, checks every table against a backup of the cluster, keeps the old `team.db` and the cluster's files, and
switches back. Deleting `storage.json` by hand would also return to `team.db`, but with what it held when it was moved, losing what
was written since.

## Upgrading

Upgrade every host. The first upgraded host that starts migrates the cluster's schema, once, as a guarded write (two hosts starting
together cannot both do it). A host still running an older Team finds a schema it does not know and refuses to read or write it,
with the message `database schema version N is newer than this build supports; upgrade werkbord-team`, so a mixed-version cluster
degrades to *unavailable on the old hosts* rather than writing with the wrong idea of the schema. Upgrade one host at a time and the
workspace stays available through the others.

## When a quorum is lost

Writes stop and Team says so; everything below is only for when the hosts that are gone are **not coming back**.

*First, try to bring them back.* A quorum returning is the normal recovery and needs nothing: writes resume with no loss.

*If they cannot come back*, rqlite's documented recovery is to write a new cluster configuration on the surviving hosts and
start them from it. It **can lose the writes the lost hosts held**, so it is for last resort, and Team never does it for you:

1. Stop `werkbord-team serve` on every surviving Workspace Host.
2. Decide which survivor has the most recent data (the one with the highest position in `storage status` before the loss, or the one
   that was last the leader). Recover that one **alone** first.
3. On it, create `<data>/storage/node/raft/peers.json` naming only the nodes that will exist, for example one node:
   `[{"id": "<its node id, in storage.json>", "address": "<its raft address, in storage.json>", "non_voter": false}]`.
4. Start `werkbord-team serve` on it. It becomes a cluster of one (a Workspace Host with no high availability) and the workspace is
   writable again.
5. Add replacement hosts as in [Adding a Workspace Host](#adding-a-workspace-host); on the other old hosts, move their `storage/` and
   `pki/storage.sealed` aside first, and have an administrator remove their devices' Workspace Host role.

The old hosts' registry entries are removed with `werkbord-team host remove` once a quorum exists. Keep your backups: a restore into a
new cluster is the other way back.

## Settings

| Setting | Default | |
| --- | --- | --- |
| `WERKBORD_TEAM_STORAGE` | `replicated` | what a *new* workspace is: `replicated`, or `single-file`. A workspace that exists is where `storage.json` says (none: one file). |
| `WERKBORD_TEAM_STORAGE_PORT` | `4001` | this host's database node's HTTP port |
| `WERKBORD_TEAM_STORAGE_RAFT_PORT` | `4002` | its Raft port (the network's policy lets only Workspace Hosts reach 4001-4002) |
| `WERKBORD_TEAM_DATABASE_DIR` | — | extra directories the pinned database program may be in (never the `PATH`) |
| `WERKBORD_TEAM_BACKUP_*` | | see [Backups](#backups) |

## What is on a host's disk

| | |
| --- | --- |
| `storage.json` | where the data is: the kind, this host's node, its address, its role (`voter`, `replica`, `removed`). Nothing secret. |
| `pki/storage.sealed` | the database's three passwords and the cluster's name, sealed like the workspace's keys. Never in the database, the API or a log. |
| `storage/node/` | rqlite's files: its database, its Raft log and snapshots |
| `storage/replica/` | this host's own copy that it reads from and runs use cases on; made again from the cluster whenever it cannot be trusted |
| `storage/auth.json` | what rqlite is told of its users (mode 0600) |
| `backups/` | the rollback copy of a migration and the safety copies of restores |

## Security notes

- The database is **never reachable from outside** the workspace: its nodes bind to loopback or to an address on the workspace's
  private network, and the supervisor, the admin client and the storage client all refuse any other address.
- Three users, with the permissions each needs: the application's (read, write, load, backup), an administrator's (everything,
  used for membership changes), and the one a joining node presents (join only). The passwords are random and per workspace.
- The password for the application user and a host's copy of the data are on every Workspace Host. That is part of what makes a
  Workspace Host a high-trust machine.
- **A host that is cut off from the cluster still reads, and authenticates, from its last copy.** A token revoked, a device revoked or a member
  removed while it was cut off is not known to it until it reconnects (seconds after it does). It cannot write anything while cut off. This
  is the price of a read-only mode that works when the quorum does not; it is the reason `storage status` shows how stale a copy may be.
- Raft's port is not authenticated; the Nebula tunnel and the network's default-deny policy are what keep everything but Workspace Hosts
  away from it. rqlite's own TLS is not used, which is why a database node must be on loopback or on the workspace's private network.

## Testing

`make test-rqlite` fetches the pinned program and runs the tests that start real clusters: the supervisor, the replicated store
(leader and follower failure, re-election, an old host rejoining, a partition that leaves two of three connected, an isolated host
refusing writes, the loss of two of three leaving the workspace read-only and its recovery, adding and removing hosts, a lost answer to a
write, a backup restored, a legacy database moved), and the server's storage (promotion, removal, migration and its failures, rollback).
They skip when the program has not been fetched; CI sets `WERKBORD_REQUIRE_RQLITE=1` so that they cannot be skipped there. The service's
own tests also run against a real cluster: `WERKBORD_TEST_STORE=rqlite go test ./internal/team/service/`
(`WERKBORD_TEST_STORE_NODES=3` for three nodes). The partition tests need `lsof` and cut the network between nodes with a proxy in front
of each node's Raft port, which needs no privileges.

## Bumping the pinned rqlite

Moving the pin is a reviewed change to `internal/team/infra/rqlite/manifest.go`: the version, the commit, the SHA-256 of each published
archive (the release lists them) and of the program inside it, and `third_party/rqlite/`, together. `scripts/fetch-rqlite.sh` checks every
download against the pin. Read the release's changelog for anything that changes the HTTP API this package uses (`/db/execute`,
`/db/query`, `/db/backup`, `/db/load`, `/nodes`, `/status`, `/readyz`, `/leader`, `/remove`) or the flags it passes, and run
`make test-rqlite`. The tests that assert the program reports exactly the pinned version and commit fail if the pin and the program disagree.

## Not built yet

- Rotating the database's credentials, and with it evicting a host for cause without moving to a new cluster.
- S3-compatible backup destinations (the interface is there; the customer-owned bucket is the only kind that will exist).
- Console screens for storage (the CLI and the API are complete).
- Windows.
