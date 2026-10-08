# Replacing Team hosts

Three independent voting Workspace Hosts tolerate loss of one. Two require both for writes; one has no redundancy. A Connectivity Host is a separate role and holds no CA/database authority unless it is also a Workspace Host.

When a healthy quorum remains:

1. Inspect `werkbord-team storage status` and `werkbord-team network status` locally. Take and verify a customer-encrypted backup.
2. Prepare a patched replacement with the exact reviewed signed Team bundle, secure key storage and reachable customer endpoints. Enroll it using a current invitation and an independently compared fingerprint. Approve its device key locally on an administrator device. Use the existing owner's/member's identity to avoid a needless new license seat.
3. Promote with `werkbord-team host promote <new-device-id>`. On that host collect the recipient-encrypted authority/cluster material with `werkbord-team host collect`, then start `serve` (or use the Team app's equivalent). The node joins as a replica, catches up, is checked, and only then becomes a voter. Do not count it toward redundancy until status confirms this.
4. Remove the old voter with `werkbord-team host remove <old-device-id>` through a live quorum. The operation refuses a membership change that cannot be made safely. Then revoke its device. Removing a role does not erase the old disk or secrets.
5. Recheck voter membership, successful writes, data position and connectivity from a separate network. Destroy/decommission old copies according to the customer's retention policy.

If an ordinary Connectivity Host is lost, enroll and advertise a replacement discovery/relay endpoint and remove/revoke the old device. Existing direct peer tunnels may continue; new or NAT-isolated devices may be unreachable until another useful lighthouse/relay is available. Operate independent connectivity endpoints if that matters. A probe of an enrollment TCP endpoint does not prove UDP forwarding or relay traversal.

If the old Workspace Host is **compromised**, removal/revocation is containment, not recovery of secrecy: it already holds workspace records, the application root, Nebula CA and database credentials. Those authority/cluster keys do not rotate in place. Stop remote action delivery, keep target-local start policy `ask`/`off`, and rebuild a new workspace/network with fresh authority and credentials. Restore reviewed coordination data only, then re-enroll devices and explicitly compare/reapprove their pins. Do not import an old full authority image into the supposedly clean replacement.

If quorum is gone, do not remove voters independently or start several single-host histories. Use the fenced recovery procedure in [TEAM_DISASTER_RECOVERY.md](TEAM_DISASTER_RECOVERY.md).
