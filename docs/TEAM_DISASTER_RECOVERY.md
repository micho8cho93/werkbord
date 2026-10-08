# Team disaster recovery

Quorum loss stops writes and remote authorization. This is intentional. Bring surviving machines/links back before changing membership. Normal quorum return needs no manual data merge; stale hosts catch up. Cached local storage inspection is only diagnostic and can be stale.

For a crash/outage, preserve every host's stopped disk, rqlite log, verified backup and separate authority unlock material. Establish which writes were committed through a quorum. A host's last cached position is evidence, not a guarantee that it holds the newest committed state.

If a majority will return, restore connectivity/power and let Raft elect/catch up. Do not force peers membership or restore an older database merely to make a minority writable. Loss of a lighthouse/relay may block remote clients even while the Workspace Hosts still form quorum; repair connectivity independently of database membership.

If lost hosts can never return:

1. Fence them physically and at the customer's network before proceeding. Stop all surviving Team services. Prevent any old host/image from starting on the old network during or after recovery.
2. Choose one verified survivor or customer backup and record the expected data-loss boundary. Retain immutable incident copies. Recover **one** history first.
3. Use the explicit rqlite peers recovery documented in [TEAM_STORAGE.md](TEAM_STORAGE.md#when-a-quorum-is-lost), or restore the verified backup into a deliberately prepared cluster. No automatic quorum shrink or merge is implemented. This operator operation can discard newer writes and must be reviewed as a destructive recovery decision.
4. Bring replacement hosts in through enrollment, encrypted provisioning, replica verification and voter promotion. Reconcile old devices only after a safe quorum exists.
5. Review restored membership/revocations/licenses. Expire live message windows, clear stale local approvals and reissue exposed credentials before opening remote access. Verify a normal write from each remaining host and a current read from a separate member device.

`peers.json` recovery is an upstream last-resort operation, not a tested guarantee that an arbitrary damaged cluster retains every commit. Do not improvise addresses/IDs or bootstrap several disconnected leaders. There is no supported Byzantine/majority-corruption recovery, automatic CA rotation, full-image secret redaction or recovery without authority unlock material. Compromise of workspace authority calls for a fresh workspace/network and independently reapproved device trust, even if the coordination database itself is restored.

The release gate requires a clean signed install, real remote create/join, three-host failure/partition tests, Keychain restart/update tests and backup restoration on independent machines. Local loopback tests do not replace those deployment checks; see [TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md).
