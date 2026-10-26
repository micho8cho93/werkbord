# UI and Team simplification

Continuation of the October 26 simplification plan. Phases 0–3 and 4C are committed through `5651946`.
Personal phones use the relay; Tailscale remains an optional connection for remote runners.

## 4A: shared presentation

Use the existing `internal/uipresentation` library rather than introduce a second Svelte build for Team.
Personal and Team already share board geometry, motion and planning gestures. Extend that library with
the common design tokens, segmented controls, project tabs and timeline calculations. Each product
keeps its own data loading, status vocabulary, permissions, routes and action handlers. Remove the
superseded copies; keep the console independently embedded in `werkbord-team`, with no Node requirement
for a Team source build. No Team code or credentials enter Personal or the desktop shell.

## 4B: one host first

Creating a team uses this computer as its first host. Present the next steps as inviting people,
approving their devices and connecting their own runners. Show automatic backup status and explain
that the host must remain online. Keep backup controls in ordinary Settings. Put extra hosts,
connectivity and quorum details in an Advanced group, retaining direct links and visible recovery
actions when something needs attention. Keep rqlite, its single-node default, enrollment approval and
the native runner trust confirmation. Verify the device service's existing daily backup defaults.

## 5: validation

Run `make check`, the Team and unified desktop browser suites, the workflow, planning, phone and plan
browser suites, installer and desktop update/signing tests, and the relevant entitlement, network,
backup and recovery tests. Check upgrade compatibility with the stable and latest preview artifacts
in isolated directories; record exactly what was exercised and any external or physical-device
validation that cannot be completed here. Record size changes against the original Phase 0 baseline
where available, and against the start of 4A (`5651946`, 1,834 tracked files).

Commit each phase after its checks pass. Ordinary implementation does not bump the version or create
a release tag. Release preparation requires a separate user request under `AGENTS.md`.
