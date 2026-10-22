# License issuance and recovery operations

The product owner, Michel, is the initial operator and recovery custodian.
Additional operators can be authorised using the procedure below. The vendor
console and register are implemented; **production activation and publication remain on hold** until
operator enrolment and independent recovery checks below are completed.

## What runs where

The operator console is `cmd/werkbord-team/licensing`, built with
`make build-licensing`. It is never packaged in customer apps. It listens only on
`http://127.0.0.1:7439` and uses the dedicated Supabase project
`werkbord-licensing` (`czereygxndotenlqndbj`, EU West, organisation `micho8cho93`).
The existing waitlist project is separate. Local `config.json` contains only the
project URL, publishable API key and pinned public issuer information.

The browser signs in using Supabase Auth, then verifies a TOTP authenticator.
Database operations check the configured operator email, email confirmation,
MFA assurance and the current server-side session. Other accounts and deleted
sessions cannot access the register. Customer apps do not contact this service.

The issuer private key stays on the operator's Mac, encrypted using the standard
age passphrase format. Unlock it privately in Terminal when starting the console.
The signing session lasts thirty minutes or until locked/terminated. The browser
and Supabase receive signed files, not private signing material. Passwords and
recovery passphrases must never be put in chat, flags, environment variables or
logs. Ordinary form drafts persist in this browser tab; credentials do not.

The active production public key is
`ChctbT2EmgTYU37lb0e1_KkzSO9GR32pkRdcTR-UoYU`, fingerprint
`b66484e825f6148e826f46e48f7134d7d00bb2ea139a700182dca1ef5ddd502b`.
Fingerprint means SHA-256 of the decoded 32-byte Ed25519 public key. The previous
public issuer remains trusted by customer builds, but the new operator's issuance,
import and recovery tools currently pin the replacement issuer. They refuse test
files and files signed by an unconfigured historical issuer.

## Initial activation

1. Confirm Michel's chosen email is enabled in the private operator allow-list.
   The initial production address has been configured. A new recovery destination
   starts with an empty allow-list and needs its operators configured. Do not
   grant general authenticated-user access or use user-editable metadata for
   authorisation.
2. Open the console, create the operator account with that email, confirm the
   email, sign in and enrol the authenticator. Store independent recovery access
   for the email account, Supabase organisation and authenticator.
3. Protect the recovered production key using `protect-key` below. Use a strong,
   unique passphrase and retain independent access to it in a password manager.
4. Copy the encrypted key to independent storage and run `key-check` on that
   retrieved copy. Do not remove the existing plaintext primary until both the
   encrypted primary and independent copy have been verified.
5. Import only `team-license-01.json` through `team-license-10.json` from the
   prepared Desktop folder. Supply their provenance. Imports preserve the exact
   bytes and IDs; duplicate imports return their original records. No payment or
   customer delivery is assumed.
6. Download an encrypted register backup, copy it independently, verify it with
   `backup-check`, and complete an actual restore drill into an empty recovery
   destination. Preserve dated verification receipts.
7. Select production hosting/backup arrangements and record their costs. The
   project was provisioned at a quoted $0/month on the Free tier. Free projects
   may pause after inactivity and do not provide downloadable managed database
   backups. A created free project alone is not production admission.

The standalone operator bundle on the Desktop contains the executable, public
configuration, launch/protection commands and these instructions.

## Add or disable operators

Operator access is managed in the dedicated licensing project's Supabase SQL
Editor, using the project owner's administrative access. There is no operator
management screen in the local console yet. The allow-list supports multiple
email addresses; adding another does not replace Michel's access. Keep the
approved addresses and dated access changes in the independent recovery notes.

To add an operator, replace the example address, run the following against
`werkbord-licensing` (`czereygxndotenlqndbj`), and check the returned row:

```sql
insert into licensing_private.operator_emails(email, enabled)
values (lower('operator@example.com'), true)
on conflict (email) do update set enabled = excluded.enabled
returning email, enabled;
```

The person then creates their own console account, confirms that email and
enrols their own authenticator. Do not share passwords, MFA factors or the
Supabase project owner's administrative account. Each enabled operator has the
same register permissions, including issuance requests, expansion, closure,
imports and backup access. This does not grant Supabase dashboard access or
provide the issuer private key: signing still requires a separately authorised,
locally unlocked signer. Michel remains responsible for key recovery.

To disable an operator without removing their historical identity:

```sql
update licensing_private.operator_emails
set enabled = false
where email = lower('operator@example.com')
returning email, enabled;
```

The backend checks this setting on every register operation, including signing
completion, so the next request is refused even with an existing session. Keep
at least one independently recoverable operator enabled. Re-enabling uses the
same add statement; changing an address requires authorising the new address,
verifying its account/MFA access, then disabling the old address. Operator
allow-lists and Auth enrolment are configured separately from register snapshots
during recovery; reapply the approved list before restoring license records.

## Issue, expand, close and deliver

**New license** records the customer, seats, optional runtime/support dates,
request reference, reason and optional order reference. Leaving runtime expiry
blank makes a perpetual runtime license. A support end date does not stop the
runtime. There is no pre-generated batch limit or fixed issuance quota in this
tool; hosting resources and storage still require capacity management.

The register reserves one operation and its terms before signing. The local
signer fetches those approved terms using the current operator session and signs
them. A separate server-side verifier checks the signature and claims before the
register atomically archives the exact file and finalises the operation.

A request reference is an idempotency key: repeat matching requests retrieve the
same operation; different terms under the same reference are refused. After a
network error, refresh and inspect **Prepared requests** and the history before
starting another issue. **Sign request** retries the reserved issue. **Cancel
request** retains its audit history and releases the pending operation; it cannot
cancel a completed signed issue.

For a customer upgrading from three seats to ten, open their entitlement and use
**Expand seats**. The new file is revision two of the same entitlement; revision
one remains archived and appears as **Replaced**. You do not close the entitlement
first. Existing dates are retained unless extended; an expired date must be
extended before a new signed expansion can be issued. Seat reductions and changing
the customer are deliberately excluded from expansion.

Use **Close license** when the customer ends the entitlement. Record the reason.
Closure prevents further expansion and preserves every original file and event.
It is a commercial register status, **not remote revocation**: old offline files
retain their original validity. The operator has chosen to keep customer
licensing offline. Do not claim that closing or replacing a file remotely disables
it, or that it stops an old public DMG using an already valid license.

Download the exact archived file and send it through the authorised customer
channel. **Record delivery** is a separate explicit action; downloading a file
does not prove delivery and the console does not send messages or take payments.
Back up completed issuance before delivery. The customer imports the replacement
through the normal Team license interface. Its seat limit must cover live members,
including the owner. Devices do not consume additional seats.

## Encrypt and check the issuer

All commands take file paths and public configuration as arguments. Passphrases
are entered privately at the Terminal prompt. Paths below are examples; use the
actual secure operator and independent storage locations.

```sh
werkbord-licensing protect-key --config config.json \
  --input issuer-private.pem --out issuer-private.pem.age

werkbord-licensing key-check --config config.json \
  --key-file /independent-storage/issuer-private.pem.age

werkbord-licensing serve --config config.json \
  --key-file issuer-private.pem.age
```

`protect-key` never overwrites an existing output, verifies encryption before
saving, uses owner-only output permissions and leaves the original untouched.
`key-check` decrypts the selected copy, verifies the complete pinned fingerprint,
and signs/verifies a domain-separated recovery challenge. Its JSON receipt
contains no private key or usable customer license. Save the receipt with the
backup inventory, not with the recovery passphrase.

## Register backups and restoration

**Back up register** exports a transactionally consistent snapshot containing
issuer metadata, entitlements, operations, original signed files and audit events.
The local console verifies artifact hashes/signatures and encrypts the snapshot
using age before downloading it. It contains no private signing key. Retain dated
generations and copy them outside the issuing Mac. Keep the passphrase independent
of the stored encrypted archive. A same-Mac download is only a staging copy.

```sh
werkbord-licensing backup-check --config config.json \
  --input /independent-storage/werkbord-register-TIMESTAMP.json.age
```

`backup-check` decrypts in memory, verifies all original files, their metadata,
unique request identities, revision continuity and event references. Its receipt
records the encrypted file hash, artifact count, date and result. A successful
schema/crypto test is not proof that the file was stored independently.

For a lost-Mac or service recovery:

1. Fence the former writer and retrieve the executable/source, public issuer
   fingerprint, encrypted key, register archive and recovery credentials from
   independent locations. Suspected theft/key compromise requires a separate
   issuer rotation decision; restoring a backup does not resolve compromise.
2. Provision an empty recovery Supabase project and apply the checked-in
   migrations. Configure the authorised operator email and Auth settings; use
   a new public configuration for the destination project. Do not repurpose or
   overwrite a live register.
3. Verify the retrieved key using `key-check` and the snapshot using
   `backup-check`. Both must pass before resuming issuance.
4. Sign into the empty console. Under **Back up register**, select **Restore a
   backup into this empty register**. Choose the encrypted archive and enter
   its passphrase. Local and server verification precede atomic restoration.
   A nonempty destination is refused. Original IDs, bytes, request references
   and events are retained; a restoration receipt is appended.
5. Retrieve a known historical artifact and compare its hash with the original
   archive. Verify that its completed request reference still identifies that
   original operation. Reconcile pending operations; never invent new IDs to
   conceal an uncertain outcome. Preserve the drill receipt and recovery date.
6. Only then resume signing and customer delivery. End the temporary recovery
   session and retain the encrypted source backups.

The console restore transfer currently supports encrypted snapshots up to 10 MiB.
Before exceeding that operational size, use a consistent Postgres dump/restore
process that preserves the schema and exact artifact text, and test its recovery.
This is a recovery transport limit, not an issuance quota. No Storage bucket is
used; original files live in the database and are included in its snapshots.

## Validation and production gate

Automated tests cover the Go signer, encrypted recovery, historical signature
verification, tampering, cross-origin/host request denial, canonical TypeScript
verification, transaction/idempotency rules, expansion, closure, cancellations,
MFA/nonoperator/deleted-session denial and restore refusal on nonempty history.
Database fixtures are rolled back. Browser interaction checks use synthetic data
and do not establish a completed real operator login or customer delivery.

Production activation still requires the actual operator enrolment, protection of
the existing primary key, independently stored key/register backups, a real
restore drill and suitable hosting arrangements. Public DMG publication also
retains its own signing, notarisation and Team admission requirements in
[TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md).

References: [age format and library](https://pkg.go.dev/filippo.io/age),
[Supabase backups](https://supabase.com/docs/guides/platform/backups),
[Supabase production availability](https://supabase.com/docs/guides/deployment/going-into-prod#availability),
[Team license protocol](TEAM_LICENSE.md).
