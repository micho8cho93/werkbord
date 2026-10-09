# Versioning and tags

The repository contains two **separately versioned products** (see [PRODUCTS.md](PRODUCTS.md)). Each has its own
semantic version, its own tag series, and its own release. The same policy is in [AGENTS.md](../AGENTS.md) so that every
coding agent follows it.

| Product | Version file | Tag | Example |
| --- | --- | --- | --- |
| Individual Werkbord | `cmd/werkbord/VERSION` | `werkbord-vMAJOR.MINOR.PATCH` | `werkbord-v1.4.2` |
| Werkbord Team | `cmd/werkbord-team/VERSION` | `werkbord-team-vMAJOR.MINOR.PATCH` | `werkbord-team-v2.1.3` |

There are **no generic repository-wide tags** (`v2.1.0`). The products must be able to evolve independently: a Team fix
does not bump the individual product, and the reverse. (`werkbord-v…` never matches `werkbord-team-v…`: after
`werkbord-` comes `v` or `team-v`, so a tag pattern for one never selects the other.)

## The rule

**Every implementation commit has a version bump and a tag.** For each one:

1. Work out which product(s) it changes.
2. Raise that product's version in its `VERSION` file, before the commit is complete.
3. Make sure the version metadata agrees (`VERSION` is the source; builds are stamped from it, see below).
4. Commit the change **and** the version bump together.
5. Tag that commit with the product's tag.
6. Verify the tag.

```bash
# 1–4: edit cmd/<product>/VERSION in the same commit as the change
git commit -am "…"

# 5–6: annotated tag on HEAD, named from the VERSION file; make tag then verifies it
make tag PRODUCT=werkbord            # werkbord-v<cmd/werkbord/VERSION>
make tag PRODUCT=werkbord-team       # werkbord-team-v<cmd/werkbord-team/VERSION>
make verify-tag PRODUCT=werkbord     # run again at any time
```

If one commit changes both products, decide for each independently and tag both (two tags on one commit is fine). If
several implementation commits are made in a task, every one is bumped and tagged, not only the last. Documentation-only
commits get no tag (unless the documentation is the release). Tags are created locally; pushing them
(`git push origin werkbord-v1.4.2`) is a separate, deliberate step.

### Which number

- **PATCH** — bug fixes, small improvements, refactors without meaningful new functionality.
- **MINOR** — new backward-compatible features or meaningful functionality.
- **MAJOR** — breaking changes (a data or API change that is not backward compatible) or a new product generation.
  While a product is `0.x`, a breaking change raises MINOR.

A change to a **shared package** (`internal/sqlitekit`, `internal/httpkit`, `internal/logging`) bumps a product only if
it changes that product's behaviour. A pure refactor that both products pick up is a PATCH for each product whose
behaviour it could touch; a change only one product's code path reaches bumps only that product.

### What `make verify-tag` / `scripts/verify-tag.sh` checks

The tag exists and is annotated; it points at the commit (HEAD by default); the product's `VERSION` file **at that
commit** says the tag's version; the version is higher than the product's other tags; and the tag follows the product's
naming. CI repeats this when a tag is pushed.

## Where the version shows up

`VERSION` is the single source for each product. `scripts/product.sh <product> build-version` turns it into what a build
reports (`werkbord version`, `werkbord-team version`, `/api/health`):

- a clean checkout of the commit its tag points at reports exactly `v0.8.0`;
- anything else (a later commit, a dirty tree, a tag not made yet) reports `v0.8.0-<commits since the tag>-g<sha>[-dirty]`,
  which the updater treats as *built from source*, never as an installed release;
- release builds (`make dist`, CI) are stamped with the bare tag version.

`make build` and `make build-team` stamp the right one. A plain `go build` reports `dev`.

The Mac app (`desktop/`) belongs to the individual product and has no version of its own: `make desktop` stamps the
window and the `werkbord` program inside it with the same version (the bundle's `CFBundleShortVersionString` is its
numbers), so a change to `desktop/`, `internal/launcher` or any `scripts/*desktop*` script is a change to Werkbord and bumps
`cmd/werkbord/VERSION`. Its disk image is `Werkbord_<version>_darwin_universal.dmg` (one image for Apple Silicon and Intel),
and the release workflow attaches it to the individual release (see [Releases](#releases) and
[DESKTOP_RELEASE.md](DESKTOP_RELEASE.md)).
`internal/archtest` fails the tests if a `VERSION` file is malformed.

The separate Team Mac app (`cmd/werkbord-team/desktop/`) uses the Team version. Its background service and window
share that version; the optional bundled individual runner keeps its own individual version. `make team-desktop-package`
creates `WerkbordTeam_<version>_darwin_universal.dmg`. See [TEAM_DESKTOP.md](TEAM_DESKTOP.md) for its independent build,
service lifecycle and release requirements.

## Releases

Pushing a product tag makes CI (`.github/workflows/release.yml`) check the tag against `VERSION`, run `make check`,
build the archives for every platform, and publish a GitHub release associated with the tag.
GitHub display titles use the separate presentation sequence recorded in
[RELEASE_SEQUENCE.md](RELEASE_SEQUENCE.md); build versions and tags keep the semantic
versions described above. Apply the reserved title and original build version note to
new releases without changing their assets or latest selection:

- `werkbord-vX.Y.Z` → `werkbord_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) and `checksums.txt`, marked **latest**.
  A stable one (no `-` after the version) also gets, from the macOS jobs of the same workflow, the Mac app:

  | Asset | What it is |
  | --- | --- |
  | `Werkbord_<version>_darwin_universal.dmg` and `….dmg.sha256` | the disk image people download: signed with a Developer ID, notarized by Apple, stapled; one image for Apple Silicon and Intel |
  | `Werkbord.dmg` | **the same file, byte for byte, with no version in its name**, so that `https://github.com/micho8cho93/werkbord/releases/latest/download/Werkbord.dmg` is always the newest app (the website's button, and a one-line `curl`) |
  | `Werkbord_<version>_darwin_universal.zip` and `….zip.sha256` | the archive the installed app updates itself from (the same app, zipped), signed with the update key |
  | `appcast.xml` | the feed the installed app reads: one item (this release), signed with the update key. Published **last**, after the disk image has been checked on a fresh Mac; `…/releases/latest/download/appcast.xml` is the address apps use, and only individual releases are marked latest |

  They are **part of the individual release but not of its CLI release job**: they are added to the release that job
  made, only if it succeeded, and a failure in them turns the workflow red without touching the archives or
  `checksums.txt`, which the installers and `werkbord update` read. None of them is in `checksums.txt` (that file is the CLI
  archives' and keeps its format); each has its own `.sha256` where it is a download.
- `werkbord-team-vX.Y.Z` → an offline, reviewed release build produces `werkbord-team_<version>_<os>_<arch>.tar.gz`,
  `checksums.txt` and `checksums.txt.sig`, marked **not latest**. Generic CI no longer publishes unsigned Team CLI
  archives. An offline signing custodian supplies the separate release key; see [TEAM_INSTALL.md](TEAM_INSTALL.md).
  The separate `release-team-desktop.yml` workflow adds `WerkbordTeam_<version>_darwin_universal.dmg` and its checksum
  to that Team release after signing and notarization. It preserves the release's latest status and never publishes
  an individual appcast update.

This matters because `…/releases/latest` is how the individual installer and `werkbord update` find the newest
release: only the individual product's releases may be "latest", or a Team release would be offered to every
individual install (the updater would refuse it, but the update would fail). The Team installer
(`scripts/install-team.sh`) finds the latest *Team* release in the releases feed instead. Both installers refuse the
other product's release by name.

A tag with a `-` after the version (`werkbord-v1.5.0-rc1`) is a pre-release. Its VERSION file includes the same prerelease suffix. Prereleases are never marked latest. An explicitly requested ad-hoc Mac preview may be attached as `Werkbord-preview.dmg` to a prerelease; it is never the stable `Werkbord.dmg` or an appcast update (see [DESKTOP_RELEASE.md](DESKTOP_RELEASE.md#test-the-app-while-approval-is-pending)).

## History

Before the products were separated, releases were tagged with a bare `v0.N.0` (one milestone, one minor version). Those
versions are the individual product's. Their product tags now exist beside them, pointing at the same commits:

| Version | Commit | Milestone |
| --- | --- | --- |
| 0.1.0 | `dabf24a` | The local-first controller and PWA foundation |
| 0.2.0 | `bab1f07` | Hardened Git inspection and worktree records; token required by default |
| 0.3.0 | `553e904` | The local coding-agent runtime |
| 0.4.0 | `516a780` | Project as the scope of the app; per-task execution policies |
| 0.5.0 | `e85f596` | The Git Control Center |
| 0.6.0 | `4297a29` | Repository health; the Control Center for exceptions |
| 0.7.0 | `b8da120` | One-command install and setup, background service, embedded private networking, GitHub connection, execution defaults |
| 0.8.0 | the "per-product versioning and Werkbord Team foundation" commit | The V1 multi-runner control plane and orchestration (`c386c8b`, committed after 0.7.0 without a version or tag), the audit (`4365d3e`), shared SQLite and HTTP packages, product-specific releases and tags, per-product installers |
| 0.8.1 | the "Wait for a dead Codex server" commit | A failed Codex start waits for the dead server's last words, so the error says why (a race that failed CI on Linux) |
| Team 0.1.0 | the same commit | The Team foundation: workspace, owner, members, projects, project membership, Owner and Member roles |
| Team 0.2.0 | the "collaborative workflow" commit | Project roles, invite links, the shared board and tickets with atomic claiming, local-runner handoff, reported Git metadata, repository awareness, activity, live board sync. Database schema 1 → 2 (migrates in place, backs up first). The individual product is unchanged (0.8.0). |
| Team 2.0.0 | the "Team V2 integration and hardening" commit | One coherent console (Workspace, Projects, Board, My Work, Reviews, Repository, Activity), workspace-wide sync with reconnect recovery, concurrency and security hardening, and the security review. Schema 2 → 3 (a workspace revision; migrates in place, backs up first). Major: the second generation of Team ("V2"): the console's navigation and the sync contract changed, and the schema moved. The HTTP API only gained routes, so existing clients keep working. The individual product is unchanged (0.8.1). |

`werkbord-v0.8.0` is a MINOR on 0.7.0 because it is the first tag since the multi-runner control plane and orchestration
landed untagged. The bare `v0.7.0` tag still exists and always will (the `werkbord update --version` command and the
installer find releases before 0.8.0 under their bare tag). **Do not create new bare `vX.Y.Z` tags.**

Recreating the product tags for the history in a clone that lacks them (the commit IDs are stable):

```bash
git tag -a werkbord-v0.1.0 dabf24a -m "werkbord-v0.1.0 — Build the local-first controller and PWA foundation"
git tag -a werkbord-v0.2.0 bab1f07 -m "werkbord-v0.2.0 — Harden Git inspection and worktree records; require API token by default"
git tag -a werkbord-v0.3.0 553e904 -m "werkbord-v0.3.0 — Add the local coding-agent runtime"
git tag -a werkbord-v0.4.0 516a780 -m "werkbord-v0.4.0 — Make Project the scope of the app and add per-task execution policies"
git tag -a werkbord-v0.5.0 e85f596 -m "werkbord-v0.5.0 — Add the Git Control Center"
git tag -a werkbord-v0.6.0 4297a29 -m "werkbord-v0.6.0 — Add repository health and make the Control Center about exceptions"
git tag -a werkbord-v0.7.0 b8da120 -m "werkbord-v0.7.0 — Add one-command install, private networking, GitHub connection and execution defaults"
```
