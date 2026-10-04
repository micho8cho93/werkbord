# Agent instructions

This is the canonical instruction file for every coding agent working in this repository (Codex, Claude Code, and
others). `CLAUDE.md` only points here, so there is one policy and it cannot drift. Keep new rules in this file.

## Versioning and Git Tag Policy

Every implementation commit MUST have a corresponding version update and Git tag.

Werkbord contains separately versioned products.

### Individual Werkbord

Use semantic versions and tags:

`werkbord-vMAJOR.MINOR.PATCH`

Example: `werkbord-v1.4.2`

### Werkbord Team

Werkbord Team is a separately installable and separately versioned product even though it lives in the same
repository.

Use:

`werkbord-team-vMAJOR.MINOR.PATCH`

Example: `werkbord-team-v2.1.3`

### Rules

For EVERY implementation commit:

1. Determine which product(s) were changed.
2. Increment the appropriate semantic version before completing the commit.
3. Ensure the application's version metadata reflects that version.
4. Commit the implementation and version change together.
5. Create the corresponding Git tag pointing to that commit.
6. Verify the tag points to the correct commit.

Use semantic versioning appropriately:

- **PATCH**: bug fixes, small improvements, refactors without meaningful feature additions.
- **MINOR**: new backward-compatible features or meaningful functionality.
- **MAJOR**: breaking changes or major product generations.

If a commit changes both Werkbord and Werkbord Team, independently determine whether each product requires a version
increment and tag. Shared-package changes should only increment product versions when they affect that product.

### Important

Do NOT use generic repository-wide tags such as `v2.1.0`. Use product-specific tags: `werkbord-v1.5.0`,
`werkbord-team-v2.1.0`. The two products must be able to evolve independently.

### Agent workflow

Before finishing any coding task:

- inspect the current product version
- inspect existing Git tags
- determine the correct next semantic version
- update version metadata
- commit
- create the corresponding tag
- verify the tag

An implementation task is NOT complete until this process has been performed.

If multiple implementation commits are created during a task, apply this process to EVERY implementation commit rather
than only the final commit.

Do not create tags for documentation-only commits unless the documentation represents a product release.

Tags are created locally. Do not push commits or tags unless the user asks.

### Where versions live and how to apply the policy here

- Individual Werkbord: `cmd/devboard/VERSION` (the executable is still called `devboard`). Tag `werkbord-vX.Y.Z`.
- Werkbord Team: `cmd/werkbord-team/VERSION`. Tag `werkbord-team-vX.Y.Z`.
- Existing tags: `git tag --list 'werkbord-v*'` and `git tag --list 'werkbord-team-v*'`. The bare `v0.7.0` and earlier
  are history; never create another bare `vX.Y.Z`.
- After committing: `make tag PRODUCT=werkbord` and/or `make tag PRODUCT=werkbord-team` (annotated tag from the VERSION
  file, then verified), and `make verify-tag PRODUCT=<product>` to check again. Details: `docs/VERSIONING.md`.
- A commit is "individual" if it changes anything the `devboard` executable is built from (everything under `cmd/devboard`,
  `internal/` outside `internal/team`, `web/`, its scripts); "Team" if it changes `internal/team`, `cmd/werkbord-team` or
  Team's installer. Shared packages (`internal/sqlitekit`, `internal/httpkit`, `internal/logging`) count for a product only
  if they change that product's behaviour.

## Product boundaries

The two products live in one repository and must stay separable. Read `docs/PRODUCTS.md` before changing code near the
boundary. In short:

- Team-specific code lives only under `internal/team/` and `cmd/werkbord-team/`. Never add a Team flag, branch or screen
  to the individual product, and never import Team from outside those two places.
- Team imports only the shared packages in the allow-list in `internal/archtest/boundary_test.go`. Something both
  products need becomes a shared package with no product behaviour; it is not copied and not reached across.
- A Team workspace coordinates; it never executes. Team must not start processes, hold Git/GitHub/agent credentials, or
  reach into a member's machine. `internal/archtest` enforces part of this; do not weaken those tests to make a change fit.
- `make check` runs everything (both products and the boundary tests); `make verify-isolation` proves the individual
  product builds and passes without Team.
