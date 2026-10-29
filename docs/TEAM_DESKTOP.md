# Werkbord Team in the Werkbord desktop app

Werkbord Team is part of the Werkbord app: there is no second app and no second window. A team of up to two people is free; a team of three or more needs Premium ([PLANS.md](PLANS.md)). The Werkbord window shows Team in its own sidebar next to Individual (see [ARCHITECTURE.md §22](ARCHITECTURE.md#the-window-and-its-workspaces)), using Werkbord’s existing visual system, and Team keeps everyday work (Workspace, Projects, Board, My Work, Reviews, Activity) separate from administration (Members, Devices, Workspace Hosts, Connectivity, Backups, Settings).

## For users

In Werkbord choose **Add a Team**. After you agree in a dialog, the operating system asks for administrator authorization to install Team’s background service, which creates the private network interface. macOS uses its administrator prompt, Linux uses polkit, and Windows uses UAC. Nothing is installed or started until you approve it.

Choose **Create Team**, enter the team and owner names, then create the workspace. Team creates the workspace and device identities, the first Owner, private network, single-node database, first Workspace Host and automatic backups. It determines whether an advertised address is eligible for connectivity and detects an existing local Werkbord runner. One host is the ordinary starting arrangement: keep it online while the team works. The initial Workspace page offers **Invite people**, **Devices** and **Check backups**, and reports whether automatic backups are actually enabled.

An administrator creates an invitation in **Members**. A link click or QR scan opens Team; the recipient verifies it, names the computer and joins. Pending enrollment survives window closure and service restart. An administrator approves the device in **Devices**. If the free runner is missing, **Install / set up free runner** uses the signed bundled individual Werkbord executable’s normal setup. An installed individual app can also be opened. Team never installs it automatically. Existing credentials and individual policies remain supported.

Runners perform the work. Scheduled and remote work need an online runner. Workspace Hosts keep Team available. Extra hosts and connectivity controls are under **Settings → Advanced**; three hosts can keep the workspace available when one goes offline. Connectivity Hosts help devices connect across networks; enabling the role alone cannot make an unreachable address accessible. Sleeping laptops are weaker choices than desktops, Mac minis, office servers and suitable NAS/server systems. Status and warnings reflect observed quorum, database, connectivity and backup health; Team does not claim confirmed remote access merely because a role is enabled.

Workspace puts active work first. Backups and device approvals remain in ordinary Settings; Workspace Hosts in Advanced contains the full infrastructure report. Direct links and recovery actions reveal the selected advanced section automatically. One healthy host is informational rather than an attention item; unavailable hosts and failed backups still raise warnings. Devices and Workspace Hosts explain and disable unsafe Host removal, including removal of the last surviving copy. Runner requests continue showing progress through delivery until they complete or are refused.

There is one Owner, any number of Admins and many Members. Admins can manage projects, members, invitations, devices and infrastructure. Only the Owner appoints/removes Admins. An Admin is not automatically a Host, and a Host is not automatically an Admin.

## Lifetime and removal

The Werkbord window is separate from the Team daemon. It runs as a macOS LaunchDaemon, a Linux systemd service, or a Windows service, so closing or quitting the window leaves Team, hosting, connectivity and the individual runner running. The service starts at boot, including before login. One OS account owns the local Team installation; another account cannot replace its credentials.

Settings offers these distinct choices:

- Remove the desktop app with its OS package/uninstaller; Team’s service and the runner remain (remove the service first from Workspaces and devices, which also removes its local connection secrets).
- Keep the service, or explicitly stop it while retaining data. Stopping persists across reboot; opening Team starts it again.
- Leave the workspace and retain a local archive, or leave and remove its local workspace secrets/data.
- After a checked leave, remove the Team service, local connection secrets and settings from the welcome screen. Explicitly retained archives/backups are preserved. The individual runner is unaffected.

A Host cannot leave or delete its workspace data unless the live cluster proves removal keeps a healthy quorum and another copy. The final voting Host is refused. Unreachable storage is insufficient proof. The OS uninstaller independently refuses removal while a workspace directory exists. Host demotion stops the database and API, removes authority credentials and preserves the old database before returning the device to member operation. Durable transition markers complete archive moves and enrollment recovery across restarts.

## Implementation and security

Team seals device keys with macOS Keychain and uses a separate authority wrapping key for workspace root/CA/database material. Linux and Windows use a protected passphrase file under the service data directory. Failed key access stops rather than falling back to a less-protected file. The actual signed root-service Keychain lifecycle remains a production validation gate. See [TEAM_SECURITY.md](TEAM_SECURITY.md), [PLANS.md](PLANS.md), [TEAM_INSTALL.md](TEAM_INSTALL.md) and [TEAM_SECURITY_GATE.md](TEAM_SECURITY_GATE.md).

Team code is under `internal/team/` and `cmd/werkbord-team/`. The installer is part of the Werkbord app (`desktop/internal/teaminstall`): the app’s own executable, run with a fixed argument, and for its one privileged step run as root by macOS after the authorization. It imports no code of this repository and starts only fixed programs by absolute path; architecture tests enforce both. The daemon executes only the pinned Nebula/rqlite infrastructure through existing narrowly reviewed supervisors, never agents, Git or shell work.

macOS installs its service under `/Library/Application Support/Werkbord Team` and uses `/Library/LaunchDaemons/dev.werkbord.team.plist`. Linux uses `/var/lib/werkbord-team` and systemd; Windows uses `%ProgramData%\Werkbord Team` and the Service Control Manager. Data, keys and copied helpers use private permissions. The app compares the authenticated running service version with its own before opening Team. A mismatch leads to Devices & Hosting → Update Team, which requests OS administrator authorisation. On macOS, an enrolled service uses the verified-backup maintenance path: every local workspace is backed up and verified separately, and its identity and health are checked after updating or restoring the previous programs. See [TEAM_INSTALL.md](TEAM_INSTALL.md#maintenance-updating-a-service-that-holds-a-workspace). Updates verify the signed/root-owned app, stage every helper before stopping the old service and roll back helpers if startup fails. The window’s private credential is paired through the authorized local installer.

The window contacts only authenticated loopback `/api/device/v1` on port 7431. `/api/team/v1` is forwarded using this device’s sealed workspace credential to literal loopback or workspace-private addresses, with no proxy or redirects. The local policy API is never served on the overlay. A Team page cannot call the app: it asks the shell over a neutral message channel, and the shell’s relay allows only the listed methods (see ARCHITECTURE.md §22). The app’s native bindings exist for the shell alone; they expose no arbitrary command, file/path, HTTP request or workspace administration method. Shared `internal/nativebridge` contains only the WebKit/Wails call protocol.

A signed request names a semantic action, target device, user, workspace, expiry and replay identity. Hosts store/route it. The recipient verifies the signature and ownership, compares the sender with a locally approved application key, and rechecks expiry/revocation on retries. A compromised routing Host cannot replace an approved sender key. Members approve their own other devices locally and compare the displayed identity fingerprints. The recipient then uses a scoped local access token for the individual controller’s narrow route allow-list. The full controller credential is exchanged once and not retained by Team. Local runner policies and normal execution approvals still apply. Phase 2 task-specific approvals are stored by Individual and consumed with the run ID in its run-creation transaction, so an uncertain response recovers that run without starting twice. No remote arbitrary-shell API exists.

There is no licence. A team may have two people on the Free plan and any number on Premium; the rule is enforced in the transaction that adds a person ([PLANS.md](PLANS.md)). The Premium unlock key and a billing backend are future work; this release does not pretend those are implemented.

## Maintainer build and validation

The unified desktop app targets macOS 13 or newer (universal), Debian/Ubuntu Linux amd64 and arm64, and Windows amd64. Windows arm64 is not included because the pinned Team database/network payloads do not support it. Linux uses GTK 3 and WebKit2GTK 4.1; its `.deb` declares runtime dependencies. The Windows app embeds Microsoft's WebView2 bootstrapper and offers to install the runtime if it is missing. Team service installation uses each platform’s native authorization and service manager; unsupported platforms fail closed.

```sh
make desktop-check            # native macOS compile of the app (installer included), logic tests and vet
make desktop-package          # development .app + DMG, carrying the Team service (ad hoc signature)
make test-unified-installer   # mounts that DMG and checks versions, signatures and the Team service
make test-team-desktop-browser # real APIs/rqlite + disposable individual bridge, no OS install
make check
```

The Mac packager produces a universal app. Linux packages build on native amd64 and arm64 runners; Windows packages target amd64. All bundles carry hash-pinned Nebula and rqlite programs plus required notices. Installer checks validate the platform's app trust and fixed program pins before use.

There is no standalone Team Host archive in the public release. The one unified app release carries Team's service and installer for each supported OS. The release workflow ([DESKTOP_RELEASE.md](DESKTOP_RELEASE.md)) builds each app from the same commit and version; Team has no release input of its own.

Before Team replaces installed service files, the app verifies the release trust for its OS: Developer ID on macOS, the
AuthentiCode publisher thumbprint on Windows, and a protected root-owned installation tree on Linux. A development build is
not accepted for privileged setup.

Local validation builds the app and cross-compiles the Linux and Windows service paths without installing a system service. The release workflow builds packages on native Linux and Windows runners and installs the Windows app, but the Team service's boot, networking, replacement and removal lifecycle still needs clean-device acceptance on each OS. Production macOS distribution also requires the distributor’s signing credentials and notarization.

Owner controls and shared request scheduling are documented in
[EXECUTION_COORDINATION.md](EXECUTION_COORDINATION.md). Metadata-only grants
cannot dispatch; a dispatch-only grant cannot approve. Valid signatures never
replace local policy checks. Revocation prevents new authorization without
silently terminating an accepted local process.

## The window

Team is shown inside the Werkbord window, next to Individual; navigation, host volunteering, security boundaries, lifecycle and
validation are in [ARCHITECTURE.md §22](ARCHITECTURE.md#22-one-app-the-window-the-team-service-and-how-they-are-shipped).
