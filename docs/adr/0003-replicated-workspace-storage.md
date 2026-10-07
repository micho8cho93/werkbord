# ADR 0003: Replicated workspace storage

- **Status:** accepted and implemented in Team 2.6.1 (2.6.0 was tagged, but its release failed in CI before anything was published; phase 3 of the architecture in [ADR 0001](0001-team-production-architecture.md)). The runner message path and licensing are not part of it.
- **Applies to:** Werkbord Team. The individual product shares `internal/sqlitekit` (two options it does not use) and is otherwise unchanged.
- **Read with:** [TEAM_STORAGE.md](../TEAM_STORAGE.md) (how to run it), [TEAM_NETWORK.md](../TEAM_NETWORK.md), [TEAM_SECURITY.md](../TEAM_SECURITY.md).

## Context

ADR 0001 decided that a workspace's data lives in [rqlite](https://rqlite.io) (SQLite replicated with Raft) on its Workspace
Hosts, three of them recommended, and left one question open: rqlite executes a transaction as a *single request of
statements*; it has no transaction a client holds open while it thinks. Team's use cases read, decide and write inside one
function (`store.Store.Update`), and the contract they rely on says they see their own writes, are atomic, and are serialised
against every other write. This ADR settles how, from rqlite's documentation and source, checked on 2026-10-07 against
v10.5.2, and from running the program.

## Decision

### 1. Ship rqlite; do not reimplement Raft

rqlite is MIT-licensed, and does what the design needs: Raft replication, leader election, membership changes, snapshots,
read-consistency levels, backup and restore. Werkbord ships the unmodified program and supervises it.

- **Pinned.** v10.5.2 (2026-10-04, commit `a73dd2e6…`) with the SHA-256 of the Linux archives and of the program inside each,
  in `internal/team/infra/rqlite/manifest.go`. The project publishes Linux and Windows binaries and **none for macOS**, so
  for macOS `scripts/fetch-rqlite.sh` builds the pinned commit from source with the flags the project's own release build uses
  (so the program reports exactly `v10.5.2` and that commit). A build is not reproducible across toolchains, so what is
  checked at run time for such a program is its own report of version and commit and the build record the script left beside
  it, and the status says so (`hashPinned: false`); a Werkbord release for macOS pins the hash of its own build.
- **Verified at every start.** The program is looked for in fixed directories (never the `PATH`), checked against the pin,
  copied to a private directory, run once with `-version`, and checked again against the pin immediately before it is started.
- **One program, one start.** `internal/team/infra/rqlite` follows the rule `internal/team/infra/nebula` set: it has no
  function that runs "a command"; its only start of a process is one function, with the literal `rqlited` and flags built from
  a typed description. `internal/archtest` holds it to that (`rqlite_supervisor_test.go`).
- **Flags that matter.** `-fk` (Team's schema depends on foreign keys: cascades and the composite keys that keep a member off
  another workspace's project) and `-auth` (three users: the application's, an administrator's, and the one a joining node
  presents, with the permissions each needs and no more). `-raft-cluster-remove-shutdown` is **not** used: a node leaves the
  cluster only by a membership change someone asked for, never because it was stopped.
- **Never exposed.** A node binds only to loopback or to an address inside the workspace's private network; a wildcard or a
  public address is refused by the supervisor, the admin client, and the storage client (`TestTheDatabaseClientsTalkOnlyToPrivateAddresses`).
  Raft's own port is not authenticated, so the network's policy lets only Workspace Hosts reach it (ports 4001-4002, group
  `workspace-host`, already in `internal/team/infra/overlay`), and the Nebula tunnel is what encrypts it. rqlite's own TLS is
  not configured: a host that is not on the workspace's private network and not on loopback is not a supported arrangement.
- **Not used:** rqlite's automatic backups (they go to S3, GCS or a file on a schedule the leader keeps; Team takes backups itself, see 6),
  its discovery modes, its CDC, its queued writes, its read-replica-only mode beyond joining a host as a replica first.

### 2. A use case runs on this host's copy, and the cluster is asked to take what it wrote

This is the heart of it. There are two designs that need no interactive transaction:

- *Single-command use cases:* rewrite every use case as one parameterised request with SQL guards. That is a rewrite of ~100
  functions, and every one would have to be proved again.
- *Optimistic concurrency:* run the use case as it is, against a copy of the data, recording what it wrote; send the writes as
  **one rqlite transaction that begins with a guard on the position in history the use case ran at**; if the position moved, the
  cluster rolls the whole request back and the use case is run again from the new position. `store.Store` already says a
  function "is free to call it more than once", which is why the service layer has no effect outside its transaction.

Team does the second. `internal/team/store/replicated` is the store; `protocol.go` is the argument in full. In short:

- The cluster's database holds, besides Team's tables, `_fence` (one row: the **position** `seq`, a **chain** hash of every write
  so far, and an **epoch**, the history's identity) and `_wal` (one row per write: its statements, its ID, the next link).
- Every host keeps a **copy** of the database: a SQLite file that is the cluster's data exactly as of a position. A use case's
  function runs on it through a recording layer (`store.Queryer`), in one SQLite transaction, so its reads see its own writes
  and `UPDATE … RETURNING`, `ON CONFLICT`, constraints, the claim guard and the revision triggers all behave as they do on a
  single file, because it is the same SQLite and the same SQL.
- If it wrote, the statements that succeeded are sent to the cluster as `/db/execute?transaction`: first
  `UPDATE _fence SET seq=?, chain=?, ok = CASE WHEN seq=? AND chain=? AND epoch=? THEN 1 ELSE NULL END` (`ok` is `NOT NULL`, so a
  guard that does not hold is a constraint failure, and rqlite rolls the whole request back, which this was verified to do),
  then the write's row in `_wal`, then the statements. Raft orders requests, so of two hosts that ran at the same position
  exactly one is applied and the other is refused and runs again. A ticket is claimed by one person, whichever hosts they are on.
- Other hosts bring their copy up to date by reading `_wal` after their position and applying the same statements in the same
  order, checking each is the next link of the chain. A host that has fallen further behind than the log reaches, or whose chain
  or epoch does not match, **takes a fresh copy** of the database from the cluster (rqlite's `/db/backup`) instead. Making a new
  copy is a new file and never waits for a read (`replica.go`).
- A copy that differs from the cluster's is found by a digest of every table (`Verify`), and replaced.

Why it is safe: the guard compares to a position the cluster's own Raft log defines; every write moves it by one; the copy a
use case ran on was read at a position no later than the one it guards (and a read at an earlier position can only make the
guard fail, never pass wrongly), and a **linearizable** read of the position is made before each attempt, so a use case sees
every write acknowledged before it began.

### 3. The answer to a write is lost: never sent twice, never assumed

A request that is refused before it was looked at (no node answers, no leader) is certain not to have been applied, and is
tried on another node. One that was sent and whose answer did not arrive may have been applied. It is **never sent again as it
was**. Instead the host moves the position itself with a write that does nothing (a *barrier*, guarded at the same position): if
that succeeds the lost write can never be applied (its guard names a position that has gone), and was not; if it fails because
the position moved, the cluster's log says who moved it (every write leaves its ID there) and so whether it was the lost one.
`TestAWriteWhoseAnswerIsLostIsAppliedExactlyOnce` drops the answer, and the request, with a proxy and checks both.

### 4. When there is no quorum, writes stop and reads continue

Reads are served from this host's copy and need nothing from the cluster, so a host that cannot reach a quorum still shows
what it last had and says how old that may be. A write needs a linearizable read of the position (a quorum) and a Raft-committed
transaction (a quorum): without one it is refused as `domain.ErrReadOnly`, which the API answers as `503 read_only`, after a short
wait (8 seconds) for a leader that is being elected. Nothing is accepted on a minority to be merged later, and **no merging is
done anywhere**: there is one history, the cluster's.

The honest consequence: a host that is cut off keeps answering reads, including authentication, from its last copy. A revocation
or a removed member made while that host was cut off is not known to it until it is reconnected. It is documented in
TEAM_SECURITY.md; the alternative (refusing every request without a quorum) would make a workspace unreadable exactly when its
administrators most need to see it.

### 5. Topology, honestly

One host is valid and has no high availability. Two hosts hold two copies but a write needs both: the loss of either leaves no safe
write quorum, so two are no more available than one. Three are recommended and tolerate one failure. Five are for those who need
two. Four is no more tolerant than three. The status API says exactly this (`domain.DescribeTopology`), with the live numbers.

### 6. Membership changes use rqlite's, and are checked first

- **Adding a host.** An administrator gives a device the Workspace Host capability and runs `host promote`. The first host makes
  the cluster ready (a quorum must answer; a workspace that began on loopback is moved onto the private network with rqlite's
  documented `peers.json` re-addressing, allowed only for a cluster of one), and seals to the device's sealing key, with the
  workspace's keys, the database's credentials, its node's name and addresses and the nodes to join through. The device collects
  them (`host collect`), runs `serve`, and its node **joins as a read-only replica**, takes the whole database, and is checked
  against the cluster's data (`Verify`). Only then is it made a voter, which rqlite has no call for in place: the documented way is to remove the replica
  and have it join again, with its data. Only after that does it record itself active; until then the registry says *joining*.
- **Removing a host.** `domain.CheckRemoval` refuses a removal unless a quorum answers now and a quorum of the voters that remain
  will, so the workspace is never left unable to write; a leader is moved off first (`POST /leader`); then rqlite's
  `DELETE /remove`; then the registry changes. Revoking a device that is a cluster member, or removing its owner, is refused until
  the host is removed. A removed host notices, stops its node and keeps its files aside.
- **Not built:** recovery from a lost quorum is rqlite's documented manual procedure (`peers.json`), written up in TEAM_STORAGE.md
  and never automatic, because it can lose data; rotation of the database credentials; eviction of a host *for cause* (revoke the
  device and rotate the credentials by hand).

### 7. Schema migrations run once, against the cluster

A migration is a script of statements; it is split into statements (`script.go` understands Team's, including triggers) and
applied as a write like any other, guarded at the position it ran at. Of two hosts that start together the one whose write is
taken first migrates and the other finds the schema current when it looks again. A database newer than the build is refused
by name, and not touched. A migration that needs foreign keys off (which rqlite cannot do inside a transaction) is refused;
none of Team's does.

### 8. Moving an existing `team.db`

`werkbord-team storage migrate`: find and verify the program, check the file is not in use, copy it (`VACUUM INTO`, so the
original is only read), have SQLite check it, keep the copy as the rollback data, start an empty node, load the copy with rqlite's
own restore, **read the cluster back and compare** schema, row counts and a hash of every row with the original, open the store
(which applies this build's migrations once), check this host's copy, and only then write `storage.json`, which is what switches
the workspace. A failure at any point puts what was made aside and leaves `storage.json` absent, so the next start uses `team.db`
exactly as before. `storage rollback` moves a workspace held by one host back into one file with what it holds *now*.

### 9. Backups: replication is not a backup

A host with `WERKBORD_TEAM_BACKUP_DIR` takes a backup (the leader's confirmed copy of the whole database) on a schedule, writes it
whole-or-not-at-all with a record beside it, **reads it back and restores it into a scratch copy and migrates that**, and applies the
retention (the newest N always, and any younger than a limit). A restore verifies first, takes and keeps a copy of what it replaces,
loads through rqlite's restore, and gives the history a new epoch so that every host takes a fresh copy and none mistakes itself for
merely behind. The destination is an interface (`replicated.Destination`) with one implementation, a directory, so that an
S3-compatible bucket **the customer owns** can be another; nothing of Werkbord's is ever one.

## Consequences and what this does not do

- A host keeps two copies of the data (rqlite's and its own). That is the price of running use cases unchanged; it also lets a host read with no quorum.
- Every write takes two round trips to the cluster (confirm the position, commit), and contention between hosts costs a retry.
  Team's writes are a few per second at most; the in-process writer lock means a host never contends with itself.
- Consistency across hosts is read-your-writes on one host and within moments on another (the copy is brought up to date every
  250 ms); the change-wait long poll is woken when a copy takes in another host's writes.
- The pinned program is Linux (amd64, arm64) and macOS (built from source). Windows is not supported.
- A workspace that began on one host and wants a second must have its network node running on the first so that the database
  can move onto the private network.
- Raft membership of a host is its device ID; changing a node's address is possible only for a cluster of one.
