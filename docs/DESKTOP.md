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

1. Download [`Werkbord.dmg`](https://github.com/micho8cho93/werkbord/releases/latest/download/Werkbord.dmg) (one file for Apple
   Silicon and Intel; it is the same file as `Werkbord_<version>_darwin_universal.dmg` on the [releases page](https://github.com/micho8cho93/werkbord/releases)).
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

The menu bar has **Werkbord → Check for Updates…** (under *About Werkbord*), **View → Open in Browser** (the same controller,
signed in, in a tab), and **Help → Show Diagnostics…** and **Show Controller Log**. Keep the app in **Applications**: it updates
itself there ([Updates](#updates)).

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

There are two things to update and **one question** to ask about them. The release is the unit: a person is asked once to
move to Werkbord 1.3, and when it is done both are on 1.3.

| | What it is | Replaced by | Guarantees |
| --- | --- | --- | --- |
| **The app** | the window, the menu, the loading screen, and a copy of the program (`Contents/Helpers/werkbord`) | [Sparkle](https://sparkle-project.org) 2, embedded, which downloads an archive, **verifies its EdDSA signature**, and swaps the bundle | the archive must verify with the public key in the app (`SUPublicEDKey`), from a feed that is itself signed (`SURequireSignedFeed`), over https; a version that is not newer is never installed; the new app's own Apple code signature is checked too |
| **The program** | `~/.local/bin/werkbord`, which is the controller and the command line | only the program's own installer: `werkbord update` (from the network) or `install-release` (from the copy in the app) | checksum (a download), refusal while agents are working or a runner has unfinished work, a database snapshot, and the old program and database put back if the new one does not start |

**Knowing.** The controller reports whether a newer stable release exists at `GET /api/update` (behind the token like
the rest of the API). It asks GitHub's `/releases/latest` (no API, no credentials; "stable" because that never points at
a pre-release, and Werkbord Team's releases are published so they can never take it), keeps the answer for six hours (a
failure for fifteen minutes), and never asks about a build from source. It is the **one request Werkbord makes on its own**
(Git remotes, GitHub through your own `gh` and the private network are all things you turned on or did), so it can be turned
off: `"noUpdateCheck": true` in `config.json` (or `WERKBORD_NO_UPDATE_CHECK=1`). The web app shows:

> **Werkbord 1.3.0 is available** You have 1.2.0. Your data is backed up first. **[Update now]** **[Later]**

in the app. In a browser or on a phone the same banner says how to update instead (`werkbord update` on the computer that
runs Werkbord) and has no button, because **applying an update is never something a request to the controller can
do**: no client, however authenticated, can make your computer replace its own program. **Later** hides that release
until a newer one appears. **Settings → This computer** shows the state and **Check now**.

**Doing.** **Update now** and **Werkbord → Check for Updates…** (the one menu item, under *About Werkbord*, where a Mac has
it; Help has none) do the same thing, which depends on what is behind:

1. **The app already carries a newer program than the one installed** (the app was updated, and the controller could not move at the
   time because agents were working). A native question, then `install-release` from the app's own copy: no download, no network.
2. **The app is older than the release.** Sparkle looks at the feed without showing anything; if it has the update, **its own window**
   opens (release notes, *Install Update*, *Remind Me Later*). That window is the one question. It downloads, verifies the
   signature, quits the app, replaces the bundle and relaunches. Nothing of the controller is touched by the swap: it is not the
   app's child, and the program in `~/.local/bin` is not inside the app. Then:
3. **When the new app opens and finds a controller running an older program, it moves it to the program it carries**, by the
   same `install-release` the installer uses. It says "Updating Werkbord 1.2.0 → 1.3.0…" on the loading screen. If agents are
   working, or a runner has unfinished work, the installer refuses and **the controller is left exactly as it is**: the same
   process, on its old program; the window opens, tells you why once, and the move happens the next time you open Werkbord with
   nothing running, or when you choose **Update now** (step 1). It never goes backwards: an app older than the installed program
   leaves it alone.
4. **Only the program is behind** (the feed has no update of the app yet, for instance while a release is still being published,
   or Sparkle is not in this build). The update the terminal has, unchanged: `werkbord update`, behind a native question.

So one release is one click: the banner, Sparkle's window, and the loading screen are three views of the same update, not
three prompts. A newer app with an older controller keeps working because the two share only a short list of calls (below),
which a test pins.

**Privacy.** Sparkle has **no schedule** here (`SUEnableAutomaticChecks` is false and the updater is told so again at start), no
automatic download or install, and sends no profile of your computer (`SUSendProfileInfo` false). It asks the feed only when you
ask. And it honours `noUpdateCheck` / `WERKBORD_NO_UPDATE_CHECK`: with that on, "Check for Updates…" says looking for updates is
turned off and **nothing is requested**. In one sentence: *Werkbord asks GitHub for the newest release every six hours unless you
turn that off (`noUpdateCheck`), and fetches a small signed feed when you press Check for Updates; nothing else leaves your computer
to do with updates.* A build from source, a development run (`make desktop-dev`), and a build with no update-signing key do not
carry Sparkle and never ask. The feed's address cannot be changed in a release (it is checked in the finished app); the build
that can, for tests, is refused by `--release`.

**The feed.** `https://github.com/micho8cho93/werkbord/releases/latest/download/appcast.xml`. GitHub resolves
`/releases/latest/download/<file>` through the release marked **latest**, and only an individual stable release is marked latest
([VERSIONING.md](VERSIONING.md)): a Team release never is, so it can never become the feed. Each release uploads its own
`appcast.xml` (one item: that release; nothing older that could be offered again). The release workflow publishes it **last**,
after the disk image has been checked on a fresh Mac, so a bad image never reaches installed apps; and removes it if the published
feed does not check out. Between the CLI release and the feed (some minutes) a click on *Check for Updates…* finds no feed and
falls to step 4: the program still updates.

**The trust chain, and what each link stops.** An update archive is signed with the update key (Ed25519, `generate_appcast`), and
so is the feed. The private key is a GitHub secret that only the signing job holds; the public key is in the repository and in every
app. A feed or archive altered on the way, or published by anyone without the key, fails verification **before the archive is
opened** (`SUVerifyUpdateBeforeExtraction`). A checksum alone would not be enough: whoever can replace the file can replace its
checksum. Sparkle also checks the new app's own Apple code signature (and refuses an update that changes both the update key and the
Apple team at once), and the app is notarized, so a copy stripped of its signature does not open. Tested with the real Sparkle: a tampered archive, a signature by
another key, a feed signed by another key, an unsigned feed, a lower version, and an old archive offered under a newer version
number are each refused, and the app and the controller stay as they were (`scripts/test-desktop-update.sh`).

**What the shell and the controller share.** `GET /api/health` (public: is it a controller, which version), `GET /api/control-center` (authenticated read-only update safety), `GET /api/projects`
(needs the token: does it accept this computer's?), the files `token` and `config.json` in the data directory, and the commands
`version`, `start`, `setup`, `update` and `install-release` of the installed program. Nothing else: the launcher's source is
checked against that list by a test (`TestTheLauncherAsksTheControllerForOnlyTheseThings`), and the shell may not touch the
network or the controller except through the launcher. What a page may ask of the shell is the other short list
([above](#what-a-page-may-ask-of-the-app)); `Info` gained one optional field (`updater`) that older pages ignore. A newer shell
with an older controller, and the reverse, therefore keep working: both directions are exercised by the update test's
deferred scenario.

**Limits.** Sparkle replaces an app that is in a folder you can write to, so keep Werkbord in **Applications**, not on the disk
image or in Downloads (macOS runs an app from a downloaded disk image from a read-only copy, which cannot be replaced; Sparkle
says so). The update window is Sparkle's and was not clicked by any test: the tests cover everything it triggers, with an unattended
build; what no test can show is Apple *accepting* a particular signed build, which is the first real release's check
([DESKTOP_RELEASE.md](DESKTOP_RELEASE.md)).

## Building it

macOS only, with the Xcode command line tools (`xcode-select --install`), Go and Node (as for `make build`):

```bash
make desktop            # dist/desktop/Werkbord.app
make desktop-package    # that, and dist/desktop/Werkbord_<version>_darwin_<arch>.dmg (+ .sha256)
make desktop-dev        # run the window from source, against a controller built from this tree
make desktop-dev-stop   # stop that controller
make desktop-test       # the app's tests (they need no window system, so they run anywhere; `make check` includes them)
make desktop-check      # desktop-test, and on a Mac that the window code builds
```

`ARCH=amd64 make desktop` builds for Intel (`arm64` is the default on Apple Silicon). `VERSION=v1.2.3` stamps a release
version; by default a build reports what `scripts/product.sh werkbord build-version` says, which is exactly `v1.2.3`
only on a clean checkout of that tag.

`make desktop-dev` is for working on the app: it runs the window with `go run`, uses `bin/werkbord` as its program,
and keeps **its own data (`.desktop-dev/`) and port (127.0.0.1:7499)**, installing nothing and making no login service.
`DESKTOP_DEV_DATA=…` and `DESKTOP_DEV_ADDR=…` change them. Set `WERKBORD_DESKTOP_LOG_LEVEL=debug` to see every page call
in `~/Library/Logs/Werkbord/desktop.log`.

- It sets that controller up itself, as a background process, before the window opens (unless one already answers). Left to
  itself the window does that only when the computer has no login service; on a computer that has one (the installation you
  use) it would start that service instead and wait for a controller that never comes.
- Closing the window leaves the controller running, as for the installed app. `make desktop-dev-stop` stops it, by its own
  process. **Never use `bin/werkbord stop`, `start` or `restart` for it:** they act on the installed login service whatever
  data directory or port is set, so they would stop the installation you use.
- The window allows one instance: quit it (⌘Q) before opening it again with other settings.
- By default the window looks for Team's service where the installed one is (127.0.0.1:7431, with the credential in
  `~/Library/Application Support/werkbord-team-desktop/`). To try Team without that service, point the window at another
  one, for example a `werkbord-team daemon` of your own on other ports: `WERKBORD_DESKTOP_TEAM_BASE` (its loopback address),
  `WERKBORD_DESKTOP_TEAM_KEY_FILE` (the file holding its credential) and `WERKBORD_DESKTOP_STATE` (the window's own state
  file, so that your real one is not changed).

`make desktop-release` is the signed, notarized, universal disk image a release publishes (it needs the Apple credentials in the
environment and refuses to build anything else: [Signing and notarization](#signing-and-notarization)). A build with
`desktop/build/darwin/sparkle-public-key` (or `SPARKLE_PUBLIC_KEY`) also carries Sparkle and can update itself; without a key it
does not, and a development run never does. `WERKBORD_DESKTOP_NO_SERVICE=1` makes the app set Werkbord up without a login service,
as `werkbord setup --no-service` does, for trying it on a computer where it must not make one.

What is in the bundle (`scripts/build-desktop.sh`):

```
Werkbord.app/Contents/MacOS/Werkbord        the window (desktop/, Wails v2)
Werkbord.app/Contents/Helpers/werkbord      the controller and command line: the program the release archives carry
Werkbord.app/Contents/Resources/icon.icns   from desktop/build/appicon.png
Werkbord.app/Contents/Frameworks/Sparkle.framework   the updater (only in a build that has the update key); loaded at run time
```

A release's window and program are **universal** (Apple Silicon and Intel, joined with `lipo`), so there is one disk image and nobody has
to know their chip. The program is about 71 MB universal (36 MB a chip), the disk image 39 MB. The app **does not thin** the program when
it copies it to `~/.local/bin`: that copy and the one `install-release` makes are the paths that must never fail, a fat executable runs
exactly like a thin one (the system maps only the slice it needs), and the first `werkbord update` from a terminal replaces it with the
thin release archive anyway. If the 35 MB ever matters, thin it with Go's `debug/macho` where the program is copied (the slice keeps its
own signature) rather than with a tool the person may not have.

It does **not** contain Codex or Claude Code: Werkbord finds the ones you have installed, as it always did. The app is
not sandboxed (it must run your agents and Git in your repositories and write the login service), and asks for no
entitlements ([entitlements.plist](../desktop/build/darwin/entitlements.plist) says why).

### Where the code is

```
desktop/                 the app: its own Go module (devboard/desktop), so cgo and WebKit are never needed by `make check` or Linux CI
  main.go ui.go menu.go    the window, the menu bar, the Wails glue (macOS only)
  updater_darwin.{go,m,h}  Sparkle, loaded at run time, and the "Check for Updates…" menu item (cgo; never part of the controller's build)
  updater_testenv_darwin.go, updater_test_darwin.go   the test build only (-tags updatertest): see Testing
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
| The signing policy, the temporary keychain, what a release refuses, and that the hardened program, controller, Tailscale node and app (both chips) run | `scripts/test-desktop-sign.sh` (`make test-desktop-sign`) |
| The app updating itself with the real Sparkle: a good update; a tampered archive, a wrong key, a wrong feed key, an unsigned feed, a lower version and an old archive under a newer number refused; nothing asked when updates are off; the controller kept running while agents work; one menu item | `scripts/test-desktop-update.sh` (`make test-desktop-update`; builds the app twice) |
| The shell's update flow (finish the program, Sparkle, the program's own update), the relaunch guard | `desktop/internal/shell/shell_test.go` |
| An app newer than the running controller brings it up to date only when nothing is working; a refusal leaves it untouched and says why once; `noUpdateCheck` stops every request; the launcher's calls are pinned | `internal/launcher/launcher_test.go`, `e2e_test.go` |
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
- The Intel half of the app was run only under Rosetta on an Apple Silicon Mac (the signing test runs the signed app both ways); it has not been
  run on an Intel Mac.
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

**Individual is blank in the window, though the rest of it shows.** Werkbord before 1.11.3 registered its service worker
(the page's offline support) in the window too, and WebKit never completes a framed load that a worker answers: the page
loaded once and was blank from the next launch on, for every address it had been opened at. From 1.11.3 the page does not
register one when the window frames it, the worker leaves framed loads to the network, and the page removes one that an
earlier version registered. A window that still has an old worker is blank for one more launch and then works. To clear
it at once, quit the app and delete the `ServiceWorkers` folders under
`~/Library/WebKit/<bundle id>/WebsiteData/Default/*/*/` (the bundle id is `dev.werkbord.desktop`); nothing else there needs
to go, and the worker is not registered again.

## Unified desktop workspaces

Phase 3 adds one everyday desktop shell with Personal and multiple Team
workspaces while retaining isolated backend services, user-owned execution and
explicit Team installation. The existing Team app remains a compatible installer
and console; it is no longer required as a second everyday window. See
[UNIFIED_DESKTOP.md](UNIFIED_DESKTOP.md) for navigation, host volunteering,
security boundaries, lifecycle, validation and operational limits.

## Phase 4 packaging

The primary bundle includes `Contents/Helpers/Werkbord Team.app`, preserved with its independent Apple and offline release signatures, plus `Resources/components.txt` and `compatibility.json`. Team is inert for free Personal use. The nested upstream Nebula signature is checked against its immutable pin by Team’s verifier; it is never re-signed to make publisher checks pass. Backend upgrades retain their original trust paths. See [UNIFIED_DISTRIBUTION.md](UNIFIED_DISTRIBUTION.md). Sparkle now rechecks active work and Team maintenance before relaunching; a pending agent or enrolled Team workspace defers the swap.
