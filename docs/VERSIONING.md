# Versioning and tags

Werkbord is **one product with one version and one release** (see [STRUCTURE.md](STRUCTURE.md) and
[UNIFICATION.md](UNIFICATION.md)). The Mac app, the `werkbord` controller and command line, and the Werkbord Team
service (`werkbord-team`) are built, versioned, tagged and released together. The same policy is in
[AGENTS.md](../AGENTS.md) so that every coding agent follows it.

| | |
| --- | --- |
| Version file | `cmd/werkbord/VERSION` (the only one) |
| Tag | `werkbord-vMAJOR.MINOR.PATCH`, with `-preview.N` for a preview |
| Example | `werkbord-v4.0.0-preview.1` |

There are **no generic repository-wide tags** (`v2.1.0`) and no new `werkbord-team-v…` tags. The unified series starts at
`4.0.0-preview.1`, above Individual's last release (`1.11.4-preview.1`) and Team's last (`3.10.2`), so an installed copy of
either is always updated upwards.

## The rule

**Every implementation commit has a version bump and a tag.** For each one:

1. Raise the version in `cmd/werkbord/VERSION`, before the commit is complete.
2. Make sure the version metadata agrees (`VERSION` is the source; both executables and the Mac app are stamped from it).
3. Commit the change **and** the version bump together.
4. Tag that commit.
5. Verify the tag.

```bash
# 1–3: edit cmd/werkbord/VERSION in the same commit as the change
git commit -am "…"

# 4–5: annotated tag on HEAD, named from the VERSION file; make tag then verifies it
make tag                  # werkbord-v<cmd/werkbord/VERSION>
make verify-tag           # run again at any time
```

A change to Team bumps the same version as a change to the controller or the app: there is nothing to decide per
component. If several implementation commits are made in a task, every one is bumped and tagged, not only the last.
Documentation-only commits get no tag (unless the documentation is the release). Tags are created locally; pushing them
(`git push origin werkbord-v4.0.0-preview.1`) is a separate, deliberate step.

### Which number

- **PATCH** — bug fixes, small improvements, refactors without meaningful new functionality.
- **MINOR** — new backward-compatible features or meaningful functionality.
- **MAJOR** — breaking changes (a data or API change that is not backward compatible) or a new product generation.
  While the version is `0.x`, a breaking change raises MINOR.

### What `make verify-tag` / `scripts/verify-tag.sh` checks

The tag exists and is annotated; it points at the commit (HEAD by default; with no commit given, HEAD may be later than the tag
if every commit since changed only `docs/` or a top-level `*.md`, which get no tag, whereas CI passes the commit and needs an exact match); `cmd/werkbord/VERSION` **at that commit** says
the tag's version; the version is higher than the other release tags; and the tag is named `werkbord-vX.Y.Z`. CI repeats
this when a tag is pushed.

## Where the version shows up

`VERSION` is the single source. `scripts/product.sh <executable> build-version` turns it into what a build reports
(`werkbord version`, `werkbord-team version`, `/api/health`), and gives the same answer for both executables:

- a clean checkout of the commit its tag points at reports exactly `v0.8.0`;
- anything else (a later commit, a dirty tree, a tag not made yet) reports `v0.8.0-<commits since the tag>-g<sha>[-dirty]`,
  which the updater treats as *built from source*, never as an installed release;
- release builds (`make dist`, CI) are stamped with the bare tag version.

`make build` and `make build-team` stamp it. A plain `go build` reports `dev`.

The Mac app (`desktop/`) has no version of its own: `make desktop` stamps the window and the `werkbord` program inside it
with the same version (the bundle's `CFBundleShortVersionString` is its numbers), so a change to `desktop/`,
`internal/launcher` or any `scripts/*desktop*` script bumps `cmd/werkbord/VERSION`. Its disk image is
`Werkbord_<version>_darwin_universal.dmg` (one image for Apple Silicon and Intel), and the release workflow attaches it to
the release (see [Releases](#releases) and [DESKTOP_RELEASE.md](DESKTOP_RELEASE.md)).
`internal/archtest` fails the tests if `VERSION` is malformed, or if a second `VERSION` file for Team comes back.

Werkbord Team's service and the installer in the app are stamped with this same version; see
[UNIFICATION.md](UNIFICATION.md) for what remains of the old separation.

## Releases

Pushing a release tag makes CI (`.github/workflows/release.yml`) check the tag against `VERSION`, run `make check`,
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
- **Team's command-line archives** → `werkbord-team_<version>_<os>_<arch>.tar.gz`, `checksums-team.txt` and
  `checksums-team.txt.sig`, attached to the same release. CI does not make these: they are built and signed on the offline
  release workstation (`make dist PRODUCT=werkbord-team` with the release signing key; see [TEAM_INSTALL.md](TEAM_INSTALL.md)),
  because the key must never be in CI. The signature names the release tag, so it cannot be replayed on another release.
  The Mac app needs none of this: its Team installer is built, signed and notarized in the same job as the app.

This matters because `…/releases/latest` is how the installer and `werkbord update` find the newest release: only stable
releases are marked "latest". `scripts/install-team.sh` finds the latest stable release in the releases feed.

A tag with a `-` after the version (`werkbord-v4.1.0-rc1`) is a pre-release. Its VERSION file includes the same prerelease suffix. Prereleases are never marked latest. An explicitly requested ad-hoc Mac preview may be attached as `Werkbord-preview.dmg` to a prerelease; it is never the stable `Werkbord.dmg` or an appcast update (see [DESKTOP_RELEASE.md](DESKTOP_RELEASE.md#test-the-app-while-approval-is-pending)).

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
