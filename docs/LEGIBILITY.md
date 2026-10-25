# Legibility floor

Two numbers hold for every piece of text in the Individual app, in both themes and at desktop and phone width:

- **Contrast** is at least 4.5:1 against what is behind it (3:1 for text of 24px, or 18.66px and bold). Status words on paper
  use the `*-text` tokens, never the bare `--ok` / `--warn` / `--danger`, which are for dots and fills.
- **Size** is at least **11.5px**. Since 4.84 the web app's type scale is 12 / 13 / 14 / 16 / 20 / 24px, so its smallest text
  (section labels, counts, machine text) is 12px, and the 11.5px floor below is what the measurements and the phone UI still hold to;
  body text is 13–14px.

Two checks keep them there: `web/src/lib/contrast.test.ts` (the tokens in `app.css` against the grounds text sits on, part of
`npx vitest run`) and `scripts/measure-legibility.cjs` (every visible text node of the rendered screens).

## Measuring

Everything runs against the disposable browser fixture, never a real controller. From the repository root:

```bash
make web web-embed                       # the PWA is embedded in the fixture binary
go build -o /tmp/werkbord-fixture ./scripts/browser-fixture
mkdir -p /tmp/werkbord-fixture-data
GIT_CONFIG_GLOBAL=/dev/null WERKBORD_BROWSER_DIR=/tmp/werkbord-fixture-data WERKBORD_BROWSER_ADDR=127.0.0.1:17499 \
  WERKBORD_BROWSER_EXECUTION=1 /tmp/werkbord-fixture &

# a repository project with a run waiting on a question, a second one with a finished run in Review, and a work project
PERSONAL_BROWSER_URL=http://127.0.0.1:17499 SEED_DIR=/tmp/werkbord-fixture-seed node scripts/seed-browser-fixture.cjs

# measure (Playwright's Chromium: cd web && npx playwright install chromium, once)
PLAYWRIGHT_MODULE=$PWD/web/node_modules/playwright PERSONAL_BROWSER_URL=http://127.0.0.1:17499 \
  node scripts/measure-legibility.cjs --check
```

`--check` exits 1 when anything is under either floor; `--json out.json` keeps the findings. The script visits 28 pages (Needs you,
Projects, the nine Settings sections, and each project's sections and a task panel for a question, a review and a backlog task)
in light and dark at 1440 and 390 wide. Contrast is computed from the real painted colours: each element's colour is composited
over every ancestor's background (a gradient face is checked at each of its stops, the worst taken), and text carries its
ancestors' opacity. Screen-reader-only text is not measured.

## Result (4.17.1-preview.1)

Same fixture, same seed, same script, before (`werkbord-v4.17.0-preview.1`) and after.

| | Before | After |
| --- | --- | --- |
| Text nodes measured | 5 482 | 5 418 |
| Contrast failures, distinct | 5 (98 nodes) | **0** |
| Text under 11.5px, distinct | 49 (1 704 nodes) | **0** |

| Text | Where | Before | After |
| --- | --- | --- | --- |
| "0 running" (and the other zero counts) | Board legend, light | 2.75:1 (60% opacity on secondary text) | 6.87:1 |
| same | Board legend, dark | 3.43:1 | 7.76:1 |
| "ready" | Settings → Agents, light | 3.45:1 (`--ok` on a tray) | 5.32:1 (`--ok-text`) |
| "clean" | Git, light | 3.45:1 | 5.32:1 |
| "Session finished" | Task panel feed, light | 3.71:1 (`--ok` on paper) | 5.72:1 |
| Done tick on a setup card | Onboarding and Settings, light / dark | white on green 4.05:1 / 2.56:1 | 6.25:1 / 7.75:1 |

The Done tick is an icon, so the script does not count it; it is listed because the same fix covers it.

| Size | Before | After |
| --- | --- | --- |
| Phone tab bar labels | 10px (11px above 430px) | 11.5px, columns never narrower than their label |
| Rail and sidebar section links, counts | 10 – 11px | 11.5px |
| Run and time columns, Git headings, `Details`, `Run result`, run ids | 10 – 11px | 11.5px |
| Inline `code` | 0.88em (11.44px in a 13px line) | `max(0.88em, 11.5px)` |

The phone tab bar was the one place where the floor cost something: seven tabs at 11.5px do not fit 360px side by side with
the old equal columns, so each column now grows to its own label (`minmax(min-content, 1fr)`) and the labels sit 0.02em closer.
