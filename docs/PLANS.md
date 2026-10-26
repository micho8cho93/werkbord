# Plans: Free and Premium

Werkbord is one product with one build. The customer app has no per-team licences, seats, expiry, renewal, vendor account or vendor
server dependency. There are two plans, and the only thing that tells them apart is whether a person's app holds a valid **unlock key**
(Premium) or not (Free). Nothing is built, packaged or installed differently for either: there is no Premium build, and no
release script, build tag or linker flag may select an edition (`internal/archtest` fails if one does).

| | Free | Premium |
| --- | --- | --- |
| Personal projects, runs, Git, Salie | everything | everything |
| Teams | create or join any number of teams of **up to 2 people** (you and a partner) | teams of any size |
| Price | none | a one-time purchase, not yet on sale |

## The unlock key

A key is one string, `wbpremium-<payload>.<signature>`, signed with a dedicated Ed25519 key held by the vendor’s protected issuing server (or its offline vendor tool), and checked offline against a public key stamped into the build (`internal/entitlement`). The payload is
`{schema, product, id, holder?, issuedAt}`: it names no team, no seats and no account, and it never expires. Pasting it into the
app once is all there is; it works offline, forever.

- **Making keys** normally uses the protected [Premium operator console](PREMIUM_OPERATIONS.md). Each authorised operator signs in and verifies MFA; the server keeps the signing key and records one permanent key per person. The customer app never contacts that service.

  The offline vendor tool is also available and is never shipped (`cmd/werkbord-team/vendor`):

  ```sh
  go run ./cmd/werkbord-team/vendor keygen  --private-out issuer.pem --public-out issuer.pub     # once
  go run ./cmd/werkbord-team/vendor premium --out key.txt --holder "Ada Lovelace" < issuer.pem   # per buyer
  ```

  The private key never enters a customer build, the public page or Git. The 43 characters of `issuer.pub` become the repository variable
  `PREMIUM_ISSUER_PUBLIC_KEY`, which the build stamps into both executables
  (`-X werkbord/internal/entitlement.issuerPublicKey=…`, in `scripts/release-build-flags.sh` and `team-build-flags.sh`).
  A build without it can check no key, so everyone on it is on the Free plan; the release check warns about that.
- **A leaked key** is the known weakness of anything offline. A key is used once per team, so one key cannot fill a team, and a
  signed blocklist inside app releases is the later remedy.

## The rules

A team of up to `entitlement.FreeTeamLimit` (2) people is free. A person is a member, the owner included; a person's extra
devices are not extra people. A team of more than two needs every member on Premium. The Team service holds it
(`internal/team/service/plan.go`), in the same transaction as the change, so concurrent requests and concurrent Workspace Hosts
cannot get past it:

- **Adding the person who would take a team past two** needs the workspace **owner** on Premium (the owner's computer is where
  the team started and normally the first Workspace Host) and, when the person joins by themselves (a device enrollment or a
  project invitation), a key of their own, which the host checks. A member an administrator creates by name has no key to give
  yet: they join, and are read-only until they present one.
- **In a team of more than two, a member without a key is read-only.** Everything that would change the team's coordination
  (projects, tickets, comments, labels, memory, MCP, members) is refused with `ErrPremiumRequired`, HTTP **402**, code
  `premium_required`. Reading is untouched. So is everything that keeps the team running or safe: containment (removing a member,
  revoking a device, replacing a credential), a device's heartbeat, the private network and hosts, backups, enrollment approval
  and presenting a key. The API names those routes (`planExempt` in `internal/team/api/api.go`); a route it does not name is
  refused for such a member, so a new route is closed until someone decides otherwise.
- **When a team is back to two people everyone has full access again.** An existing team of more than two (a development team,
  or one that was licensed before) is read-only for every member without a key, the owner included, until a key is presented.

The host stores only the key's **ID** (`member_premium`, once per team), never the key.

## Where a key goes

1. The person pastes the key into the app (Settings → Plan; the sidebar's plan chip leads there). The app checks it and keeps it in its own
   per-user configuration directory (`werkbord-desktop/premium.key`, mode 0600, `desktop/internal/premium`), and shows only its ID and
   label, never the text. A build without the issuer's public key says so instead of calling every key wrong.
2. Their computer's device service (`POST /api/device/v1/premium`) checks it, keeps it (`premiumKey` in its state, which
   leaving a workspace does not clear) and shows it as `premium: {id, holder}` in its state, never the key.
3. The app gives the key to each Team workspace's device service when it is unlocked and whenever a workspace is opened, created or
   joined. That service gives it to the team it belongs to (`PUT /api/team/v1/me/premium`) whenever that team does not have it, every cycle
   the device syncs, and to a team it is joining (`premiumKey` in the enrollment request, which the device signs).
4. The team's host checks it with the same public key and records the ID. `GET /api/team/v1/plan` says what the host knows:
   `{enforced, freeLimit, people, premium, ownerPremium, readOnly}`.

## What a person sees

- **Settings → Plan** (the window's own settings, the same everywhere): Free and Premium side by side, the current plan, and one field for
  the unlock key. "Get Premium" appears only when `buyURL` in `desktop/internal/shell/premium.go` is set; until Premium is on sale the page
  says so.
- **The sidebar's plan chip** (Free plan · Upgrade, or Premium · Active) opens it.
- **In a Team page**: the people page says how full the Free plan is (`2 of 2 people`); a member without a key in a team of more than two
  sees a banner and, when a change is refused with 402, a prompt ("Teams of 3 or more need Premium") whose button asks the window to open
  Plan (frame event `plan`, accepted only from a Team's own page, carrying nothing).

## Test seams

A service built without `EnforcePlan` has no limits (service tests). `scripts/test-plan-browser.cjs` (`make test-plan-browser`) is the
end-to-end check: a real Team service stamped with a throwaway issuer, real keys, and the console in a browser. `config.Config.Unmetered` switches the rules off for a
production wiring, and the browser scripts that build their own `werkbord-team` set it with `-X main.testPremium=1`. It is never
read from the environment or a file, no release or packaging script sets it, and an architecture test keeps it out of them.

## Offline limits

Nothing offline can stop a person who modifies their own copy of the program, and Werkbord does not try to. The plan boundary
is a fair, low-friction one, not copy protection.

## What was removed

Team used to require a vendor-signed offline licence per workspace (seats, dates, a support end, an owner-only renewal, a
vendor issuing tool and a hosted operator console with a Supabase register). The customer-facing licence system is gone: `internal/team/license`, the
licence gate and its API (`PUT /api/team/v1/license`, `POST /api/device/v1/license`), `werkbord-team license import`,
`WERKBORD_TEAM_LICENSE_FILE`, the console's licence screens, `LICENSE_ISSUER_PUBLIC_KEY` and its release secret, and the
old per-workspace issuing UI. The existing hosted operator authentication and historical register are retained for vendor administration; a separate Premium register and protected `premium-issuer` function now issue person-level unlock keys. The `workspace_licenses` table of an existing Team database is no longer read or
written; it stays in the schema, because migrations only move forward and hosts of different versions can share a database
during maintenance.

`cmd/werkbord-team/vendor` signs unlock keys and a release's Team archive checksums. Design and order of work:
[unified-platform/PLAN.md](unified-platform/PLAN.md).
