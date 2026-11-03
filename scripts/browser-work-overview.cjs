// The Work page in the web app: it opens on its Overview (the one the Mac window shows), the queue and plans are its other tab, All projects
// is the same page over every project, and a row dragged to another of Now, Next and Later keeps that priority in its own project.
// Run through the harness: BROWSER_SCRIPT=scripts/browser-work-overview.cjs node scripts/test-browser.cjs   (make test-work-overview-browser)
const fs = require('node:fs'), assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || require('node:path').join(__dirname, '../web/node_modules/playwright'));
const base = process.env.PERSONAL_BROWSER_URL, token = 'disposable-browser-credential';
const sleep = ms => new Promise(r => setTimeout(r, ms));
const api = async (method, route, body) => {
  const r = await fetch(base + '/api' + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const t = await r.text(); if (!r.ok) throw Error(method + route + ' ' + r.status + ' ' + t); return t ? JSON.parse(t) : null;
};
const ok = msg => console.log('PASS ' + msg);

(async () => {
  await api('POST', '/onboarding/complete', {});
  for (const surface of ['desktop', 'settings', 'analytics', 'phone']) await api('POST', '/onboarding/tours/seen', { surface }).catch(() => {});
  const project = await api('POST', '/projects', { path: fs.realpathSync(process.env.WERKBORD_BROWSER_REPO), name: 'Overview fixture' });
  const mk = (title, priority) => api('POST', `/projects/${project.id}/tasks`, { title, description: 'What to change.', execution: { agent: 'fixture', priority } });
  const urgent = await mk('Fix the login redirect', 'high');
  const normal = await mk('Update onboarding copy', 'normal');
  await mk('Migrate the runs table', 'low');
  await api('POST', `/projects/${project.id}/work-plans`, { title: 'Launch', description: '', taskIds: [], decisions: '', questions: '' });

  const browser = await chromium.launch();
  const errors = [];
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block' });
  await ctx.addInitScript(t => localStorage.setItem('werkbord.token', t), token);
  const page = await ctx.newPage(); page.setDefaultTimeout(15000);
  page.on('pageerror', e => errors.push(e.message));
  try {
    // A project's Work opens on Overview.
    await page.goto(base + `/#/p/${project.id}/work`);
    const tabs = page.getByRole('navigation', { name: 'Work sections' });
    await page.getByRole('heading', { name: 'Running now', exact: false }).first().waitFor();
    assert.equal(await tabs.getByRole('link', { name: 'Overview', exact: true }).getAttribute('aria-current'), 'page', 'Work opens on Overview');
    for (const name of [/^Up next/, 'Plans', 'Catch up']) await page.getByRole('heading', { name }).first().waitFor();
    assert.equal(await page.getByRole('heading', { name: 'Now', exact: true }).count(), 0, 'the queue is the other tab');
    await page.getByText('Update onboarding copy').first().waitFor();
    await page.getByPlaceholder('Search work…').fill('nothing matches this');
    await page.getByText('Update onboarding copy').first().waitFor({ state: 'hidden' });
    await page.getByPlaceholder('Search work…').fill('onboarding');
    await page.getByText('Update onboarding copy').first().waitFor();
    ok('a project’s Work opens on Overview, with Up next, Plans and Catch up, and search narrows it');
    await tabs.getByRole('link', { name: /^Queue & Plans/ }).click();
    for (const name of ['Now', 'Next', 'Later']) await page.getByRole('heading', { name, exact: true }).waitFor();
    ok('Queue & Plans is the other tab');

    // All projects: the same page over every project, under the window's header.
    await page.goto(base + '/#/control');
    await page.getByRole('heading', { name: /^Up next/ }).waitFor();
    assert.match(await page.getByRole('navigation', { name: 'Where you are' }).innerText(), /All Projects\s*›\s*Work/);
    await page.getByRole('navigation', { name: 'Work sections' }).getByRole('link', { name: /^Queue & Plans/ }).click();
    for (const name of ['Now', 'Next', 'Later']) await page.getByRole('heading', { name, exact: true }).waitFor();
    assert((await page.locator('[data-order-handle]').count()) >= 3, 'every row has a grip');
    ok('All projects has Overview and Queue & Plans under the shared header, and every row has a grip');

    // Drag "Update onboarding copy" from Next to Later: its own project keeps the new priority.
    const row = page.locator('[data-order-item]').filter({ hasText: 'Update onboarding copy' });
    await row.hover();
    const grip = await row.locator('[data-order-handle]').boundingBox();
    const later = await page.locator('section[data-order-drop="tasks:later"]').boundingBox();
    await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2, { steps: 8 });
    await page.mouse.down(); await sleep(150);
    await page.mouse.move(later.x + later.width / 2, later.y + later.height / 2 + 10, { steps: 24 }); await sleep(600);
    await page.mouse.up();
    const deadline = Date.now() + 15000; let saved;
    while (Date.now() < deadline) {
      saved = (await api('GET', `/projects/${project.id}/tasks`)).tasks.find(t => t.id === normal.id);
      if (saved.execution?.priority === 'low') break; await sleep(300);
    }
    assert.equal(saved.execution?.priority, 'low', 'the task keeps low priority in its own project');
    assert.equal((await api('GET', `/projects/${project.id}/tasks`)).tasks.find(t => t.id === urgent.id).execution?.priority, 'high', 'nothing else moved');
    ok('a row dragged to Later saves low priority in its own project');

    assert.deepEqual(errors, [], 'no page errors');
    console.log('PASS the Work page in the web app: Overview, Queue & Plans, All projects, grips');
  } finally { await browser.close(); }
})().catch(e => { console.error(e.stack || e.message); process.exitCode = 1; });
