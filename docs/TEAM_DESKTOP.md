# Werkbord Team desktop and background service

Werkbord Team is a separately licensed and separately installed app. It uses Werkbord’s existing visual system and keeps everyday work (Workspace, Projects, Board, My Work, Reviews, Activity) separate from administration (Members, Devices, Workspace Hosts, Connectivity, Backups, License, Settings).

## For users

Download and open **Werkbord Team.app**. macOS asks for administrator authorization to install its background service, which creates the private network interface. This is a normal macOS dialog; no terminal or separate networking account is needed.

Choose **Create Team**, import the license supplied with the purchase, enter the team and owner names, then create the workspace. Team creates the workspace and device identities, the first Owner, private network, single-node database, first Workspace Host and automatic backups. It determines whether an advertised address is eligible for connectivity and detects an existing local Werkbord runner. The Workspace screen recommends adding two more hosts for fault tolerance.

An administrator creates an invitation in **Members**. A link click or QR scan opens Team; the recipient verifies it, names the computer and joins. Pending enrollment survives window closure and service restart. An administrator approves the device in **Devices**. If the free runner is missing, **Install / set up free runner** uses the signed bundled individual Werkbord executable’s normal setup. An installed individual app can also be opened. Team never installs it automatically. Existing `devboard` aliases, credentials and individual policies remain supported.

Runners perform the work. Scheduled and remote work need an online runner. Workspace Hosts keep Team available, and three are recommended. Connectivity Hosts help devices connect across networks; enabling the role alone cannot make an unreachable address accessible. Sleeping laptops are weaker choices than desktops, Mac minis, office servers and suitable NAS/server systems. Status and warnings reflect observed quorum, database, connectivity and backup health; Team does not claim confirmed remote access merely because a role is enabled.

Workspace puts active work first, followed by a compact availability summary with links to the relevant settings. Workspace Hosts contains the full infrastructure report. Devices and Workspace Hosts explain and disable unsafe Host removal, including removal of the last surviving copy. Runner requests continue showing progress through delivery until they complete or are refused.

There is one Owner, any number of Admins and many Members. Admins can manage projects, members, invitations, devices and infrastructure. Only the Owner appoints/removes Admins. An Admin is not automatically a Host, and a Host is not automatically an Admin.

## Lifetime and removal

The native window is separate from `werkbord-team daemon`, installed as macOS LaunchDaemon `dev.werkbord.team`. Closing or quitting the window leaves Team, hosting, connectivity and the individual runner running. The service starts at boot, including before login. Only one macOS account owns the Team installation on a Mac; another account cannot replace its credentials.

Settings offers these distinct choices:

- Remove the GUI only by moving the app to the Trash; its service and runner remain.
- Keep the service, or explicitly stop it while retaining data. Stopping persists across reboot; opening Team starts it again.
- Leave the workspace and retain a local archive, or leave and remove its local workspace secrets/data.
- After a checked leave, remove the Team service, local connection secrets, license and settings from the welcome screen. Explicitly retained archives/backups are preserved. The individual runner is unaffected.

A Host cannot leave or delete its workspace data unless the live cluster proves removal keeps a healthy quorum and another copy. The final voting Host is refused. Unreachable storage is insufficient proof. The OS uninstaller independently refuses removal while a workspace directory exists. Host demotion stops the database and API, removes authority credentials and preserves the old database before returning the device to member operation. Durable transition markers complete archive moves and enrollment recovery across restarts.

## Implementation and security

Team code is under `internal/team/` and `cmd/werkbord-team/`. The Wails module at `cmd/werkbord-team/desktop/` imports neither product’s Go implementation; it owns fixed, locally initiated OS installation actions. Architecture tests enforce both module and binding boundaries. The daemon executes only the pinned Nebula/rqlite infrastructure through existing narrowly reviewed supervisors, never agents, Git or shell work.

The root-owned service installation is `/Library/Application Support/Werkbord Team` and its plist is `/Library/LaunchDaemons/dev.werkbord.team.plist`. Data, keys and copied helpers use private permissions. Opening an updated app checks the installed service version and requests normal macOS authorization to update it. Updates verify the app bundle, stage every helper before stopping the old service and roll back helpers if startup fails. No service executable is run from a user-writable app location. The GUI’s private credential is paired through the authorized local installer.

The GUI contacts only authenticated loopback `/api/device/v1` on port 7431. `/api/team/v1` is forwarded using this device’s sealed workspace credential to literal loopback or workspace-private addresses, with no proxy or redirects. The local policy API is never served on the overlay. The native binding is limited to connection, app information, pending invitations, HTTPS external links, local runner setup and fixed service lifecycle choices. It exposes no arbitrary command, file/path, HTTP request or workspace administration method. Shared `internal/nativebridge` contains only the WebKit/Wails call protocol and is used by both desktop frontends without product imports.

A signed request names a semantic action, target device, user, workspace, expiry and replay identity. Hosts store/route it. The recipient verifies the signature and ownership, compares the sender with a locally approved application key, and rechecks expiry/revocation on retries. A compromised routing Host cannot replace an approved sender key. Members approve their own other devices locally and compare the displayed identity fingerprints. The recipient then uses a scoped local access token for the individual controller’s narrow route allow-list. The full controller credential is exchanged once and not retained by Team. Local runner policies and normal execution approvals still apply. Task-specific remote-start approvals are spent before dispatch, so an uncertain response cannot start twice. No remote arbitrary-shell API exists.

A license is an offline signed document (`claims`, `signature`), bound to product, customer, seats and validity. The build contains only the vendor’s Ed25519 public issuer key. Original claim bytes survive file import. Create Team requires an active license. Enrollment does not ask each member to import the owner’s license. A billing backend, workspace-wide entitlement replication and automatic seat enforcement are future work; this release does not pretend those are implemented.

## Maintainer build and validation

macOS 13 or newer is the supported desktop/service platform. Linux and Windows use clean platform implementations that report unsupported desktop installation/setup explicitly; the existing Team CLI and server remain portable. No fake success or privileged service fallback is used.

```sh
make team-desktop-check       # native compile, installer logic tests and vet
make team-desktop             # universal development .app (ad hoc signature)
make team-desktop-package     # development .app + DMG
make test-team-desktop-browser # real APIs/rqlite + disposable individual bridge, no OS install
make check verify-isolation
```

`ARCH=arm64` or `ARCH=amd64` produces a native development build. Distribution is universal. The packager bundles the original hash-pinned and upstream-signed Nebula release. It builds rqlite from its pinned source commit, combines architectures and signs it; the exact resulting hash is stamped into the Team service and a matching build record. The optional free runner helper is separately built/versioned. Required third-party notices are included. Installer checks validate signatures and fixed program pins before use.

Production builds use `make team-desktop-release` with `LICENSE_ISSUER_PUBLIC_KEY` (32-byte Ed25519, raw URL base64), a Developer ID Application certificate (`CODESIGN_IDENTITY`), and Apple notarization credentials (`NOTARY_KEY_FILE`, `NOTARY_KEY_ID`, `NOTARY_ISSUER`). Release builds fail if any are absent and never ship a test issuer. This repository contains no vendor private license signing key. An issuer signs `werkbord-team/license/v1\0` followed by the exact JSON claim bytes; see `internal/team/license` for format and validation. Development builds without a configured issuer cannot create a licensed workspace; browser tests generate an ephemeral issuer only inside the disposable test fixture.

`.github/workflows/release-team-desktop.yml` handles only Team product tags, independently of individual desktop updates. Protect its `team-desktop-release` environment with required reviewers and approved branches/product tags. Store Apple credentials and `TEAM_LICENSE_ISSUER_PUBLIC_KEY` there. Only the read-only build job receives them; the write-enabled publishing job uploads the DMG/checksum to the existing Team CLI release without changing latest/status. A fresh Mac verifies the downloaded checksum, signatures, notarization tickets and Gatekeeper assessment. A dispatch builds reviewable artifacts without publishing. No Sparkle or individual appcast is reused for Team.

### Configure a signed Team release

Create the **`team-desktop-release`** environment in the repository's [GitHub settings](https://github.com/micho8cho93/werkbord/settings/environments). Allow `main` for dry runs and `werkbord-team-v*` tags for releases, and add required reviewers. GitHub may create an empty environment when a workflow first references it; its existence does not mean signing is configured.

Add these environment secrets. The Apple certificate and notarization key are obtained as described in [DESKTOP_RELEASE.md, steps 1–5](DESKTOP_RELEASE.md#1-enrol-in-the-apple-developer-program), but must be stored in **`team-desktop-release`** for this product. Secrets in the individual app's `desktop-release` environment are not available to Team.

| Secret | Value |
| --- | --- |
| `APPLE_CERTIFICATE_P12` | Base64 of the Developer ID Application certificate and private key exported as `.p12` |
| `APPLE_CERTIFICATE_PASSWORD` | The `.p12` export password |
| `APPLE_NOTARY_KEY` | Complete text of the App Store Connect `.p8` API key |
| `APPLE_NOTARY_KEY_ID` | The API key's ten-character Key ID |
| `APPLE_NOTARY_ISSUER` | The API key's Issuer ID (UUID) |
| `TEAM_LICENSE_ISSUER_PUBLIC_KEY` | The vendor's 32-byte Ed25519 **public** key, encoded as raw URL base64 (43 characters, no `=` padding) |

`APPLE_SIGNING_IDENTITY` is optional when the certificate contains exactly one valid Developer ID Application identity. The license issuer's private key stays with the offline license issuer and is never uploaded to GitHub or bundled. Team does not need Sparkle signing keys.

Run `gh workflow run release-team-desktop.yml --ref main` after the workflow change is on GitHub. The first check reports all missing or malformed credentials together before importing keys or building. A dry run produces the signed, notarized DMG as the `team-desktop` workflow artifact without publishing. After that succeeds, push the next Team release tag to publish it. Retrying a failed workflow uses its original commit; it does not pick up later workflow fixes.

If no Apple signing credentials are available, `make team-desktop-package` still builds an ad hoc development DMG for local testing. It cannot satisfy the signed release workflow. A missing-credential failure leaves the independently published Team CLI archives available, but no desktop installer is published.

Validation here builds and verifies an ad hoc development bundle without installing a system service. Real-service installation at boot and production notarization require a separate clean Mac acceptance run with the distributor’s signing credentials.
