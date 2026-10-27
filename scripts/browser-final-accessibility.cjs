// Behavior checks against an explicitly identified, freshly seeded disposable fixture.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../web/node_modules/playwright');
const base = process.env.PERSONAL_BROWSER_URL;
const artifacts = process.env.BROWSER_ARTIFACT_DIR;
assert(base && artifacts, 'explicit fixture URL and artifact directory required');
const token = 'disposable-browser-credential';
const url = new URL(base);
assert(url.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname), 'loopback fixture required');
(async () => {
  const health = await fetch(base + '/api/health').then(r => r.json());
  assert.equal(health.version, 'browser-fixture');
  fs.mkdirSync(artifacts, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  try {
    const layouts = [
      { id: 'phone', width: 375, height: 812, hasTouch: false, isMobile: false },
      { id: 'coarse-desktop', width: 1440, height: 900, hasTouch: true, isMobile: false },
      { id: 'touch-phone', width: 375, height: 812, hasTouch: true, isMobile: true },
    ];
    for (const layout of layouts) {
      const ctx = await browser.newContext({ viewport: { width: layout.width, height: layout.height }, hasTouch: layout.hasTouch, isMobile: layout.isMobile, reducedMotion: 'reduce' });
      const page = await ctx.newPage();
      page.setDefaultTimeout(10000);
      await page.goto(base + '/#token=' + token);
      await page.waitForFunction(() => !location.hash.includes('token='));
      await page.goto('about:blank');
      let releaseOverview;
      let sawOverview;
      const overviewGate = new Promise(resolve => { releaseOverview = resolve; });
      const overviewIntercepted = new Promise(resolve => { sawOverview = resolve; });
      await page.route('**/api/control-center', async route => {
        sawOverview();
        await overviewGate;
        try {
          await route.continue();
        } catch (error) {
          if (!String(error).includes('Route is already handled')) throw error;
        }
      });
      await page.goto(base + '/#/control');
      await overviewIntercepted;
      await page.getByText('Checking availability…', { exact: true }).waitFor();
      await page.getByText('Runner details appear when workspace status loads.', { exact: true }).waitFor();
      assert.equal(await page.getByText('Connect a runner to start agent work.', { exact: true }).count(), 0, 'loading does not look like an empty runner setup');
      releaseOverview();
      await page.unroute('**/api/control-center');
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
      assert(box.width >= 44 && box.height >= 44, `Review target at ${layout.id}: ${JSON.stringify(box)}`);
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

      const runnerPanel = page.locator('section[aria-labelledby="infra-title"]');
      const runnerDetails = runnerPanel.locator('details.runner-details');
      const runnerSummary = runnerPanel.locator('.runner-summary');
      assert.match(await runnerSummary.innerText(), /\d+ online/);
      assert.equal(await runnerDetails.evaluate(el => el.open), false, 'healthy runner inventory starts collapsed');
      await runnerDetails.locator('summary').focus();
      await page.keyboard.press('Enter');
      assert.equal(await runnerDetails.evaluate(el => el.open), true, 'runner details open from the keyboard');
      await page.keyboard.press('Space');
      assert.equal(await runnerDetails.evaluate(el => el.open), false, 'runner details close from the keyboard');

      const usage = page.locator('details.usage-details');
      assert.equal(await usage.evaluate(el => el.open), false, 'secondary usage detail starts collapsed');
      if (layout.hasTouch) await usage.locator('summary').tap();
      else await usage.locator('summary').click();
      assert.equal(await usage.evaluate(el => el.open), true, 'usage remains available through the disclosure');
      assert(await usage.getByText('Only what agents reported. An estimate is not a bill.', { exact: true }).isVisible());
      if (layout.hasTouch) await usage.locator('summary').tap();
      else await usage.locator('summary').click();

      const projectFilter = page.getByRole('combobox', { name: 'Filter by project', exact: true });
      await projectFilter.selectOption({ label: 'Venue search' });
      await page.getByText('Nothing needs your attention.', { exact: true }).waitFor();
      await page.getByRole('button', { name: 'Clear', exact: true }).click();
      await all.waitFor();
      assert(await page.locator('[data-kind="question"]').count() > 0, 'clearing the project filter restores actionable attention');
      assert.equal(await page.locator('.pane.side').getByText('Running now', { exact: true }).count(), 0, 'the dashboard omits an empty active-work panel');
      if (layout.hasTouch) {
        for (const [label, target] of [
          ['runner disclosure', runnerDetails.locator('summary')],
          ['usage disclosure', usage.locator('summary')],
          ['runner settings link', runnerPanel.getByRole('link', { name: 'Manage', exact: true })],
          ['attention filter', group.getByRole('button', { name: /^All:/ })],
          ['project row', page.locator('.pane.side .prow').first()],
        ]) {
          const targetBox = await target.boundingBox();
          assert(targetBox && targetBox.width >= 44 && targetBox.height >= 44, `${label} touch target at ${layout.id}: ${JSON.stringify(targetBox)}`);
        }
      }

      for (const theme of ['light', 'dark']) {
        await page.evaluate(t => document.documentElement.dataset.theme = t, theme);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false, `dashboard has no horizontal overflow at ${layout.id}/${theme}`);
        await page.screenshot({ path: path.join(artifacts, `attention-${layout.id}-${theme}.png`) });
      }

      const longActivity = 'The agent is reviewing the sync retry path, checking the backoff limits, and preparing a focused regression check before changing the client.';
      await page.route('**/api/control-center', async route => {
        const response = await route.fetch();
        const overview = await response.json();
        const active = overview.runs.find(item => item.run.state === 'waiting_for_user');
        if (active) {
          active.run.state = 'running';
          delete active.run.waiting;
          active.run.activity = longActivity;
          active.run.activityAt = new Date().toISOString();
          overview.questions = [];
        }
        const reviewItem = overview.review[0];
        if (reviewItem) {
          const baseTask = reviewItem.task;
          const time = Date.now();
          const later = { ...baseTask, id: baseTask.id + '-later', title: 'Later scheduled work', orchestration: { enabled: true, scheduledAt: new Date(time + 3 * 60 * 60 * 1000).toISOString() } };
          const sooner = { ...baseTask, id: baseTask.id + '-sooner', title: 'Sooner scheduled work', orchestration: { enabled: true, scheduledAt: new Date(time + 60 * 60 * 1000).toISOString() } };
          overview.orchestration = [
            { task: later, projectName: reviewItem.projectName, decision: { taskId: later.id, state: 'waiting_schedule', reason: 'Waiting for scheduled time' } },
            { task: sooner, projectName: reviewItem.projectName, decision: { taskId: sooner.id, state: 'waiting_schedule', reason: 'Waiting for scheduled time' } },
          ];
        }
        await route.fulfill({ response, body: JSON.stringify(overview) });
      });
      await page.reload();
      const side = page.locator('.pane.side');
      const running = side.locator('section').filter({ hasText: 'Running now' });
      await running.getByRole('link', { name: 'Add retry to the sync client', exact: true }).waitFor();
      const activityDisclosure = running.locator('details.run-activity');
      await activityDisclosure.locator('summary').press('Enter');
      assert.equal(await activityDisclosure.locator('p.mm').innerText(), longActivity, 'long activity is available beyond its one-line preview');
      const next = side.locator('section').filter({ hasText: 'Next executions' });
      await next.getByText('Scheduled', { exact: false }).first().waitFor();
      assert.deepEqual(await next.locator('.run a').allTextContents(), ['Sooner scheduled work', 'Later scheduled work'], 'scheduled work is ordered by its actual time');
      for (const theme of ['light', 'dark']) {
        await page.evaluate(t => document.documentElement.dataset.theme = t, theme);
        await page.screenshot({ path: path.join(artifacts, `active-queue-${layout.id}-${theme}.png`) });
      }
      await page.unroute('**/api/control-center');

      await page.route('**/api/control-center', async route => {
        const response = await route.fetch();
        const overview = await response.json();
        overview.runners = overview.runners.map((r, i) => i === 0 ? { ...r, online: false } : r);
        await route.fulfill({ response, body: JSON.stringify(overview) });
      });
      await page.reload();
      await runnerSummary.getByText(/offline/).waitFor();
      assert.equal(await runnerDetails.evaluate(el => el.open), true, 'offline runner details open for immediate inspection');
      await page.route('**/api/events**', route => route.abort('internetdisconnected'));
      await page.reload();
      await page.context().setOffline(true);
      await page.getByTestId('connection-status').filter({ hasText: 'Live updates disconnected' }).waitFor();
      await runnerPanel.getByText('Availability unknown', { exact: true }).waitFor();
      assert.equal(await runnerDetails.evaluate(el => el.open), true, 'disconnect keeps the last runner report available');
      assert.equal(await runnerDetails.locator('.mm.end').first().innerText(), 'availability unknown', 'stale runner data is not labelled online or offline');
      assert.equal(await runnerDetails.locator('.mm.end').first().getAttribute('aria-label'), 'availability unknown', 'screen readers receive the same unknown state');
      assert.equal(await page.evaluate(() => navigator.onLine), false, 'the browser offline state is exercised');
      await page.context().setOffline(false);
      await page.unroute('**/api/events**');

      await page.unroute('**/api/control-center');
      let failControlCenter = true;
      await page.route('**/api/control-center', async route => {
        if (failControlCenter) {
          await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'unavailable', message: 'Control Center unavailable' } }) });
        } else {
          await route.continue();
        }
      });
      await page.reload();
      await page.getByTestId('connection-status').filter({ hasText: 'Live updates disconnected' }).waitFor();
      await page.getByRole('button', { name: 'Reconnect', exact: true }).waitFor();
      failControlCenter = false;
      await page.getByRole('button', { name: 'Reconnect', exact: true }).click();
      await page.getByTestId('connection-status').waitFor({ state: 'detached' });
      await page.unroute('**/api/control-center');
      if (layout.id === 'touch-phone') {
        const answer = page.locator('[data-kind="question"]').getByRole('button', { name: 'Yes', exact: true });
        const answerResponse = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/answer'));
        await answer.tap();
        assert((await answerResponse).ok(), 'the inline answer reaches the disposable runner');
        await page.locator('[data-kind="question"]').waitFor({ state: 'detached' });
      }
      await ctx.close();
    }
    console.log('PASS loading, attention filters, runner/usage disclosure, empty state, keyboard, touch target, active and scheduled work, offline recovery, and light/dark renders');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
