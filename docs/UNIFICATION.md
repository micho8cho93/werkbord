# Unification: one product, one version, one release

Werkbord used to be two products in one repository (Individual and Team), each with its own version, tag, release,
installer and app. Phases 3 and 4 put both behind one window and one installer but kept them separate underneath. That is
no longer the approach. From `werkbord-v4.0.0-preview.1`:

- there is **one version** (`cmd/werkbord/VERSION`), **one tag series** (`werkbord-vX.Y.Z`) and **one release** with one
  Mac app, at all times unified ([VERSIONING.md](VERSIONING.md));
- Werkbord Team is a part of Werkbord, not a second product. It keeps the pieces that are different in kind, not in
  packaging: the privileged service that creates the private network interface, the Workspace Host (Nebula, rqlite) and
  the offline license check;
- the **trust rules stay**: Team never executes anything, never holds Git or agent credentials, and never reaches into a
  member's machine; no private signing key is ever linked into Team; nothing in Team points at a Werkbord-operated
  service. Those are about trust, not packaging, and `internal/archtest` keeps enforcing them.

## Decisions

| Question | Decision |
| --- | --- |
| One version for everything? | Yes. `4.0.0-preview.1` first, above Individual `1.11.4-preview.1` and Team `3.10.2`, so no update path sees a downgrade. |
| Adoption of pre-unified installs | Not needed. Nobody else has one, so the migration layer is deleted (stage 3). |
| `werkbord-team serve` / `daemon` | Stays, as the headless Workspace Host on Linux and servers. The Mac service already runs `daemon`. What goes is its role as a second, separately installed product. |
| `devboard` command and `DEVBOARD_*` | Unchanged and unrelated to this work. They keep working indefinitely. |

## Stages

Each stage is its own commit (with its version bump and tag), and `make check` passes after each.

| # | Stage | Status |
| --- | --- | --- |
| 1 | **Policy and version scheme.** One `VERSION` file, `product.sh`, `verify-tag.sh`, `make tag`, the version archtest, AGENTS.md, VERSIONING.md. | done in `4.0.0-preview.1` |
| 2 | **Release pipeline.** One release workflow: `release-team-desktop.yml` and `prepare-unified-team.sh` are gone, `release.yml` has no Team tag, and the nested Team installer is built, signed and notarized in the same job as the app (`build-team-desktop.sh --nested`). The offline Team manifest chain (`vendor desktop-release` / `verify-desktop-release`, `TEAM_RELEASE_PUBLIC_KEY`, `TEAM_OFFLINE_MANIFEST`, `team-desktop-manifest.sh`) is replaced by a check that the Team service installs only from an app signed by the release's Apple Developer team (`platform/release.go`). The license issuer stays. Team's command-line archives are still signed offline, now against the one tag, as `checksums-team.txt(.sig)` attached to the same release; `install-team.sh` follows the `werkbord-v` series. | done in `4.1.0-preview.1` |
| 3 | **Legacy removal.** The adoption/migration layer is gone (`desktop/internal/migration`, the shell's `Migrate`/`MigrationStatus`, the "Your existing installation" panel, the launcher's `AllowBundledUpgrade` gate, `docs/MIGRATION.md`); the `werkbord-team connector` and `handoff` command lines and `install-team-connector.sh` are gone (the Team service's synchronization, `internal/team/connector`, replaces them, and the console's handoff stays a document to copy or download). The browser suite no longer needs the `handoff` binary: `browser-bridge.cjs` makes the same two API calls, and `test-browser.cjs` now makes its own licensed Team, which a license requirement had broken. The "Update Werkbord…" button for a controller older than 1.6 stays for now and goes with the rest of the version-skew UI in stage 5. | done in `4.2.0-preview.1` |
| 4 | **One app.** There is one Mac app and no nested one. The privileged-service installer is `desktop/internal/teaminstall` (the old `cmd/werkbord-team/desktop/internal/platform`): the app's own executable, run as `Werkbord --activate`, `--service ACTION` or `--verify-release`, and, for its one privileged step, by macOS as root with `--team-service` (all dispatched in `main` before any window or log exists). The standalone Team window (its Wails module, frontend, `Info.plist` and the second `werkbord://` registration) is gone, along with the "set up the free runner" action the shell never used. The Team service, rqlite, Nebula and licenses are built into `Contents/Helpers` by `build-desktop.sh` (`build-team-desktop.sh`, `check-team-desktop.sh` and the `team-desktop*` targets are gone; `check-team-payload.sh` replaces the payload checks). The architecture test for the installer is `team_installer_test.go`: no repository code, no process but fixed absolute paths. | done in `4.3.0-preview.1` |
| 5 | **Compatibility and isolation.** `compatibility.json`, `components.txt` and `check-unified-desktop.sh` are gone; the build checks that the controller and Team's service report the one version, and Help → Diagnostics shows it. Personal's version-number floor (`MinimumVersion`, the "Update Werkbord…" button and `UpdatePersonal`) is gone: nobody has a controller that old, and the launcher already upgrades the program. The app's update guard no longer defers on an enrolled Team workspace (the app never replaces the root service on its own, so an app update does not touch it); it still waits for working agents. **Kept on purpose:** Team's "outdated" state and **Update Team**, because an installed root service really can be older than the app, and what decides it is whether the service answers the listing, not a table of versions. `verify-isolation` (the copy-without-Team build) and its CI step are gone; the architecture tests are reframed as trust rules (`internal/archtest`): the controller links no Team code and holds no Team credential or API, Team links none of the code that executes, and the desktop app links neither. `werkbord-team` stays its own executable, so "Team's build cannot link `os/exec`" holds exactly as before, at package level. | done in `4.4.0-preview.1` |
| 6 | **Docs and naming.** Fold `UNIFIED_DESKTOP`, `UNIFIED_DISTRIBUTION` and `PHASE4_READINESS` into `ARCHITECTURE.md`; rewrite `PRODUCTS.md`; retire `TEAM_INSTALL.md`/`TEAM_DESKTOP.md` install parts; use "Individual" and not "Personal" everywhere. | |

Not on the list, because a rewrite is a design task of its own and not cleanup: replacing Team's vanilla-JS console
(`internal/team/console`) with Svelte screens in the shell. The shell still reuses the console's create/join screens.
