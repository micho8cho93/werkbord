# License issuance and recovery operations

Status: **proposed process; publication on hold**. The release run for
`werkbord-v4.69.2-preview.1` was cancelled on 2026-10-21 before publication. This
document does not grant Team production admission or change customer runtime
licensing. The product owner is the sole issuance and recovery operator.

## What we are protecting

Customers import ordinary signed Team licenses into the public app. They do not
need a license server, vendor account or test application. The operator needs to
retain the ability to issue licenses and explain exactly what was issued.

Keep three things separate:

| Item | Contents | Where it belongs |
| --- | --- | --- |
| Issuer key | The private signing key, public key, issuer ID and fingerprint | Protected operator storage; private material encrypted at rest and unlocked only for signing |
| Issuance register | Customer reference, request reference, license terms, lifecycle events and artifact hashes | Owner-only local operator store, with versioned encrypted backups |
| Customer artifact | The original signed license JSON | Immutable operator archive plus the copy delivered to that customer |

No private key, customer register or recovery password goes into the source
repository, GitHub Actions, a public release or a customer app. Release builds
receive public verification keys only. The Apple, Sparkle, license and archive
manifest signing keys have separate purposes and remain separate.

The current replacement license issuer has public-key SHA-256 fingerprint
`b66484e825f6148e826f46e48f7134d7d00bb2ea139a700182dca1ef5ddd502b`.
This is SHA-256 of the decoded 32-byte Ed25519 public key. See
[TEAM_LICENSE.md](TEAM_LICENSE.md) for the current and retained public issuers.
The previous public issuer is verification-only: its private key has not been
located. A local test issuer must never be selected for public issuance.

## One managed issuance path

The existing `vendor license` command signs a document and refuses to overwrite
its output. It does **not** provide the register and recovery controls described
below. A managed operator command is still needed; until it exists, this is a
design, not a claim that those controls have been implemented.

Each issue, renewal or seat change follows this sequence:

1. Record the request before signing. Require a stable request reference, a
   customer reference, approved seats and runtime/support terms. An order line
   can supply the request reference later; manually issued licenses use an
   explicit manual reference. Do not infer that a manually issued license was
   paid for.
2. Validate the request and select the active issuer by its pinned fingerprint.
   Reserve a unique license ID and a pending register entry under an exclusive
   operation lock. Retrying the same request returns its original result;
   changed terms under the same reference are refused.
3. Unlock the encrypted signing key into the signer's standard input. Private
   bytes and recovery passwords are never command-line arguments, environment
   values or log output. No temporary plaintext key file is needed for routine
   issuance.
4. Sign canonical claims and verify the result against the pinned public key.
   Save the exact original signed bytes using a new owner-only file, flush them
   to disk, and complete the register entry with the file hash. Never overwrite
   a previously issued artifact or replace its terms in place.
5. Back up the completed register and artifacts. Once the snapshot is checked,
   the file is ready for delivery. Record actual delivery separately: time,
   channel and customer reference. A failed delivery is retried using the same
   bytes and license ID; it does not issue another license.

The managed command must reconcile a pending entry after interruption. If its
artifact exists and verifies with the expected claims and hash, finish that
entry. Otherwise stop for repair. Do not silently invent another license ID or
forget a file that may already have been delivered.

## Register and artifact rules

Use a local SQLite register for transactions and uniqueness, and immutable
artifact directories named by license ID. This needs no hosted database or
customer runtime service. Take SQLite backups with its backup interface; do not
copy a live database file without its transaction state.

Every record needs:

- Stable request reference, unique license ID, customer reference and operator.
- Edition, seats, issue time, optional runtime expiry and optional support end.
- Issuer ID and public-key fingerprint, exact artifact hash and archive path.
- Status events with timestamps: prepared, issued, delivered, replaced or
  commercially cancelled. Changes append events rather than rewrite history.
- Order/payment reference when one exists; an explicit missing/manual value
  otherwise. A renewal or change links to the license it supersedes.

The register enforces unique license IDs and request references. It does not
contain the signing key. Preserve the original JSON exactly, including its
signed claim encoding. A CSV export may help bookkeeping, but it is not the
authoritative register and is never edited and imported over issued history.

Commercial cancellation is a register event, **not remote revocation**. An
already issued offline license remains valid according to its signed terms.
Seat reductions or replacements likewise do not remotely destroy old files.
See the offline limitations in [TEAM_LICENSE.md](TEAM_LICENSE.md).

## Two backup schedules

### Key recovery pack

Create this when an issuer is provisioned or rotated. Include its private and
public keys, issuer metadata, fingerprint and these recovery instructions.
Encrypt the pack before it leaves the operator's storage. Keep an independent
copy that can be recovered without the issuing Mac. A password-manager copy and
a disconnected external-drive copy are the preferred layout; the operator's
available destinations are still being confirmed.

Store the recovery password in the password manager or another independent
recovery channel, never beside the external-drive archive. Confirm that access
to the password manager itself can be recovered without this Mac. The primary
private key should also be encrypted at rest, with an interactive unlock for
each issuance session. Retire the current clear PEM only after the encrypted
primary and independent recovery copy have both been tested.

The Mac already has GnuPG. Its passphrase-based encryption can create a portable
encrypted pack without introducing a custom encryption format. Use its
interactive password entry and disable symmetric passphrase caching during the
recovery test. Do not place a production recovery password in a shell command,
script, environment variable, chat or adjacent password file.

### Register and artifact snapshots

Create an encrypted, timestamped snapshot after each issuance batch and before
customer delivery. Include the consistent register backup, all referenced
original license files, public issuer registry and a manifest of file hashes.
This routine snapshot contains **no private signing key**.

Never overwrite the last good generation. Record the snapshot hash, which
issuance records it contains, its independent storage location and the time it
was verified. A snapshot still on the issuing Mac is a staging copy, not the
independent backup.

## Written recovery procedure

Run this once before publication, after changing key protection or backup
destinations, and periodically thereafter. Record the date, source snapshot,
fingerprint, record count, result and operator. Do not record the password.

1. Retrieve the encrypted recovery pack from the independent destination. Use a
   fresh protected recovery directory and an empty GnuPG home so a cached local
   unlock cannot make the test appear to succeed.
2. Enter the recovery password through GnuPG's interactive prompt. Restore the
   issuer metadata and derive the public key from the restored private key.
   Compare the complete fingerprint with the separately recorded expected
   fingerprint. Stop on any mismatch.
3. Sign and verify a fixed, domain-separated recovery challenge. This proves the
   restored private key works without creating a usable customer license or
   signing an archive with a license key.
4. Retrieve and restore the register snapshot and original artifacts. Check the
   database, every manifest hash, every license signature and all unique IDs.
   Reconcile pending issuance entries and inspect missing backup generations
   before any new issuance.
5. Confirm that retrying an existing request returns its archived artifact and
   cannot create another license. Do not send any files during this drill.
6. Record the successful drill, remove the temporary recovery directory, and
   retain the encrypted originals. A failed drill blocks issuance and
   publication until corrected.

For an actual lost-Mac recovery, fence the former operator store before resuming
issuance from the restored one. Keep one authoritative writer. If theft or key
compromise is suspected, stop issuance and plan an issuer rotation; restoring a
backup does not resolve compromise. Retain the public verification keys needed
for previously issued licenses.

## Current batch and publication conditions

The ten licenses prepared on 2026-10-21 have been inventoried with exact file
hashes, unique IDs, customer labels, terms and issuer fingerprint. Original
signed files have been copied into owner-only issuer storage. They are marked
**prepared**, with no payment or customer delivery assumed. The inventory is a
migration seed, not yet the transactional register described above.

Before resuming publication, finish and review these concrete items:

- The sole operator's issuance and recovery instructions are agreed.
- The managed issuance/register path is implemented and the existing ten files
  are imported without reissuing or changing their signed bytes.
- The primary private key is encrypted, an independent key recovery copy is
  stored, and access to its recovery password is confirmed.
- A consistent register/artifact snapshot is stored independently.
- A recovery drill from the independent copies passes and its receipt is saved.
- Duplicate requests, interrupted issuance, altered backup data and a wrong
  restored key are refused by the operator tools.

The public build must still pass its own signing, notarization and license
verification checks. Completing this operator process does not waive the other
Team production-admission requirements in
[TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md).

## References

- [GnuPG operation commands](https://www.gnupg.org/documentation/manuals/gnupg/Operational-GPG-Commands.html): passphrase encryption and disabling its symmetric passphrase cache.
- [NIST SP 800-57 Part 1 Rev. 5](https://nvlpubs.nist.gov/nistpubs/specialpublications/nist.sp.800-57pt1r5.pdf), Appendix B.3.1: private signing-key backup is an explicit custody decision, and recovery needs strong protection. Preserving issuance for installed apps with a pinned issuer is the reason for this controlled recovery copy; this document is not a claim of NIST compliance.
