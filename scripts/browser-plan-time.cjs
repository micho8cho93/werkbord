// Planning a task for a day and a time. A plan is for people: it shows in the project Calendar for human work as well as agent work,
// it keeps its time of day, and it never starts an agent unless the person says so (the choice in the editor, or the offer after
// a placement). Undated work waits in the No date tray and is planned by clicking a time, with no agent started.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const base = process.env.PERSONAL_BROWSER_URL;
const owner = 'disposable-browser-credential';
async function call(method, route, body, ok = true) {
  const response = await fetch(base + '/api' + route, { method, headers: { Authorization: 'Bearer ' + owner, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const raw = await response.text();
  if (ok) assert(response.ok, `${method} ${route}: ${response.status} ${raw}`);
  return { status: response.status, body: raw ? JSON.parse(raw) : null };
}
const wait = async (check, what) => { for (let i = 0; i < 100; i++) { if (await check()) return; await new Promise(r => setTimeout(r, 100)); } assert.fail('timed out: ' + what); };
const dayOf = (offset) => { const d = new Date(); d.setUTCDate(d.getUTCDate() + offset); return d.toISOString().slice(0, 10); };

(async () => {
  let project = (await call('GET', '/projects')).body.projects.find(p => p.repoPath === fs.realpathSync(process.env.WERKBORD_BROWSER_REPO));
  if (!project) project = (await call('POST', '/projects', { path: fs.realpathSync(process.env.WERKBORD_BROWSER_REPO), name: 'Plan time fixture' })).body;
  const pid = project.id;
  await call('POST', '/onboarding/complete', {}); // setup and the first-visit tour are not what is tested here
  await call('POST', '/onboarding/tours/seen', { surface: 'desktop' });
  const task = async (title, extra = {}) => (await call('POST', `/projects/${pid}/tasks`, { title, ...extra })).body;
  const read = async (id) => (await call('GET', `/projects/${pid}/tasks`)).body.tasks.find(t => t.id === id);
  const human = await task('Write the release notes', { workMode: 'human' });
  const agent = await task('Tidy the sync client');

  // A time needs one day: the server refuses a range with a time, and a time with no day.
  const [a, b] = [dayOf(1), dayOf(3)];
  assert.equal((await call('PATCH', `/projects/${pid}/tasks/${human.id}`, { version: human.version, plan: { start: a, end: b, time: '09:30' } }, false)).status, 400, 'a time on a range is refused');
  assert.equal((await call('PATCH', `/projects/${pid}/tasks/${human.id}`, { version: human.version, plan: { time: '09:30' } }, false)).status, 400, 'a time with no day is refused');
  assert.equal((await call('PATCH', `/projects/${pid}/tasks/${human.id}`, { version: human.version, plan: { start: a, time: 'noon' } }, false)).status, 400, 'a time that is not HH:MM is refused');

  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block' });
  await ctx.addInitScript(token => { localStorage.setItem('werkbord.token', token); localStorage.setItem('werkbord-tour-desktop-v2', 'fixture'); }, owner);
  const page = await ctx.newPage(); page.setDefaultTimeout(10000);
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  try {
    await page.goto(`${base}/#/p/${pid}/calendar`);
    // Both kinds of work wait in the tray, human work too.
    const tray = page.locator('aside[aria-label="Planning"]');
    await tray.getByRole('heading', { name: /No date/ }).waitFor();
    await tray.getByText(human.title, { exact: true }).waitFor();
    await tray.getByText(agent.title, { exact: true }).waitFor();

    // Click a task, click a time: it is planned, nothing starts, and the agent start is offered, not taken.
    await tray.getByRole('button', { name: new RegExp(agent.title) }).click();
    const today = new Date(); const localDay = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`;
    const column = page.locator(`.dcol[data-plan-day="${localDay}"]`);
    await column.waitFor();
    const box = await column.boundingBox();
    await page.mouse.click(box.x + box.width / 2, box.y + 11 * 48 + 10); // 11:00 on a 48px hour
    await wait(async () => (await read(agent.id)).plan?.time, 'the plan is saved with a time').catch(async (e) => {
      console.error('page status:', await page.getByRole('status').allTextContents(), 'alerts:', await page.getByRole('alert').allTextContents());
      await page.screenshot({ path: (process.env.BROWSER_ARTIFACT_DIR || '/tmp') + '/plan-time-fail.png' }); throw e;
    });
    const placed = await read(agent.id);
    assert.equal(placed.plan.start, localDay);
    assert.match(placed.plan.time, /^1[01]:\d\d$/);
    assert(!placed.orchestration?.enabled && !placed.orchestration?.scheduledAt, 'planning did not schedule an agent');
    await page.locator('.ev.plan', { hasText: agent.title }).waitFor();
    await page.getByRole('status').getByText('No agent starts.').waitFor();
    await page.screenshot({ path: (process.env.BROWSER_ARTIFACT_DIR || '/tmp') + '/calendar-planned-time.png' });

    // The offer starts an agent only when taken.
    await page.getByRole('button', { name: 'Also start the agent then' }).click();
    await wait(async () => (await read(agent.id)).orchestration?.enabled, 'taking the offer schedules the agent');
    assert.equal((await read(agent.id)).plan.time, placed.plan.time, 'the plan keeps its time');

    // The editor: a time with a date, and the agent start is its own unticked choice.
    await call('PATCH', `/projects/${pid}/tasks/${human.id}`, { version: (await read(human.id)).version, plan: { start: localDay, time: '15:00' } });
    await page.reload();
    await page.locator('.ev.plan', { hasText: human.title }).click();
    const dialog = page.getByRole('dialog', { name: 'Edit task' });
    await dialog.getByLabel(/^Time/).waitFor();
    assert.equal(await dialog.getByLabel(/^Time/).inputValue(), '15:00');
    assert.equal(await dialog.getByTestId('start-agent-at-plan').count(), 0, 'human work has no agent to start');
    await dialog.getByLabel(/^Time/).fill('16:30');
    await dialog.getByRole('button', { name: /^Save/ }).click();
    await wait(async () => (await read(human.id)).plan.time === '16:30', 'editing the time saves it');

    // Dates across several days drop the time rather than keep one that means nothing.
    await dialog.waitFor({ state: 'detached' });
    assert.deepEqual(errors, [], 'no page errors');
    console.log('PASS plan time: human and agent work planned for a time, shown in the Calendar, never starting an agent unless chosen');
  } catch (e) { await page.screenshot({ path: (process.env.BROWSER_ARTIFACT_DIR || '/tmp') + '/plan-time-failure.png' }).catch(() => {}); throw e; } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exit(1); });
