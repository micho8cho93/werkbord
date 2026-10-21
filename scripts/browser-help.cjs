const fs = require('node:fs');
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');

const base = process.env.PERSONAL_BROWSER_URL;
const projectsPath = process.env.SEED_PROJECTS;
const token = 'disposable-browser-credential';
assert.ok(base, 'PERSONAL_BROWSER_URL must target the isolated browser fixture');
assert.ok(projectsPath, 'SEED_PROJECTS must point to this pass\'s projects.json');
const projects = JSON.parse(fs.readFileSync(projectsPath, 'utf8'));

(async () => {
  const health = await fetch(new URL('/api/health', base));
  const identity = await health.json();
  assert.equal(identity.version, 'browser-fixture', 'refusing to browse a non-fixture controller');

  const browser = await chromium.launch({ headless: true });
  const errors = [];
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  page.on('pageerror', (error) => errors.push(error.message));
  try {
    await page.goto(`${base}/#token=${token}`);
    await page.getByRole('link', { name: 'Help', exact: true }).waitFor();
    await page.getByRole('link', { name: 'Help', exact: true }).click();
    await page.getByRole('heading', { name: 'Find the next step' }).waitFor();
    assert.equal(await page.locator('.guides > li').count(), 9, 'desktop Help lists all bundled guides');

    const search = page.getByRole('searchbox', { name: 'Search Help and shortcuts' });
    await search.fill('Ctrl+K');
    assert.equal(await page.locator('.guides > li').count(), 1, 'shortcut terms find the keyboard guide');
    assert.match(await page.locator('.guides').innerText(), /Navigate with the keyboard/);
    await search.fill('private assistant transcript zebra');
    assert.equal(await page.locator('.guides > li').count(), 0, 'search does not query assistant conversation history');
    assert.match(await page.locator('.help .empty').innerText(), /No bundled guide matches/);
    await search.fill('');

    await page.getByRole('heading', { name: 'Find the next step' }).click();
    await page.keyboard.press('Control+k');
    await page.getByRole('dialog', { name: 'Jump to' }).waitFor();
    await page.locator('#jt-search').fill('help');
    assert.equal(await page.locator('#jt-list [role=option]').count(), 1, 'Jump to finds the bundled Help destination');
    await page.keyboard.press('Enter');
    await page.getByRole('heading', { name: 'Find the next step' }).waitFor();

    await page.goto(`${base}/#token=${token}`);
    await page.goto(`${base}/#/p/${projects.review}/board`);
    await page.getByRole('heading', { name: 'Second repo' }).waitFor();
    await page.getByRole('link', { name: 'Help', exact: true }).click();
    await page.getByRole('link', { name: 'Open project work' }).first().click();
    assert.match(page.url(), new RegExp(`/p/${projects.review}/runs$`), 'repository guide returns to a repository view');

    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto(`${base}/#/p/${projects.work}/board`);
    await page.getByRole('heading', { name: 'Venue search' }).waitFor();
    await page.getByRole('button', { name: 'More' }).click();
    await page.getByRole('dialog', { name: 'More' }).getByRole('link', { name: 'Help' }).click();
    await page.getByRole('link', { name: 'Open planning or schedule' }).click();
    assert.match(page.url(), new RegExp(`/p/${projects.work}/timeline$`), 'work guide links to Timeline instead of repository-only Calendar');

    await page.goto(`${base}/#/p/${projects.review}/board`);
    await page.getByRole('button', { name: 'More' }).click();
    await page.getByRole('dialog', { name: 'More' }).getByRole('link', { name: 'Help' }).click();
    await page.getByRole('heading', { name: 'Find the next step' }).waitFor();
    assert.match(page.url(), /#\/help$/);
    assert.equal(await page.getByRole('searchbox', { name: 'Search Help and shortcuts' }).count(), 1, 'phone can search Help');

    await page.goto(`${base}/#/settings`);
    await page.getByRole('link', { name: 'Help and keyboard shortcuts' }).click();
    await page.getByRole('heading', { name: 'Find the next step' }).waitFor();
    assert.match(page.url(), /#\/help$/);

    await page.getByRole('searchbox', { name: 'Search Help and shortcuts' }).focus();
    await page.keyboard.press('Tab');
    const focusStyle = await page.evaluate(() => {
      const el = document.activeElement;
      return el ? getComputedStyle(el).outlineStyle : 'none';
    });
    assert.notEqual(focusStyle, 'none', 'keyboard focus remains visible after Help search');

    const touchContext = await browser.newContext({
      viewport: { width: 375, height: 812 },
      isMobile: true,
      hasTouch: true,
      reducedMotion: 'reduce',
    });
    const touchPage = await touchContext.newPage();
    await touchPage.goto(`${base}/#token=${token}`);
    await touchPage.goto(`${base}/#/help`);
    const touchEvidence = await touchPage.evaluate(() => ({
      coarse: matchMedia('(pointer: coarse)').matches,
      reduced: matchMedia('(prefers-reduced-motion: reduce)').matches,
      searchHeight: document.querySelector('.search-field input')?.getBoundingClientRect().height,
      smallestLink: Math.min(...[...document.querySelectorAll('.guide-link')].map((el) => el.getBoundingClientRect().height)),
    }));
    assert.equal(touchEvidence.coarse, true, 'phone context uses a coarse pointer');
    assert.equal(touchEvidence.reduced, true, 'reduced motion preference reaches the page');
    assert.ok(touchEvidence.searchHeight >= 44, 'Help search meets the touch target floor');
    assert.ok(touchEvidence.smallestLink >= 44, 'guide links meet the touch target floor');
    await touchContext.close();

    assert.deepEqual(errors, [], 'Help navigation has no JavaScript errors');
    console.log('Help browser flows passed: desktop rail, Jump to, bundled search, repository/work links, phone More, Settings link, keyboard focus, coarse-pointer targets and reduced-motion preference.');
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
