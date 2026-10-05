// The Team console. Plain JavaScript, no build step. Every piece of text from the
// server goes in with textContent, never as HTML.
//
// The console only ever talks to the Team server it was loaded from. "Open in my
// runner" gives the member the ticket's context to take into their OWN Werkbord;
// the console never contacts a Werkbord, and nothing here reaches another
// member's computer.
'use strict';

const app = document.getElementById('app');
const state = {
  token: null, me: null, tab: 'projects', projectId: null, ptab: 'board', ticketId: null, secret: null, error: '', info: '',
  invite: null,   // an invite code from the address, waiting to be used
  data: null,     // the open project: its board and what the open tab shows
  handoff: null,  // a handoff the member just opened
};

try {
  const tok = /(?:^|[#&])token=([^&]+)/.exec(location.hash);
  const inv = /(?:^|[#&])invite=([^&]+)/.exec(location.hash);
  if (tok) sessionStorage.setItem('werkbord-team-token', decodeURIComponent(tok[1]));
  if (inv) state.invite = decodeURIComponent(inv[1]);
  if (tok || inv) history.replaceState(null, '', location.pathname); // tokens and codes never stay in the address bar
  state.token = sessionStorage.getItem('werkbord-team-token');
} catch (_) { /* storage can be unavailable; the sign-in form still works for the session */ }

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, '');
    else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) el.append(kid instanceof Node ? kid : document.createTextNode(kid ?? ''));
  return el;
}

async function api(method, path, body, opts) {
  const res = await fetch('/api/team/v1' + path, {
    method,
    headers: { ...(state.token ? { Authorization: 'Bearer ' + state.token } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
    signal: opts && opts.signal,
  });
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({}));
  if (res.status === 401 && state.token) { signOut(); throw new Error('Your token is not valid any more. Sign in again.'); }
  if (!res.ok) throw new Error(data.error ? data.error.message : 'Request failed (' + res.status + ')');
  return data;
}

function can(permission) { return state.me && state.me.permissions.includes(permission); }
function pcan(permission) { return !!(state.data && state.data.board.can.includes(permission)); }

function signOut() {
  try { sessionStorage.removeItem('werkbord-team-token'); } catch (_) {}
  stopSync();
  Object.assign(state, { token: null, me: null, secret: null, data: null, projectId: null, ticketId: null, handoff: null });
  render();
}

async function act(fn) {
  state.error = ''; state.info = '';
  try { await fn(); } catch (e) { state.error = e.message; }
  await render();
}

// ---- small helpers ----

function ago(iso) {
  if (!iso) return '';
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return Math.floor(s / 60) + ' min ago';
  if (s < 86400) return Math.floor(s / 3600) + ' h ago';
  if (s < 86400 * 30) return Math.floor(s / 86400) + ' d ago';
  return new Date(iso).toLocaleDateString();
}
function when(iso) { return h('time', { datetime: iso, title: iso ? new Date(iso).toLocaleString() : '' }, ago(iso)); }

function copy(text, label) {
  return h('button', { class: 'plain', type: 'button', onclick: async (e) => {
    const b = e.currentTarget;
    try { await navigator.clipboard.writeText(text); b.textContent = 'Copied'; }
    catch (_) { b.textContent = 'Copy failed: select the text instead'; }
    setTimeout(() => { b.textContent = label; }, 1800);
  } }, label);
}

function field(label, control) { return h('label', {}, label, control); }
function isHTTPS(u) { return typeof u === 'string' && /^https:\/\//i.test(u); }

// Inputs keep what was typed when the page re-renders because someone else changed something.
// Only fields the person has touched are kept; the rest show the fresh data.
app.addEventListener('input', (e) => { if (e.target && e.target.dataset) e.target.dataset.dirty = '1'; });
function snapshot(root) {
  const keep = {};
  for (const el of root.querySelectorAll('[name]')) if (el.dataset.dirty === '1' || el === document.activeElement) keep[el.name] = { v: el.type === 'checkbox' ? el.checked : el.value, focus: el === document.activeElement, at: el.selectionStart };
  return keep;
}
function restore(root, keep) {
  for (const el of root.querySelectorAll('[name]')) {
    const k = keep[el.name];
    if (!k) continue;
    if (el.type === 'checkbox') el.checked = k.v; else if (k.v !== '' && el.tagName !== 'SELECT') el.value = k.v;
    if (k.focus) { el.focus(); try { if (k.at != null) el.setSelectionRange(k.at, k.at); } catch (_) {} }
  }
}

// ---- rendering ----

let rendering = Promise.resolve();
function render() { rendering = rendering.then(renderNow, renderNow); return rendering; }

async function renderNow() {
  if (!state.token) { stopSync(); app.replaceChildren(state.invite ? joinScreen() : signIn()); return; }
  try { state.me = await api('GET', '/me'); } catch (e) { if (state.token) state.error = e.message; app.replaceChildren(signIn()); return; }
  if (state.invite) { app.replaceChildren(joinScreen()); return; }
  let body;
  try {
    if (state.tab === 'members') { stopSync(); body = await membersView(); }
    else if (state.projectId) body = await projectView();
    else { stopSync(); body = await projectsView(); }
  } catch (e) { state.error = e.message; body = h('p', { class: 'error' }, e.message); }
  const keep = snapshot(app);
  app.classList.toggle('wide', !!(state.projectId && state.tab === 'projects'));
  app.replaceChildren(h('div', {},
    h('header', {},
      h('h1', {}, 'Werkbord Team'),
      h('span', {}, state.me.workspace.name),
      h('span', { class: 'who' }, state.me.member.name + ' · ' + state.me.member.role + ' ',
        h('button', { class: 'link', onclick: signOut }, 'Sign out'))),
    h('p', { class: 'note' }, 'Team coordinates the work. It does not run anything: every member uses their own computer, ',
      'their own Werkbord runner and their own Git, GitHub and agent credentials.'),
    h('nav', {},
      h('button', { 'aria-current': state.tab === 'projects' ? 'page' : null, onclick: () => { state.tab = 'projects'; state.projectId = null; state.secret = null; render(); } }, 'Projects'),
      h('button', { 'aria-current': state.tab === 'members' ? 'page' : null, onclick: () => { state.tab = 'members'; state.secret = null; render(); } }, 'Members')),
    state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
    state.info ? h('p', { class: 'ok', role: 'status' }, state.info) : '',
    secretBox(),
    body));
  restore(app, keep);
}

function signIn() {
  const input = h('input', { type: 'password', autocomplete: 'off', placeholder: 'wbt_…', required: true, 'aria-label': 'Token' });
  return h('div', {},
    h('header', {}, h('h1', {}, 'Werkbord Team')),
    h('div', { class: 'panel' },
      h('h2', {}, 'Sign in'),
      state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
      h('p', { class: 'muted' }, 'Paste your token. The person who runs the server gave it to you.'),
      h('form', { onsubmit: (e) => { e.preventDefault(); state.token = input.value.trim(); state.error = '';
          try { sessionStorage.setItem('werkbord-team-token', state.token); } catch (_) {} render(); } },
        h('label', {}, 'Token', input), h('button', { class: 'primary' }, 'Sign in'))));
}

function secretBox() {
  if (!state.secret) return '';
  const s = state.secret;
  const link = location.origin + (s.kind === 'invite' ? '/#invite=' : '/#token=') + s.token;
  return h('div', { class: 'secret', role: 'status' },
    h('strong', {}, s.kind === 'invite' ? 'Invite for ' + s.name : s.self ? 'Your token' : 'Token for ' + s.name),
    h('p', { class: 'muted' }, s.kind === 'invite'
      ? 'Shown once; it is not stored and cannot be shown again. Anyone with this link can join the project until it expires or is used up, so send it privately.'
      : s.self ? 'Shown once; it is not stored and cannot be shown again. Keep it private: it is how you sign in.'
      : 'Shown once; it is not stored and cannot be shown again. Send it to them privately.'),
    h('code', {}, s.token),
    h('span', { class: 'muted' }, s.kind === 'invite' ? 'Or the link to send: ' : s.self ? 'Or a link that signs you in: ' : 'Or a link that signs them in: '),
    h('code', {}, link),
    h('div', { class: 'actions' }, copy(link, 'Copy link'), copy(s.token, 'Copy ' + (s.kind === 'invite' ? 'code' : 'token')),
      h('button', { class: 'plain', onclick: () => { state.secret = null; render(); } }, 'Done')));
}

// ---- joining with an invite ----

function joinScreen() {
  const code = state.invite;
  const done = () => { state.invite = null; };
  const content = [h('h2', {}, 'Join a project'),
    state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : ''];
  if (state.token && state.me) {
    content.push(h('p', {}, 'You are signed in as ', h('strong', {}, state.me.member.name), '. Use the invite to join its project.'),
      h('div', { class: 'actions' },
        h('button', { class: 'primary', onclick: () => act(async () => {
          const j = await api('POST', '/invites/join', { code });
          done(); state.tab = 'projects'; state.projectId = j.project.id; state.ptab = 'board'; state.info = 'You joined ' + j.project.name + '.';
        }) }, 'Join the project'),
        h('button', { class: 'plain', onclick: () => { done(); render(); } }, 'Not now')));
  } else {
    const name = h('input', { name: 'join-name', required: true, maxlength: 80, autocomplete: 'name' });
    const email = h('input', { name: 'join-email', type: 'email', maxlength: 254, autocomplete: 'email' });
    content.push(h('p', { class: 'muted' }, 'You were invited to a project. Choose the name your teammates will see; you get your own token to sign in with.'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => {
          const j = await api('POST', '/invites/redeem', { code, name: name.value, email: email.value });
          done();
          try { sessionStorage.setItem('werkbord-team-token', j.token); } catch (_) {}
          state.token = j.token; state.tab = 'projects'; state.projectId = j.project.id; state.ptab = 'board';
          state.secret = { kind: 'token', name: j.member.name, self: true, token: j.token };
          state.info = 'Welcome to ' + j.project.name + '. Keep your token somewhere safe: it is how you sign in again.';
        }); } },
        field('Your name', name), field('Email (optional)', email), h('button', { class: 'primary' }, 'Join')),
      h('p', { class: 'muted' }, 'Already have a token? ', h('button', { class: 'link', onclick: () => { state.error = ''; render_signin(); } }, 'Sign in first'), '.'));
  }
  return h('div', {}, h('header', {}, h('h1', {}, 'Werkbord Team')), h('div', { class: 'panel' }, content));
}

function render_signin() { const code = state.invite; state.invite = null; app.replaceChildren(signIn()); state.invite = code; }

// ---- members ----

async function membersView() {
  const members = await api('GET', '/members');
  const rows = members.map((m) => h('div', { class: 'row' },
    h('div', { class: 'grow' }, h('strong', {}, m.name), ' ', h('span', { class: 'muted' }, m.email || '')),
    h('span', { class: 'badge' }, m.role),
    (m.id === state.me.member.id || can('members.manage'))
      ? h('button', { class: 'plain', onclick: () => act(async () => { const r = await api('POST', '/members/' + m.id + '/token'); state.secret = { kind: 'token', name: r.member.name, token: r.token }; }) }, 'New token') : '',
    (can('members.manage') && m.role !== 'owner')
      ? h('button', { class: 'danger', onclick: () => confirm('Remove ' + m.name + ' from the workspace? Anything they are working on goes back on the board.') && act(() => api('DELETE', '/members/' + m.id)) }, 'Remove') : ''));
  const panels = [h('div', { class: 'panel' }, h('h2', {}, 'Members (' + members.length + ')'), rows)];
  if (can('members.manage')) {
    const name = h('input', { name: 'm-name', required: true, maxlength: 80 });
    const email = h('input', { name: 'm-email', type: 'email', maxlength: 254 });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'Add a member'),
      h('p', { class: 'muted' }, 'Or invite people to a single project with an invite link from that project\'s People tab.'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { const r = await api('POST', '/members', { name: name.value, email: email.value });
          state.secret = { kind: 'token', name: r.member.name, token: r.token }; name.value = ''; email.value = ''; }); } },
        field('Name', name), field('Email (optional)', email), h('button', { class: 'primary' }, 'Add'))));
  }
  return h('div', {}, panels);
}

// ---- projects ----

async function projectsView() {
  const projects = await api('GET', '/projects');
  const rows = projects.map((p) => h('div', { class: 'row' },
    h('div', { class: 'grow' }, h('button', { class: 'link', onclick: () => openProject(p.id) }, p.name),
      p.repository ? h('div', { class: 'muted' }, p.repository) : ''),
    p.archived ? h('span', { class: 'badge' }, 'archived') : ''));
  const panels = [h('div', { class: 'panel' }, h('h2', {}, 'Projects (' + projects.length + ')'),
    rows.length ? rows : h('p', { class: 'muted' }, can('projects.view_all') ? 'No projects yet.' : 'You are not on any project yet. Ask for an invite link.'))];
  if (can('projects.create')) {
    const name = h('input', { name: 'p-name', required: true, maxlength: 80 });
    const desc = h('input', { name: 'p-desc', maxlength: 2000 });
    const repo = h('input', { name: 'p-repo', placeholder: 'https://github.com/owner/repo (optional)', maxlength: 2048 });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'New project'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { const p = await api('POST', '/projects', { name: name.value, description: desc.value, repository: repo.value });
          name.value = ''; desc.value = ''; repo.value = ''; await openProjectNow(p.id); }); } },
        field('Name', name), field('Description (optional)', desc), field('Repository', repo), h('button', { class: 'primary' }, 'Create'))));
  }
  return h('div', {}, panels);
}

function openProject(id) { state.projectId = id; state.ptab = 'board'; state.ticketId = null; state.secret = null; state.handoff = null; state.data = null; render(); }
async function openProjectNow(id) { state.projectId = id; state.ptab = 'board'; state.ticketId = null; state.handoff = null; state.data = null; }

// The open project. Its board is loaded with everything the other tabs need, and
// kept current by a long poll: when any member changes the project, the server
// answers and the page reloads what it shows.
async function loadProject() {
  const id = state.projectId;
  const board = await api('GET', '/projects/' + id + '/board');
  const d = { id, board, tab: state.ptab };
  if (state.ptab === 'repository') d.repo = await api('GET', '/projects/' + id + '/repository');
  if (state.ptab === 'activity') d.activity = await api('GET', '/projects/' + id + '/activity?limit=100');
  if (state.ptab === 'people') {
    d.people = board.people;
    d.invites = board.can.includes('invites.manage') ? await api('GET', '/projects/' + id + '/invites') : [];
    d.everyone = board.can.includes('members.manage') ? await api('GET', '/members') : [];
  }
  state.data = d;
  return d;
}

async function projectView() {
  const d = await loadProject();
  startSync(d.id, d.board.revision);
  const p = d.board.project;
  const tab = (id, label) => h('button', { 'aria-current': state.ptab === id ? 'page' : null, onclick: () => { state.ptab = id; state.ticketId = null; state.handoff = null; render(); } }, label);
  const content = state.ptab === 'repository' ? repositoryTab(d) : state.ptab === 'activity' ? activityTab(d) : state.ptab === 'people' ? peopleTab(d) : boardTab(d);
  return h('div', {},
    h('p', {}, h('button', { class: 'link', onclick: () => { state.projectId = null; state.data = null; state.ticketId = null; state.handoff = null; render(); } }, '← Projects')),
    h('div', { class: 'panel project-head' },
      h('h2', {}, p.name, p.archived ? ' (archived)' : ''),
      p.description ? h('p', {}, p.description) : '',
      p.repository ? h('p', { class: 'muted' }, 'Repository: ', p.repository) : h('p', { class: 'muted' }, 'No repository address yet.'),
      h('p', { class: 'muted' }, 'You are ' + (d.board.member ? (d.board.role === 'owner' ? 'an owner' : d.board.role === 'reviewer' ? 'a reviewer' : 'a member') : 'looking at this project without being on it') + ' here.'),
      can('projects.manage') ? h('button', { class: 'plain', onclick: () => act(() => api('PATCH', '/projects/' + p.id, { archived: !p.archived })) }, p.archived ? 'Unarchive' : 'Archive') : ''),
    h('nav', { class: 'sub' }, tab('board', 'Board'), tab('repository', 'Repository'), tab('activity', 'Activity'), tab('people', 'People & invites')),
    content);
}

// ---- keeping boards in step ----

let sync = { id: null, ctl: null };
function stopSync() { if (sync.ctl) sync.ctl.abort(); sync = { id: null, ctl: null }; }
function startSync(id, revision) {
  if (sync.id === id && sync.ctl && !sync.ctl.signal.aborted) { sync.rev = revision; return; }
  stopSync();
  const ctl = new AbortController();
  sync = { id, ctl, rev: revision };
  (async () => {
    let failures = 0;
    while (!ctl.signal.aborted) {
      try {
        const r = await api('GET', '/projects/' + id + '/sync?since=' + sync.rev + '&wait=20', null, { signal: ctl.signal });
        failures = 0;
        if (r.changed && !ctl.signal.aborted) { sync.rev = r.revision; await refreshProject(); }
      } catch (_) {
        if (ctl.signal.aborted) return;
        await new Promise((res) => setTimeout(res, Math.min(30000, 1000 * 2 ** failures++)));
      }
    }
  })();
}
// Reload the open project; what someone is typing in it is kept.
async function refreshProject() {
  if (!state.projectId || state.tab !== 'projects') return;
  await render(); // render() keeps what is being typed (see snapshot and restore)
}

// ---- the board ----

const STATUS_HELP = {
  backlog: 'Ideas that are not ready to start.',
  available: 'Ready: any member can claim one.',
  in_progress: 'Someone is working on it.',
  review: 'Submitted; waiting for a reviewer.',
  done: 'Finished.',
};

function personName(d, id) {
  if (!id) return '';
  const p = d.board.people.find((x) => x.id === id);
  return p ? p.name : 'a former member';
}

function boardTab(d) {
  const b = d.board;
  const cols = b.statuses.map((s) => {
    const items = b.tickets.filter((k) => k.status === s.status);
    return h('section', { class: 'col', 'aria-label': s.label },
      h('h3', {}, s.label, ' ', h('span', { class: 'badge' }, String(items.length))),
      h('p', { class: 'muted small' }, STATUS_HELP[s.status]),
      items.map((k) => card(d, k)));
  });
  const parts = [];
  if (state.ticketId) {
    const k = b.tickets.find((x) => x.id === state.ticketId);
    if (k) parts.push(ticketPanel(d, k)); else state.ticketId = null;
  }
  parts.push(h('div', { class: 'board' }, cols));
  if (pcan('tickets.create') && !b.project.archived) parts.push(newTicketForm(d));
  return h('div', {}, parts);
}

function card(d, k) {
  const mine = k.assigneeId === state.me.member.id;
  const pr = k.pullRequest;
  const flags = [];
  if (pr) {
    flags.push(h('span', { class: 'badge' + (pr.mergeable === 'conflicting' && pr.state === 'open' ? ' bad' : '') },
      'PR' + (pr.number ? ' #' + pr.number : '') + ' ' + pr.state + (pr.state === 'open' && pr.mergeable === 'conflicting' ? ' · conflicts' : '')));
    if (pr.state === 'open' && pr.behind > 0) flags.push(h('span', { class: 'badge warn' }, pr.behind + ' behind'));
  }
  return h('article', { class: 'card' + (mine ? ' mine' : '') + (k.id === state.ticketId ? ' open' : '') },
    h('div', { class: 'muted small' }, k.key),
    h('button', { class: 'link title', onclick: () => { state.ticketId = k.id === state.ticketId ? null : k.id; state.handoff = null; render(); } }, k.title),
    k.assigneeId ? h('div', { class: 'owner' }, mine ? 'You' : personName(d, k.assigneeId), k.status === 'in_progress' ? ' · active' : '') : '',
    flags.length ? h('div', { class: 'flags' }, flags) : '',
    (k.status === 'available' && d.board.member && pcan('tickets.claim'))
      ? h('button', { class: 'primary small', onclick: () => act(() => api('POST', projectPath(k) + '/claim')) }, 'Claim') : '');
}

function projectPath(k) { return '/projects/' + state.projectId + '/tickets/' + k.id; }

function newTicketForm() {
  const title = h('input', { name: 't-title', required: true, maxlength: 200 });
  const desc = h('textarea', { name: 't-desc', rows: 3, maxlength: 20000 });
  const reqs = h('textarea', { name: 't-reqs', rows: 3, maxlength: 20000, placeholder: 'Acceptance criteria, constraints, links, anything the person doing it needs to know' });
  const ready = h('input', { name: 't-ready', type: 'checkbox' });
  return h('div', { class: 'panel' }, h('h2', {}, 'New ticket'),
    h('form', { class: 'stack', onsubmit: (e) => { e.preventDefault(); act(async () => {
        await api('POST', '/projects/' + state.projectId + '/tickets', { title: title.value, description: desc.value, requirements: reqs.value, status: ready.checked ? 'available' : 'backlog' });
        title.value = ''; desc.value = ''; reqs.value = ''; ready.checked = false; }); } },
      field('Title', title), field('Description', desc), field('Requirements and context', reqs),
      h('label', { class: 'check' }, ready, ' Ready to be claimed (otherwise it goes to the backlog)'),
      h('button', { class: 'primary' }, 'Create ticket')));
}

// ---- one ticket ----

function ticketPanel(d, k) {
  const b = d.board, me = state.me.member.id;
  const mine = k.assigneeId === me;
  const pr = k.pullRequest;
  const path = projectPath(k);
  const actions = [];
  const run = (method, p, body) => () => act(() => api(method, path + p, body));
  const isCreator = k.creatorId === me;
  const canEditText = (isCreator || pcan('tickets.edit')) && k.status !== 'done';

  if (k.status === 'backlog' && (isCreator || pcan('tickets.edit'))) actions.push(h('button', { class: 'plain', onclick: run('POST', '/move', { status: 'available' }) }, 'Make available'));
  if (k.status === 'available') {
    if (b.member && pcan('tickets.claim')) actions.push(h('button', { class: 'primary', onclick: run('POST', '/claim') }, 'Claim this ticket'));
    if (isCreator || pcan('tickets.edit')) actions.push(h('button', { class: 'plain', onclick: run('POST', '/move', { status: 'backlog' }) }, 'Move to backlog'));
  }
  if ((k.status === 'in_progress' || k.status === 'review') && mine && pcan('handoff.own'))
    actions.push(h('button', { class: 'primary', onclick: () => act(async () => { state.handoff = await api('POST', path + '/handoff'); }) }, 'Open in my runner'));
  if (k.status === 'in_progress' && (mine || pcan('tickets.assign'))) actions.push(h('button', { class: 'plain', onclick: run('POST', '/release') }, mine ? 'Release' : 'Take it back'));
  if (k.status === 'review' && pcan('tickets.review')) {
    const note = h('input', { name: 'rc-note-' + k.id, placeholder: 'What should change? (optional)', maxlength: 1000 });
    actions.push(h('button', { class: 'primary', onclick: run('POST', '/complete') }, 'Mark done'),
      h('span', { class: 'inline' }, note, h('button', { class: 'plain', onclick: () => act(() => api('POST', path + '/request-changes', { note: note.value })) }, 'Request changes')));
  }
  if (k.status === 'done' && pcan('tickets.reopen')) actions.push(h('button', { class: 'plain', onclick: run('POST', '/move', { status: 'available' }) }, 'Reopen'));
  if (pcan('tickets.assign') && ['backlog', 'available', 'in_progress'].includes(k.status)) {
    const who = b.people.filter((p) => p.projectRole && p.id !== k.assigneeId);
    if (who.length) {
      const pick = h('select', { name: 'assign-pick', 'aria-label': 'Assign to' }, who.map((p) => h('option', { value: p.id }, p.name)));
      actions.push(h('span', { class: 'inline' }, pick, h('button', { class: 'plain', onclick: () => act(() => api('POST', path + '/assign', { memberId: pick.value })) }, k.assigneeId ? 'Reassign' : 'Assign')));
    }
  }

  const facts = h('dl', { class: 'facts' },
    h('dt', {}, 'Status'), h('dd', {}, statusLabel(b, k.status)),
    h('dt', {}, 'Active owner'), h('dd', {}, k.assigneeId ? (mine ? 'You' : personName(d, k.assigneeId)) : 'Nobody'),
    h('dt', {}, 'Created by'), h('dd', {}, personName(d, k.creatorId), ' ', when(k.createdAt)),
    k.claimedAt && k.assigneeId ? [h('dt', {}, 'Claimed'), h('dd', {}, when(k.claimedAt))] : '',
    k.submittedAt ? [h('dt', {}, 'Submitted'), h('dd', {}, when(k.submittedAt), k.reviewerId ? ' · reviewer ' + personName(d, k.reviewerId) : '')] : '',
    k.completedAt ? [h('dt', {}, 'Completed'), h('dd', {}, when(k.completedAt))] : '',
    h('dt', {}, 'Updated'), h('dd', {}, when(k.updatedAt)),
    h('dt', {}, 'Branch'), h('dd', {}, k.branch ? [h('code', {}, k.branch), ' ', copy(k.branch, 'Copy')] : h('span', { class: 'muted' }, 'chosen when someone claims it')),
    h('dt', {}, 'Pull request'), h('dd', {}, pr ? prLine(pr) : h('span', { class: 'muted' }, 'none reported')));

  const commits = k.commits && k.commits.length
    ? h('ul', { class: 'commits' }, k.commits.map((c) => h('li', {}, h('code', {}, c.sha.slice(0, 8)), ' ', c.subject, c.author ? h('span', { class: 'muted' }, ' · ' + c.author) : '')))
    : h('p', { class: 'muted' }, 'No commits reported yet. Your own Werkbord reports them as you work.');

  return h('div', { class: 'panel ticket' },
    h('div', { class: 'ticket-head' },
      h('h2', {}, k.key + ' · ' + k.title),
      h('button', { class: 'plain', onclick: () => { state.ticketId = null; state.handoff = null; render(); } }, 'Close')),
    h('div', { class: 'actions' }, actions),
    state.handoff && state.handoff.ticket.id === k.id ? handoffBox(state.handoff) : '',
    h('div', { class: 'two' },
      h('div', {},
        h('h3', {}, 'Description'), h('p', { class: 'prose' }, k.description || '—'),
        h('h3', {}, 'Requirements and context'), h('p', { class: 'prose' }, k.requirements || '—'),
        canEditText ? editTicket(k, path) : ''),
      h('div', {}, facts, h('h3', {}, 'Commits'), commits,
        (mine || pcan('git.report_any')) && (k.status === 'in_progress' || k.status === 'review') ? gitForm(k, path) : '')));
}

function statusLabel(b, s) { const c = b.statuses.find((x) => x.status === s); return c ? c.label : s; }

function prLine(pr) {
  const parts = [isHTTPS(pr.url) ? h('a', { href: pr.url, target: '_blank', rel: 'noopener noreferrer' }, pr.number ? '#' + pr.number : 'Open the pull request') : h('span', {}, pr.url)];
  parts.push(' ', h('span', { class: 'badge' }, pr.state + (pr.draft ? ' · draft' : '')));
  if (pr.state === 'open') {
    parts.push(' ', h('span', { class: 'badge' + (pr.mergeable === 'conflicting' ? ' bad' : '') }, pr.mergeable === 'unknown' ? 'mergeability not known' : pr.mergeable));
    if (pr.behind > 0) parts.push(' ', h('span', { class: 'badge warn' }, pr.behind + ' behind ' + (pr.baseBranch || 'base')));
    if (pr.behind === 0) parts.push(' ', h('span', { class: 'badge' }, 'up to date'));
  }
  return parts;
}

function editTicket(k, path) {
  const title = h('input', { name: 'e-title-' + k.id, value: k.title, maxlength: 200 });
  const desc = h('textarea', { name: 'e-desc-' + k.id, rows: 3 }); desc.value = k.description;
  const reqs = h('textarea', { name: 'e-reqs-' + k.id, rows: 3 }); reqs.value = k.requirements;
  return h('details', {}, h('summary', {}, 'Edit this ticket'),
    h('form', { class: 'stack', onsubmit: (e) => { e.preventDefault(); act(() => api('PATCH', path, { title: title.value, description: desc.value, requirements: reqs.value, version: k.version })); } },
      field('Title', title), field('Description', desc), field('Requirements and context', reqs), h('button', { class: 'plain' }, 'Save')));
}

// What the member's own Werkbord reports, entered by hand: useful until it reports for them.
function gitForm(k, path) {
  const pr = k.pullRequest || {};
  const branch = h('input', { name: 'g-branch-' + k.id, placeholder: k.branch || 'branch name', maxlength: 200 });
  const url = h('input', { name: 'g-url-' + k.id, placeholder: 'https://github.com/owner/repo/pull/12', maxlength: 2048 }); if (pr.url) url.value = pr.url;
  const num = h('input', { name: 'g-num-' + k.id, type: 'number', min: 0, placeholder: 'number' }); if (pr.number) num.value = pr.number;
  const stateSel = h('select', { name: 'g-state-' + k.id }, ['open', 'merged', 'closed'].map((s) => h('option', { value: s, selected: (pr.state || 'open') === s }, s)));
  const mergeSel = h('select', { name: 'g-merge-' + k.id }, ['unknown', 'mergeable', 'conflicting'].map((s) => h('option', { value: s, selected: (pr.mergeable || 'unknown') === s }, s)));
  const behind = h('input', { name: 'g-behind-' + k.id, type: 'number', min: 0, placeholder: 'commits behind base (blank: unknown)' }); if (pr.behind >= 0 && pr.url) behind.value = pr.behind;
  return h('details', {}, h('summary', {}, 'Record branch and pull request'),
    h('p', { class: 'muted' }, 'Your own Werkbord normally reports these. Team never reads your repository or calls GitHub; it only records what you tell it.'),
    h('form', { class: 'stack', onsubmit: (e) => { e.preventDefault(); act(async () => {
        const body = {};
        if (branch.value.trim()) body.branch = branch.value.trim();
        if (url.value.trim()) body.pullRequest = { url: url.value.trim(), number: Number(num.value) || 0, state: stateSel.value, mergeable: mergeSel.value,
          ...(behind.value !== '' ? { behind: Number(behind.value) } : {}) };
        await api('PUT', path + '/git', body); }); } },
      field('Branch', branch), field('Pull request address', url), field('Number', num), field('State', stateSel), field('Mergeable', mergeSel), field('Behind', behind),
      h('button', { class: 'plain' }, 'Save')),
    k.status === 'in_progress' && pr.url ? h('button', { class: 'primary', onclick: () => act(() => api('POST', path + '/submit', {})) }, 'Submit for review') : '',
    k.status === 'in_progress' && !pr.url ? h('p', { class: 'muted' }, 'To submit for review: record the pull request above, or submit without one below.') : '',
    k.status === 'in_progress' && !pr.url ? h('button', { class: 'plain', onclick: () => act(() => api('POST', path + '/submit', {})) }, 'Submit for review without a pull request') : '');
}

function handoffBox(hf) {
  const cmd = 'WERKBORD_TEAM_TOKEN=<your token> werkbord-team handoff --server ' + location.origin + ' --ticket ' + hf.ticket.key + ' --runner http://127.0.0.1:7420';
  const json = JSON.stringify(hf, null, 2);
  return h('div', { class: 'handoff' },
    h('strong', {}, 'Open ' + hf.ticket.key + ' in your own Werkbord'),
    h('p', { class: 'muted' }, 'This gives you the ticket\'s context to take into your Werkbord, on your computer. It does not connect to anyone\'s runner, and nobody else can use it to run anything on yours. It holds no credentials and no paths.'),
    h('ol', {},
      h('li', {}, 'In your Werkbord, add a task to your checkout of ', h('code', {}, hf.git.repository || 'this project\'s repository'), ' and paste the task text below as its description.'),
      h('li', {}, 'Run it on the branch ', h('code', {}, hf.git.branch), '. Commit and push there, then open a pull request. Do not merge it: the reviewer does that.'),
      h('li', {}, 'Come back and submit the ticket for review.')),
    h('div', { class: 'actions' }, copy(hf.prompt, 'Copy task text'), copy(json, 'Copy handoff (JSON)'),
      h('button', { class: 'plain', onclick: () => {
        const a = h('a', { href: URL.createObjectURL(new Blob([json], { type: 'application/json' })), download: hf.ticket.key.toLowerCase() + '-handoff.json' });
        document.body.append(a); a.click(); a.remove(); } }, 'Download handoff'),
      h('button', { class: 'plain', onclick: () => { state.handoff = null; render(); } }, 'Hide')),
    h('details', {}, h('summary', {}, 'Or from a terminal on your computer'),
      h('p', { class: 'muted' }, 'This creates the task in the Werkbord running on your own computer (a localhost address only), using your own tokens. Your tokens stay in your environment, not in the command.'),
      h('code', {}, cmd), copy(cmd, 'Copy command')),
    h('details', {}, h('summary', {}, 'Task text'), h('pre', {}, hf.prompt)));
}

// ---- repository ----

function repositoryTab(d) {
  const r = d.repo;
  const attention = r.attention.length
    ? r.attention.map((a) => h('div', { class: 'row attn ' + a.level }, h('span', { class: 'badge ' + (a.level === 'problem' ? 'bad' : a.level === 'warning' ? 'warn' : '') }, a.level), h('div', { class: 'grow' }, a.message)))
    : h('p', { class: 'muted' }, 'Nothing needs attention, as far as members have reported.');
  const prs = r.pullRequests.length
    ? r.pullRequests.map((p) => h('div', { class: 'row' },
        h('div', { class: 'grow' }, h('strong', {}, p.ticketKey), ' ', p.title, h('div', { class: 'muted' }, p.branch, p.owner ? ' · ' + p.owner : '')),
        prLine(p.pullRequest)))
    : h('p', { class: 'muted' }, 'No pull requests reported.');
  const branches = r.branches.length
    ? h('table', {}, h('thead', {}, h('tr', {}, ['Branch', 'Ticket', 'Who', 'Ahead / behind', 'Last activity', ''].map((c) => h('th', {}, c)))),
        h('tbody', {}, r.branches.map((b) => h('tr', {},
          h('td', {}, h('code', {}, b.name)),
          h('td', {}, b.ticketKey ? b.ticketKey + ' (' + b.ticketStatus.replace('_', ' ') + ')' : h('span', { class: 'muted' }, b.base ? 'base branch' : 'none')),
          h('td', {}, b.owner || b.reportedBy || ''),
          h('td', {}, b.base ? '' : b.ahead + ' / ' + (b.behind < 0 ? '?' : b.behind)),
          h('td', {}, when(b.lastActivity)),
          h('td', {}, b.stale ? h('span', { class: 'badge warn' }, 'stale') : b.active ? h('span', { class: 'badge' }, 'active') : '')))))
    : h('p', { class: 'muted' }, 'No branches reported yet.');
  return h('div', {},
    h('p', { class: 'muted' }, 'What members\' own Werkbords have reported about the repository' + (r.repository ? ' (' + r.repository + ')' : '') + '. Team cannot see the repository itself, and never merges or resolves conflicts: that stays in each developer\'s checkout and on your Git host.'),
    h('div', { class: 'panel' }, h('h2', {}, 'Needs attention'), attention),
    h('div', { class: 'panel' }, h('h2', {}, 'Pull requests'), prs),
    h('div', { class: 'panel scroll' }, h('h2', {}, 'Branches'), branches,
      h('p', { class: 'muted small' }, 'A branch is stale after ' + r.staleAfterDays + ' days without activity.')));
}

// ---- activity ----

const ACTIVITY = {
  'ticket.created': 'created', 'ticket.claimed': 'claimed', 'ticket.released': 'released', 'ticket.reassigned': 'reassigned',
  'ticket.work_submitted': 'submitted work on', 'ticket.pull_request_created': 'opened a pull request for', 'ticket.review_requested': 'asked for a review of',
  'ticket.changes_requested': 'asked for changes to', 'ticket.completed': 'completed', 'ticket.reopened': 'reopened', 'ticket.moved': 'moved',
  'ticket.handed_off': 'opened in their own runner:', 'ticket.pull_request_merged': 'recorded the merge of', 'project.member_joined': 'joined the project',
};

function activityTab(d) {
  const rows = d.activity.map((a) => h('div', { class: 'row' },
    h('div', { class: 'grow' }, h('strong', {}, a.actorName), ' ' + (ACTIVITY[a.kind] || a.kind), a.ticketKey ? [' ', h('strong', {}, a.ticketKey)] : '',
      a.detail ? h('span', { class: 'muted' }, ' · ' + a.detail) : ''),
    h('span', { class: 'muted' }, when(a.createdAt))));
  return h('div', { class: 'panel' }, h('h2', {}, 'Activity'), rows.length ? rows : h('p', { class: 'muted' }, 'Nothing has happened yet.'));
}

// ---- people and invites ----

function peopleTab(d) {
  const pid = d.id, manage = pcan('members.manage');
  const rows = d.people.map((p) => {
    const role = h('select', { name: 'role-' + p.id, 'aria-label': 'Role of ' + p.name }, ['owner', 'reviewer', 'member'].map((r) => h('option', { value: r, selected: p.projectRole === r }, r)));
    return h('div', { class: 'row' },
      h('div', { class: 'grow' }, h('strong', {}, p.name), ' ', h('span', { class: 'muted' }, p.email || '')),
      manage ? [role, h('button', { class: 'plain', onclick: () => act(() => api('PUT', '/projects/' + pid + '/members/' + p.id, { role: role.value })) }, 'Set role'),
        h('button', { class: 'danger', onclick: () => confirm('Take ' + p.name + ' off this project? Tickets they are working on go back on the board.') && act(() => api('DELETE', '/projects/' + pid + '/members/' + p.id)) }, 'Remove')]
        : h('span', { class: 'badge' }, p.projectRole));
  });
  const panels = [h('div', { class: 'panel' }, h('h2', {}, 'People on this project (' + d.people.length + ')'), rows)];
  if (manage) {
    const onIt = new Set(d.people.map((p) => p.id));
    const others = d.everyone.filter((m) => !onIt.has(m.id));
    if (others.length) {
      const pick = h('select', { name: 'add-pick', 'aria-label': 'Member' }, others.map((m) => h('option', { value: m.id }, m.name)));
      panels.push(h('div', { class: 'panel' }, h('h2', {}, 'Add someone who is already in the workspace'),
        h('form', { onsubmit: (e) => { e.preventDefault(); act(() => api('PUT', '/projects/' + pid + '/members/' + pick.value, {})); } }, field('Member', pick), h('button', { class: 'primary' }, 'Add'))));
    }
  }
  if (pcan('invites.manage')) {
    const role = h('select', { name: 'inv-role' }, [h('option', { value: 'member' }, 'member'), h('option', { value: 'reviewer' }, 'reviewer')]);
    const uses = h('input', { name: 'inv-uses', type: 'number', min: 1, max: 100, value: '1' });
    const hours = h('input', { name: 'inv-hours', type: 'number', min: 1, max: 720, value: '168' });
    const list = d.invites.map((i) => {
      const live = !i.revokedAt && new Date(i.expiresAt) > new Date() && i.uses < i.maxUses;
      return h('div', { class: 'row' },
        h('div', { class: 'grow' }, i.role + ' · used ' + i.uses + ' of ' + i.maxUses, h('div', { class: 'muted' }, (i.revokedAt ? 'revoked' : live ? 'expires ' + new Date(i.expiresAt).toLocaleString() : 'no longer valid'))),
        live ? h('button', { class: 'danger', onclick: () => act(() => api('DELETE', '/projects/' + pid + '/invites/' + i.id)) }, 'Revoke') : '');
    });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'Invite people'),
      h('p', { class: 'muted' }, 'Anyone with the link can join this project with a name of their choosing and gets their own token. Send it privately. It cannot be shown again.'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => {
          const r = await api('POST', '/projects/' + pid + '/invites', { role: role.value, maxUses: Number(uses.value) || 1, expiresInHours: Number(hours.value) || 168 });
          state.secret = { kind: 'invite', name: d.board.project.name + ' (' + r.invite.role + ')', token: r.code }; }); } },
        field('They join as', role), field('Can be used', uses), field('Valid for (hours)', hours), h('button', { class: 'primary' }, 'Create invite link')),
      list.length ? h('div', {}, h('h3', {}, 'Invites'), list) : ''));
  }
  return h('div', {}, panels);
}

render();
