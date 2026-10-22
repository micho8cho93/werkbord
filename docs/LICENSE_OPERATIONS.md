# License issuance and recovery operations

The product owner, Michel, is the initial operator and recovery custodian.
Additional operators can be authorised using the procedure below. The vendor
console and register are implemented. Public previews may be published for
evaluation; **production admission still requires** operator enrolment and the
independent recovery checks below.

## What runs where

The hosted operator console is **https://micho8cho93.github.io/werkbord/**.
GitHub Pages serves public interface files only. The dedicated Supabase project
`werkbord-licensing` (`czereygxndotenlqndbj`, EU West, organisation `micho8cho93`)
provides Auth, the transactional register and the protected signing function.
The existing waitlist project is separate. Public `config.json` contains only
project and issuer public information; it contains no administrative or signing key.

The browser signs in using Supabase Auth, then verifies a TOTP authenticator.
The QR image is normalised from the SDK's SVG data URL; manual setup and copying
the setup key remain available. Passwords, authenticator secrets and session
tokens are not persisted by the console. Ordinary form drafts persist only in the
browser tab. Do not send passwords, MFA codes or recovery passphrases in chat.

The hosted signer uses `WERKBORD_LICENSE_SIGNING_PKCS8`, a backend-only Supabase
Edge Function secret. This replaces the original local-only custody policy to
allow issuance without a running Mac. Auth verifies the token; the database
rechecks the approved email, email confirmation, MFA assurance and current live
session before signing and again at completion. Signing fetches the reserved
terms from the register, never caller-supplied claims. Completion verifies the
signature against the pinned issuer. Private signing material never enters
GitHub Pages, browser bundles, customer apps, release assets or source control.
CORS permits the configured Pages origin; it is additional protection, not a
substitute for operator authentication.

The optional local console is still built with `make build-licensing` and listens
on `http://127.0.0.1:7439`. Its signer unlocks an encrypted age key privately in
Terminal for thirty minutes, then clears its in-memory copy. The browser receives
signed files only. Hosted issuance does not require this local process. Customer
apps verify their imported licenses offline and never contact the operator service.

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
2. Open the hosted console, create the operator account with that email, confirm the
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
provide the issuer private key. Hosted signing is available to enabled operators
after MFA verification; the optional local signer requires a separate Terminal
unlock. Michel remains responsible for key recovery.

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

The register reserves one operation and its terms before signing. The protected
signer fetches those approved terms using the current operator session and signs
them. Signature and claim verification precedes completion, then the
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

## Hosted deployment and signer recovery

`npm ci && npm test && npm run build:hosted` in
`cmd/werkbord-team/licensing/ui` builds `dist/licensing`. The pinned
`licensing-pages` workflow publishes that directory on a main-branch push. Its
publish job runs only the pinned Pages deployment action with Pages/OIDC write
permissions; it has no checkout, shell step, repository write permission or
private secrets. The hosted website is public; every register action still
requires an authorised, confirmed operator with MFA and a live session.

The Edge Function is deployed with:

```sh
supabase functions deploy licensing-artifact --use-api --no-verify-jwt \
  --project-ref czereygxndotenlqndbj
```

The gateway JWT option is disabled because the function verifies each token with
Auth and enforces the stricter database operator/MFA/session guard itself. Never
remove either verification layer. Set `WERKBORD_LICENSING_ORIGINS` to the exact
approved browser origin when changing hosting; update Auth `site_url` and its
exact confirmation redirects too. Browser email/password registration remains
subject to Supabase Auth delivery/rate limits; configure suitable SMTP before
adding operators outside the organisation's permitted default-mail recipients.

For signing-key recovery, retrieve and check the encrypted independent copy.
Decrypt it privately to a temporary owner-only PEM using age (passphrase at the
Terminal prompt), then run the vendor-only provisioning helper with an
administratively authenticated Supabase CLI:

```sh
node cmd/werkbord-team/licensing/provision-signing-key.mjs \
  --project-ref czereygxndotenlqndbj --key-file /private/recovery/issuer.pem
```

The helper pins the issuer fingerprint, refuses broadly readable key files,
transfers the DER through a temporary owner-only environment file and removes
that transfer file. It never prints private signing material or puts it in
command arguments. Delete the temporary decrypted recovery PEM after verifying
hosted issuance. The existing plaintext primary must be retired only after the
independent encrypted copies and recovery access are verified. Losing access to
Supabase must not mean losing the only signing-key copy.

The hosted console generates a file; the downloaded DMG imports it under its
normal **Create Team** flow. Operator Auth is separate from the customer's Team
owner credentials. Releases before `4.69.2-preview.1` cannot trust this replacement
issuer: restarting those apps does not add the new public key. Use the newly
published preview or a later public build, then test import, workspace creation,
seat expansion and restart in a disposable workspace.

## Register backups and restoration

**Back up register** exports a transactionally consistent snapshot containing
issuer metadata, entitlements, operations, original signed files and audit events.
The hosted browser verifies hashes/signatures and encrypts the snapshot with
the standard age format before downloading it; its backup passphrase stays in the
browser. The optional local console provides the same format and verification. It contains no private signing key. Retain dated
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
   `backup-check`. Both must pass before resuming issuance. Re-provision the hosted
   signing secret using the procedure below, or unlock the optional local signer.
4. Sign into the empty console. Under **Back up register**, select **Restore a
   backup into this empty register**. Choose the encrypted archive and enter
   its passphrase. Browser/local and server verification precede atomic restoration.
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
SQL unit fixtures are rolled back. End-to-end acceptance uses a temporary
operator with a separate password/MFA factor and explicitly labelled disposable
licenses. Disable and remove that account after testing; retain its closed license
history. This does not establish Michel's personal MFA setup or customer delivery.

Production activation still requires the actual operator enrolment, protection of
the existing primary key, independently stored key/register backups, a real
restore drill and suitable hosting arrangements. Public DMGs retain
their own signing, notarisation and Team admission requirements in
[TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md).

References: [age format and library](https://pkg.go.dev/filippo.io/age),
[Supabase backups](https://supabase.com/docs/guides/platform/backups),
[Supabase production availability](https://supabase.com/docs/guides/deployment/going-into-prod#availability),
[Team license protocol](TEAM_LICENSE.md).
