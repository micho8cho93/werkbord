# Installing Dev Board

```bash
curl -fsSL https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install.sh | sh
```

macOS and Linux, Intel and ARM. No account, no Docker, no root, no hosted database.

## What it does

1. **Downloads** the release for your computer and **checks it** against the `checksums.txt` published with
   it. A download that does not match is not installed, and neither is an executable that does not report the
   version it claims.
2. **Installs** one file, `~/.local/bin/devboard` (`DEVBOARD_INSTALL_DIR` changes it). It is written beside its
   destination and renamed into place, so an upgrade over a running Dev Board is atomic.
3. Runs **`devboard setup`**, which:
   - creates the data directory (default: the OS config directory, `…/devboard`), its `logs/` and `worktrees/`,
     and `config.json`; picks the next free port if 7420 is taken by something else and remembers it;
   - creates the access token (mode 0600) and the SQLite database, upgrading it first if it is older (after a
     copy; see [Upgrades](#upgrades));
   - installs a **background service** and starts it (see below), so the controller keeps running when the
     terminal closes and starts again when you log in. If the system has no service manager Dev Board can use, it
     runs as a detached background process instead and says that it will not restart at login;
   - waits for the controller to answer and shows that **this computer is registered as your first runner**
     (the controller does this itself whenever it starts);
   - turns on **phone access**: Dev Board joins your own Tailscale network from inside the controller. If you are
     not signed in to Tailscale yet it opens the sign-in page in your browser and waits (up to three minutes; it
     carries on without you if you take longer, and Settings shows the same state). See [PHONE.md](PHONE.md);
   - opens Dev Board in your browser, where setup finishes: GitHub (optional), your coding agents (detected, not
     installed for you), and the repositories to add as projects.

Setup is safe to run again: it replaces the controller with the new binary (never a second one beside it), keeps
your data and token, and does not turn phone access back on if you turned it off.

Pass flags to setup after `sh -s --`, or set the variable beside each:

| Flag | Variable | Effect |
| --- | --- | --- |
| `--no-service` | `DEVBOARD_NO_SERVICE=1` | run in the background without installing a login service |
| `--no-start` | `DEVBOARD_NO_START=1` | prepare everything, do not start the controller |
| `--no-open` | `DEVBOARD_NO_OPEN=1` | do not open a browser |
| `--no-network` | `DEVBOARD_NO_NETWORK=1` | skip phone access |
| `--network-wait 5m` | | how long to wait for the Tailscale sign-in |
| | `DEVBOARD_NO_SETUP=1` | (installer only) install the executable and stop |
| | `DEVBOARD_VERSION=v1.2.3` | (installer only) install that release instead of the latest |
| | `DEVBOARD_BASE_URL` | (installer only) where releases are, for a mirror |

## The service

It runs as you, not as root, because agents run as you, in your repositories, with your own sign-ins.

| System | What is installed | Stop at logout? |
| --- | --- | --- |
| macOS | a launchd user agent, `~/Library/LaunchAgents/dev.devboard.controller.plist` | starts at login, restarted if it crashes |
| Linux | a systemd user unit, `~/.config/systemd/user/devboard.service` (and `loginctl enable-linger`, where allowed, so it also runs without a login session) | as above |
| Windows | a scheduled task, `Devboard`, at log-on | as above |
| anything else | a detached process, recorded in the data directory | does not restart at login |

A service does not inherit your shell's `PATH`, and the agents (`claude`, `codex`) are found through it, so setup
**captures the `PATH` it was run with** into the service. If you install an agent somewhere new, run
`devboard setup` again from a terminal where it works; `devboard doctor` notices when your shell finds an agent
that the controller does not.

The controller's log is `<data dir>/logs/controller.log` (rotated at 10 MB at each restart); `devboard logs` shows
the end of it.

## Commands

| Command | |
| --- | --- |
| `devboard status` | what is running, where, as which service, phone address, runner, agents; `--json` for scripts. Exits 1 when the controller is not running |
| `devboard start` / `stop` / `restart` | manage the service. They manage the controller the service owns and never start a second one beside it: if one is already answering (say, `devboard serve` in a terminal) `start` says so and `stop` tells you it was not started by the service |
| `devboard open` | open the app in your browser, signed in. `--print` prints the link instead; `--phone` brings up phone access if needed and shows the address; `--qr` adds a QR code |
| `devboard doctor` | check the controller, database, Git, Codex, Claude, private network, GitHub, runner registration and every configured project. `--json`, `--strict` (warnings fail too). It never prints a secret |
| `devboard update` | install the latest release over this executable and restart the controller; `--check` only looks, `--version vX.Y.Z` picks one |
| `devboard uninstall` | remove the login service. Your data stays |
| `devboard logs [-n N]` | the end of the controller's log |

## Upgrades

`devboard update` (or running the installer again) replaces the executable and restarts the controller. When the
new version has database migrations, the controller **copies the database first** to
`<data dir>/backups/devboard-v<N>-<time>.db` (the newest five are kept) and then upgrades it; a database written
by a newer version than the one running is refused rather than damaged. If the new version does not come up,
`devboard update` puts the old executable back and restarts it, and says where the backup is.

Nothing is installed unless its checksum matches the release's. A release is a GitHub release; set
`DEVBOARD_RELEASE_URL` (and `DEVBOARD_BASE_URL` for the installer) to use a mirror.

## Uninstalling

```bash
devboard uninstall            # the login service
rm ~/.local/bin/devboard      # the program
rm -rf "<data dir>"           # your projects, tasks, settings and token: only if you mean it
```

`devboard status` prints the data directory. If phone access was on, Dev Board's device also appears in your
Tailscale admin console under Machines, where you can remove it.

## Windows (experimental)

```powershell
irm https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install.ps1 | iex
```

It installs `devboard.exe` under `%LOCALAPPDATA%\Programs\Devboard`, adds it to your PATH and runs setup, with a
scheduled task for the service. The controller, board, Git views, repository health, GitHub and phone access are
the same. **Coding agents do not run natively on Windows yet** (the runtime uses Unix process control): to run
agents there, install inside WSL with the Linux installer. The Windows installer and service code are written but
have not been run on a Windows machine.

## Building a release

```bash
make dist                    # dist/devboard_<version>_<os>_<arch>.tar.gz|zip and checksums.txt; the version is cmd/devboard/VERSION
make test-install            # the installer, end to end, against a release server on this computer
```

Pushing a tag `werkbord-vX.Y.Z` (the individual product's tag; see [VERSIONING.md](VERSIONING.md)) runs
`.github/workflows/release.yml`, which checks that the tag agrees with `cmd/devboard/VERSION`, runs the checks, builds those
archives and publishes them as the GitHub release marked *latest*, which is where `install.sh` and `devboard update` look.
`DEVBOARD_VERSION` takes `v1.2.3` or the tag `werkbord-v1.2.3`. Werkbord Team is released and installed separately, with
its own tag (`werkbord-team-vX.Y.Z`) and installer; see [TEAM.md](TEAM.md).
