# Codex prompts: UX excellence sequence

Run these in order in `/Users/Michel/Desktop/werkbord`. Paste the shared preamble plus **one numbered prompt**
per session. Finish and inspect each pass before starting the next. These prompts execute
[the 30/40 → 40/40 plan](ux-excellence-plan.md); completing them does not automatically earn 40/40.

## Shared preamble — paste with every prompt

```text
Work in /Users/Michel/Desktop/werkbord. Read AGENTS.md, PRODUCT.md, DESIGN.md,
docs/archive/ux-excellence-plan.md and docs/archive/ux-simplification-audit.md before editing.
Use the applicable Impeccable workflow, preserving Werkbord's quiet technical identity
and Operate-mode task clarity. This is refinement of the existing product.

Make no Go or Team source changes. The unified cmd/werkbord/VERSION metadata update
is the required exception. Use existing APIs and the Individual shell relay only.
Never start, stop, install, seed or reconfigure the real controller. Never push.
Full checks may build Go and run disposable Team fixtures without changing source.
Use only the isolated fixture procedure in docs/archive/ux-excellence-prompts.md.

Verify each reported defect on the current fixture before fixing it. Preserve weekly
navigation, deep links, shortcuts, inline answers, source provenance, stale/offline
guards, pending acknowledgment, Archive Undo, repository confirmations, assistant
behavior and reduced motion. Do not fabricate API evidence or enabling actions.
Record an out-of-scope API dependency and complete the independent authorized work.

Add meaningful behavior-based regression tests. Render changed surfaces at 1440x900
and 375x812 in light and dark; inspect keyboard focus, coarse-pointer behavior and
reduced motion where relevant. Rebuild/restart/reseed the fixture after UI changes:
its embedded UI does not update merely because source files changed.

Run relevant targeted tests, web checks/lint/tests, production web/embed/shell builds,
and the applicable browser suites before committing. Run make check for each release.
Do not weaken checks or omit failures from the report. Inspect the diff for prohibited
changes and preserve unrelated user work. Update DESIGN.md for changed interaction
rules and append evidence, artifact paths and limitations to the UX audit.

Every implementation commit must include its semantic version increment. Inspect the
current VERSION and werkbord-v* tags at execution time; use PATCH for fixes and MINOR
for meaningful new functionality. Commit implementation and version together, then
run make tag and make verify-tag. Do this for every implementation commit; never
reuse the version written in an earlier prompt. Documentation-only commits need no
product tag unless they represent a release.

Complete the selected pass autonomously within these boundaries. Stop only your own
fixture processes and preserve review artifacts. Report what changed, evidence,
remaining limitations, commit, version and verified local tag. Never claim a score
increase without a fresh assessment. Do not start the next numbered pass.
```

## Isolated fixture procedure

Use this procedure in each implementation pass. From the repository root, first check that port 17499 is unused
(`lsof -nP -iTCP:17499 -sTCP:LISTEN`). If occupied, choose another unused loopback port and substitute it throughout;
do not terminate its listener. Keep these variables and the child PID in the same shell session.

```sh
make web web-embed web-shell
UX_PASS_DIR=$(mktemp -d /tmp/werkbord-ux-pass.XXXXXX)
go build -o "$UX_PASS_DIR/fixture" ./scripts/browser-fixture
WERKBORD_BROWSER_ADDR=127.0.0.1:17499 \
WERKBORD_BROWSER_EXECUTION=1 WERKBORD_BROWSER_ASSISTANT=1 \
GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
"$UX_PASS_DIR/fixture" > "$UX_PASS_DIR/fixture.log" 2>&1 &
UX_PASS_PID=$!

for UX_READY_ATTEMPT in {1..30}; do
  kill -0 "$UX_PASS_PID" || break
  if curl -fs http://127.0.0.1:17499/api/health > "$UX_PASS_DIR/health.json"; then
    break
  fi
  sleep 1
done
```

Before continuing, confirm the child is alive and the health JSON says `version: browser-fixture`. Abort seeding
if startup failed or that identity is absent. The guarded seeder independently refuses ordinary controllers.

```sh
PERSONAL_BROWSER_URL=http://127.0.0.1:17499 SEED_DIR="$UX_PASS_DIR/seed" \
node scripts/seed-browser-fixture.cjs > "$UX_PASS_DIR/projects.json"

PLAYWRIGHT_MODULE="$PWD/web/node_modules/playwright" \
PERSONAL_BROWSER_URL=http://127.0.0.1:17499 \
node scripts/measure-legibility.cjs --check --json "$UX_PASS_DIR/legibility.json" \
  --screenshots "$UX_PASS_DIR/surfaces"
```

The seed supplies a committed Review task, a Doing run waiting on “Commit the fixture work?”, and Venue search,
a work project with three tasks. Use IDs from `projects.json`. Additional failure states should use controlled
frontend/browser response mocks, without editing Go fixtures. Explicitly target this fixture in browser tools;
do not use a dev-server proxy that defaults to the real controller.

At shutdown, signal only the PID recorded above, then wait for it. A wait status caused by SIGTERM is expected.
For a rebuild, stop that child, build web/embed/shell and the fixture again, start a fresh fixture and reseed.
Retain logs and screenshots; copy useful evidence to a named local `.impeccable/review/` directory.

```sh
kill -TERM "$UX_PASS_PID"
wait "$UX_PASS_PID"
```

## Prompt 1 — attention, counts and work vocabulary

```text
Execute workstream 1 of docs/archive/ux-excellence-plan.md.

Inspect current attention derivation and render mixed question/review state in the
Individual app and shell before editing. Replace remaining user-facing “needs input”
summaries with “Needs you”, preserving internal/API state names. Make the interruption
badge's scope understandable: it excludes Review while the All list includes Review.
Use visible contextual explanation and accurate accessible count names, without
silently changing count semantics.

In the shell, show an expanded question once in its answer card. Preserve workspace,
project and task provenance; keep the collapsed row informative. Clarify Calendar
execution scheduling versus Timeline work planning where dates are chosen. Work
projects must retain planning context without execution controls.

Test zero/multiple projects, mixed attention, collapsed/expanded questions, pending
acknowledgment, stale/offline refusal and work projects. Acceptance: one question per
expanded screen, all options/context retained, truthful counts and clear date meaning.
Complete the shared verification/documentation/version/tag workflow for this pass.
```

## Prompt 2 — keyboard, touch and accessibility

```text
Execute workstream 2 of docs/archive/ux-excellence-plan.md after checking Prompt 1's evidence.

Reproduce remaining interaction defects in LabelManager, Sheet and navigation.
Give compact coarse-pointer controls, including swatches, at least 44x44px hit areas
while preserving desktop density. Use native color radios or correct roving tabindex
and arrow navigation: one tab stop, human color names, supplementary hex values and
a selection indicator that does not depend on color.

Add a visible-on-focus skip link to the main work region. Correct navigation semantics
and focus order through Board, details, Settings and shell. Extend Sheet tests for
nested sheets, disabled fieldsets, collapsed content, dynamic actions, disconnected
openers, Escape and focus restoration. Verify shortcuts do not interrupt typing.

Run answer/review/create journeys with keyboard, coarse-pointer emulation, 200% zoom,
both themes and reduced motion. Perform actual VoiceOver/real-device checks if
available; record unavailable manual checks as unverified, not automated passes.
Acceptance: no lost or hidden focus, correct radio arrows, usable names/announcements
and no clipped essential actions at zoom. Complete the shared release workflow.
```

## Prompt 3 — make Review immediately actionable

```text
Execute workstream 3 of docs/archive/ux-excellence-plan.md.

Inspect TaskPanel, result/handoff components and MergeSheet on the seeded Review task.
At 375x812 lead with the task title and Review changes, visible without expanding
details. Place Merge… alongside when existing authoritative evidence allows it.
Keep Archive/Edit available with secondary emphasis and avoid competing duplicates
of the primary actions.

Summarize branch/PR provenance and reported test confidence once; retain full evidence
through clear disclosure. Never present reported test notes as verified success.
Explain beside Accept work/Reject work that they record a verdict and do not merge
or move the task. Preserve Doing continuation/replies and every repository safeguard.

Test local review, open PR, unverified tests, no changes, failed evidence lookup,
stale merge refusal, confirmation, Done transition and Undo. Verify review/verdict/
merge are distinguishable in the UI; record human comprehension as unverified unless
actually observed. Complete the shared verification and release workflow.
```

## Prompt 4 — phone navigation hierarchy

```text
Execute workstream 4 of docs/archive/ux-excellence-plan.md.

Record baseline phone navigation for Runs, Overview, project settings and a Git
subsection. Prototype a compact secondary chooser for Git/Settings against the
existing wrapped/scrolling sections. Preserve primary weekly destinations, More,
project switching, browser Back, section addresses and task-panel return location.
Do not impose five primary tabs just because it is conventional.

Show an explicit current-location label at each level and discoverable overflow.
The shell's narrow icon rail needs accurate accessible names and an obvious way to
reveal labels. Verify long project names, work projects, multiple workspaces, large
text and safe-area insets without clipping or overlapping the bottom bar.

Make both alternatives concrete and reviewable. If participant observations exist,
choose using task completion. Otherwise ask for a preference while doing independent
route/focus checks; document the evidence gap and any provisional choice. Do not
invent a user study or call scripted navigation proof of human discoverability.
Implement one coherent layout, test routing/focus and retained weekly capabilities,
then complete the shared release workflow.
```

## Prompt 5 — recovery, drafts and uncertain delivery

```text
Execute workstream 5 of docs/archive/ux-excellence-plan.md.

Use controlled browser responses to reproduce Git health enabled-success,
enabled-failure, explicitly disabled and unknown/unreachable states. Inspect current
API behavior; a disabled fixture or generic 503 alone does not prove a production
defect. Explain explicit disabling and link only to an existing enabling path.
If no authorized UI/API path exists, state the limitation and record the dependency.

Put safe recovery adjacent to errors in HealthPanel, error boundaries, New task,
question cards and connection banners. Preserve unsent answers/briefs through
disconnect and reload. Distinguish retry-safe failure from uncertain delivery;
do not add retries that can duplicate a task, run or answer. Use existing API
guarantees only. Keep optional setup failures quiet and requested failures explicit.

Test long/localized errors, offline entry, lost responses, reconnect and changed Git
evidence. Acceptance: actionable next steps, no lost drafts or unsafe duplicates,
unknown states remain unknown, controller checks still govern confirmations.
Complete the shared verification/documentation/version/tag workflow.
```

## Prompt 6 — useful bundled Help

```text
Execute workstream 6 after terminology and navigation from Prompts 1–5 have settled.

Build a local bundled frontend Help surface within the existing router, consistently
reachable from More/Settings and Jump to where supported. Add no Go endpoint or
remote resources. Organize around answering an agent, inspecting/merging work,
continuing a run, planning versus scheduling, choosing a runner, disconnection
recovery and keyboard navigation.

Use short worked examples, valid links to existing views and accurate recovery advice.
Explain count scope, verdict versus merge and reported versus verified tests. Add
contextual links only where evidence shows confusion; preserve the quiet hierarchy.
If adding search, search bundled help/shortcuts, never assistant conversation history.

Test discovery/navigation from phone, desktop and keyboard, links with repository and
work projects, and search behavior if implemented. Render Help in both themes and
check the shared type/contrast floors. Prepare a realistic comprehension task; do not
claim successful human use unless observed. This meaningful new functionality needs
a MINOR version increment under the unified release policy. Complete all shared checks.
```

## Prompt 7 — full verification and independent reassessment

```text
Execute workstream 7, the final evidence pass, after Prompts 1–6.

Read the accumulated evidence and create a neutral five-developer formative test
guide covering answer, fixture review/merge, verdict meaning, planning/scheduling,
Git navigation and draft recovery. Include phone/keyboard users and an assistive-
technology check. Use actual participant results if available; do not send outreach
or invent participants. Report success counts, wrong turns, assistance, critical
errors and time. Missing human/device observations remain explicit limitations.

Run make check, make web web-embed web-shell and make test-browser. Also run the
unified desktop shell and both assistant browser suites, using their disposable
fixtures and named artifact directories. Rebuild/reseed the guarded baseline fixture
and re-render every routed surface and dialog at 1440x900 and 375x812, both themes.
Extend the capture inventory for new Help/navigation surfaces: the old 120 captures
are a baseline, not a completeness target. Inspect shell collapsed/expanded questions,
keyboard focus, reduced motion, long/sparse content and controlled failure variants.
Run legibility measurement and distinguish measured routed text from visual-only
dialog evidence. Fix reproduced in-scope regressions with tests and rerun affected
checks; complete full checks after final changes.

Run the Impeccable critique with TWO independent assessors; I explicitly authorize
these critique sub-agents. Keep assessor A blind to historical scores, the plan's
score targets and assessor B's detector report until A has scored all ten heuristics.
Provide A current product/design context and fresh evidence only. B independently
checks implementation/accessibility evidence and detector findings. Follow the
skill's isolation and critique-storage instructions. Distinguish direct observations,
source inferences, false positives and untested cases; no fabricated live inspection.

After scoring, compare every heuristic against the 30/40 baseline and preserve the
earlier 26/40 historical comparison. Update docs/archive/ux-simplification-audit.md, DESIGN.md
and the local critique snapshot. Claim 40/40 only if a fresh review supports ten 4s;
otherwise publish the honest score with bounded remaining fixes/evidence needs.
Do not polish indefinitely to force the target or award points for completed prompts.

For any implementation changes, increment the unified version, commit, make tag and
make verify-tag as required. For a documentation-only final pass, follow AGENTS.md's
documentation exception rather than creating an artificial release. Stop only owned
fixtures, preserve artifacts and never push. Report full-check results, render
coverage, independent score comparison, human-evidence limitations and local release
identity when applicable.
```

## Check commands for the final pass

Run from the repository root. Each standalone browser script below creates its own disposable services; inspect
its current environment options before execution. Set separate artifact directories so evidence is not overwritten.

```sh
make check
make web web-embed web-shell
make test-browser
node --test scripts/seed-browser-fixture.test.cjs
BROWSER_ARTIFACT_DIR="$UX_PASS_DIR/shell" node scripts/test-unified-desktop-browser.cjs
PLAYWRIGHT_MODULE="$PWD/web/node_modules/playwright" \
BROWSER_ARTIFACT_DIR="$UX_PASS_DIR/assistant" node scripts/browser-assistant.cjs
PLAYWRIGHT_MODULE="$PWD/web/node_modules/playwright" \
BROWSER_ARTIFACT_DIR="$UX_PASS_DIR/shell-assistant" node scripts/browser-shell-assistant.cjs
git diff --check
```

This document supplies future execution prompts. Creating it does not start implementation, operate controllers,
authorize outreach, change the product version or push anything.
