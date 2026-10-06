# The desktop app

Werkbord for the Mac, as an app: download it, drag it to Applications, open it. No Terminal.

It is **the same Werkbord**, in a native window. The controller (Go, SQLite, your agents) does not live in the app: it is
the background service `werkbord setup` has always made, and it keeps running when you close the window or quit the app.
The window loads the controller's own web app, so what you see is what a browser or your phone sees, from the same
controller.

```
                  Werkbord controller
                  Go + SQLite + agents          (a login service; keeps running)
                           |
        +------------------+------------------+
        |                  |                  |
   Werkbord.app        a browser           your phone
   (this page)      127.0.0.1:7420     Tailscale, see PHONE.md
```

Nothing about the controller, its database, its token, its address or phone access changed to make this, and none of
them depends on the app: `werkbord` in a terminal and the curl installer keep working exactly as before.

## For everyone: install and open

1. Download `Werkbord_<version>_darwin_arm64.dmg` (Apple Silicon; `…_amd64.dmg` for Intel) from the
   [releases page](https://github.com/micho8cho93/werkbord/releases).
2. Open it and drag **Werkbord** onto **Applications**.
3. Open **Werkbord** from Applications.

The first time, the app sets Werkbord up on this computer (a few seconds): it puts the `werkbord` program in
`~/.local/bin`, creates your data and database, installs the login service so Werkbord is still working after you
close the window and after you log in again, starts it, and opens the window. After that, opening the app finds the
controller already running and connects to it.

A release's disk image is signed with a Developer ID and notarized by Apple (see [Signing and notarization](#signing-and-notarization)),
so the first time you open it macOS asks the one question it asks about every app from the Internet ("Werkbord is an
app downloaded from the Internet. Are you sure you want to open it?" with *Apple checked it for malicious software and
none was detected*), and **Open** is all it needs. A build that is **not** notarized (one you made yourself with
`make desktop`, or an ad-hoc signed one from CI) does not ask when it is built on the Mac that runs it, and on another Mac
macOS 15 (Sequoia) and later refuses it with *"Werkbord" Not Opened*. Control-click → Open no longer gets past that
(it did before macOS 15). Instead: try to open the app once, then **System Settings → Privacy & Security**, scroll to
*Security*, press **Open Anyway** beside the message about Werkbord, and confirm with your password or Touch ID.

Closing the window hides the app, as on any Mac; click it in the Dock to bring it back. **Cmd-Q quits the app. Neither
stops your controller, your agents or your schedules.** To stop Werkbord itself, use `werkbord stop` (it refuses while
agents are working) or `werkbord uninstall` to remove the login service.

Phone access is set up from **Settings → Phone access** (the app does not open a browser sign-in page for you the way
`werkbord setup` does in a terminal). Links that leave Werkbord (GitHub's sign-in, Tailscale's, pull requests) open in
your normal browser.

The menu bar has **View → Open in Browser** (the same controller, signed in, in a tab), and **Help → Check for
Updates…**, **Show Diagnostics…** and **Show Controller Log**.

## For developers and advanced users: one installation, two ways in

The curl installer is unchanged and stays the way to install on Linux, on a server, or from a terminal:

```bash
curl -fsSL https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install.sh | sh
```

**Both paths operate the same underlying controller**, not two copies of it:

| | The app | The installer |
| --- | --- | --- |
| Program | `~/.local/bin/werkbord` (and `devboard`) | the same file |
| Data, database, token, `config.json` | the data directory `werkbord setup` chose (`…/Application Support/werkbord`, or `devboard` on an install from before the rename) | the same directory |
| Login service | `dev.werkbord.controller` (launchd) | the same one (a service under the pre-rename label is found and used as it is) |
| Commands | all of them, from a terminal, once `~/.local/bin` is on your `PATH` | all of them |
| Updates | `werkbord update`, behind **Update now** | `werkbord update` |

So: installing with the curl script and then opening the app joins your installation (it does not make another); using
the app first and `werkbord` later is the same installation. The app finds an existing installation from the login
service's own definition (which executable, which data directory), so it joins the right one even though a program
started from the Finder has none of your terminal's environment.

To use `werkbord` from a terminal after installing the app, add `~/.local/bin` to your `PATH`
(`echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc`).

## How it connects

When it opens, the app (`internal/launcher`) does this, and nothing more:

1. **Looks for the installation**: the data directory and executable the login service was set up with, else the
   defaults (and `WERKBORD_DATA_DIR` if it is set).
2. **Asks the controller if it is there** (`GET /api/health`, which is public). If one answers, it checks that it
   accepts this computer's token (`GET /api/projects`) and **connects, changing nothing**: no restart, no upgrade, no
   second process.
3. Only if nothing answers:
   - **no program installed**: it copies the one inside the app to `~/.local/bin/werkbord` (staged and renamed into
     place, and run once to see it reports its version) and runs `werkbord setup --no-open --no-network`;
   - **an older program installed than the app carries**, and nothing running: it runs the app's `install-release`
     on it, the same entry point the installer uses (database snapshot, rollback). If that is refused (say a runner
     journal has unfinished work) the window still opens, on the program that is there, and says so;
   - otherwise it runs `werkbord start`.
4. Waits for the controller to answer, checks the token, and loads the web app.

It **drives the `werkbord` program** for every one of those. The app does not start processes, write service
definitions or open the database itself, so the lifecycle has one implementation, and the guarantees it already had
apply: a running controller is never started beside, and a second controller on one data directory refuses to start
(`controller.lock`). The tests run the real program to prove it (`internal/launcher/e2e_test.go`).

Three things a program started from the Finder lacks, and what the app does about each:

- **The environment** (data directory): read from the login service's definition, and passed to every command.
- **`PATH`**: `werkbord setup` records the `PATH` it runs with into the login service, and your coding agents (`claude`,
  `codex`) are found through it. A Finder-launched app has almost none, so the app asks your login shell for its `PATH`
  and gives that to `setup`. Agents installed with nvm or npm are found.
- **A home for the program**: the controller is **never run from inside `Werkbord.app`**. An update replaces the
  executable; inside a signed app that would break its signature, and moving or deleting the app would orphan the login
  service. The app ships a copy and installs it where the installer puts it.

If anything fails, the window says why in words (another program on port 7420, a controller that refuses the token, a
log line from the controller) with **Try again**, **Show details**, **Open logs** and **Copy details**.

## Authentication

The controller's API requires its access token everywhere, on loopback too ([ARCHITECTURE.md](ARCHITECTURE.md#10-security-assumptions)).
The app does not change that and has no way around it. It does what `werkbord open` does, as the same user:

1. It reads the token from `<data dir>/token` (mode 0600), the file `werkbord token` prints.
2. It opens `http://127.0.0.1:<port>/#token=<token>`. The token is in the **fragment**, which no server ever receives
   and no log records. The web app keeps it (as it does in any browser) and takes it out of the address bar at once.

No new credential, no new endpoint, no relaxed check. Browsers and phones are authenticated exactly as before; the
private-network listener still demands the token whatever happens on loopback. The token never appears in the app's log,
the controller's log or the diagnostics (they are scrubbed of it, and a test pins that). If you rotate it
(`werkbord token --rotate`), **View → Reload** reconnects with the new one.

If the controller answers but does not accept this computer's token (it was started with a token of its own, or it is
another installation), the app says so instead of showing a sign-in prompt for a token you cannot find.

### What a page may ask of the app

The window can do four things a web page cannot, and the web app asks for them through a small bridge
(`web/src/lib/desktop.ts`) that only exists inside the app:

| Call | What it does |
| --- | --- |
| `Info` | says "this is the desktop app", so the page shows **Update now** and opens links in the browser |
| `OpenExternal(url)` | opens an `http(s)` address in your default browser (nothing else: not `file:`, not a program's handler) |
| `RequestUpdate` | **asks you in a native dialog the page cannot press**, then runs the update |
| `UpdateStatus`, `Diagnostics`, … | read-only |

Wails makes every exported method callable from a page the controller serves (it refuses other origins), so that list is
**pinned by a test** (`TestThePageFacingSurfaceIsExactlyWhatIsListed`): a new method is a decision, not an accident. None
returns anything the page does not already hold, and nothing acts without a native confirmation.

## Updates

**Knowing.** The controller reports whether a newer stable release exists at `GET /api/update` (behind the token like
the rest of the API). It asks GitHub's `/releases/latest` (no API, no credentials; "stable" because that never points at
a pre-release, and Werkbord Team's releases are published so they can never take it), keeps the answer for six hours (a
failure for fifteen minutes), and never asks about a build from source. It is the one outbound request the controller makes
that you did not ask for (Git remotes, GitHub through your own `gh` and the private network are all things you turned on or
did), so it can be turned off: `"noUpdateCheck": true` in `config.json` (or `WERKBORD_NO_UPDATE_CHECK=1`) turns it off. The web app shows:

> **Werkbord 1.2.0 is available** You have 1.1.0. Your data is backed up first. **[Update now]** **[Later]**

in the app. In a browser or on a phone the same banner says how to update instead (`werkbord update` on the computer that
runs Werkbord) and has no button, because **applying an update is never something a request to the controller can
do**: no client, however authenticated, can make your computer replace its own program. **Later** hides that release
until a newer one appears. **Settings → This computer** shows the state and **Check now**.

**Doing.** **Update now** (or **Help → Check for Updates…**) asks you, then runs the installed program's own
`werkbord update`. That is the updater the terminal has, unchanged: the download is checked against the release's
`checksums.txt`, the new program is run once before it replaces anything, **it refuses while agents are working** (or
runner journals are unfinished), the **database is snapshotted**, and the old program and database are **put back if the
new controller does not come up**. When it succeeds the window reloads on the new version, and your browser and phone
reconnect by themselves.

**What an update updates.** The controller program, and with it the web app, which is what the window shows. The
native shell (the window, the menu, this page's loading screen) is the app you installed: it changes rarely and is
replaced by installing a newer disk image. An old shell with a new controller keeps working, because the two only share
the few calls above.

### The production requirement: in-place updating of the app itself

Replacing `Werkbord.app` from inside itself is deliberately **not** implemented. It needs, to be done safely, what
this repository does not have yet: an Apple Developer ID and notarization (a modified, unsigned bundle is refused by
Gatekeeper), an update channel whose *signature* (not only a checksum) the app verifies before it replaces itself (for
instance [Sparkle](https://sparkle-project.org)'s EdDSA-signed appcast), and a helper that swaps the bundle after the app
quits. Until then, the unsafe shortcut (downloading a `.dmg` over HTTPS and replacing the app) is not offered: you install
a new disk image yourself, and the controller, which is what matters day to day, updates safely in place as above.

## Building it

macOS only, with the Xcode command line tools (`xcode-select --install`), Go and Node (as for `make build`):

```bash
make desktop            # dist/desktop/Werkbord.app
make desktop-package    # that, and dist/desktop/Werkbord_<version>_darwin_<arch>.dmg (+ .sha256)
make desktop-dev        # run the window from source, against a controller built from this tree
make desktop-test       # the app's tests (they need no window system, so they run anywhere; `make check` includes them)
make desktop-check      # desktop-test, and on a Mac that the window code builds
```

`ARCH=amd64 make desktop` builds for Intel (`arm64` is the default on Apple Silicon). `VERSION=v1.2.3` stamps a release
version; by default a build reports what `scripts/product.sh werkbord build-version` says, which is exactly `v1.2.3`
only on a clean checkout of that tag.

`make desktop-dev` is for working on the app: it runs the window with `go run`, uses `bin/werkbord` as its program,
and keeps **its own data (`.desktop-dev/`) and port (127.0.0.1:7499)**, installing nothing and making no login service,
so it never touches the installation you use. `DESKTOP_DEV_DATA=…` and `DESKTOP_DEV_ADDR=…` change them. Set
`WERKBORD_DESKTOP_LOG_LEVEL=debug` to see every page call in `~/Library/Logs/Werkbord/desktop.log`.

What is in the bundle (`scripts/build-desktop.sh`):

```
Werkbord.app/Contents/MacOS/Werkbord        the window (desktop/, Wails v2)
Werkbord.app/Contents/Helpers/werkbord      the controller and command line: the program the release archives carry
Werkbord.app/Contents/Resources/icon.icns   from desktop/build/appicon.png
```

It does **not** contain Codex or Claude Code: Werkbord finds the ones you have installed, as it always did. The app is
not sandboxed (it must run your agents and Git in your repositories and write the login service), and asks for no
entitlements ([entitlements.plist](../desktop/build/darwin/entitlements.plist) says why).

### Where the code is

```
desktop/                 the app: its own Go module (devboard/desktop), so cgo and WebKit are never needed by `make check` or Linux CI
  main.go ui.go menu.go    the window, the menu bar, the Wails glue (macOS only)
  internal/shell/          everything the app does (connect, diagnostics, updating, what a page may ask): pure Go, tested without a window
  frontend/dist/           the loading and error screen: the one page that is not the controller's web app
  build/                   Info.plist, entitlements, the icon source
internal/launcher/       finds, installs and starts the controller by driving the werkbord program (tests run the real thing)
internal/update/         Check and Checker: whether a newer release exists (also behind GET /api/update)
internal/daemon/         Describe: what the installed login service says (which program, which data)
scripts/build-desktop.sh the .app and .dmg
web/src/lib/desktop*.ts  the bridge, in the web app; UpdateBanner.svelte
```

`desktop/` is part of the **individual** product (it versions with `cmd/werkbord/VERSION`, and its releases carry the
`werkbord-v…` tag). `internal/archtest` fails if the controller's build or `go.mod` ever learns of the window toolkit,
or if `desktop/` stops being a separate module or reaches Team.

### Testing

| What | Where |
| --- | --- |
| An existing controller is joined and left alone; no duplicates; one that is not running is started; first run installs and sets up; upgrade rules; the login service decides which installation | `internal/launcher/launcher_test.go` (real controller in-process, fake program) |
| The same with the **real program in real processes**: install, start, two more connections (same pid), a second `serve` refused, active runs stop an update, a real checksummed update with a database snapshot, no downgrade, joining a terminal-made installation | `internal/launcher/e2e_test.go` (uses a temporary `HOME`, `--no-service` and a free port: it cannot touch your installation) |
| Authentication stays enforced; the sign-in link carries the token only in the fragment; `/api/update` needs the token and is read-only | `internal/api`, `internal/controller`, `internal/config` tests |
| Version and update logic: newer/older/equal/pre-release/source builds, caching, opt-out | `internal/update/update_test.go` |
| What a page may ask, the update confirmation, Later, refused updates, diagnostics | `desktop/internal/shell/shell_test.go` |
| The bridge's protocol, external-link rules, update offers | `web/src/lib/desktop.test.ts`, `updates.test.ts` |
| Wails and the window build and sign | `make desktop-check`, `make desktop-package` (CI, on macOS) |
| The signing policy, the temporary keychain, what a release refuses, and that the hardened program, controller, Tailscale node and app run | `scripts/test-desktop-sign.sh` (`make test-desktop-sign`) |
| Notarization (accepted, rejected, no network, wrong credentials, no credentials) and the check of a published release | `scripts/test-notarize-desktop.sh` |

Closing the window and quitting the app stopping nothing is structural: the controller is a launchd job that is not the
app's child, and the app has no code path that stops it.

## Signing and notarization

`make desktop` signs ad hoc, which runs on the Mac that built it and nowhere else. What a release publishes is signed
with a Developer ID, notarized and stapled, all by `scripts/build-desktop.sh --release`, which can only be run with the
owner's Apple credentials (the steps to get and install them are in [DESKTOP_RELEASE.md](DESKTOP_RELEASE.md)).

| What | Where | What it guarantees |
| --- | --- | --- |
| `scripts/build-desktop.sh --release` | the build | Refuses to build unless the result can be published: a Developer ID Application identity (never ad hoc), a secure timestamp, the hardened runtime, a version that is exactly a release and equals `cmd/werkbord/VERSION`, and credentials to notarize. It then checks all of that on what it made. |
| signing, inside-out | the build | The program in `Contents/Helpers` first, then the app that holds it, then the disk image; each piece by name (never `--deep`, which signs whatever is there the same way and hides what should not be). |
| `scripts/check-desktop-signature.sh` | the build, the tests, the release check | One definition of "signed for distribution": **every** piece of code in the bundle is signed by the same Developer ID team with the hardened runtime and a timestamp, and the app holds only the entitlements in `desktop/build/darwin/entitlements.plist` (none). Code added later (an updater and its helpers) is caught here. |
| `scripts/notarize-desktop.sh` | the build | Sends the app, then the disk image, to Apple's notary service with an App Store Connect API key (not an Apple ID password), waits for **Accepted** (Apple's own log is printed and the build fails on anything else), staples the ticket, and asks Gatekeeper. The app is done first so that an app dragged out of the disk image carries its own ticket and opens offline. |
| `scripts/verify-desktop-release.sh <tag>` | CI after the upload, and by hand | Downloads the image as a person would, checks its `.sha256`, mounts it, and checks signature, ticket, Gatekeeper and versions of what is inside. |
| `scripts/ci-keychain.sh` | CI | Imports the certificate into a temporary keychain with a random password, never the login keychain and never to disk outside the runner's temporary directory, and deletes it when the job ends. |

Entitlements: none are requested. Under the hardened runtime the app and the program it carries run with no exception:
the launcher's end-to-end scenarios, the embedded Tailscale node and a controller started from the signed program all
pass on hardened-runtime binaries (`scripts/test-desktop-sign.sh`). If a future change needs one, it goes in the entitlements file
with the reason next to it, and `check-desktop-signature.sh` will fail until the two agree.

The controller program inside the app is copied to `~/.local/bin` at first run and carries its own signature; nothing runs
from inside the bundle except the window.

**What the tests cannot do.** macOS will not let `codesign` use a self-signed certificate unless it is added to the trust
settings, which a test must not change, and nothing here has a real Developer ID. So the tests sign for real (ad hoc, with
the hardened runtime and the same arguments) through a recorder that reports what a certificate would have added, and
Apple's notary service and Gatekeeper are fakes that behave as Apple does in each situation. What no test can show is Apple
*accepting* a particular build: that is `verify-desktop-release.sh` on the first real release.

## Limits

- macOS only for now. The shell and launcher are written to not assume it, but the login-service discovery
  (`daemon.Describe`) reads launchd's definition only, and the bundle script is macOS-specific. Windows needs a WebView2
  build, an installer (the `.ps1` installer's service code has not been run on Windows either), and `Describe` for the
  scheduled task.
- Intel builds are made by the same script (`ARCH=amd64`) and verified to build and sign; they have not been run.
- The first connection does not turn on phone access (Tailscale sign-in needs your attention in a browser): turn it on
  in Settings.
- Closing the window hides the app (Wails's `HideWindowOnClose`) and relies on macOS un-hiding a hidden app when its Dock
  icon is clicked. That was checked as far as the hidden state (the window leaves the screen, the controller is
  untouched) but not with a real click on the Dock icon.

## Troubleshooting

| Look at | Where |
| --- | --- |
| What the app did | `~/Library/Logs/Werkbord/desktop.log` (`WERKBORD_DESKTOP_LOG_LEVEL=debug` for more) |
| What the controller did | **Help → Show Controller Log**, or `werkbord logs` |
| Everything at once, safe to paste into a bug report | **Help → Show Diagnostics…** (the token is never in it) |
| Is it the same installation as my terminal's? | `werkbord status` prints the data directory and address; Diagnostics prints the same |
