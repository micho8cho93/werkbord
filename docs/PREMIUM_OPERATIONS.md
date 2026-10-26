# Premium operator console

The vendor console at <https://micho8cho93.github.io/werkbord/> issues **one permanent Premium unlock key per person**. Free includes personal work and teams of up to two people. In teams of three or more, every member, including the owner, needs their own key; members without one have read-only access. Keys are not tied to a team and never expire. One key cannot identify two people in the same team. See [PLANS.md](PLANS.md).

## Issue and deliver

1. Sign in with an email already enabled in the existing hosted operator allow-list, then verify your authenticator. Account creation alone grants no operator access.
2. Choose **New Premium key**. Enter the person's name or email (at most 80 UTF-8 bytes), and optionally an order reference. There are no seats, team names, renewal dates or support terms.
3. Choose **Issue Premium key**. The server reserves a request ID, signs the app-compatible `wbpremium-…` text, verifies it and retains the result. An interrupted operation can be retried with the same request reference, including after a reload. A prepared entry in the register can be opened and completed.
4. Copy or download the key and send it privately to the holder. The console does not send email or confirm delivery. In the app, they open **Settings → Plan**, paste the text and choose **Unlock Premium**.

Search by person, key ID or order reference. Opening an issued entry retrieves and verifies its original key for redelivery. Sign-out clears the browser's draft and in-memory keys. Keys and operator sessions are never saved to browser storage; only non-secret draft details and the request ID survive a reload.

## Signing custody and app releases

Issuance runs on the existing vendor Supabase project, `czereygxndotenlqndbj`. The new `premium-issuer` function and `premium_private.keys` register are separate from historical Team licences. Existing `licensing_private.operator_id` checks the operator allow-list, confirmed account, MFA and live Auth session on each database operation. Public SQL wrappers are security invokers. Register tables have no direct client grants, and RLS denies direct access. Only the service role may complete a signed result, and completion rechecks the operator's live session.

The function validates tokens through Auth itself, so its legacy gateway JWT setting is disabled; this does **not** disable authentication. CORS accepts only `https://micho8cho93.github.io`. Public configuration contains only the project URL and publishable API key. Signing material, service keys, tokens and passwords are never bundled into the page.

Premium uses a dedicated Ed25519 issuer, distinct from the historical Team licence signer and software release keys. Its server secrets are `PREMIUM_SIGNING_KEY_PKCS8` (base64 PKCS#8 DER) and `PREMIUM_ISSUER_PUBLIC_KEY` (raw URL base64). Customer builds need only the latter, supplied through the GitHub repository variable of the same name and stamped into the app and Team executable by the existing release scripts. Older Free-only builds without that public key cannot check the new keys: publish an app release with this variable before delivering Premium to customers. Premium remains not on sale until the purchase flow is configured separately.

On the release workstation, the owner-only recovery primary is held outside Git at:

```
~/Library/Application Support/Werkbord Premium Operator/premium-issuer.pem
~/Library/Application Support/Werkbord Premium Operator/premium-issuer.pub
```

The provisioning helper creates the pair once and reuses it on retries. It sends secrets to the CLI through stdin, never command arguments or logs:

```sh
node scripts/provision-premium-issuer.mjs czereygxndotenlqndbj \
  "$HOME/Library/Application Support/Werkbord Premium Operator"
gh variable set PREMIUM_ISSUER_PUBLIC_KEY \
  < "$HOME/Library/Application Support/Werkbord Premium Operator/premium-issuer.pub"
```

Do not run it with a different directory to rotate an established issuer. The app currently trusts one Premium issuer; replacing it would invalidate existing keys in new builds. Retain an independently encrypted backup of the primary before relying on it for recovery. A local copy is not evidence of independent recovery.

## Backups and historical records

**Back up Premium register** downloads an age-encrypted JSON snapshot after a passphrase of at least 14 characters is entered. The browser fetches and verifies each original issued key before encryption. The backup contains holder labels, order references, request IDs, issuer public keys and original signed keys. Retain it privately with the passphrase stored separately. This console does not restore over a live register; recovery requires an administrator to reconcile the snapshot in an empty protected destination and verify original signatures and IDs before enabling issuance.

The old `licensing_*` tables and existing `licensing-artifact` function remain unchanged. Historical licence originals retain their original meaning; they do not unlock Premium and are not silently converted. The new interface offers no seat expansion, renewal or closure action. Closing a record would not revoke a permanent offline key; there is no online revocation service.

## Build, test and publish

`make test-premium-console` installs locked dependencies if needed, tests the protected handler and actual Go vendor key interoperability, and builds `dist/licensing`. `make check` includes this target. No vendor issuing code or secret is linked into customer executables.

The restored `licensing-pages` workflow tests and builds the page, then publishes `dist/licensing` through the existing GitHub Pages deployment environment on relevant main-branch pushes or manual dispatch. It contains no signing credentials. Follow AGENTS.md's unified version/tag process for implementation commits, and do not push commits or tags without the user's request.

The SQL migration upgrades the **existing operator project**, whose historical allow-list and live MFA guard are prerequisites; it is not a new customer Team database migration. Keep all vendor migrations out of customer installs.

## Acceptance on 26 October 2026

The new migration and `premium-issuer` v1 were deployed to the existing project. A dedicated Premium key was provisioned and its public half configured for subsequent app releases. An actual disposable operator session was denied without MFA and without allow-list membership. After authorisation it issued a key; an idempotent retry returned the identical result, and the customer Go verifier accepted it. Temporary acceptance identities and their register entries were removed afterwards. The browser flow also issued and downloaded a second disposable key through the real backend, and exported an encrypted register backup that decrypted successfully with both signatures intact and the downloaded original matching. Desktop (1280px) and mobile (390px) layouts were inspected; a local asset interception served the candidate page under its allowed origin, so this verifies the candidate against the live backend, not a published Pages deployment. Full `make check`, the added console checks, SQL grants/RLS checks and version-boundary checks passed. These checks do not constitute personal operator enrolment, customer delivery or independent key/register recovery.

The security advisor reports the existing disabled leaked-password protection setting; [Supabase's remediation](https://supabase.com/docs/guides/auth/password-security#password-strength-and-leaked-password-protection) applies. The private register's RLS-with-no-policy notice is intentional deny-all direct access; access is through the guarded functions.
