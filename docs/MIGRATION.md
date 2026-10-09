# Adopting an existing installation

Phase 4 uses **in-place adoption**. The shell changes which window you use; it
does not move databases, merge Personal and Team, recreate identities, rename
repositories, revoke runner pairings, or restart jobs as part of adoption.

On first connection an existing installation is detected and a native dialog
offers **Back up and adopt**. Cancel keeps it available with bundled backend
upgrades deferred. The same controls are in **Workspaces and devices → Your
existing installation**, including retry and **Roll back adoption**.

| Existing installation | What is retained |
| --- | --- |
| Individual | Launcher's resolved service/env data path, `devboard.db`, tasks, token, runner journals/pairings, config and existing repository/worktree paths |
| Devboard | Its original data path, `devboard` command and every `DEVBOARD_*` setting; no forced rename |
| Team only | Its user state and root-owned service path, membership/device identity, workspace slots, signed licenses, sealed Host configuration and replicated storage |
| Both | Both existing services and separate databases; Personal and each Team remain independent |

The per-user `werkbord-desktop/migration` directory holds private backups and the
atomic versioned `migration.json` marker. Managed user files are copied into a new
generation; SQLite snapshots use SQLite `VACUUM INTO`, including WAL pages, followed
by an integrity check. Every snapshot file is hashed. Worktrees, repositories,
agent/Git sign-ins held externally, Keychain keys, tsnet state and logs are retained
at their original paths; this adoption snapshot is **not** a machine/Host disaster
recovery backup. Root-owned Team/rqlite state is never copied by the user-scoped
shell. Use Team's established checked workspace backups for Host disaster recovery.

The durable phases are `prepared → verified`, or `rolled_back`. The snapshot is
published before the marker. A failed probe or interrupted run leaves `prepared`;
retry checks the same snapshot and existing services. Process crashes release the
advisory lock, so stale lock files do not block retry. A corrupt/unknown marker or
changed snapshot fails closed and is preserved for inspection. Keep every affected
directory and resolve it with an administrator; never delete the marker to guess
an identity. Verification requires Personal health/authentication and accessible
enrolled Team workspaces. Legacy Team versions without a compatible device listing
must first follow [TEAM_INSTALL.md](TEAM_INSTALL.md), with checked Host backups.

Rollback changes only the adoption marker. Because original state was never
relocated or rewritten, restoring an old database over current tasks would lose
work; rollback deliberately keeps all jobs and data created since adoption. The
original applications/services remain available. Retry creates another private
snapshot generation; earlier generations stay on disk.

Native **empty-service** replacement has a separate root-owned installer journal.
Interrupted helper/metadata swaps recover before another installation attempt;
old helpers, service definition and access metadata are restored together. The
backend fences mutations while the journal is prepared. Enrolled devices and
Hosts refuse replacement and require administrator maintenance, preserving quorum,
identity and schedules instead of forcing a restart.

After verification, inspect projects/tasks, runner ownership, credentials, every
Team membership and its license, and Host/backup status. Old GUI bundles can then
be moved to Trash only on your explicit decision. Never remove the data directories,
retained installation generations or backups as part of GUI cleanup. Removing Team
capability requires safely leaving all slots and does not remove Personal. No
automatic cleanup, license alteration or release publication occurs in migration.
