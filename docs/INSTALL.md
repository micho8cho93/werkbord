# Installing Werkbord

```bash
curl -fsSL https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install.sh | sh
```

macOS and Linux, Intel and ARM. No account, no Docker, no root, no hosted database.

This is the installer for a terminal, a server or Linux. On a Mac, the [desktop app](DESKTOP.md) is the other way in:
it installs the same program in the same place, with the same data directory and login service, so the two are one
installation. Installing one and using the other does not make a second.

## What it does

1. **Downloads** the release for your computer and **checks it** against the `checksums.txt` published with
   it. A download that does not match is not installed, and neither is an executable that does not report the
   version it claims.
2. **Installs** one file, `~/.local/bin/werkbord` (`WERKBORD_INSTALL_DIR` changes it). It is written beside its
   destination and renamed into place, so an upgrade over a running Werkbord is atomic.
3. Runs **`werkbord setup`**, which:
   - creates the data directory (default: the OS config directory, `…/werkbord`), its `logs/` and `worktrees/`,
     and `config.json`; picks the next free port if 7420 is taken by something else and remembers it;
   - creates the access token (mode 0600) and the SQLite database, upgrading it first if it is older (after a
     copy; see [Upgrades](#upgrades));
   - installs a **background service** and starts it (see below), so the controller keeps running when the
     terminal closes and starts again when you log in. If the system has no service manager Werkbord can use, it
     runs as a detached background process instead and says that it will not restart at login;
   - waits for the controller to answer and shows that **this computer is registered as your first runner**
     (the controller does this itself whenever it starts);
   - turns on **phone access**: Werkbord joins your own Tailscale network from inside the controller. If you are
     not signed in to Tailscale yet it opens the sign-in page in your browser and waits (up to three minutes; it
     carries on without you if you take longer, and Settings shows the same state). See [PHONE.md](PHONE.md);
   - opens Werkbord in your browser, where setup finishes: GitHub (optional), your coding agents (detected, not
     installed for you), and the repositories to add as projects.

Setup is safe to run again: it replaces the controller with the new binary (never a second one beside it), keeps
your data and token, and does not turn phone access back on if you turned it off.

Pass flags to setup after `sh -s --`, or set the variable beside each:

| Flag | Variable | Effect |
| --- | --- | --- |
| `--no-service` | `WERKBORD_NO_SERVICE=1` | run in the background without installing a login service |
| `--no-start` | `WERKBORD_NO_START=1` | prepare everything, do not start the controller |
| `--no-open` | `WERKBORD_NO_OPEN=1` | do not open a browser |
| `--no-network` | `WERKBORD_NO_NETWORK=1` | skip phone access |
| `--network-wait 5m` | | how long to wait for the Tailscale sign-in |
| | `WERKBORD_NO_SETUP=1` | (installer only) install the executable and stop |
| | `WERKBORD_VERSION=v1.2.3` | (installer only) install that release instead of the latest |
| | `WERKBORD_BASE_URL` | (installer only) where releases are, for a mirror |

## The service

It runs as you, not as root, because agents run as you, in your repositories, with your own sign-ins.

| System | What is installed | Stop at logout? |
| --- | --- | --- |
| macOS | a launchd user agent, `~/Library/LaunchAgents/dev.werkbord.controller.plist` | starts at login, restarted if it crashes |
| Linux | a systemd user unit, `~/.config/systemd/user/werkbord.service` (and `loginctl enable-linger`, where allowed, so it also runs without a login session) | as above |
| Windows | a scheduled task, `Werkbord`, at log-on | as above |
| anything else | a detached process, recorded in the data directory | does not restart at login |

A service does not inherit your shell's `PATH`, and the agents (`claude`, `codex`) are found through it, so setup
**captures the `PATH` it was run with** into the service. If you install an agent somewhere new, run
`werkbord setup` again from a terminal where it works; `werkbord doctor` notices when your shell finds an agent
that the controller does not.

The controller's log is `<data dir>/logs/controller.log` (rotated at 10 MB at each restart); `werkbord logs` shows
the end of it.

## Commands

| Command | |
| --- | --- |
| `werkbord status` | what is running, where, as which service, phone address, runner, agents; `--json` for scripts. Exits 1 when the controller is not running |
| `werkbord start` / `stop` / `restart` | manage the service. They manage the controller the service owns and never start a second one beside it: if one is already answering (say, `werkbord serve` in a terminal) `start` says so and `stop` tells you it was not started by the service |
| `werkbord open` | open the app in your browser, signed in. `--print` prints the link instead; `--phone` brings up phone access if needed and shows the address; `--qr` adds a QR code |
| `werkbord doctor` | check the controller, database, Git, Codex, Claude, private network, GitHub, runner registration and every configured project. `--json`, `--strict` (warnings fail too). It never prints a secret |
| `werkbord update` | install the latest release over this executable and restart the controller; `--check` only looks, `--version vX.Y.Z` picks one |
| `werkbord uninstall` | remove the login service. Your data stays |
| `werkbord logs [-n N]` | the end of the controller's log |

## Upgrades

`werkbord update` (or rerunning the installer for a newer release) upgrades the executable and restarts the controller through the same recovery lifecycle. When the
new version has database migrations, the controller **copies the database first** to
`<data dir>/backups/devboard-v<N>-<time>.db` (the newest five are kept) and then upgrades it; a database written
by a newer version than the one running is refused rather than damaged. If the new version does not come up,
`werkbord update` puts the old executable back and restarts it, and says where the backup is.

The updater also takes a complete SQLite snapshot before replacing the executable. It stops any running paired
runner service first and remembers that the runner must recover. An update of a running installation is complete
only after the new controller reports the expected version, can read projects, SQLite passes its integrity check,
and the required runner completes a fresh authenticated sync on the new version. Then the old executable
(`<executable>.prev`) is removed. `werkbord runner status` includes the last successful sync's version, runner ID,
controller address and timestamp, alongside the service state and last sync error.

If the controller fails verification, the updater stops it, restores the executable and database snapshot, and
restarts the prior installation. Failed database files remain in `backups/failed-update-*` for inspection. A runner
reconnect failure leaves the new controller running and retains the prior executable and database snapshot; the
error names both paths and the recovery commands. Inspect `werkbord status`, `werkbord doctor`, and
`werkbord runner status` before retrying. Recovery that cannot prove ownership still requires runner resolution.

Installer upgrades use the downloaded release's recovery code, even when the installed executable predates it.
They refuse active work and implicit downgrades; use the installed `werkbord update --force` only when intentional
interruption or downgrade is needed. Older pinned releases without the installer recovery entry point are refused
for an existing installation; clean installation of those releases is still supported. Rerunning the same version
leaves it installed; use `werkbord setup` explicitly to change startup options.

Updating an intentionally stopped controller leaves it stopped and retains `.prev` for later verification.
Start it and verify controller/database/runner health before removing that exact rollback file. Another update
refuses to overwrite an unresolved `.prev`; recover or finish verifying the earlier installation first.

`werkbord update` is also what the desktop app's **Update now** runs, and what `GET /api/update` (the "Werkbord X is
available" banner) tells you to run: the controller only *reports* that a newer release exists (once every six hours at most,
never for a build from source, and not at all with `"noUpdateCheck": true`), and applying it is always done on this computer.

Nothing is installed unless its checksum matches the release's. A release is a GitHub release; set
`WERKBORD_RELEASE_URL` (and `WERKBORD_BASE_URL` for the installer) to use a mirror.

## Renamed from Dev Board

Werkbord was called Dev Board, and its command `devboard`. Since 1.0 the command is `werkbord`. An existing install
needs nothing from you: update it the way you always have (`devboard update`, or the installer), and

- `devboard` keeps working, as another name for `werkbord` (a link beside it; `devboard.cmd` on Windows);
- every `DEVBOARD_*` variable keeps working; a `WERKBORD_*` one wins if both are set;
- the data stays where it is (`…/devboard` in your user config directory): nothing is moved or copied;
- phone access keeps its address: a computer that joined your tailnet as `devboard-<name>` stays `devboard-<name>`;
- the login service keeps running under its old label until the next `werkbord setup`, which replaces it with
  `dev.werkbord.controller` (`werkbord.service`, the `Werkbord` task); only ever one runs.

A new install uses the new names throughout. There is no deprecation warning, and no plan to remove the old names.

## Uninstalling

```bash
werkbord uninstall            # the login service
rm ~/.local/bin/werkbord      # the program
rm -rf "<data dir>"           # your projects, tasks, settings and token: only if you mean it
```

If you used the desktop app, delete `Werkbord.app` as well. Deleting the app does **not** remove Werkbord: the controller
and its login service are the ones above, so `werkbord uninstall` (or `rm ~/.local/bin/werkbord`) is what removes them.

`werkbord status` prints the data directory. If phone access was on, Werkbord's device also appears in your
Tailscale admin console under Machines, where you can remove it.

## Windows (experimental)

```powershell
irm https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install.ps1 | iex
```

It installs `werkbord.exe` under `%LOCALAPPDATA%\Programs\Werkbord`, adds it to your PATH and runs setup, with a
scheduled task for the service. The controller, board, Git views, repository health, GitHub and phone access are
the same. **Coding agents do not run natively on Windows yet** (the runtime uses Unix process control): to run
agents there, install inside WSL with the Linux installer. The Windows installer and service code are written but
have not been run on a Windows machine.

## Building a release

```bash
make dist                    # dist/werkbord_<version>_<os>_<arch>.tar.gz|zip and checksums.txt; the version is cmd/werkbord/VERSION
make test-install            # the installer, end to end, against a release server on this computer
```

Pushing a tag `werkbord-vX.Y.Z` (the individual product's tag; see [VERSIONING.md](VERSIONING.md)) runs
`.github/workflows/release.yml`, which checks that the tag agrees with `cmd/werkbord/VERSION`, runs the checks, builds those
archives and publishes them as the GitHub release marked *latest*, which is where `install.sh` and `werkbord update` look.
`WERKBORD_VERSION` takes `v1.2.3` or the tag `werkbord-v1.2.3`. Werkbord Team is released and installed separately, with
its own tag (`werkbord-team-vX.Y.Z`) and installer; see [TEAM.md](TEAM.md).
