// Phone navigation behavior against the explicitly seeded disposable browser fixture.
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const os = require('node:os');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');

const base = process.env.PERSONAL_BROWSER_URL || 'http://127.0.0.1:17499';
const token = 'disposable-browser-credential';
const artifacts = process.env.BROWSER_ARTIFACT_DIR || fs.mkdtempSync(path.join(os.tmpdir(), 'werkbord-navigation-ws4-'));
fs.mkdirSync(artifacts, { recursive: true });

async function api(route) {
  const response = await fetch(base + route, { headers: { Authorization: `Bearer ${token}` } });
  if (!response.ok) throw new Error(`${response.status} ${await response.text()}`);
  return response.json();
}

(async () => {
  const health = await api('/api/health');
  assert.equal(health.version, 'browser-fixture', 'navigation tests require the disposable browser fixture');
  const { projects } = await api('/api/projects');
  const repo = projects.find((project) => project.name === 'Fixture repo');
  const work = projects.find((project) => project.name === 'Venue search');
  assert(repo && work, 'fixture must contain its seeded repository and work project');

  const browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
    const capture = async (name, route) => {
      await page.goto(`${base}/#${route}`);
      for (const theme of ['light', 'dark']) {
        await page.evaluate((value) => { document.documentElement.dataset.theme = value; }, theme);
        for (const [size, width, height] of [['desktop', 1440, 900], ['phone', 375, 812]]) {
          await page.setViewportSize({ width, height });
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, `${name} has no horizontal overflow at ${size}/${theme}`);
          await page.screenshot({ path: path.join(artifacts, `${name}-${size}-${theme}.png`) });
        }
      }
    };
  try {
    await page.goto(`${base}/#token=${token}`);
    await page.waitForFunction(() => !location.hash.includes('token='), null, { timeout: 10000 });

    // Runs remains a primary destination and Overview/settings remain reachable through More.
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto(`${base}/#/p/${repo.id}/runs`);
    const primary = page.getByRole('navigation', { name: 'Primary', exact: true });
    const runs = primary.getByRole('link', { name: 'Runs', exact: true });
    await runs.waitFor();
    assert.equal(await runs.getAttribute('aria-current'), 'page', 'Runs stays a primary phone destination');
    const bar = await primary.boundingBox();
    assert(bar && bar.y >= 0 && bar.y + bar.height <= 812, 'bottom navigation stays inside the phone viewport');

    await primary.getByRole('button', { name: 'More', exact: true }).click();
    const more = page.getByRole('dialog', { name: 'More', exact: true });
    await more.getByRole('link', { name: 'Overview', exact: true }).click();
    assert.match(page.url(), new RegExp(`#\\/p\\/${repo.id}\\/overview$`), 'Overview keeps its project address');
    await page.getByRole('heading', { name: 'Overview', exact: true }).waitFor();
    await primary.getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('dialog', { name: 'More', exact: true }).getByRole('link', { name: 'Project settings', exact: true }).click();
    assert.match(page.url(), new RegExp(`#\\/p\\/${repo.id}\\/defaults$`), 'project settings stays in More and keeps its address');
    await page.getByRole('heading', { name: 'Project settings', exact: true }).waitFor();
    await primary.getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('dialog', { name: 'More', exact: true }).getByRole('link', { name: 'Settings', exact: true }).click();
    assert.match(page.url(), /#\/settings$/, 'global Settings stays in More');

    // The compact chooser replaces the wrapped Git row only on phones. Its native select keeps focus,
    // writes the existing query address, and follows browser Back and direct deep links.
    await page.goto(`${base}/#/p/${repo.id}/git?branches`);
    const gitChooser = page.getByRole('combobox', { name: 'Git section', exact: true });
    await gitChooser.waitFor();
    assert.equal(await gitChooser.inputValue(), 'branches');
    assert.equal(await page.getByRole('navigation', { name: 'Git sections', exact: true }).count(), 0, 'wrapped links are replaced by the compact phone chooser');
    await gitChooser.focus();
    await gitChooser.selectOption('history');
    assert.match(page.url(), new RegExp(`#\\/p\\/${repo.id}\\/git\\?history$`), 'Git chooser preserves the section query address');
    assert.equal(await gitChooser.inputValue(), 'history');
    assert.equal(await gitChooser.evaluate((el) => el === document.activeElement), true, 'selection keeps keyboard focus on the chooser');
    await page.goBack();
    await page.waitForFunction((id) => location.hash === `#/p/${id}/git?branches`, repo.id);
    assert.equal(await gitChooser.inputValue(), 'branches', 'browser Back restores the prior Git subsection');
    await page.goto(`${base}/#/p/${repo.id}/git?health`);
    assert.equal(await gitChooser.inputValue(), 'health', 'a Git subsection deep link selects the addressed location');
    await capture('git-branches', `/p/${repo.id}/git?branches`);

    // Settings uses the same one-row hierarchy. Section addresses remain shareable and Back-restorable.
    await page.goto(`${base}/#/settings?runners`);
    const settingsChooser = page.getByRole('combobox', { name: 'Settings section', exact: true });
    await settingsChooser.waitFor();
    assert.equal(await settingsChooser.inputValue(), 'runners');
    assert.equal(await page.getByRole('navigation', { name: 'Settings sections', exact: true }).count(), 0, 'the long grouped phone row is replaced by the compact chooser');
    await settingsChooser.selectOption('github');
    assert.match(page.url(), /#\/settings\?github$/, 'Settings chooser keeps the existing section address');
    await page.goBack();
    await page.waitForFunction(() => location.hash === '#/settings?runners');
    assert.equal(await settingsChooser.inputValue(), 'runners');
    await capture('settings-runners', '/settings?runners');

    // Stress the project heading with a long display name without changing fixture data.
    await page.route(`${base}/api/projects**`, async (route) => {
      if (route.request().method() !== 'GET' || new URL(route.request().url()).pathname !== '/api/projects') return route.continue();
      const response = await route.fetch();
      const payload = await response.json();
      payload.projects = payload.projects.map((project) => project.id === repo.id
        ? { ...project, name: 'Northern European Venue Operations and Logistics' }
        : project);
      await route.fulfill({ response, json: payload });
    });
    await page.goto(`${base}/#/p/${repo.id}/runs`);
    await page.reload();
    await page.getByRole('heading', { name: 'Northern European Venue Operations and Logistics', exact: true }).waitFor();
    await page.setViewportSize({ width: 375, height: 812 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, 'a long project name does not widen the phone viewport');
    await page.unroute(`${base}/api/projects`);

    // Work projects retain Overview and planning, with no repository-only weekly tabs.
    await page.goto(`${base}/#/p/${work.id}/board`);
    await page.getByRole('heading', { name: 'Venue search', exact: true }).waitFor();
    const workTabs = primary.locator('.tab');
    const workLabels = await workTabs.allTextContents();
    assert(workLabels.some((label) => label.trim() === 'Board'));
    assert(workLabels.some((label) => label.trim() === 'Timeline'));
    for (const unavailable of ['Calendar', 'Git', 'Runs']) assert(!workLabels.some((label) => label.trim() === unavailable), `${unavailable} is not offered for a work project`);
    await primary.getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('dialog', { name: 'More', exact: true }).getByRole('link', { name: 'Overview', exact: true }).waitFor();
    await page.keyboard.press('Escape');

    const task = (await api(`/api/projects/${repo.id}/tasks`)).tasks.find((item) => item.title === 'Dark mode audit');
    assert(task, 'fixture must contain the seeded task used to check task-panel return');
    await page.goto(`${base}/#/p/${repo.id}/board`);
    await page.goto(`${base}/#/p/${repo.id}/task/${task.id}`);
    await page.getByRole('link', { name: 'Back to the board', exact: true }).waitFor();
    await page.getByRole('link', { name: 'Back to the board', exact: true }).click();
    await page.waitForFunction((id) => location.hash === `#/p/${id}/board`, repo.id);
    assert.match(page.url(), new RegExp(`#\\/p\\/${repo.id}\\/board$`), 'closing a task panel returns to the owning project board');

    // At 200%-zoom-equivalent width, the chooser and bottom bar do not create document overflow.
    await page.setViewportSize({ width: 720, height: 450 });
    for (const route of [`/p/${repo.id}/git?worktrees`, '/settings?alerts']) {
      await page.goto(`${base}/#${route}`);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, `${route} has no horizontal overflow at the zoom-equivalent viewport`);
      const activeChooser = page.locator('.mobile-section select');
      await activeChooser.waitFor();
      const box = await activeChooser.boundingBox();
      assert(box && box.x >= 0 && box.x + box.width <= 720, `${route} chooser remains within the viewport`);
    }

    const touchContext = await browser.newContext({ viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true });
    const touchPage = await touchContext.newPage();
    await touchPage.goto(`${base}/#token=${token}`);
    await touchPage.waitForFunction(() => !location.hash.includes('token='));
    await touchPage.goto(`${base}/#/p/${repo.id}/git?branches`);
    assert.equal(await touchPage.evaluate(() => matchMedia('(pointer: coarse)').matches), true, 'phone touch emulation exposes a coarse pointer');
    const touchGit = touchPage.getByRole('combobox', { name: 'Git section', exact: true });
    const touchGitBox = await touchGit.boundingBox();
    assert(touchGitBox && touchGitBox.height >= 44, 'Git chooser meets the touch target floor');
    const touchBar = await touchPage.getByRole('navigation', { name: 'Primary', exact: true }).boundingBox();
    assert(touchBar && touchBar.y + touchBar.height <= 812 && touchBar.height >= 64, 'touch bottom bar stays within the viewport and reserves its content area');
    await touchPage.emulateMedia({ reducedMotion: 'reduce' });
    assert.equal(await touchPage.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches), true, 'reduced-motion preference is available during phone navigation');
    await touchPage.goto(`${base}/#/settings?runners`);
    const touchSettings = await touchPage.getByRole('combobox', { name: 'Settings section', exact: true }).boundingBox();
    assert(touchSettings && touchSettings.height >= 44, 'Settings chooser meets the touch target floor');
    await touchContext.close();

    assert.deepEqual(errors, [], 'navigation has no browser runtime errors');
    console.log(`PASS phone Runs, More destinations, Git and Settings chooser routes/Back/focus, work project tabs, viewport bounds; artifacts: ${artifacts}`);
  } finally {
    await browser.close();
  }
})().catch((error) => { console.error(error.stack); process.exit(1); });
