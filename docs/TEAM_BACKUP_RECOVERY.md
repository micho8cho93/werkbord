# Team backup and recovery

Customers own and operate backups; Werkbord has no backup service or copy of their data. Replication tolerates host failures but also replicates accidental deletion, malicious metadata and a bad authorized restore. It is not a backup.

Configure a private mounted destination with `WERKBORD_TEAM_BACKUP_DIR`, `WERKBORD_TEAM_BACKUP_EVERY`, `WERKBORD_TEAM_BACKUP_KEEP` and `WERKBORD_TEAM_BACKUP_KEEP_FOR`. Scheduled backups are checked by hash, SQLite integrity, history position and a scratch restore/migration. Keep an off-machine/offline generation and test restoration periodically. The checksum record detects corruption; it is not a signature against an attacker who replaces both files.

```sh
werkbord-team storage backup
werkbord-team storage backups --dir /customer/backups
werkbord-team storage verify-backup <backup-name> --dir /customer/backups
```

Database backups are plaintext SQLite. Encrypt them using customer-managed backup tooling, restrict access and keep encryption keys outside the same backup. They contain workspace data, credential hashes, public device keys, signed licenses and encrypted host-provisioning records. They exclude ordinary runner private keys, repositories and agent credentials. Those local assets require separate runner backups.

Protect these distinct recovery materials:

| Material | Recovery requirement |
| --- | --- |
| Verified database plus its checksum/position record | Restores coordination state, including signed license and revocations as of that backup |
| Workspace Host `pki/` and configuration | Sealed workspace root, Nebula CA, device identity and cluster credentials; needed to retain network identity |
| Authority/device unlock material | System/login Keychain recovery or externally held passphrase; a `pki/` directory alone cannot be unsealed |
| Local device state | Sender pins, replay state, local runner mappings and approvals; never roll it back while live messages can still be accepted |

Keychain keys are not in a Team database backup and are not portable ciphertext metadata. Export/recover them through controlled OS/operator procedures, or maintain securely passphrase-protected recovery hosts. There is no Team Keychain export or vault rewrapping command. A full disk image may include plaintext `storage/auth.json` and temporary node keys; protect it as authority-level material. Never attach it to a crash report.

For a deliberate restore, stop the initiating Team service and coordinate with every host/runner. Verify the chosen backup and keep a safety copy:

```sh
werkbord-team storage restore <backup-name> --dir /customer/backups \
  --safety-dir /customer/before-restore --yes
```

The command verifies first, saves what it replaces, loads through rqlite and gives restored history a new identity so replicas refresh. Writes after the backup are lost from the restored workspace. `--another-workspace` permits an intentional restore from another cluster; it does not recreate a missing CA/root or repair a compromised one. Backing up a single-file evaluation workspace requires SQLite's consistent backup facility or a stopped database, never a lone live WAL file copy.

A restore can roll back member/device revocations and replay records. Keep remote access disabled, let all message/proof acceptance windows expire (at least five minutes plus skew), reapply incident revocations, reissue exposed member/device credentials, clear stale local approvals, verify the license and compare workspace fingerprints before reconnecting runners. Expired certificates require renewal or re-enrollment. Restoring a license does not bypass its signature/expiry, and offline vendor revocation remains unavailable.

Loss of both authority unlock material and every healthy authority host means a new workspace/network and re-enrollment. See [TEAM_HOST_REPLACEMENT.md](TEAM_HOST_REPLACEMENT.md) and [TEAM_DISASTER_RECOVERY.md](TEAM_DISASTER_RECOVERY.md).
