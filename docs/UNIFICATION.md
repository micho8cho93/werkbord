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
| 2 | **Release pipeline.** Delete `release-team-desktop.yml` and `prepare-unified-team.sh`; remove the Team tag triggers and Team-only branches from `release.yml`; remove the offline Team release manifest chain (`vendor desktop-release`/`verify-desktop-release`, `TEAM_RELEASE_PUBLIC_KEY`, the review-candidate flow, `platform.RequireRelease`) and the `werkbord-team-v` tag it binds. Keep the license *issuer* in `cmd/werkbord-team/vendor`. Publish the Team archives (for Linux hosts) from the one release. | next |
| 3 | **Legacy removal.** Delete `desktop/internal/migration`, `web/src/shell/Migration.svelte`, `desktop/internal/shell/migration.go`, `docs/MIGRATION.md` and the "Update Werkbord…" path for controllers older than 1.6. Delete the Team CLI wrappers `werkbord-team connector` and `handoff` (the in-service synchronization and **Open in Individual** replace them) and `scripts/install-team-connector.sh`. Keep the `internal/team/connector` package: `daemon_sync.go` uses it. | |
| 4 | **One app.** Fold the privileged-service installer (`cmd/werkbord-team/desktop/internal/platform`) into the main app, and remove the standalone Team window (`main.go`, `frontend/`), its `Info.plist` and the second `werkbord://` registration. Remove the nested `Werkbord Team.app`, `scripts/build-team-desktop.sh`, `check-team-desktop.sh`, `team-desktop-manifest.sh` and the `team-desktop*` Makefile targets. | |
| 5 | **Compatibility and isolation.** Remove `compatibility.json`, `components.txt`, the component-version skew UI (`teamlink` `outdated`, the "Update Team" refusal) and Sparkle's deferral on a Team slot. Replace `verify-isolation` and the "Individual must not mention Team" tests with the security-only boundary tests; decide how "Team's build can't link `os/exec`" holds once there is one release. | |
| 6 | **Docs and naming.** Fold `UNIFIED_DESKTOP`, `UNIFIED_DISTRIBUTION` and `PHASE4_READINESS` into `ARCHITECTURE.md`; rewrite `PRODUCTS.md`; retire `TEAM_INSTALL.md`/`TEAM_DESKTOP.md` install parts; use "Individual" and not "Personal" everywhere. | |

Not on the list, because a rewrite is a design task of its own and not cleanup: replacing Team's vanilla-JS console
(`internal/team/console`) with Svelte screens in the shell. The shell still reuses the console's create/join screens.
