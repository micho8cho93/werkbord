# Agent instructions

This is the canonical instruction file for every coding agent working in this repository (Codex, Claude Code, and
others). `CLAUDE.md` only points here, so there is one policy and it cannot drift. Keep new rules in this file.

## Versioning and Git Tag Policy

Werkbord is **one product with one version and one release**. The Mac app, the `werkbord` controller and command line,
and the Werkbord Team service (`werkbord-team`, the Workspace Host and its command line) are built, versioned, tagged and
released together, at all times unified. There is no separate Team version, tag, release or installer.

Every implementation commit MUST have a corresponding version update and Git tag.

Use semantic versions and one tag series:

`werkbord-vMAJOR.MINOR.PATCH` (a preview adds `-preview.N`)

Example: `werkbord-v4.0.0-preview.1`

The unified series starts at `4.0.0-preview.1`, above Individual's last `1.11.4-preview.1` and Team's last `3.10.2`, so no
update path ever sees a downgrade. The `werkbord-team-v*` tags are history: never create another one, and never create a
generic repository-wide tag such as `v2.1.0`.

### Rules

For EVERY implementation commit:

1. Increment the version in `cmd/werkbord/VERSION` (the one version file) before completing the commit.
2. Ensure the application's version metadata reflects it: both executables and the Mac app are stamped from that file.
3. Commit the implementation and version change together.
4. Create the tag with `make tag`, pointing at that commit.
5. Verify it with `make verify-tag`.

Use semantic versioning appropriately:

- **PATCH**: bug fixes, small improvements, refactors without meaningful feature additions.
- **MINOR**: new backward-compatible features or meaningful functionality.
- **MAJOR**: breaking changes or major product generations.

A change to any part of Werkbord, Team included, bumps the one version. Do not decide per component.

### Agent workflow

Before finishing any coding task:

- inspect the current version in `cmd/werkbord/VERSION`
- inspect existing Git tags (`git tag --list 'werkbord-v*'`)
- determine the correct next semantic version
- update `cmd/werkbord/VERSION`
- commit
- run `make tag`, then `make verify-tag`

An implementation task is NOT complete until this process has been performed.

If multiple implementation commits are created during a task, apply this process to EVERY implementation commit rather
than only the final commit.

Do not create tags for documentation-only commits unless the documentation represents a product release.

Tags are created locally. Do not push commits or tags unless the user asks.

### GitHub release presentation

GitHub release display numbers are separate from the build versions and Git tags.
Follow `docs/RELEASE_SEQUENCE.md` when preparing or publishing a release: reserve its
display sequence, apply the presentation title and add the original build
version note. Preserve the actual tag, VERSION metadata, assets, preview status and
latest selection. Do not reset build versions to the display sequence.

### Where versions live and how to apply the policy here

- Everything: `cmd/werkbord/VERSION`. Tag `werkbord-vX.Y.Z`. `cmd/werkbord-team/VERSION` no longer exists and
  `internal/archtest` fails if it comes back. The executable is `werkbord`; `devboard`, its name before the rename, is
  installed beside it and must keep working, as must every `DEVBOARD_*` variable.
- Existing tags: `git tag --list 'werkbord-v*'`. The `werkbord-team-v*` tags and the bare `v0.7.0` and earlier are history.
- After committing: `make tag` (annotated tag from the VERSION file, then verified) and `make verify-tag` to check again.
  Details: `docs/VERSIONING.md`.
- The Mac app (`desktop/`) and the Team service are part of the one release and have no version of their own.

## Product boundaries

Individual and Team are one release, but the *code* still has boundaries that are about trust, not packaging. Read
`docs/STRUCTURE.md` before changing code near them. In short:

- Team-specific code lives only under `internal/team/` and `cmd/werkbord-team/`. Never add a Team flag, branch or screen
  to the individual product, and never import Team from outside those two places.
- Team imports only the shared packages in the allow-list in `internal/archtest/boundary_test.go`. Something both
  products need becomes a shared package with no product behaviour; it is not copied and not reached across.
- A Team workspace coordinates; it never executes. Team must not start processes, hold Git/GitHub/agent credentials, or
  reach into a member's machine. `internal/archtest` enforces part of this; do not weaken those tests to make a change fit.
- The controller (`cmd/werkbord`) links no Team code, and the desktop app (a module of its own) links no Team code and no
  execution engine. The Team service is a separate executable and a separate process on purpose: some of it runs as root.
- `make check` runs everything, the boundary tests included.
