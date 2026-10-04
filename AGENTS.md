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
