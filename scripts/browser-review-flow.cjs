// Review-flow regression against the explicit seeded browser fixture.
// The page context is isolated; every Git association and result below comes from fixture API responses.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');

const base = process.env.PERSONAL_BROWSER_URL;
const projectsPath = process.env.UX_FIXTURE_PROJECTS;
const artifacts = process.env.BROWSER_ARTIFACT_DIR || fs.mkdtempSync(path.join(require('node:os').tmpdir(), 'werkbord-review-flow-'));
const token = 'disposable-browser-credential';
if (!base || !projectsPath) throw Error('Set PERSONAL_BROWSER_URL and UX_FIXTURE_PROJECTS from the guarded seeded fixture.');
fs.mkdirSync(artifacts, { recursive: true });

async function api(method, route, data) {
  const res = await fetch(base + route, {
    method,
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: data ? JSON.stringify(data) : undefined,
  });
  const raw = await res.text();
  if (res.status >= 400) throw Error(`${res.status} ${raw}`);
  return raw ? JSON.parse(raw) : null;
}

async function captureMatrix(page, name) {
  for (const theme of ['light', 'dark']) {
    await page.evaluate(value => document.documentElement.dataset.theme = value, theme);
    for (const [width, height] of [[1440, 900], [375, 812]]) {
      await page.setViewportSize({ width, height });
      await page.waitForTimeout(180);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false, `${name} fits ${width}x${height}/${theme}`);
      await page.screenshot({ path: path.join(artifacts, `${name}-${width}x${height}-${theme}.png`), fullPage: true });
    }
  }
}

(async () => {
  const projects = JSON.parse(fs.readFileSync(projectsPath, 'utf8'));
  const projectId = projects.review;
  const tasks = (await api('GET', `/api/projects/${projectId}/tasks`)).tasks;
  const task = tasks.find(item => item.state === 'review');
  assert(task, 'the guarded seed provides a Review task');
  const runs = (await api('GET', `/api/projects/${projectId}/tasks/${task.id}/runs`)).runs;
  const run = runs.find(item => item.endedAt && item.id === task.latestRunId) || runs.find(item => item.endedAt);
  assert(run?.handoff, 'the seeded Review task has a finished run handoff');
  const overview = await api('GET', `/api/projects/${projectId}/git`);
  const branch = overview.branches.find(item => item.scope === 'local' && item.werkbord.taskId === task.id && item.werkbord.runId === run.id);
  assert(branch, 'fixture Git evidence identifies the task-owned branch and run');
  assert(run.handoff.results || run.handoff.summary, 'the seeded run provides an actual result');

  const browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE ? { executablePath: process.env.BROWSER_EXECUTABLE } : {}) });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await context.newPage();
    page.setDefaultTimeout(12000);
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(`${base}/#token=${token}`);
    await page.goto(`${base}/#/p/${projectId}/task/${task.id}`);
    await page.getByRole('heading', { name: task.title, exact: true }).waitFor();

    const panel = page.locator('.panel');
    const decision = panel.locator('.head');
    const assessment = decision.getByRole('group', { name: 'Your assessment of this run' });
    const reviewLink = decision.getByRole('link', { name: 'Review changes', exact: true });
    await assessment.waitFor();
    await reviewLink.waitFor();
    assert(await assessment.getByText('Not recorded', { exact: false }).isVisible(), 'the reversible assessment is beside the Review decision');
    assert(await decision.getByText(/records your assessment.*does not merge, move or archive the task/i).isVisible(), 'the decision explains its effect before submission');
    const evidence = panel.locator('details.review-evidence');
    assert.equal(await evidence.locator('[aria-label="Your assessment of this run"]').count(), 0, 'full run evidence does not duplicate the assessment');
    await captureMatrix(page, 'seeded-task-review');

    const touchReviewContext = await browser.newContext({ viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true, storageState: await context.storageState() });
    const touchReview = await touchReviewContext.newPage();
    await touchReview.goto(`${base}/#/p/${projectId}/task/${task.id}`);
    const touchAssessment = touchReview.getByRole('group', { name: 'Your assessment of this run' });
    await touchAssessment.waitFor();
    assert.equal(await touchReview.evaluate(() => matchMedia('(pointer: coarse)').matches), true, 'task review touch check uses coarse-pointer emulation');
    const touchVerdicts = await Promise.all(['Accept work', 'Reject work'].map(label => touchAssessment.getByRole('button', { name: label, exact: true }).boundingBox()));
    const touchStatus = await touchAssessment.getByRole('status').boundingBox();
    assert(touchStatus && touchVerdicts[0] && touchVerdicts[1] && touchVerdicts.every(box => box.width >= 44 && box.height >= 44), `coarse-pointer assessment controls meet 44px: ${JSON.stringify({ touchStatus, touchVerdicts })}`);
    assert(Math.abs(touchVerdicts[0].y - touchVerdicts[1].y) < 1 && touchStatus.y < touchVerdicts[0].y, 'phone status sits above the paired accept/reject choices');
    const touchHeader = touchReview.locator('.panel .head');
    const titleBox = await touchHeader.getByRole('heading', { name: task.title, exact: true }).boundingBox();
    const reviewBox = await touchHeader.getByRole('link', { name: 'Review changes', exact: true }).boundingBox();
    const archiveBox = await touchHeader.getByRole('button', { name: 'Archive task', exact: true }).boundingBox();
    const editBox = await touchHeader.getByRole('button', { name: 'Edit task', exact: true }).boundingBox();
    assert(titleBox && reviewBox && archiveBox && editBox && titleBox.y < reviewBox.y && reviewBox.y + reviewBox.height <= archiveBox.y && reviewBox.y + reviewBox.height <= editBox.y, 'phone task title and review decision precede Archive and Edit');
    assert(archiveBox.height >= 44 && editBox.height >= 44, 'secondary task actions retain 44px touch targets');

    const tabs = touchReview.locator('.panel .tabs');
    const tabButtons = tabs.getByRole('tab');
    const tabBoxes = await tabButtons.evaluateAll(elements => elements.map(element => {
      const rect = element.getBoundingClientRect();
      return { y: rect.y, height: rect.height };
    }));
    assert.equal(tabBoxes.length, 5, 'the task view chooser retains all five labeled destinations');
    assert(tabBoxes.every(box => box.height >= 44 && Math.abs(box.y - tabBoxes[0].y) < 1), `phone task destinations form one touch-sized row: ${JSON.stringify(tabBoxes)}`);
    await tabButtons.last().focus();
    const tabScroll = await tabs.evaluate(element => ({ left: element.scrollLeft, width: element.clientWidth, total: element.scrollWidth }));
    assert.equal(await tabButtons.last().evaluate(element => element === document.activeElement), true, 'the final task destination remains keyboard focusable');
    assert(tabScroll.total > tabScroll.width && tabScroll.left > 0, `keyboard focus reveals the later task destinations in the horizontal row: ${JSON.stringify(tabScroll)}`);
    for (const theme of ['light', 'dark']) {
      await touchReview.evaluate(value => document.documentElement.dataset.theme = value, theme);
      await touchReview.screenshot({ path: path.join(artifacts, `seeded-task-review-touch-375x812-${theme}.png`), fullPage: true });
    }
    await touchReviewContext.close();

    const assessmentRoute = `**/api/projects/${projectId}/runs/${run.id}/assessment`;
    let releaseFailure;
    let signalPending;
    const pending = new Promise(resolve => { signalPending = resolve; });
    await page.route(assessmentRoute, route => {
      signalPending();
      releaseFailure = () => route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'controlled_failure', message: 'Controlled assessment failure. Try again.' } }),
      });
    });
    const response = page.waitForResponse(item => item.url().includes(`/api/projects/${projectId}/runs/${run.id}/assessment`)).then(value => ({ value }), error => ({ error }));
    const accepting = assessment.getByRole('button', { name: 'Accept work', exact: true }).click();
    await pending;
    assert.equal(await assessment.getByRole('button', { name: 'Accept work', exact: true }).isDisabled(), true, 'the assessment stays pending until acknowledgment');
    assert.equal((await assessment.getByRole('status').innerText()).trim(), 'Saving assessment…', 'pending status is explicit and does not claim acknowledgment');
    await releaseFailure();
    await accepting;
    const assessmentResponse = await response;
    assert(assessmentResponse.value && assessmentResponse.value.status() === 503, `the controlled failure reaches the decision: ${assessmentResponse.error || ''}`);
    await decision.getByText('Controlled assessment failure. Try again.', { exact: true }).waitFor();
    assert(await assessment.getByText('Not recorded', { exact: false }).isVisible(), 'a failed assessment remains unacknowledged');
    await captureMatrix(page, 'seeded-task-review-assessment-error');
    await page.unroute(assessmentRoute);
    await assessment.getByRole('button', { name: 'Accept work', exact: true }).click();
    await assessment.getByText('Accepted', { exact: true }).waitFor();
    assert.equal((await api('GET', `/api/projects/${projectId}/tasks`)).tasks.find(item => item.id === task.id).state, 'review', 'recording a verdict does not move the task');
    await captureMatrix(page, 'seeded-task-review-accepted');

    const result = run.handoff.results || run.handoff.summary;
    const href = await reviewLink.getAttribute('href');
    assert(href.includes(`task=${task.id}`) && href.includes(`run=${run.id}`), 'Review changes carries the exact task/run origin');
    await reviewLink.click();
    await page.waitForFunction(() => location.hash.includes('/git/branch/local/'));
    const reviewContext = page.getByRole('region', { name: 'Review from task', exact: true });
    await reviewContext.getByText(result, { exact: true }).waitFor();
    if (run.handoff.tests.length) {
      await reviewContext.getByText('Agent-reported tests · not independently verified', { exact: true }).waitFor();
    } else {
      await reviewContext.getByText('No test notes reported', { exact: true }).waitFor();
    }
    assert((await reviewContext.getByRole('link', { name: new RegExp(`Back to ${task.title}`) }).getAttribute('href')).includes(`/task/${task.id}`), 'the result context has a direct return to the task');
    assert((await reviewContext.getByRole('link', { name: new RegExp(`Back to ${task.title}`) }).getAttribute('href')).includes(`run=${run.id}`), 'the return link preserves the exact run');
    assert.equal(await reviewContext.getByRole('link', { name: 'Full run and Git evidence', exact: true }).count(), 1, 'full evidence remains available at the task');
    assert((await reviewContext.getByRole('link', { name: 'Full run and Git evidence', exact: true }).getAttribute('href')).includes(`run=${run.id}`), 'full evidence opens the associated run');
    await captureMatrix(page, 'seeded-task-owned-branch');

    let currentTask = (await api('GET', `/api/projects/${projectId}/tasks`)).tasks.find(item => item.id === task.id);
    await api('PATCH', `/api/projects/${projectId}/tasks/${task.id}`, { version: currentTask.version, state: 'done' });
    await page.waitForFunction(() => !document.querySelector('[aria-label="Review from task"]'));
    currentTask = (await api('GET', `/api/projects/${projectId}/tasks`)).tasks.find(item => item.id === task.id);
    await api('PATCH', `/api/projects/${projectId}/tasks/${task.id}`, { version: currentTask.version, state: 'review' });
    await reviewContext.waitFor();
    await reviewContext.getByText(result, { exact: true }).waitFor();

    const file = page.locator('.g-file').first();
    await file.waitFor();
    const fileHref = await file.getAttribute('href');
    assert(fileHref.includes(`task=${task.id}`) && fileHref.includes(`run=${run.id}`), 'file drill-down retains the exact task/run origin');
    await file.click();
    await page.getByRole('region', { name: 'Review from task', exact: true }).getByText(result, { exact: true }).waitFor();
    await captureMatrix(page, 'seeded-task-owned-file');
    await page.getByRole('link', { name: `← ${branch.name}`, exact: true }).click();
    await reviewContext.waitFor();

    const actions = page.locator('.branch-actions');
    const summary = actions.locator(':scope > summary');
    assert.equal(await actions.getAttribute('open'), null, 'secondary branch actions start closed');
    await captureMatrix(page, 'seeded-branch-actions-closed');
    await summary.focus();
    assert.equal(await summary.evaluate(element => element === document.activeElement), true, 'branch actions can receive keyboard focus');
    await page.keyboard.press('Enter');
    const actionGroups = actions.locator(':scope > .branch-action-list > details.branch-action-group');
    assert.equal(await actionGroups.count(), 2, 'repository mutations and task/history navigation are separate choices');
    const repositoryActions = actionGroups.nth(0);
    const taskHistory = actionGroups.nth(1);
    assert.equal(await repositoryActions.locator('summary').innerText(), 'Repository actions');
    assert.equal(await taskHistory.locator('summary').innerText(), 'Task and history');
    assert.equal(await repositoryActions.getAttribute('open'), null, 'repository operations remain collapsed after opening the parent group');
    assert.equal(await taskHistory.getAttribute('open'), null, 'task navigation remains collapsed after opening the parent group');
    assert.equal(await actions.getByRole('button', { name: 'Open task', exact: true }).count(), 0, 'task destination has no inert duplicate button');
    await captureMatrix(page, 'seeded-branch-actions-open');
    await repositoryActions.locator('summary').focus();
    await page.keyboard.press('Enter');
    assert(await actions.getByRole('button', { name: 'Push', exact: true }).isVisible(), 'keyboard opens repository operations');
    await taskHistory.locator('summary').focus();
    await page.keyboard.press('Enter');
    assert.equal(await actions.getByRole('link', { name: 'Open task', exact: true }).count(), 1, 'the task destination appears once');
    assert.equal(await actions.getByRole('button', { name: 'Open run', exact: true }).count(), 0, 'run destination has no inert duplicate button');
    assert.equal(await actions.getByRole('link', { name: 'Open run', exact: true }).count(), 1, 'the run destination appears once');
    assert((await actions.getByRole('link', { name: 'Open run', exact: true }).getAttribute('href')).includes(`run=${run.id}`), 'Open run selects the branch-associated run');

    await page.emulateMedia({ reducedMotion: 'reduce' });
    assert.equal(await summary.evaluate(element => getComputedStyle(element, '::before').transitionDuration), '0s', 'the disclosure honors reduced motion');
    const changedFiles = page.getByRole('heading', { name: 'Changed files', exact: true });
    const beforeJump = await changedFiles.boundingBox();
    await page.getByRole('button', { name: 'Review changes', exact: true }).click();
    const filesHeading = await changedFiles.boundingBox();
    const jumpMetrics = await page.evaluate(() => {
      const main = document.querySelector('main');
      const files = document.getElementById('files');
      return {
        windowY: window.scrollY,
        documentHeight: document.scrollingElement?.scrollHeight,
        mainTop: main?.scrollTop,
        mainHeight: main?.scrollHeight,
        mainClientHeight: main?.clientHeight,
        mainOverflow: main && getComputedStyle(main).overflowY,
        filesTop: files?.getBoundingClientRect().top,
      };
    });
    console.log('Review changes scroll metrics:', JSON.stringify({ filesHeading, ...jumpMetrics }));
    const tabbar = await page.getByRole('navigation', { name: 'Primary', exact: true }).boundingBox();
    assert(beforeJump && filesHeading && tabbar && filesHeading.y < beforeJump.y && filesHeading.y + filesHeading.height <= tabbar.y, `Review changes moves Changed files into view above phone navigation with reduced motion: ${JSON.stringify({ beforeJump, filesHeading, tabbar })}`);

    await repositoryActions.getByRole('button', { name: 'Merge…', exact: true }).click();
    const merge = page.getByRole('dialog', { name: /Merge / });
    await merge.getByText(/Merge .* into main, on this computer only/).waitFor();
    assert(await merge.getByText(/Nothing is pushed/).isVisible(), 'merge preflight states its repository effect');
    await captureMatrix(page, 'seeded-branch-merge-preflight');
    await merge.getByRole('button', { name: 'Cancel', exact: true }).click();

    const exactRunEvents = page.waitForRequest(request => request.url().includes(`/api/projects/${projectId}/runs/${run.id}/events`));
    await actions.getByRole('link', { name: 'Open run', exact: true }).click();
    await page.waitForFunction(id => location.hash.includes(`?run=${id}`), run.id);
    await exactRunEvents;
    await page.getByRole('heading', { name: task.title, exact: true }).waitFor();
    assert.equal(await page.evaluate(() => new URLSearchParams(location.hash.split('?')[1]).get('run')), run.id, 'Open run selects the branch-associated run in the task page');

    await page.goto(`${base}/#/p/${projectId}/git/branch/local/${encodeURIComponent(branch.name)}`);
    await page.getByRole('heading', { name: branch.name, exact: true }).waitFor();
    assert.equal(await page.getByRole('region', { name: 'Review from task', exact: true }).count(), 0, 'direct navigation has no task context');
    await captureMatrix(page, 'seeded-direct-branch');
    await page.goto(`${base}/#/p/${projectId}/git/branch/local/main`);
    await page.getByRole('heading', { name: 'main', exact: true }).waitFor();
    assert.equal(await page.getByRole('region', { name: 'Review from task', exact: true }).count(), 0, 'sparse direct navigation has no task context');
    await captureMatrix(page, 'seeded-sparse-main-branch');

    const touchBranchContext = await browser.newContext({ viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true, storageState: await context.storageState() });
    const touchBranch = await touchBranchContext.newPage();
    await touchBranch.goto(`${base}/#/p/${projectId}/git/branch/local/${encodeURIComponent(branch.name)}`);
    await touchBranch.locator('.branch-actions > summary').tap();
    await touchBranch.locator('.branch-action-group').first().locator('summary').tap();
    assert.equal(await touchBranch.evaluate(() => matchMedia('(pointer: coarse)').matches), true, 'branch action check uses coarse-pointer emulation');
    const touchPush = await touchBranch.getByRole('button', { name: 'Push', exact: true }).boundingBox();
    assert(touchPush && touchPush.width >= 44 && touchPush.height >= 44, `coarse-pointer Push target meets 44px: ${JSON.stringify(touchPush)}`);
    await touchBranchContext.close();

    await context.close();
    assert.deepEqual(errors, [], `review pages report no JavaScript errors: ${errors.join('; ')}`);
    console.log(`PASS seeded review flow: ${task.id} → ${run.id} → ${branch.name}`);
    console.log('PASS verdict discovery, pending/failure/recovery, live context invalidation, reported tests, task return, direct/sparse Git, file drill-down, keyboard/coarse pointer, reduced motion, merge preflight/cancel');
    console.log(`Screenshots: ${artifacts}`);
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error.stack || error); process.exitCode = 1; });
