// Behavior checks against an explicitly identified, freshly seeded disposable fixture.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const base = process.env.PERSONAL_BROWSER_URL;
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
assert(base && artifacts, 'explicit fixture URL and artifact directory required');
const url = new URL(base);
assert(url.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname), 'loopback fixture required');
(async () => {
  const health = await fetch(base + '/api/health').then(r => r.json());
  assert.equal(health.version, 'browser-fixture');
  fs.mkdirSync(artifacts, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  try {
    for (const touch of [false, true]) {
      const ctx = await browser.newContext({ viewport: { width: touch ? 1440 : 375, height: touch ? 900 : 812 }, hasTouch: touch, reducedMotion: 'reduce' });
      const page = await ctx.newPage();
      await page.goto(base + '/#token=disposable-browser-credential');
      await page.waitForFunction(() => !location.hash.includes('token='));
      await page.goto(base + '/#/control');
      const group = page.getByRole('group', { name: 'What to show', exact: true });
      const all = group.getByRole('button', { name: /^All:/ });
      const review = group.getByRole('button', { name: /^Review:/ });
      await all.waitFor();
      assert.equal(await all.getAttribute('aria-pressed'), 'true');
      // Native filter buttons use Tab and Space; they do not promise an arrow-key tab widget.
      await review.focus();
      await page.keyboard.press('Space');
      assert.equal(await review.getAttribute('aria-pressed'), 'true');
      assert.equal(await all.getAttribute('aria-pressed'), 'false');
      assert.equal(await page.locator('[data-kind="question"]').count(), 0);
      const action = page.locator('[data-kind="review"]').getByRole('link', { name: 'Review', exact: true });
      await action.waitFor();
      const box = await action.boundingBox();
      assert(box.width >= 44 && box.height >= 44, `Review target at ${touch ? 'coarse desktop' : 'phone'}: ${JSON.stringify(box)}`);
      await action.focus();
      await page.keyboard.press('Enter');
      await page.locator('#task-title').waitFor();
      assert.equal(await page.locator('#task-title').evaluate(el => el === document.activeElement), true);
      await page.keyboard.press('Escape');
      await page.goto(base + '/#/control');
      await group.getByRole('button', { name: /^Questions:/ }).click();
      assert.equal(await page.locator('[data-kind="review"]').count(), 0);
      await all.click();
      assert.equal(await all.getAttribute('aria-pressed'), 'true');
      assert(await page.locator('[data-kind="question"]').count() > 0);
      assert(await page.locator('[data-kind="review"]').count() > 0);
      for (const theme of ['light', 'dark']) {
        await page.evaluate(t => document.documentElement.dataset.theme = t, theme);
        await page.screenshot({ path: path.join(artifacts, `attention-${touch ? 'coarse-desktop' : 'phone'}-${theme}.png`) });
      }
      await ctx.close();
    }
    console.log('PASS native attention filter keyboard behavior, preserved mixed-count scope, Review touch size and task focus');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
