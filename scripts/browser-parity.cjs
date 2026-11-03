// Parity between the Mac window page (the built shell, web/shell, with the web app in its frame) and the web app opened on its own, both
// from the same build and the same fixture. For each screen it records what a person can see (headings, navigation, tabs, buttons and
// links, fields, landmarks) on both sides, takes a screenshot of each, and reports what is on one side only. Run through the harness:
//
//   make test-parity                                    (builds first; writes the report and screenshots to output/parity)
//   BROWSER_SCRIPT=scripts/browser-parity.cjs node scripts/test-browser.cjs        (the same, on the last build)
//
// Environment: PARITY_ONLY=work,settings limits the screens; PARITY_OUT=dir moves the report; PARITY_THEME=dark checks dark;
// PARITY_STRICT=1 makes any difference that is not in scripts/parity-allowed.json an error (that is what make check will run).
const fs = require('node:fs'), path = require('node:path'), http = require('node:http');
const root = path.resolve(__dirname, '..');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || path.join(root, 'web/node_modules/playwright'));
const personalURL = process.env.PERSONAL_BROWSER_URL, shellPort = Number(new URL(process.env.PLANNING_SHELL_URL).port);
const token = 'disposable-browser-credential';
const out = process.env.PARITY_OUT || path.join(root, 'output', 'parity');
const only = (process.env.PARITY_ONLY || '').split(',').filter(Boolean);
const theme = process.env.PARITY_THEME || '';
const strict = process.env.PARITY_STRICT === '1';
const sleep = ms => new Promise(r => setTimeout(r, ms));
const day = n => { const d = new Date(); d.setDate(d.getDate() + n); return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0'); };
const api = async (method, route, body) => {
  const r = await fetch(personalURL + '/api' + route, { method, headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const t = await r.text(); if (!r.ok) throw Error(method + route + ' ' + r.status + ' ' + t); return t ? JSON.parse(t) : null;
};

// What the native app tells the shell page: the shape the shell asks for, with the fixture behind it.
const shellInit = ({ items, personalURL, token, theme }) => {
  localStorage.setItem('werkbord.token', token);
  if (theme) localStorage.setItem('werkbord.theme', theme);
  if (window.parent !== window) return;
  const load = async id => ({ id, kind: 'personal', url: personalURL, origin: personalURL });
  window.go = { main: { App: {
    Premium: async () => ({ active: false, canCheck: false, buyUrl: '' }), Info: async () => ({ version: 'fixture', platform: 'darwin' }),
    Workspaces: async () => ({ items, selected: 'personal', problems: [], team: { state: 'ready' }, teamInstaller: false, invitation: false }),
    OpenWorkspace: load, LoadWorkspace: load, Overview: async () => fetch('/fixture-overview').then(r => r.json()),
    AssistantWorkspaces: async () => [], RememberPlace: async () => {}, Relay: async () => ({ version: 'fixture', platform: 'darwin' }), PendingInvitation: async () => '', OpenExternal: async () => null,
  } } };
  window.runtime = { EventsOn: () => () => {} };
};
const items = [{ id: 'personal', kind: 'personal', name: 'Individual', state: 'ready', source: 'Fixture' }];

// ---- what a page shows ----
// Runs inside a document. Every visible heading, landmark, tab, control and field becomes one "kind: text" line, so two pages can be
// compared as sets whatever their markup. Numbers are written as # (the same fixture gives the same numbers, but a clock does not).
function collect() {
  const seen = new Set(), lines = [];
  const norm = s => (s || '').replace(/\s+/g, ' ').trim().replace(/\d+/g, '#').slice(0, 90);
  const shown = el => {
    if (el.closest('[hidden], [inert], .sr-only, [aria-hidden="true"]')) return false;
    const r = el.getBoundingClientRect(); if (r.width < 2 || r.height < 2) return false;
    const s = getComputedStyle(el); return s.visibility !== 'hidden' && s.display !== 'none' && s.opacity !== '0';
  };
  const add = (kind, text) => { text = norm(text); if (!text) return; const k = kind + ': ' + text; if (!seen.has(k)) { seen.add(k); lines.push(k); } };
  const name = el => el.getAttribute('aria-label') || el.textContent;
  for (const el of document.querySelectorAll('h1,h2,h3,h4,[role=heading]')) if (shown(el)) add('heading', el.textContent);
  for (const el of document.querySelectorAll('nav,aside,[role=tablist],[role=navigation],[role=region][aria-label],main,header,footer,dialog,[role=dialog]')) if (shown(el)) add('landmark', (el.getAttribute('role') || el.tagName.toLowerCase()) + ' ' + (el.getAttribute('aria-label') || ''));
  for (const el of document.querySelectorAll('[role=tab]')) if (shown(el)) add('tab', name(el));
  for (const el of document.querySelectorAll('button,a[href],[role=button],summary')) {
    if (!shown(el) || el.matches('[role=tab]')) continue;
    add(el.closest('nav,aside,[role=tablist]') ? 'nav' : el.tagName === 'A' ? 'link' : 'button', name(el));
  }
  for (const el of document.querySelectorAll('input:not([type=hidden]),select,textarea')) if (shown(el)) add('field', el.getAttribute('aria-label') || el.getAttribute('placeholder') || el.labels?.[0]?.textContent || el.name || el.type);
  return lines;
}
// The shell page has the web app in a frame: both documents are one screen to a person.
async function read(page) {
  const all = [];
  for (const f of page.frames()) { try { all.push(...await f.evaluate(collect)); } catch (_) { /* a frame that is going away */ } }
  return [...new Set(all)];
}

// ---- the screens: how each side gets there ----
// shell: what to do in the shell page. web: the address in the standalone app. A side that is null has no such screen (reported as that).
let P;
const screens = () => [
  { id: 'work', title: 'Work (all projects)', shell: async p => { await sidebar(p, 'Work'); }, web: '#/control' },
  { id: 'project-work', title: 'A project’s Work', shell: async p => { await pickProject(p); await sidebar(p, 'Work'); }, web: `#/p/${P.id}/work` },
  { id: 'calendar', title: 'Calendar', shell: async p => { await sidebar(p, 'Calendar'); }, web: null, note: 'The web app has no Calendar page: a project\u2019s calendar was retired and the window\u2019s Calendar is its own (web/src/shell/Calendar.svelte). Phase 4 decides which one stays' },
  { id: 'git', title: 'Git', shell: async p => { await pickProject(p); await sidebar(p, 'Git'); }, web: `#/p/${P.id}/git` },
  { id: 'agents', title: 'Agents', shell: async p => { await sidebar(p, 'Agents'); }, web: '#/sessions' },
  { id: 'analytics', title: 'Analytics', shell: async p => { await sidebar(p, 'Analytics'); }, web: '#/analytics' },
  { id: 'reviews', title: 'Reviews', skip: 'Reviews is a Team page and the fixture has no Team workspace; Individual has none. Compared once a Team fixture is part of the run' },
  { id: 'settings', title: 'Settings', shell: async p => { await p.getByRole('button', { name: 'Settings', exact: true }).first().click(); }, web: '#/settings' },
  { id: 'help', title: 'Help', shell: async p => { await p.getByRole('button', { name: /^Help/ }).first().click(); }, web: '#/help' },
];
async function sidebar(p, label) { await p.locator('nav[aria-label="Werkbord"]').getByRole('button', { name: label, exact: true }).first().click(); }
async function pickProject(p) {
  await p.getByTestId('project-context').click();
  await p.getByTestId('project-list').getByRole('option').filter({ hasText: P.name }).first().click();
}

// ---- the data both sides read: a project with queued, planned and running work, so no screen is only an empty state ----
async function seed() {
  await api('POST', '/onboarding/complete', {});
  for (const surface of ['desktop', 'settings', 'analytics', 'phone']) await api('POST', '/onboarding/tours/seen', { surface }).catch(() => {});
  P = await api('POST', '/projects', { path: fs.realpathSync(process.env.WERKBORD_BROWSER_REPO), name: 'Billing app' });
  const tasks = {};
  const mk = async (key, title, extra = {}) => { tasks[key] = await api('POST', `/projects/${P.id}/tasks`, { title, description: 'What the agent should change, and why it matters.', execution: { agent: 'fixture', ...(extra.execution || {}) }, ...Object.fromEntries(Object.entries(extra).filter(([k]) => k !== 'execution')) }); };
  await mk('running', 'Add retry to the sync client');
  await mk('fix', 'Fix login redirect', { execution: { priority: 'high' } });
  await mk('copy', 'Update onboarding copy');
  await mk('docs', 'Document release checklist', { workMode: 'human' });
  await mk('invoice', 'Refactor invoice export');
  await mk('later', 'Migrate the runs table', { execution: { priority: 'low' } });
  await mk('p1', 'Write the launch announcement', { workMode: 'human' });
  await mk('p2', 'Prepare the demo environment', { workMode: 'human' });
  await api('POST', `/projects/${P.id}/work-plans`, { title: 'Launch', description: 'Ship it', taskIds: [tasks.p1.id, tasks.p2.id], decisions: '', questions: '' });
  const plan = (key, p) => api('PATCH', `/projects/${P.id}/tasks/${tasks[key].id}`, { version: tasks[key].version, plan: p });
  await plan('p1', { start: day(0), time: '09:30' });
  await plan('p2', { start: day(0), time: '15:00' });
  await plan('fix', { start: day(1), time: '11:00' });
  // Separate file scopes, so more than one agent can work at once.
  { const list = (await api('GET', `/projects/${P.id}/tasks`)).tasks; let n = 0;
    for (const k of ['running', 'copy', 'invoice']) { const t = list.find(x => x.id === tasks[k].id); tasks[k] = await api('PATCH', `/projects/${P.id}/tasks/${t.id}`, { version: t.version, orchestration: { enabled: false, dependencies: [], expectedPaths: ['src/area' + (++n) + '/'] } }); } }
  await api('PUT', `/projects/${P.id}/orchestration`, { concurrencyLimit: 4 }).catch(() => {});
  { const rs = await api('GET', '/runners'); for (const r of (rs.runners || rs)) await api('PUT', `/runners/${r.id}`, { name: r.name, capacity: 4, automatic: r.automatic, disabled: r.disabled, projects: r.projects, allowClone: r.allowClone }).catch(() => {}); }
  for (const k of ['running', 'copy']) await api('POST', `/projects/${P.id}/tasks/${tasks[k].id}/runs`, { agentId: 'fixture' }).catch(e => console.log('run failed', k, String(e).slice(0, 200)));
  await sleep(1500);
}

// ---- running ----
const KINDS = ['landmark', 'nav', 'heading', 'tab', 'button', 'link', 'field'];
const label = l => l.replace(/^(\w+): .*/, '$1');
function diff(a, b) {
  const A = new Set(a), B = new Set(b);
  return { onlyWindow: a.filter(x => !B.has(x)), onlyWeb: b.filter(x => !A.has(x)), shared: a.filter(x => B.has(x)).length };
}
const allowedFile = path.join(__dirname, 'parity-allowed.json');
const allowed = fs.existsSync(allowedFile) ? JSON.parse(fs.readFileSync(allowedFile, 'utf8')) : {};
const isAllowed = (screen, side, line) => (allowed[screen] || []).concat(allowed['*'] || []).some(r => r.side === side && new RegExp(r.match).test(line));

(async () => {
  fs.mkdirSync(out, { recursive: true });
  await seed();
  const server = http.createServer(async (req, res) => {
    const shellRoot = path.join(root, 'desktop/frontend/dist/shell');
    const file = path.join(shellRoot, decodeURIComponent(req.url.split('?')[0]));
    if (req.url === '/fixture-overview') {
      const summary = await api('GET', '/workspace/v1/summary');
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ entries: [{ workspace: items[0], summary }], at: new Date().toISOString() })); return;
    }
    if (!file.startsWith(shellRoot) || !fs.existsSync(file) || !fs.statSync(file).isFile()) { res.writeHead(404); res.end(); return; }
    const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.png': 'image/png' };
    res.writeHead(200, { 'Content-Type': types[path.extname(file)] || 'application/octet-stream' }); fs.createReadStream(file).pipe(res);
  });
  await new Promise(r => server.listen(shellPort, '127.0.0.1', r));
  const browser = await chromium.launch({ headless: true });
  const report = [];
  try {
    for (const s of screens()) {
      if (only.length && !only.includes(s.id)) continue;
      const row = { id: s.id, title: s.title, note: s.note || '', window: [], web: [] };
      if (s.skip) { row.skipped = s.skip; row.window = []; row.web = []; row.diff = { onlyWindow: [], onlyWeb: [], shared: 0 }; report.push(row); console.log(`SKIP ${s.id}: ${s.skip}`); continue; }
      for (const side of ['window', 'web']) {
        const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, serviceWorkers: 'block' });
        await ctx.addInitScript(({ token, theme }) => { localStorage.setItem('werkbord.token', token); if (theme) localStorage.setItem('werkbord.theme', theme); }, { token, theme });
        if (side === 'window') await ctx.addInitScript(shellInit, { items, personalURL, token, theme });
        const page = await ctx.newPage(); page.setDefaultTimeout(8000);
        try {
          if (side === 'window') {
            await page.goto('http://127.0.0.1:' + shellPort + '/shell/index.html');
            await page.locator('nav[aria-label="Werkbord"]').waitFor();
            await s.shell(page);
          } else {
            if (s.web === null) { row.webMissing = true; continue; }
            await page.goto(personalURL + '/' + s.web);
            await page.locator('main, [role=main], h1').first().waitFor();
          }
          await sleep(1200);
          row[side] = await read(page);
          await page.screenshot({ path: path.join(out, `${s.id}-${side}.png`) });
        } catch (e) { row[side + 'Error'] = String(e.message).split('\n')[0]; row[side] = row[side] || []; }
        finally { await ctx.close(); }
        row[side] ||= [];
      }
      row.diff = diff(row.window, row.web);
      report.push(row);
      console.log(`${row.windowError || row.webError ? 'ERR ' : row.diff.onlyWindow.length + row.diff.onlyWeb.length ? 'DIFF' : 'SAME'} ${s.id}: ${row.diff.shared} shared, ${row.diff.onlyWindow.length} only in window, ${row.diff.onlyWeb.length} only in web${row.windowError ? ' [window: ' + row.windowError + ']' : ''}${row.webError ? ' [web: ' + row.webError + ']' : ''}`);
    }
  } finally { await browser.close(); server.close(); }

  // The report: one section per screen, differences grouped by kind, so the list of work reads from the top.
  const md = ['# Parity: Mac window page vs web app', '', `Same build, same fixture, theme ${theme || 'light'}. "Window" is the shell page with the web app in its frame; "web" is the web app on its own.`, '', '| Screen | Shared | Only in window | Only in web | Not allowed |', '|---|---:|---:|---:|---:|'];
  let unexpected = 0;
  for (const r of report) {
    r.open = { window: r.diff.onlyWindow.filter(l => !isAllowed(r.id, 'window', l)), web: r.diff.onlyWeb.filter(l => !isAllowed(r.id, 'web', l)) };
    if (r.skipped) { md.push(`| ${r.title} | skipped | | | |`); continue; }
    unexpected += r.open.window.length + r.open.web.length + (r.windowError ? 1 : 0) + (r.webError ? 1 : 0) + (r.webMissing && !isAllowed(r.id, 'web', 'missing') ? 1 : 0);
    md.push(`| ${r.title} | ${r.webMissing ? 'no web page' : r.diff.shared} | ${r.diff.onlyWindow.length} | ${r.webMissing ? '-' : r.diff.onlyWeb.length} | ${r.open.window.length + r.open.web.length + (r.webMissing && !isAllowed(r.id, 'web', 'missing') ? 1 : 0)} |`);
  }
  for (const r of report) {
    if (r.skipped) { md.push('', `## ${r.title}`, '', `Skipped: ${r.skipped}.`); continue; }
    md.push('', `## ${r.title}`, '', `Screenshots: \`${r.id}-window.png\`, \`${r.id}-web.png\`.${r.note ? ' ' + r.note + '.' : ''}`);
    if (r.webMissing) md.push('', '- The web app has no page for this screen, so everything the window shows is listed as only in the window.');
    if (r.windowError) md.push('', `- Window could not be read: ${r.windowError}`);
    if (r.webError) md.push('', `- Web could not be read: ${r.webError}`);
    for (const [side, key] of [['Only in the window', 'onlyWindow'], ['Only in the web app', 'onlyWeb']]) {
      const list = r.diff[key].slice().sort((a, b) => KINDS.indexOf(label(a)) - KINDS.indexOf(label(b))); if (!list.length) continue;
      md.push('', `**${side}** (${list.length})`, '');
      for (const l of list) md.push(`- ${isAllowed(r.id, key === 'onlyWindow' ? 'window' : 'web', l) ? '(allowed) ' : ''}${l}`);
    }
  }
  fs.writeFileSync(path.join(out, 'report.md'), md.join('\n') + '\n');
  fs.writeFileSync(path.join(out, 'report.json'), JSON.stringify(report, null, 1));
  console.log(`Parity report: ${path.join(out, 'report.md')} (${unexpected} difference${unexpected === 1 ? '' : 's'} not allowed)`);
  if (strict && unexpected) { console.error('Parity failed: see the report.'); process.exitCode = 1; }
})().catch(e => { console.error(e.stack || e.message); process.exitCode = 1; });
