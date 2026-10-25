# Plans: Free and Premium

Werkbord is one product with one build. It has no licences, no seats, no expiry, no renewal, no vendor account and no vendor
server. There are two plans, and the only thing that tells them apart is whether a person's app holds a valid **unlock key**
(Premium) or not (Free). Nothing is built, packaged or installed differently for either: there is no Premium build, and no
release script, build tag or linker flag may select an edition (`internal/archtest` fails if one does).

| | Free | Premium |
| --- | --- | --- |
| Personal projects, runs, Git, Salie | everything | everything |
| Teams | create or join any number of teams of **up to 2 people** (you and a partner) | teams of any size |
| Price | none | a one-time purchase, not yet on sale |

## The rule

A team may have at most `entitlement.TeamLimit(premium)` people: two on Free, no limit on Premium
(`internal/entitlement`). A person is a member, the owner included. A person's extra devices are not extra people.

It is enforced where a person is added, in the same transaction, in the Team service (`internal/team/service/teamsize.go`):
direct member creation, invitation redemption and device enrolment all go through one guarded insertion, so concurrent
requests and concurrent Workspace Hosts cannot take a team past it. Over the limit, the add is refused with
`domain.ErrPremiumRequired`, which the API reports as HTTP 402, code `premium_required`. Nothing else changes: a team that is
already over the limit keeps working, and removing a member, revoking a device and replacing a credential are never blocked.

The window only displays what the service decides; no screen unlocks anything.

## Where this stands

The rule and its enforcement are in place. **The unlock key is not built yet**, so every production build applies the Free
limit today. The next stages add the key and its offline check, Settings → Plan, the proof a joining member presents to the
team's host, the read-only state for free members of a team of three or more, and the upgrade prompts. The design and the
order of work are in [unified-platform/PLAN.md](unified-platform/PLAN.md).

Tests that need a team larger than two people use `config.Config.Premium` (never read from the environment or a file) or, for
the browser scripts that build their own `werkbord-team`, the linker flag `-X main.testPremium=1`. No release or packaging
script sets it. The unlock key replaces that seam.

## Offline limits

Nothing offline can stop a person who modifies their own copy of the program, and Werkbord does not try to. The plan boundary
is a fair, low-friction one, not copy protection.

## What was removed

Team used to require a vendor-signed offline licence per workspace (seats, dates, a support end, an owner-only renewal, a
vendor issuing tool and a hosted operator console with a Supabase register). All of it is gone: `internal/team/license`, the
licence gate and its API (`PUT /api/team/v1/license`, `POST /api/device/v1/license`), `werkbord-team license import`,
`WERKBORD_TEAM_LICENSE_FILE`, the console's licence screens, `LICENSE_ISSUER_PUBLIC_KEY` and its release secret, and the
operator console and its hosted functions. The `workspace_licenses` table of an existing Team database is no longer read or
written; it stays in the schema, empty of meaning, because migrations only move forward and hosts of different versions can
share a database during maintenance.

`cmd/werkbord-team/vendor` keeps its one remaining job: signing a release's Team archive checksums offline.
