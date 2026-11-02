// Planning a task for a day and a time. A plan is for people: it shows in the project Calendar for human work as well as agent work,
// it keeps its time of day, and it never starts an agent unless the person says so (the choice in the editor, or the offer after
// a placement). Undated work waits in the No date tray and is planned by clicking a time, with no agent started.
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

  // A plan for one day keeps its time, for human and agent work alike, and never schedules an agent.
  // The project Calendar page this once drove in the browser is gone (the shell Calendar is covered by browser-brief).
  const placed = (await call('PATCH', `/projects/${pid}/tasks/${agent.id}`, { version: agent.version, plan: { start: a, time: '11:00' } })).body;
  assert.equal(placed.plan.start, a);
  assert.equal(placed.plan.time, '11:00');
  assert(!placed.orchestration?.enabled && !placed.orchestration?.scheduledAt, 'planning did not schedule an agent');
  const timed = (await call('PATCH', `/projects/${pid}/tasks/${human.id}`, { version: human.version, plan: { start: a, time: '15:00' } })).body;
  assert.equal(timed.plan.time, '15:00');
  const moved = (await call('PATCH', `/projects/${pid}/tasks/${human.id}`, { version: timed.version, plan: { start: a, end: b } })).body;
  assert(!moved.plan.time, 'dates across several days drop the time rather than keep one that means nothing');
  console.log('PASS plan time: human and agent work planned for a time, never starting an agent unless chosen');
})().catch(e => { console.error(e); process.exit(1); });
