# Werkbord Team on the Mac: the background service and its installer

Werkbord Team is part of the Werkbord app: there is no second app and no second window. Each workspace needs its own license. The Werkbord window shows Team in its own sidebar next to Individual (see [ARCHITECTURE.md §22](ARCHITECTURE.md#the-window-and-its-workspaces)), using Werkbord’s existing visual system, and Team keeps everyday work (Workspace, Projects, Board, My Work, Reviews, Activity) separate from administration (Members, Devices, Workspace Hosts, Connectivity, Backups, License, Settings).

## For users

In Werkbord choose **Add a Team**. After you agree in a dialog, macOS asks for administrator authorization to install Team’s background service, which creates the private network interface. This is a normal macOS dialog; no terminal or separate networking account is needed, and nothing is installed or started until you do this.

Choose **Create Team**, import the license supplied with the purchase, enter the team and owner names, then create the workspace. Team creates the workspace and device identities, the first Owner, private network, single-node database, first Workspace Host and automatic backups. It determines whether an advertised address is eligible for connectivity and detects an existing local Werkbord runner. The Workspace screen recommends adding two more hosts for fault tolerance.

An administrator creates an invitation in **Members**. A link click or QR scan opens Team; the recipient verifies it, names the computer and joins. Pending enrollment survives window closure and service restart. An administrator approves the device in **Devices**. If the free runner is missing, **Install / set up free runner** uses the signed bundled individual Werkbord executable’s normal setup. An installed individual app can also be opened. Team never installs it automatically. Existing `devboard` aliases, credentials and individual policies remain supported.

Runners perform the work. Scheduled and remote work need an online runner. Workspace Hosts keep Team available, and three are recommended. Connectivity Hosts help devices connect across networks; enabling the role alone cannot make an unreachable address accessible. Sleeping laptops are weaker choices than desktops, Mac minis, office servers and suitable NAS/server systems. Status and warnings reflect observed quorum, database, connectivity and backup health; Team does not claim confirmed remote access merely because a role is enabled.

Workspace puts active work first, followed by a compact availability summary with links to the relevant settings. Workspace Hosts contains the full infrastructure report. Devices and Workspace Hosts explain and disable unsafe Host removal, including removal of the last surviving copy. Runner requests continue showing progress through delivery until they complete or are refused.

There is one Owner, any number of Admins and many Members. Admins can manage projects, members, invitations, devices and infrastructure. Only the Owner appoints/removes Admins. An Admin is not automatically a Host, and a Host is not automatically an Admin.

## Lifetime and removal

The Werkbord window is separate from `werkbord-team daemon`, installed as macOS LaunchDaemon `dev.werkbord.team`. Closing or quitting the window leaves Team, hosting, connectivity and the individual runner running. The service starts at boot, including before login. Only one macOS account owns the Team installation on a Mac; another account cannot replace its credentials.

Settings offers these distinct choices:

- Remove the app by moving it to the Trash; Team’s service and the runner remain (remove the service first from Workspaces and devices, which also removes its local connection secrets).
- Keep the service, or explicitly stop it while retaining data. Stopping persists across reboot; opening Team starts it again.
- Leave the workspace and retain a local archive, or leave and remove its local workspace secrets/data.
- After a checked leave, remove the Team service, local connection secrets, license and settings from the welcome screen. Explicitly retained archives/backups are preserved. The individual runner is unaffected.

A Host cannot leave or delete its workspace data unless the live cluster proves removal keeps a healthy quorum and another copy. The final voting Host is refused. Unreachable storage is insufficient proof. The OS uninstaller independently refuses removal while a workspace directory exists. Host demotion stops the database and API, removes authority credentials and preserves the old database before returning the device to member operation. Durable transition markers complete archive moves and enrollment recovery across restarts.

## Implementation and security

Team 3.0 seals device keys with OS Keychain and uses a separate authority wrapping key for workspace root/CA/database material. The service is built with cgo to use Security.framework; failed Keychain access stops rather than falling back to a file. The actual signed root-service Keychain lifecycle remains a production validation gate. See [TEAM_SECURITY.md](TEAM_SECURITY.md), [TEAM_LICENSE.md](TEAM_LICENSE.md), [TEAM_INSTALL.md](TEAM_INSTALL.md) and [TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md).

Team code is under `internal/team/` and `cmd/werkbord-team/`. The installer is part of the Werkbord app (`desktop/internal/teaminstall`): the app’s own executable, run with a fixed argument, and for its one privileged step run as root by macOS after the authorization. It imports no code of this repository and starts only fixed programs by absolute path; architecture tests enforce both. The daemon executes only the pinned Nebula/rqlite infrastructure through existing narrowly reviewed supervisors, never agents, Git or shell work.

The root-owned service installation is `/Library/Application Support/Werkbord Team` and its plist is `/Library/LaunchDaemons/dev.werkbord.team.plist`. Data, keys and copied helpers use private permissions. An updated app notices that the installed service is older and, when you choose to update it, requests normal macOS authorization (a service that belongs to a workspace is never replaced this way). Updates verify the app bundle, stage every helper before stopping the old service and roll back helpers if startup fails. No service executable is run from a user-writable app location. The window’s private credential is paired through the authorized local installer.

The window contacts only authenticated loopback `/api/device/v1` on port 7431. `/api/team/v1` is forwarded using this device’s sealed workspace credential to literal loopback or workspace-private addresses, with no proxy or redirects. The local policy API is never served on the overlay. A Team page cannot call the app: it asks the shell over a neutral message channel, and the shell’s relay allows only the listed methods (see ARCHITECTURE.md §22). The app’s native bindings exist for the shell alone; they expose no arbitrary command, file/path, HTTP request or workspace administration method. Shared `internal/nativebridge` contains only the WebKit/Wails call protocol.

A signed request names a semantic action, target device, user, workspace, expiry and replay identity. Hosts store/route it. The recipient verifies the signature and ownership, compares the sender with a locally approved application key, and rechecks expiry/revocation on retries. A compromised routing Host cannot replace an approved sender key. Members approve their own other devices locally and compare the displayed identity fingerprints. The recipient then uses a scoped local access token for the individual controller’s narrow route allow-list. The full controller credential is exchanged once and not retained by Team. Local runner policies and normal execution approvals still apply. Phase 2 task-specific approvals are stored by Individual and consumed with the run ID in its run-creation transaction, so an uncertain response recovers that run without starting twice. No remote arbitrary-shell API exists.

A license is an offline signed document (`claims`, `signature`), bound to product, customer, seats and validity. The build contains only the vendor’s Ed25519 public issuer key. Original claim bytes survive file import. Create Team requires an active license. Enrollment does not ask each member to import the owner’s license. A billing backend, workspace-wide entitlement replication and automatic seat enforcement are future work; this release does not pretend those are implemented.

## Maintainer build and validation

macOS 13 or newer is the supported desktop/service platform. Linux and Windows use clean platform implementations that report unsupported desktop installation/setup explicitly; the existing Team CLI and server remain portable. No fake success or privileged service fallback is used.

```sh
make desktop-check            # native compile of the app (installer included), logic tests and vet
make desktop-package          # development .app + DMG, carrying the Team service (ad hoc signature)
make test-unified-installer   # mounts that DMG and checks versions, signatures and the Team service
make test-team-desktop-browser # real APIs/rqlite + disposable individual bridge, no OS install
make check
```

`ARCH=arm64` or `ARCH=amd64` produces a native development build. Distribution is universal. The packager bundles the original hash-pinned and upstream-signed Nebula release. It builds rqlite from its pinned source commit, combines architectures and signs it; the exact resulting hash is stamped into the Team service and a matching build record. The optional free runner helper is separately built/versioned. Required third-party notices are included. Installer checks validate signatures and fixed program pins before use.

There is no separate Team release. The app that carries Team's service and installer is built, signed with the Developer ID,
notarized and published by the one release workflow ([DESKTOP_RELEASE.md](DESKTOP_RELEASE.md)), from the same commit and at
the same version; `scripts/build-desktop.sh` builds the Team service, the database program and the pinned network program into
`Contents/Helpers`. The only Team-specific release input is `TEAM_LICENSE_ISSUER_PUBLIC_KEY`, the public key that checks licenses.

Before the Team service replaces anything in its root-owned directory it verifies that the app it runs from was signed under
Apple's certificate chain by the Apple Developer team that signed the release (the team is stamped into the release build; see
`desktop/internal/teaminstall/release.go`). A modified copy, or one signed by anyone else, is refused. A
development build is ad hoc signed, has no team, and is not checked.

Validation here builds and verifies an ad hoc development bundle without installing a system service. Real-service installation at boot and production notarization require a separate clean Mac acceptance run with the distributor’s signing credentials.

Owner controls and shared request scheduling are documented in
[EXECUTION_COORDINATION.md](EXECUTION_COORDINATION.md). Metadata-only grants
cannot dispatch; a dispatch-only grant cannot approve. Valid signatures never
replace local policy checks. Revocation prevents new authorization without
silently terminating an accepted local process.

## The window

Team is shown inside the Werkbord window, next to Individual; navigation, host volunteering, security boundaries, lifecycle and
validation are in [ARCHITECTURE.md §22](ARCHITECTURE.md#22-one-app-the-window-the-team-service-and-how-they-are-shipped).
