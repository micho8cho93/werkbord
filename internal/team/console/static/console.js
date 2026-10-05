// The Team console. Plain JavaScript, no build step. Every piece of text from the
// server goes in with textContent, never as HTML.
//
// The console only ever talks to the Team server it was loaded from. "Open in my
// runner" gives the member the ticket's context to take into their OWN Werkbord;
// the console never contacts a Werkbord, and nothing here reaches another
// member's computer.
//
// The primary navigation is the path a piece of work takes:
//   Workspace  who is on the team, what is available, what everyone is doing
//   Projects   the projects, their people and invite links
//   Board      one project's tickets
//   My Work    what you are doing, and what waits for you
//   Reviews    work waiting for a review
//   Repository what the team's Werkbords have reported about the Git state
//   Activity   what happened
'use strict';

const app = document.getElementById('app');

const TABS = [['workspace', 'Workspace'], ['projects', 'Projects'], ['board', 'Board'], ['mywork', 'My Work'], ['reviews', 'Reviews'], ['repository', 'Repository'], ['activity', 'Activity']];
const PROJECT_TABS = new Set(['board', 'repository', 'activity', 'people']);

const state = {
  token: null, me: null, tab: 'workspace', projectId: null, ticketId: null, secret: null, error: '', info: '',
  invite: null,   // an invite code from the address, waiting to be used
  ov: null,       // the workspace overview: the projects, and the counts the navigation shows
  data: null,     // the open project: its board and what the open tab shows
  handoff: null,  // a handoff the member just opened
  online: true,   // whether the live connection to the server is up
};

try {
  const tok = /(?:^|[#&])token=([^&]+)/.exec(location.hash);
  const inv = /(?:^|[#&])invite=([^&]+)/.exec(location.hash);
  if (tok) sessionStorage.setItem('werkbord-team-token', decodeURIComponent(tok[1]));
  if (inv) state.invite = decodeURIComponent(inv[1]);
  if (tok || inv) history.replaceState(null, '', location.pathname + location.search); // tokens and codes never stay in the address bar
  state.token = sessionStorage.getItem('werkbord-team-token');
  state.projectId = sessionStorage.getItem('werkbord-team-project');
  const t = sessionStorage.getItem('werkbord-team-tab');
  if (t && (TABS.some((x) => x[0] === t) || t === 'people')) state.tab = t;
} catch (_) { /* storage can be unavailable; the sign-in form still works for the session */ }

function remember() {
  captureDrafts();
  const q = new URLSearchParams(); q.set("tab", state.tab);
  if (state.projectId) q.set("project", state.projectId);
  if (state.ticketId) q.set("ticket", state.ticketId);
  history.pushState(null, "", location.pathname + "?" + q.toString());
  try {
    sessionStorage.setItem('werkbord-team-tab', state.tab);
    if (state.projectId) sessionStorage.setItem('werkbord-team-project', state.projectId);
  } catch (_) {}
}

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, '');
    else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const kid of kids.flat(Infinity)) el.append(kid instanceof Node ? kid : document.createTextNode(kid ?? ''));
  return el;
}

async function api(method, path, body, opts) {
  const credential = state.token;
  const res = await fetch('/api/team/v1' + path, {
    method,
    headers: { ...(credential ? { Authorization: 'Bearer ' + credential } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
    signal: opts && opts.signal,
  });
  if (res.status === 401 && state.token && state.token !== credential) return api(method, path, body, opts);
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({}));
  if (res.status === 401 && state.token && state.token === credential) { signOut(); throw new Error('Your token is not valid any more. Sign in again.'); }
  if (!res.ok) {
    const err = new Error(data.error ? data.error.message : 'Request failed (' + res.status + ')');
    err.status = res.status;
    throw err;
  }
  return data;
}

function can(permission) { return state.me && state.me.permissions.includes(permission); }
function pcan(permission) { return !!(state.data && state.data.board.can.includes(permission)); }

function signOut() {
  try { sessionStorage.removeItem('werkbord-team-token'); } catch (_) {}
  stopSync();
  drafts.clear(); draftVersions.clear(); renderedScope = "";
  Object.assign(state, { token: null, me: null, ov: null, secret: null, data: null, ticketId: null, handoff: null });
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
// A link to somewhere outside Team (a pull request, a branch on the Git host) is only ever followed if it is https.
function safeHref(u) { return isHTTPS(u) ? u : null; }
function extLink(u, label) { return isHTTPS(u) ? h('a', { href: safeHref(u), target: '_blank', rel: 'noopener noreferrer' }, label) : ''; }
function sum(list, f) { return list.reduce((n, x) => n + f(x), 0); }
function plural(n, one, many) { return n + ' ' + (n === 1 ? one : many || one + 's'); }

// Drafts belong to the exact screen and project; refreshed data never changes
// the version against which a ticket edit began.
const drafts = new Map();
const draftVersions = new Map();
let renderedScope = '';
function screenScope() { return [state.tab, state.projectId || '', state.ticketId || ''].join(':'); }
function touched(e) {
 const el = e.target;
 if (!el || !el.name || el.name === 'project-switch') return;
 el.dataset.dirty = '1';
 const form = el.closest('[data-ticket-version]');
 if (form && !draftVersions.has(form.dataset.ticketId)) draftVersions.set(form.dataset.ticketId, Number(form.dataset.ticketVersion));
}
app.addEventListener('input', touched);
app.addEventListener('change', touched);
function snapshot(root) {
 const keep = { fields: {}, details: {}, focus: null, scroll: { x: window.scrollX, y: window.scrollY } };
 for (const el of root.querySelectorAll('[name]')) {
  if (el.dataset.dirty === '1') keep.fields[el.name] = { v: el.type === 'checkbox' ? el.checked : el.value };
  if (el === document.activeElement) keep.focus = { name: el.name, start: el.selectionStart, end: el.selectionEnd };
 }
 for (const [key, el] of disclosures(root)) keep.details[key] = el.open;
 return keep;
}
function disclosures(root) {
 const counts = new Map();
 return [...root.querySelectorAll('details')].map(el => {
  const label = el.querySelector('summary')?.textContent || '';
  const n = counts.get(label) || 0;
  counts.set(label, n + 1);
  return [label + ':' + n, el];
 });
}
function captureDrafts() { if (renderedScope) drafts.set(renderedScope, snapshot(app)); }
function restore(root, keep, focus) {
 if (!keep) return;
 for (const el of root.querySelectorAll('[name]')) {
  const k = keep.fields[el.name];
  if (k) { if (el.type === 'checkbox') el.checked = k.v; else el.value = k.v; el.dataset.dirty = '1'; }
 }
 for (const [key, el] of disclosures(root)) if (key in keep.details) el.open = keep.details[key];
 if (focus && keep.focus) for (const el of root.querySelectorAll('[name]')) if (el.name === keep.focus.name) {
  el.focus({ preventScroll: true });
  try { if (keep.focus.start != null) el.setSelectionRange(keep.focus.start, keep.focus.end); } catch (_) {}
 }
 if (focus && keep.scroll) window.scrollTo(keep.scroll.x, keep.scroll.y);
}
function discardTicketDraft(id) {
 draftVersions.delete(id);
 for (const keep of drafts.values()) for (const name of Object.keys(keep.fields)) if (name.endsWith('-' + id)) delete keep.fields[name];
 for (const el of app.querySelectorAll('[name]')) if (el.name.endsWith('-' + id)) delete el.dataset.dirty;
}
function ticketVersion(k) { return draftVersions.get(k.id) || k.version; }

// The connection banner and the toasts live outside #app, so a re-render never removes them.
const banner = h('p', { class: 'banner', role: 'status', hidden: true }, 'Reconnecting… what you see may be out of date. It will refresh by itself when the connection is back.');
const toasts = h('div', { class: 'toasts', 'aria-live': 'polite' });
document.body.prepend(banner);
document.body.append(toasts);
function setOnline(on) { state.online = on; banner.hidden = on; }
function toast(message) {
  const t = h('div', { class: 'toast', role: 'status' }, message);
  toasts.append(t);
  while (toasts.children.length > 4) toasts.firstChild.remove();
  setTimeout(() => t.remove(), 7000);
}

// ---- navigation ----

function navTab() { return state.tab === 'people' ? 'projects' : state.tab; }
function go(tab) { state.tab = tab; state.ticketId = null; state.handoff = null; state.secret = null; state.error = ''; state.info = ''; remember(); return render(); }
function openProject(id, tab) { state.projectId = id; state.tab = tab || 'board'; state.ticketId = null; state.handoff = null; state.data = null; state.secret = null; remember(); return render(); }
function openTicket(projectId, ticketId) { state.projectId = projectId; state.tab = 'board'; state.ticketId = ticketId; state.handoff = null; state.data = null; remember(); return render(); }
function openInRunner(it) { return act(async () => { state.handoff = await api('POST', itemPath(it) + '/handoff'); }); }
function itemPath(it) { return '/projects/' + it.project.id + '/tickets/' + it.ticket.id; }

// The project the Board, Repository and Activity tabs show: the one the member last
// opened if it is still there, else the first one they are on.
function currentProject() {
  const projects = state.ov ? state.ov.projects : [];
  let p = projects.find((x) => x.project.id === state.projectId);
  if (!p) p = projects.find((x) => x.member && !x.project.archived) || projects.find((x) => x.member) || projects[0];
  state.projectId = p ? p.project.id : null;
  return p || null;
}

function badgeOn(n, tone) { return n > 0 ? h('span', { class: 'count' + (tone ? ' ' + tone : '') }, String(n)) : ''; }

// ---- rendering ----

let rendering = Promise.resolve();
function render() { rendering = rendering.then(renderNow, renderNow); return rendering; }

async function renderNow() {
  const requestedScope = screenScope();
  if (!state.token) { stopSync(); app.replaceChildren(state.invite ? joinScreen() : signIn()); return; }
  try {
    [state.me, state.ov] = await Promise.all([api('GET', '/me'), api('GET', '/overview')]);
  } catch (e) {
    if (e instanceof TypeError) { // the network, not the token: keep what is on screen and keep trying
      setOnline(false);
      if (!app.firstChild || app.querySelector('.loading')) app.replaceChildren(unreachable());
      startSync();
      return;
    }
    if (state.token) state.error = e.message;
    app.replaceChildren(signIn());
    return;
  }
  if (state.invite) { app.replaceChildren(joinScreen()); return; }
  setOnline(true);
  let body;
  try {
    switch (state.tab) {
      case 'workspace': body = await workspaceView(); break;
      case 'projects': body = await projectsView(); break;
      case 'mywork': body = await myWorkView(); break;
      case 'reviews': body = await reviewsView(); break;
      default: body = await projectScopedView();
    }
  } catch (e) {
    if (e instanceof TypeError) { setOnline(false); startSync(); return; }
    state.error = e.message; body = h('p', { class: 'error' }, e.message);
  }
  startSync();
  if (requestedScope !== screenScope() && requestedScope.split(":")[1]) return;
  const previousScope = renderedScope;
  captureDrafts();
  const keep = drafts.get(screenScope());
  app.classList.toggle('wide', state.tab === 'board');
  const ov = state.ov;
  const counts = {
    board: sum(ov.projects, (p) => p.counts.available || 0),
    mywork: sum(ov.projects, (p) => p.mine),
    reviews: sum(ov.projects, (p) => p.toReview),
    repository: sum(ov.projects, (p) => p.problems),
  };
  app.replaceChildren(h('div', {},
    h('header', {},
      h('h1', {}, 'Werkbord Team'),
      h('span', {}, state.me.workspace.name),
      h('span', { class: 'who' }, state.me.member.name + ' · ' + state.me.member.role + ' ',
        h('button', { class: 'link', onclick: signOut }, 'Sign out'))),
    h('p', { class: 'note' }, 'Team coordinates the work. It does not run anything: every member uses their own computer, ',
      'their own Werkbord runner and their own Git, GitHub and agent credentials.'),
    h('nav', { 'aria-label': 'Werkbord Team' }, TABS.map(([id, label]) =>
      h('button', { 'aria-current': navTab() === id ? 'page' : null, onclick: () => go(id) }, label,
        badgeOn(counts[id], id === 'repository' ? 'bad' : id === 'board' ? 'quiet' : ''), ''))),
    state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
    state.info ? h('p', { class: 'ok', role: 'status' }, state.info) : '',
    secretBox(),
    body));
  restore(app, keep, previousScope === screenScope());
  renderedScope = screenScope();
}

function unreachable() {
  return h('div', { class: 'panel' }, h('h2', {}, 'Cannot reach the Team server'),
    h('p', { class: 'muted' }, 'Check your connection. This page keeps trying and will load by itself.'),
    h('button', { class: 'primary', onclick: () => render() }, 'Try now'));
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
          done(); state.tab = 'board'; state.projectId = j.project.id; remember(); state.info = 'You joined ' + j.project.name + '.';
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
          state.token = j.token; state.tab = 'board'; state.projectId = j.project.id; remember();
          state.secret = { kind: 'token', name: j.member.name, self: true, token: j.token };
          state.info = 'Welcome to ' + j.project.name + '. Keep your token somewhere safe: it is how you sign in again.';
        }); } },
        field('Your name', name), field('Email (optional)', email), h('button', { class: 'primary' }, 'Join')),
      h('p', { class: 'muted' }, 'Already have a token? ', h('button', { class: 'link', onclick: () => { state.error = ''; render_signin(); } }, 'Sign in first'), '.'));
  }
  return h('div', {}, h('header', {}, h('h1', {}, 'Werkbord Team')), h('div', { class: 'panel' }, content));
}

function render_signin() { const code = state.invite; state.invite = null; app.replaceChildren(signIn()); state.invite = code; }

// ---- keeping every view in step ----
//
// One long request per console, for the whole workspace. It returns the moment
// anything this member can see changes, with the new events so that a change by
// somebody else can be announced ("Bo claimed WB-4"). The events are a courtesy:
// what the views show is always re-read from the server, which is the authority.
// If the connection drops, the console says so, retries with a growing pause, and
// on reconnecting reloads everything it shows; the server answers with whatever
// changed in the meantime (or says "reload" if it cannot tell).

let sync = null;
function stopSync() { if (sync) sync.ctl.abort(); sync = null; }
function pause(ms, signal) {
  return new Promise((res) => { const t = setTimeout(res, ms); signal.addEventListener('abort', () => { clearTimeout(t); res(); }, { once: true }); });
}
function startSync() {
  if (sync && !sync.ctl.signal.aborted) return;
  const ctl = new AbortController();
  sync = { ctl };
  (async () => {
    let rev = null, cursor = -1, failures = 0, missed = false;
    while (!ctl.signal.aborted) {
      try {
        const q = rev === null ? 'wait=0' : 'since=' + rev + '&after=' + cursor + '&wait=20';
        const r = await api('GET', '/sync?' + q, null, { signal: ctl.signal });
        if (ctl.signal.aborted) return;
        failures = 0;
        const wasOffline = !state.online || missed;
        setOnline(true);
        if (rev === null) { rev = r.revision; cursor = r.cursor; if (wasOffline) { missed = false; await render(); } continue; }
        if (r.changed || wasOffline) {
          rev = r.revision; cursor = r.cursor; missed = false;
          if (r.reset || r.truncated) toast('You were away for a while, so everything was reloaded.');
          else announce(r.events || []);
          await render();
        }
      } catch (e) {
        if (ctl.signal.aborted) return;
        missed = true;
        if (e.status !== 429) setOnline(false);
        await pause(e.status === 429 ? 5000 : Math.min(30000, 1000 * 2 ** failures++), ctl.signal);
      }
    }
  })();
}
// Come back to the tab, or the network: reload at once rather than wait for the next poll.
document.addEventListener('visibilitychange', () => { if (!document.hidden && state.token) { stopSync(); render(); } });
window.addEventListener('online', () => { if (state.token) { stopSync(); render(); } });

const ACTIVITY = {
  'ticket.created': 'created', 'ticket.claimed': 'claimed', 'ticket.released': 'released', 'ticket.reassigned': 'reassigned',
  'ticket.work_submitted': 'submitted work on', 'ticket.pull_request_created': 'opened a pull request for', 'ticket.review_requested': 'asked for a review of',
  'ticket.changes_requested': 'asked for changes to', 'ticket.completed': 'completed', 'ticket.reopened': 'reopened', 'ticket.moved': 'moved',
  'ticket.handed_off': 'opened in their own runner:', 'ticket.pull_request_merged': 'recorded the merge of', 'project.member_joined': 'joined the project',
};

function announce(events) {
  const others = events.filter((e) => e.actorId !== state.me.member.id && e.kind !== 'ticket.handed_off');
  for (const e of others.slice(0, 3)) {
    toast(e.actorName + ' ' + (ACTIVITY[e.kind] || e.kind) + (e.ticketKey ? ' ' + e.ticketKey : '') + (e.projectName ? ' · ' + e.projectName : ''));
  }
  if (others.length > 3) toast('…and ' + (others.length - 3) + ' more changes.');
}

// ---- workspace: the team at a glance ----

function tile(label, value, hint, onclick, tone) {
  return h('button', { class: 'tile' + (tone ? ' ' + tone : ''), onclick },
    h('span', { class: 'tile-value' }, String(value)), h('span', { class: 'tile-label' }, label), h('span', { class: 'muted small' }, hint));
}

function statusName(st) { return { backlog: 'Backlog', available: 'Available', in_progress: 'In progress', review: 'In review', done: 'Done' }[st] || st; }

async function workspaceView() {
  const ov = state.ov, me = state.me.member.id;
  const members = await api('GET', '/members');
  const mine = sum(ov.projects, (p) => p.mine), toReview = sum(ov.projects, (p) => p.toReview);
  const problems = sum(ov.projects, (p) => p.problems), warnings = sum(ov.projects, (p) => p.warnings);
  const others = ov.working.filter((it) => it.ticket.assigneeId !== me);
  const best = ov.projects.filter((p) => !p.project.archived).sort((a, b) => (b.counts.available || 0) - (a.counts.available || 0))[0];

  const tiles = h('div', { class: 'tiles' },
    tile('Available', ov.available, 'ready for anyone to claim', () => best ? openProject(best.project.id, 'board') : go('projects')),
    tile('Yours', mine, 'tickets you are working on', () => go('mywork')),
    tile('Everyone else', others.length, 'being worked on or in review', () => document.getElementById('working-now') && document.getElementById('working-now').scrollIntoView({ behavior: 'smooth' })),
    tile('To review', toReview, 'waiting for you', () => go('reviews'), toReview > 0 ? 'attn' : ''),
    tile('Repository', problems + warnings, problems ? plural(problems, 'problem') : warnings ? plural(warnings, 'warning') : 'nothing needs attention', () => go('repository'), problems ? 'bad' : ''));

  const working = ov.working.length
    ? ov.working.map((it) => h('div', { class: 'row' },
        h('div', { class: 'grow' },
          h('button', { class: 'link', onclick: () => openTicket(it.project.id, it.ticket.id) }, it.ticket.key + ' · ' + it.ticket.title),
          h('div', { class: 'muted' }, it.project.name + ' · ' + (it.ticket.assigneeId === me ? 'You' : it.author || 'a former member'))),
        h('span', { class: 'badge' }, statusName(it.ticket.status)),
        mergeBadge(it),
        when(it.ticket.updatedAt)))
    : h('p', { class: 'muted' }, 'Nobody is working on anything right now.');

  const projects = ov.projects.length
    ? ov.projects.map((p) => h('div', { class: 'row' },
        h('div', { class: 'grow' }, h('button', { class: 'link', onclick: () => openProject(p.project.id, 'board') }, p.project.name), p.project.archived ? ' (archived)' : '',
          h('div', { class: 'muted' }, ['available', 'in_progress', 'review', 'done'].map((st) => (p.counts[st] || 0) + ' ' + statusName(st).toLowerCase()).join(' · '))),
        p.mine ? h('span', { class: 'badge' }, p.mine + ' yours') : '',
        p.toReview ? h('span', { class: 'badge warn' }, p.toReview + ' to review') : '',
        p.problems ? h('span', { class: 'badge bad' }, plural(p.problems, 'problem')) : ''))
    : h('p', { class: 'muted' }, can('projects.view_all') ? 'No projects yet. Create one under Projects.' : 'You are not on any project yet. Ask for an invite link.');

  return h('div', {}, tiles,
    h('div', { class: 'panel', id: 'working-now' }, h('h2', {}, 'What the team is working on'), working),
    h('div', { class: 'panel' }, h('h2', {}, 'Projects'), projects),
    membersPanels(members));
}

function mergeBadge(it) {
  const m = it.merge;
  if (!m || m === 'none') return '';
  const text = { mergeable: 'PR can merge', conflicting: 'PR has conflicts', unknown: 'PR open', merged: 'PR merged', closed: 'PR closed' }[m] || m;
  return h('span', { class: 'badge' + (m === 'conflicting' ? ' bad' : m === 'closed' ? ' warn' : '') }, text);
}

function membersPanels(members) {
  const rows = members.map((m) => h('div', { class: 'row' },
    h('div', { class: 'grow' }, h('strong', {}, m.name), ' ', h('span', { class: 'muted' }, m.email || '')),
    h('span', { class: 'badge' }, m.role),
    (m.id === state.me.member.id || can('members.manage'))
      ? h('button', { class: 'plain', onclick: () => act(async () => { const r = await api('POST', '/members/' + m.id + '/token'); const self = r.member.id === state.me.member.id;
          if (self) { stopSync(); state.token = r.token; try { sessionStorage.setItem('werkbord-team-token', r.token); } catch (_) {} }
          state.secret = { kind: 'token', name: r.member.name, self, token: r.token }; }) }, 'New token') : '',
    (can('members.manage') && m.role !== 'owner')
      ? h('button', { class: 'danger', onclick: () => confirm('Remove ' + m.name + ' from the workspace? Anything they are working on goes back on the board.') && act(() => api('DELETE', '/members/' + m.id)) }, 'Remove') : ''));
  const panels = [h('div', { class: 'panel' }, h('h2', {}, 'Members (' + members.length + ')'), rows)];
  if (can('members.manage')) {
    const name = h('input', { name: 'm-name', required: true, maxlength: 80 });
    const email = h('input', { name: 'm-email', type: 'email', maxlength: 254 });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'Add a member'),
      h('p', { class: 'muted' }, 'Or invite people to a single project with an invite link from that project\'s People page.'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { const r = await api('POST', '/members', { name: name.value, email: email.value });
          state.secret = { kind: 'token', name: r.member.name, token: r.token }; name.value = ''; email.value = ''; }); } },
        field('Name', name), field('Email (optional)', email), h('button', { class: 'primary' }, 'Add'))));
  }
  return h('div', {}, panels);
}

// ---- projects ----

async function projectsView() {
  const projects = state.ov.projects;
  const rows = projects.map((p) => h('div', { class: 'row' },
    h('div', { class: 'grow' }, h('button', { class: 'link', onclick: () => openProject(p.project.id, 'board') }, p.project.name),
      p.project.description ? h('div', { class: 'muted' }, p.project.description) : '',
      p.project.repository ? h('div', { class: 'muted' }, p.project.repository) : ''),
    p.project.archived ? h('span', { class: 'badge' }, 'archived') : '',
    h('span', { class: 'badge' }, p.member ? p.role : 'not on it'),
    h('span', { class: 'muted' }, plural(p.people, 'person', 'people')),
    h('button', { class: 'plain', onclick: () => openProject(p.project.id, 'people') }, 'People & invites')));
  const panels = [h('div', { class: 'panel' }, h('h2', {}, 'Projects (' + projects.length + ')'),
    rows.length ? rows : h('p', { class: 'muted' }, can('projects.view_all') ? 'No projects yet.' : 'You are not on any project yet. Ask for an invite link.'))];
  if (can('projects.create')) {
    const name = h('input', { name: 'p-name', required: true, maxlength: 80 });
    const desc = h('input', { name: 'p-desc', maxlength: 2000 });
    const repo = h('input', { name: 'p-repo', placeholder: 'https://github.com/owner/repo (optional)', maxlength: 2048 });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'New project'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { const p = await api('POST', '/projects', { name: name.value, description: desc.value, repository: repo.value });
          name.value = ''; desc.value = ''; repo.value = ''; state.projectId = p.id; state.tab = 'board'; state.data = null; remember(); }); } },
        field('Name', name), field('Description (optional)', desc), field('Repository', repo), h('button', { class: 'primary' }, 'Create'))));
  }
  return h('div', {}, panels);
}

// ---- one project: board, repository, activity, people ----

// Its board is loaded with everything the other project tabs need.
async function loadProject() {
  const id = state.projectId;
  const board = await api('GET', '/projects/' + id + '/board');
  const d = { id, board, tab: state.tab };
  if (state.ticketId) d.ticket = await api("GET", "/projects/" + id + "/tickets/" + state.ticketId);
  if (state.tab === 'repository') d.repo = await api('GET', '/projects/' + id + '/repository');
  if (state.tab === 'activity') d.activity = await api('GET', '/projects/' + id + '/activity?limit=100');
  if (state.tab === 'people') {
    d.people = board.people;
    d.invites = board.can.includes('invites.manage') ? await api('GET', '/projects/' + id + '/invites') : [];
    d.everyone = board.can.includes('members.manage') ? await api('GET', '/members') : [];
  }
  state.data = d;
  return d;
}

async function projectScopedView() {
  const cur = currentProject();
  if (!cur) return h('div', { class: 'panel' }, h('h2', {}, 'No project yet'),
    h('p', { class: 'muted' }, can('projects.create') ? 'Create a project under Projects to get started.' : 'You are not on any project yet. Ask for an invite link.'));
  const d = await loadProject();
  const p = d.board.project;
  const switcher = h('select', { name: 'project-switch', 'aria-label': 'Project', onchange: (e) => openProject(e.target.value, state.tab) },
    state.ov.projects.map((x) => h('option', { value: x.project.id, selected: x.project.id === p.id }, x.project.name + (x.project.archived ? ' (archived)' : ''))));
  const content = state.tab === 'repository' ? repositoryTab(d) : state.tab === 'activity' ? activityTab(d) : state.tab === 'people' ? peopleTab(d) : boardTab(d);
  return h('div', {},
    state.tab === 'people' ? h('p', {}, h('button', { class: 'link', onclick: () => go('projects') }, '← Projects')) : '',
    h('div', { class: 'panel project-head' },
      h('div', { class: 'project-bar' }, h('label', { class: 'inline' }, 'Project ', switcher),
        h('span', { class: 'muted' }, 'You are ' + (d.board.member ? (d.board.role === 'owner' ? 'an owner' : d.board.role === 'reviewer' ? 'a reviewer' : 'a member') : 'looking at this project without being on it') + ' here.'),
        state.tab === 'board' ? h('button', { class: 'plain small', onclick: () => openProject(p.id, 'people') }, 'People & invites') : '',
        can('projects.manage') ? h('button', { class: 'plain small', onclick: () => act(() => api('PATCH', '/projects/' + p.id, { archived: !p.archived })) }, p.archived ? 'Unarchive' : 'Archive') : ''),
      p.description ? h('p', {}, p.description) : '',
      p.repository ? h('p', { class: 'muted' }, 'Repository: ', isHTTPS(p.repository) ? extLink(p.repository, p.repository) : p.repository) : h('p', { class: 'muted' }, 'No repository address yet.')),
    content);
}

// ---- My Work ----

async function myWorkView() {
  const w = await api('GET', '/my-work');
  const attn = w.needsAction.length
    ? w.needsAction.map((a) => h('div', { class: 'row attn ' + a.level },
        h('span', { class: 'badge ' + (a.level === 'problem' ? 'bad' : a.level === 'warning' ? 'warn' : '') }, a.level === 'info' ? 'next' : a.level),
        h('div', { class: 'grow' }, a.message),
        a.kind === 'review_requested' || a.kind === 'ready_to_complete'
          ? h('button', { class: 'plain small', onclick: () => go('reviews') }, 'Open Reviews')
          : h('button', { class: 'plain small', onclick: () => openTicket(a.projectId, a.ticketId) }, 'Open ' + a.ticketKey)))
    : h('p', { class: 'muted' }, 'Nothing is waiting for you.');
  const list = (items, empty) => items.length ? items.map((it) => workItem(it)) : h('p', { class: 'muted' }, empty);

  const repos = w.repositories.length
    ? w.repositories.map((r) => h('div', { class: 'repo-block' },
        h('h3', {}, r.project.name, ' ', h('button', { class: 'link small', onclick: () => openProject(r.project.id, 'repository') }, 'Open the repository view')),
        r.attention.length ? r.attention.map((a) => h('div', { class: 'row attn ' + a.level }, h('span', { class: 'badge ' + (a.level === 'problem' ? 'bad' : a.level === 'warning' ? 'warn' : '') }, a.level), h('div', { class: 'grow' }, a.message)))
          : h('p', { class: 'muted' }, 'Nothing needs attention on your branches, as far as has been reported.'),
        r.branches.length ? h('div', { class: 'scroll' }, h('table', {}, h('thead', {}, h('tr', {}, ['Branch', 'Ticket', 'Ahead / behind', 'Last activity'].map((c) => h('th', {}, c)))),
          h('tbody', {}, r.branches.map((b) => h('tr', {}, h('td', {}, h('code', {}, b.name)), h('td', {}, b.ticketKey || ''), h('td', {}, b.ahead + ' / ' + (b.behind < 0 ? '?' : b.behind)), h('td', {}, when(b.lastActivity))))))) : ''))
    : h('p', { class: 'muted' }, 'Nothing reported yet. Your own Werkbord reports your branches as you work.');

  return h('div', {},
    h('div', { class: 'panel' }, h('h2', {}, 'Needs your attention'), attn,
      w.reviewsWaiting ? h('p', {}, h('button', { class: 'link', onclick: () => go('reviews') }, plural(w.reviewsWaiting, 'ticket') + ' waiting for a review you can do')) : ''),
    h('div', { class: 'panel' }, h('h2', {}, 'In progress (' + w.inProgress.length + ')'), list(w.inProgress, 'You are not working on a ticket. Claim one from a board.')),
    h('div', { class: 'panel' }, h('h2', {}, 'Submitted, waiting for review (' + w.submitted.length + ')'), list(w.submitted, 'Nothing of yours is waiting for a review.')),
    h('div', { class: 'panel' }, h('h2', {}, 'Your open pull requests (' + w.pullRequests.length + ')'),
      w.pullRequests.length ? w.pullRequests.map((it) => h('div', { class: 'row' },
        h('div', { class: 'grow' }, h('strong', {}, it.ticket.key), ' ', it.ticket.title, h('div', { class: 'muted' }, it.project.name, ' · ', it.ticket.branch)),
        prLine(it.ticket.pullRequest))) : h('p', { class: 'muted' }, 'No open pull requests.')),
    h('div', { class: 'panel' }, h('h2', {}, 'Your branches'), repos));
}

// A ticket outside its board: where it is, how its pull request stands, and where to go next.
function workItem(it, extra) {
  const k = it.ticket, pr = k.pullRequest, L = it.links || {};
  const links = [extLink(L.pullRequest, 'Pull request'), extLink(L.branch, 'Branch on the Git host'), extLink(L.compare, 'Compare with ' + (pr && pr.baseBranch || 'base')), extLink(L.repository, 'Repository')].filter((x) => x);
  const commits = k.commits && k.commits.length
    ? h('details', {}, h('summary', {}, plural(k.commits.length, 'commit')),
        h('ul', { class: 'commits' }, k.commits.map((c) => h('li', {}, isHTTPS(L.commitPrefix) ? extLink(L.commitPrefix + c.sha, c.sha.slice(0, 8)) : h('code', {}, c.sha.slice(0, 8)), ' ', c.subject, c.author ? h('span', { class: 'muted' }, ' · ' + c.author) : ''))))
    : '';
  const mine = k.assigneeId === state.me.member.id;
  return h('article', { class: 'card item' + (mine ? ' mine' : '') },
    h('div', { class: 'muted small' }, k.key, ' · ', it.project.name, ' · ', statusName(k.status), it.assignedBy ? ' · assigned to you by ' + it.assignedBy : (it.author && !mine ? ' · by ' + it.author : '')),
    h('button', { class: 'link title', onclick: () => openTicket(it.project.id, k.id) }, k.title),
    h('div', { class: 'flags' }, mergeBadge(it), pr && pr.state === 'open' && pr.behind > 0 ? h('span', { class: 'badge warn' }, pr.behind + ' behind ' + (pr.baseBranch || 'base')) : '', pr && pr.draft ? h('span', { class: 'badge' }, 'draft') : ''),
    k.branch ? h('div', {}, 'Branch ', h('code', {}, k.branch), ' ', copy(k.branch, 'Copy')) : '',
    links.length ? h('div', { class: 'links' }, links.map((l, i) => [i ? ' · ' : '', l])) : '',
    commits,
    extra || '',
    state.handoff && state.handoff.ticket.id === k.id ? handoffBox(state.handoff) : '',
    mine && (k.status === 'in_progress' || k.status === 'review')
      ? h('div', { class: 'actions' }, h('button', { class: 'primary small', onclick: () => openInRunner(it) }, 'Open in my runner'),
          h('button', { class: 'plain small', onclick: () => openTicket(it.project.id, k.id) }, 'Open ticket'))
      : '');
}

// ---- Reviews ----

async function reviewsView() {
  const q = await api('GET', '/reviews');
  const toReview = q.items.filter((i) => i.canReview && !i.mine);
  const waiting = q.items.filter((i) => i.mine || !i.canReview);
  return h('div', {},
    h('p', { class: 'muted' }, 'Reviewing and merging happen on your Git host. Here you see what needs a decision, jump to the pull request, and record the outcome so the team sees it. Team never merges anything.'),
    h('div', { class: 'panel' }, h('h2', {}, 'To review (' + toReview.length + ')'),
      toReview.length ? toReview.map(reviewItem) : h('p', { class: 'muted' }, 'Nothing is waiting for you to review.')),
    waiting.length ? h('div', { class: 'panel' }, h('h2', {}, 'Yours, waiting for a reviewer (' + waiting.length + ')'), waiting.map(reviewItem)) : '');
}

function reviewItem(it) {
  const k = it.ticket, pr = k.pullRequest;
  const path = itemPath(it);
  const note = h('input', { name: 'rv-note-' + k.id, placeholder: 'What should change? (optional)', maxlength: 1000 });
  const extra = [];
  if (it.requestedOfMe) extra.push(h('div', {}, h('span', { class: 'badge warn' }, 'asked of you')));
  if (it.canReview) {
    if (!it.canComplete && it.blocker) extra.push(h('p', { class: 'muted' }, it.blocker.charAt(0).toUpperCase() + it.blocker.slice(1), '.'));
    extra.push(h('div', { class: 'actions' },
      pr && pr.state === 'open' ? h('button', { class: 'plain small', title: 'After you merged it on your Git host', onclick: () => act(() => api('PUT', path + '/git', { pullRequest: { url: pr.url, number: pr.number, state: 'merged', baseBranch: pr.baseBranch } })) }, 'I merged it: record that') : '',
      h('button', { class: 'primary small', disabled: !it.canComplete, onclick: () => act(() => api('POST', path + '/complete')) }, 'Mark done'),
      h('span', { class: 'inline' }, note, h('button', { class: 'plain small', onclick: () => act(() => api('POST', path + '/request-changes', { note: note.value })) }, 'Request changes')),
      h('button', { class: 'plain small', onclick: () => openProject(it.project.id, 'repository') }, 'Repository state')));
  } else {
    extra.push(h('p', { class: 'muted' }, it.mine ? 'Waiting for a reviewer. You cannot sign off your own work.' : ''));
  }
  return workItem(it, extra);
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
    if (k) parts.push(ticketPanel(d, d.ticket || k)); else state.ticketId = null;
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
    h('button', { class: 'link title', onclick: () => { state.ticketId = k.id === state.ticketId ? null : k.id; state.handoff = null; remember(); render(); } }, k.title),
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
    const blocker = b.completion[k.id];
    if (blocker) actions.push(h('p', { class: 'muted' }, blocker));
    actions.push(h('button', { class: 'primary', disabled: !!blocker, onclick: run('POST', '/complete') }, 'Mark done'),
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
      h('button', { class: 'plain', onclick: () => { state.ticketId = null; state.handoff = null; remember(); render(); } }, 'Close')),
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
    ticketVersion(k) !== k.version ? h('div', { role: 'alert' }, h('p', { class: 'error' }, 'Someone changed this ticket while you were editing. Your draft is kept. Copy your text before loading their version and combining the changes.'), h('button', { class: 'plain', type: 'button', onclick: () => { discardTicketDraft(k.id); render(); } }, 'Load latest version')) : '',
    h('form', { class: 'stack', 'data-ticket-id': k.id, 'data-ticket-version': k.version, onsubmit: (e) => { e.preventDefault(); act(() => api('PATCH', path, { title: title.value, description: desc.value, requirements: reqs.value, version: ticketVersion(k) }).then(() => discardTicketDraft(k.id)));  } },
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
  const quote = s => "'" + s.replaceAll("'", "'\"'\"'") + "'";
  const cmd = 'werkbord-team handoff --server ' + quote(location.origin) + ' --project ' + quote(hf.project.id) + ' --ticket ' + quote(hf.ticket.id) + ' --runner http://127.0.0.1:7420';
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
      h('p', { class: 'muted' }, 'This creates the task in the Werkbord running on your own computer (a localhost address only), using WERKBORD_TEAM_TOKEN and DEVBOARD_TOKEN from your environment. Set both first. Run it again with --report after work, or add --watch to keep metadata synchronized from your computer. Repeated imports reuse your task.'),
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

function readLocation() {
 const q = new URLSearchParams(location.search);
 if (q.has('tab') && TABS.some(x => x[0] === q.get('tab'))) state.tab = q.get('tab');
 if (q.has('project')) state.projectId = q.get('project');
 state.ticketId = q.get('ticket');
}
readLocation();
window.addEventListener('popstate', () => { captureDrafts(); readLocation(); state.data = null; state.handoff = null; render(); });
render();
