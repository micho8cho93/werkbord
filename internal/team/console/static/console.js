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
//   Git        what the team's Werkbords have reported about the Git state
//   Activity   what happened
'use strict';

const app = document.getElementById('app');

const TABS = [['workspace', 'Workspace'], ['projects', 'Projects'], ['board', 'Board'], ['mywork', 'My Work'], ['reviews', 'Reviews'], ['repository', 'Git'], ['activity', 'Activity']];
const ADMIN_TABS = [['settings', 'This computer'], ['members', 'Members'], ['devices', 'Devices'], ['hosts', 'Workspace Hosts'], ['connectivity', 'Connectivity'], ['backups', 'Backups'], ['license', 'License']];
const PROJECT_TABS = new Set(['board', 'repository', 'activity', 'people']);

const state = {
  token: null, me: null, tab: 'workspace', projectId: null, ticketId: null, secret: null, error: '', info: '',
  invite: null,   // an invite code from the address, waiting to be used
  ov: null,       // the workspace overview: the projects, and the counts the navigation shows
  data: null,     // the open project: its board and what the open tab shows
  handoff: null,  // a handoff the member just opened
  showArchived: false, newTicketOpen: false, column: 'in_progress', repoSection: 'attention',
  online: true,   // whether the live connection to the server is up
  desktop: null, device: null, joinLink: null,
};

try {
  const tok = /(?:^|[#&])token=([^&]+)/.exec(location.hash);
  const inv = /(?:^|[#&])invite=([^&]+)/.exec(location.hash);
  const join = /(?:^|[#&])join=([^&]+)/.exec(location.hash);
  if (tok) sessionStorage.setItem('werkbord-team-token', decodeURIComponent(tok[1]));
  if (inv) state.invite = decodeURIComponent(inv[1]);
  if (join) state.joinLink = decodeURIComponent(join[1]);
  if (tok || inv || join) history.replaceState(null, '', location.pathname + location.search); // tokens and codes never stay in the address bar
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
    else if ((k === 'draggable' || k.startsWith('aria-')) && v != null) el.setAttribute(k, String(v));
    else if (v === true) el.setAttribute(k, '');
    else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const kid of kids.flat(Infinity)) el.append(kid instanceof Node ? kid : document.createTextNode(kid ?? ''));
  return el;
}

// The Werkbord mark and wordmark, and "team": the head of every screen.
function brand() {
  return h('div', { class: 'brand' }, h('img', { src: 'mark.svg', alt: '', width: 30, height: 22 }), h('span', { class: 'wm' }, 'werkbord'), h('span', { class: 'prod' }, 'team'));
}
// The same small stroke vocabulary as the individual app, kept within Team.
function icon(name) {
  const paths = {
    workspace: 'M2 2h5v5H2z M9 2h5v5H9z M2 9h5v5H2z M9 9h5v5H9z',
    projects: 'M1.5 4V2.5h5l1.5 2h6.5v9H1.5z',
    board: 'M2 2h12v12H2z M6 2v12 M10 2v12',
    mywork: 'M5 3H2v11h12V3h-3 M5 2h6v3H5z M5 9l2 2 4-4',
    reviews: 'M3 2h10v12H3z M5 8l2 2 4-4',
    repository: 'M5 2v7a3 3 0 0 0 6 0V7 M3 2a2 2 0 1 0 4 0a2 2 0 1 0-4 0 M9 5a2 2 0 1 0 4 0a2 2 0 1 0-4 0 M3 13a2 2 0 1 0 4 0a2 2 0 1 0-4 0 M5 9v2',
    activity: 'M1 8h3l2-5 4 10 2-5h3',
    settings: 'M6 1h4l.5 2 2 .8 1.8-.6 1.5 2.6-1.4 1.4v2.3l1.4 1.4-1.5 2.6-1.8-.6-2 .8-.5 2H6l-.5-2-2-.8-1.8.6L.2 11l1.4-1.4V7.3L.2 5.9l1.5-2.6 1.8.6 2-.8z M8 5a3 3 0 1 0 0 6a3 3 0 1 0 0-6',
  };
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  for (const [key, value] of Object.entries({ viewBox: '0 0 16 16', width: '18', height: '18', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.3', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true', focusable: 'false' })) svg.setAttribute(key, value);
  const path = document.createElementNS(svg.namespaceURI, 'path'); path.setAttribute('d', paths[name] || paths.projects); svg.append(path);
  return svg;
}

function settingsTab() { return ADMIN_TABS.some(([id]) => id === state.tab); }
function availableSettings() { return ADMIN_TABS.filter(([id]) => !['hosts', 'connectivity', 'backups'].includes(id) || can('devices.view_all')); }

function navigation(counts) {
  const projects = state.ov.projects.filter(p => !p.project.archived);
  return h('aside', { class: 'rail', 'aria-label': 'Werkbord Team navigation' },
    h('button', { class: 'brand-home', type: 'button', onclick: () => go('workspace'), 'aria-label': 'Werkbord Team workspace' }, brand()),
    h('div', { class: 'workspace-name', title: state.me.workspace.name }, state.me.workspace.name),
    h('nav', { class: 'rail-primary', 'aria-label': 'Werkbord Team' }, TABS.map(([id, label]) =>
      h('button', { name: 'nav-' + id, type: 'button', 'aria-label': label, 'aria-description': counts[id] ? plural(counts[id], 'ticket') : null, 'aria-current': navTab() === id ? 'page' : null, onclick: () => go(id) }, icon(id), h('span', { class: 'nav-label' }, label),
        badgeOn(counts[id], id === 'repository' ? 'bad' : id === 'reviews' ? '' : 'quiet')))),
    projects.length ? h('nav', { class: 'project-links', 'aria-label': 'Projects' },
      h('span', { class: 'lab' }, 'Projects'),
      projects.map(p => h('button', { type: 'button', 'aria-label': 'Open project ' + p.project.name, 'aria-current': PROJECT_TABS.has(state.tab) && state.projectId === p.project.id ? 'true' : null, title: p.project.name, onclick: () => openProject(p.project.id, PROJECT_TABS.has(state.tab) ? state.tab : 'board') },
        h('span', { class: 'project-dot', 'aria-hidden': 'true' }), h('span', { class: 'nav-label' }, p.project.name), badgeOn(p.toReview)))) : '',
    h('div', { class: 'rail-foot' },
      h('nav', { class: 'rail-settings', 'aria-label': 'Settings' }, h('button', { name: 'nav-settings', type: 'button', 'aria-current': settingsTab() ? 'page' : null, onclick: () => go('settings') }, icon('settings'), h('span', {}, 'Settings'))),
      h('div', { class: 'profile' }, initial(state.me.member.name), h('div', { class: 'grow' }, h('strong', {}, state.me.member.name), h('span', { class: 'muted small' }, state.me.member.role)),
        state.desktop ? '' : h('button', { class: 'link small', onclick: signOut }, 'Sign out')),
      h('span', { class: 'connection', role: 'status' }, h('span', { class: 'status-dot', 'data-online': String(state.online), 'aria-hidden': 'true' }), state.online ? 'Connected' : 'Reconnecting')));
}

function settingsLayout(body) {
  return h('div', { class: 'settings-layout' },
    h('nav', { class: 'settings-sections', 'aria-label': 'Team administration' }, availableSettings().map(([id, label]) =>
      h('button', { type: 'button', 'aria-current': state.tab === id ? 'page' : null, onclick: () => go(id) }, label))),
    h('div', { class: 'settings-content' }, body));
}
function sectionButtons(key, choices, label) {
  return h('nav', { class: 'section-controls', 'aria-label': label, 'data-scroll-key': 'section-' + key }, choices.map(([id, title, count]) =>
    h('button', { type: 'button', 'aria-current': state[key] === id ? 'page' : null, onclick: () => { state[key] = id; render(); } }, title,
      count == null ? '' : h('span', { class: 'badge' }, String(count)))));
}
function initial(name) { return h('span', { class: 'av', 'aria-hidden': 'true' }, (String(name || '?').trim()[0] || '?').toUpperCase()); }

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
  // The host says why when the reason is not the token itself: a clock that disagrees, or a member token used from another
  // computer. Signing the person out or telling them the device lost access would send them the wrong way.
  const why = res.status === 401 && data.error && (data.error.code === 'clock_skew' || data.error.code === 'device_required') ? data.error : null;
  if (why) {
    const err = new Error(why.message);
    err.status = 401; err.code = why.code;
    if (why.code === 'device_required' && !state.desktop) { state.error = why.message; signOut(); } else stopSync();
    throw err;
  }
  if (res.status === 401 && state.desktop) throw new Error('This device no longer has workspace access. Ask your administrator to approve a new invitation.');
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

let actionBusy = false;
async function act(fn) {
  if (actionBusy) return;
  actionBusy = true;
  state.error = ''; state.info = '';
  const controls = [...app.querySelectorAll('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled)')];
  controls.forEach(el => { el.disabled = true; });
  app.setAttribute('aria-busy', 'true');
  try { await fn(); } catch (e) { state.error = e.message; }
  finally { actionBusy = false; controls.forEach(el => { el.disabled = false; }); app.removeAttribute('aria-busy'); }
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
 keep.regions = [...root.querySelectorAll('[data-scroll-key]')].map(el => ({ key: el.dataset.scrollKey, x: el.scrollLeft, y: el.scrollTop }));
 for (const el of root.querySelectorAll('[name]')) {
  if (el.dataset.dirty === '1' && el.type !== 'file') keep.fields[el.name] = { v: el.type === 'checkbox' ? el.checked : el.value };
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
  if (k && el.type !== 'file') { if (el.type === 'checkbox') el.checked = k.v; else el.value = k.v; el.dataset.dirty = '1'; }
 }
 for (const [key, el] of disclosures(root)) if (key in keep.details) el.open = keep.details[key];
 if (focus && keep.focus) for (const el of root.querySelectorAll('[name]')) if (el.name === keep.focus.name) {
  el.focus({ preventScroll: true });
  try { if (keep.focus.start != null) el.setSelectionRange(keep.focus.start, keep.focus.end); } catch (_) {}
 }
 if (focus && keep.scroll) window.scrollTo(keep.scroll.x, keep.scroll.y);
 for (const region of keep.regions || []) for (const el of root.querySelectorAll('[data-scroll-key]')) if (el.dataset.scrollKey === region.key) { el.scrollLeft = region.x; el.scrollTop = region.y; }
}
function discardTicketDraft(id) {
 draftVersions.delete(id);
 for (const keep of drafts.values()) for (const name of Object.keys(keep.fields)) if (name.endsWith('-' + id)) delete keep.fields[name];
 for (const el of app.querySelectorAll('[name]')) if (el.name.endsWith('-' + id)) delete el.dataset.dirty;
}
function ticketVersion(k) { return draftVersions.get(k.id) || k.version; }

// The connection banner and the toasts live outside #app, so a re-render never removes them.
const banner = h('p', { class: 'banner', role: 'status', hidden: true }, 'Reconnecting… Work may be out of date.');
const toasts = h('div', { class: 'toasts', 'aria-live': 'polite' });
document.body.prepend(banner);
document.body.append(toasts);
function setOnline(on) {
  state.online = on; banner.hidden = on;
  const status = app.querySelector('.connection');
  if (status) status.replaceChildren(h('span', { class: 'status-dot', 'data-online': String(on), 'aria-hidden': 'true' }), on ? 'Connected' : 'Reconnecting');
}

// When the workspace's storage has no quorum of Workspace Hosts, reading works and every change is refused until enough of them
// are back; the server says so in /health, and this says it to the person, so that a refused change is not a mystery.
const readOnly = h('p', { class: 'banner', role: 'status', hidden: true });
document.body.prepend(readOnly);
async function checkReadOnly() {
  try {
    const res = await fetch('/api/team/v1/health', { headers: { Accept: 'application/json', ...(state.token ? { Authorization: 'Bearer ' + state.token } : {}) } });
    const j = await res.json();
    const st = j && j.storage;
    const previouslyReadOnly = !readOnly.hidden;
    readOnly.hidden = !(st && st.readOnly);
    if (state.desktop && state.me && !actionBusy && previouslyReadOnly !== !readOnly.hidden) await render();
    const panel = app.querySelector('.resilience');
    if (panel && !actionBusy && state.me) panel.replaceWith(await resiliencePanel(panel.classList.contains('compact-health')));
    if (!readOnly.hidden) readOnly.textContent = state.desktop ? 'This workspace is read-only for now. You can keep reading. Keep the Workspace Hosts online so Team can safely save changes again.' : 'This workspace is read-only for now: ' + (st.reason || 'its storage has no quorum') + '. You can read; changes are refused until it is back.';
  } catch (e) { /* the connection banner says when the server cannot be reached */ }
}
checkReadOnly();
setInterval(checkReadOnly, 10000);
function toast(message) {
  const t = h('div', { class: 'toast', role: 'status' }, message);
  toasts.append(t);
  while (toasts.children.length > 4) toasts.firstChild.remove();
  setTimeout(() => t.remove(), 7000);
}

// ---- navigation ----

function navTab() { return state.tab === 'people' ? 'projects' : state.tab; }
let focusMain = false;
function go(tab) { focusMain = true; state.tab = tab; state.ticketId = null; state.handoff = null; state.secret = null; state.error = ''; state.info = ''; remember(); return render(); }
function openProject(id, tab) { focusMain = true; state.projectId = id; state.tab = tab || 'board'; state.ticketId = null; state.handoff = null; state.data = null; state.secret = null; remember(); return render(); }
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
  if (typeof desktopGate === 'function' && await desktopGate()) { app.classList.remove('signed-in'); return; }
  if (!state.token) { stopSync(); app.classList.remove('signed-in'); app.replaceChildren(state.invite ? joinScreen() : signIn()); return; }
  try {
    [state.me, state.ov] = await Promise.all([api('GET', '/me'), api('GET', '/overview')]);
  } catch (e) {
    if (state.desktop) { state.error = e.message; app.replaceChildren(desktopConnecting()); scheduleDesktopRefresh(); return; }
    if (e instanceof TypeError) { // the network, not the token: keep what is on screen and keep trying
      setOnline(false);
      if (!app.firstChild || app.querySelector('.loading')) app.replaceChildren(unreachable());
      startSync();
      return;
    }
    if (state.token) state.error = e.message;
    app.classList.remove('signed-in'); app.replaceChildren(signIn());
    return;
  }
  if (state.invite) { app.classList.remove('signed-in'); app.replaceChildren(joinScreen()); return; }
  setOnline(true);
  let body;
  try {
    switch (state.tab) {
      case 'workspace': body = await workspaceView(); break;
      case 'projects': body = await projectsView(); break;
      case 'mywork': body = await myWorkView(); break;
      case 'reviews': body = await reviewsView(); break;
      case 'members': body = await membersView(); break;
      case 'devices': case 'hosts': case 'connectivity': case 'backups': case 'license': case 'settings': body = await administrationView(state.tab); break;
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
  app.classList.add('signed-in');
  const ov = state.ov;
  const counts = {
    board: sum(ov.projects, (p) => p.counts.available || 0),
    mywork: sum(ov.projects, (p) => p.mine),
    reviews: sum(ov.projects, (p) => p.toReview),
    repository: sum(ov.projects, (p) => p.problems),
  };
  const title = settingsTab() ? 'Settings' : TABS.find(([id]) => id === navTab())?.[1] || 'Projects';
  app.replaceChildren(h('div', { class: 'app-shell' + (state.tab === 'board' ? ' is-board' : '') },
    h('button', { class: 'skip-link', onclick: () => app.querySelector('#main-title').focus() }, 'Skip to content'),
    navigation(counts),
    h('main', { class: 'main-pane' },
      h('header', { class: 'top page-head' }, h('h1', { id: 'main-title', tabindex: '-1' }, title),
        state.tab === 'workspace' ? h('span', { class: 'workspace-context' }, state.me.workspace.name) : ''),
      h('div', { class: 'main-content', 'data-scroll-key': 'main' },
        state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
        state.info ? h('p', { class: 'ok', role: 'status' }, state.info) : '',
        secretBox(), settingsTab() ? settingsLayout(body) : body))));
  restore(app, keep, previousScope === screenScope());
  const activeNav = app.querySelector('.rail-primary [aria-current="page"]');
  if (activeNav && matchMedia('(max-width: 899px)').matches) activeNav.parentElement.scrollLeft = Math.max(0, activeNav.offsetLeft - 16);
  const columnNav = app.querySelector('.column-pills nav');
  const activeColumn = columnNav?.querySelector('[aria-current="page"]');
  if (activeColumn) columnNav.scrollLeft = Math.max(0, activeColumn.offsetLeft - columnNav.offsetLeft - (columnNav.clientWidth - activeColumn.offsetWidth) / 2);
  if (focusMain) { app.querySelector('#main-title').focus({ preventScroll: true }); focusMain = false; }
  renderedScope = screenScope();
}

function unreachable() {
  return h('div', { class: 'panel' }, h('h2', {}, 'Cannot reach the Team server'),
    h('p', { class: 'muted' }, 'Check your connection. This page keeps trying and will load by itself.'),
    h('button', { class: 'primary', onclick: () => render() }, 'Try now'));
}

function signIn() {
  const input = h('input', { type: 'password', autocomplete: 'off', placeholder: 'wbt_…', required: true, 'aria-label': 'Token' });
  return h('div', { class: 'gate' },
    h('header', { class: 'top' }, brand()),
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
  const invite = s.kind === 'invite';
  // A project invite is a code and nothing else. A link would carry this window's own address, which is this computer's
  // (the Team app's window is a page of a service on 127.0.0.1) and means nothing on anyone else's.
  const link = invite ? '' : location.origin + '/#token=' + s.token;
  return h('div', { class: 'secret', role: 'status' },
    h('strong', {}, invite ? 'Invite code for ' + s.name : s.self ? 'Your token' : 'Token for ' + s.name),
    h('p', { class: 'muted' }, invite
      ? 'Shown once. Share privately with a workspace member; they join with this code under Projects.'
      : s.self ? 'Shown once; it is not stored and cannot be shown again. Keep it private: it is how you sign in.'
      : 'Shown once; it is not stored and cannot be shown again. Send it to them privately.'),
    h('code', {}, s.token),
    invite ? '' : [h('span', { class: 'muted' }, s.self ? 'Or a link that signs you in: ' : 'Or a link that signs them in: '), h('code', {}, link)],
    h('div', { class: 'actions' }, invite ? '' : copy(link, 'Copy link'), copy(s.token, 'Copy ' + (invite ? 'code' : 'token')),
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
  return h('div', { class: 'gate' }, h('header', { class: 'top' }, brand()), h('div', { class: 'panel' }, content));
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
  'ticket.archived': 'archived', 'ticket.restored': 'restored', 'ticket.created': 'created', 'ticket.claimed': 'claimed', 'ticket.released': 'released', 'ticket.reassigned': 'reassigned',
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

function statusName(st) { return { backlog: 'Backlog', available: 'Available', in_progress: 'In progress', review: 'In review', done: 'Done' }[st] || st; }

async function workspaceView() {
  const ov = state.ov, me = state.me.member.id;
  const w = await api('GET', '/my-work');
  const mine = sum(ov.projects, (p) => p.mine), toReview = sum(ov.projects, (p) => p.toReview);
  const best = ov.projects.filter((p) => !p.project.archived).sort((a, b) => (b.counts.available || 0) - (a.counts.available || 0))[0];

  const working = ov.working.length
    ? ov.working.map((it) => h('div', { class: 'row' },
        h('div', { class: 'grow' },
          h('button', { class: 'link', onclick: () => openTicket(it.project.id, it.ticket.id) }, it.ticket.key + ' · ' + it.ticket.title),
          h('div', { class: 'muted' }, it.project.name + ' · ' + (it.ticket.assigneeId === me ? 'You' : it.author || 'a former member'))),
        h('span', { class: 'badge' }, statusName(it.ticket.status)),
        mergeBadge(it),
        when(it.ticket.updatedAt)))
    : h('div', { class: 'empty-state' }, h('p', { class: 'muted' }, 'No work in progress.'), h('button', { class: 'plain', onclick: () => best ? openProject(best.project.id) : go('projects') }, best ? 'Open board' : can('projects.create') ? 'Create a project' : 'Join a project'));

  const projects = ov.projects.length
    ? ov.projects.map((p) => h('div', { class: 'row' },
        h('div', { class: 'grow' }, h('button', { class: 'link', onclick: () => openProject(p.project.id, 'board') }, p.project.name), p.project.archived ? ' (archived)' : '',
          h('div', { class: 'muted' }, ['available', 'in_progress', 'review'].filter(st => p.counts[st]).map(st => p.counts[st] + ' ' + statusName(st).toLowerCase()).join(' · ') || 'No active tickets')),
        p.toReview ? h('span', { class: 'badge warn' }, p.toReview + ' to review') : '',
        p.problems ? h('span', { class: 'badge bad' }, plural(p.problems, 'problem')) : ''))
    : h('p', { class: 'muted' }, can('projects.view_all') ? 'No projects yet. Create one under Projects.' : 'You are not on any project yet. Ask for an invite code.');

  const attention = w.needsAction.map(a => h('div', { class: 'row attn ' + a.level },
    h('div', { class: 'grow' }, a.message), h('button', { class: 'plain small', onclick: () => openTicket(a.projectId, a.ticketId) }, 'Open ' + a.ticketKey)));
  const shortcut = (label, count, click, tone) => h('button', { class: 'overview-shortcut', onclick: click }, label, h('span', { class: 'badge' + (tone ? ' ' + tone : '') }, String(count)));
  return h('div', { class: 'workspace-layout' + (attention.length || toReview ? ' has-attention' : '') },
    h('section', { class: 'work-feed', id: 'working-now', 'aria-label': 'Working now' }, h('div', { class: 'section-head' }, h('h2', {}, 'Working now'), h('span', { class: 'badge' }, String(ov.working.length))), working),
    h('aside', { class: 'workspace-side', 'aria-label': 'Workspace summary' },
      h('section', { class: 'overview-attention' }, h('h2', {}, 'Needs you'), attention.length ? attention : h('p', { class: 'muted' }, 'All caught up.'),
        shortcut('My Work', mine, () => go('mywork')), shortcut('To review', toReview, () => go('reviews'), toReview ? 'warn' : ''),
        shortcut('Available to claim', ov.available, () => best ? openProject(best.project.id) : go('projects'))),
      h('section', {}, h('div', { class: 'section-head' }, h('h2', {}, 'Projects'), h('button', { class: 'link', onclick: () => go('projects') }, 'View all')), projects),
      state.desktop && !state.device.runner.configured ? h('button', { class: 'plain', onclick: () => go('settings') }, 'Connect your runner') : ''));
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
    (!state.desktop && (m.id === state.me.member.id || can('members.manage')))
      ? h('button', { class: 'plain', onclick: () => act(async () => { const r = await api('POST', '/members/' + m.id + '/token'); const self = r.member.id === state.me.member.id;
          if (self) { stopSync(); state.token = r.token; try { sessionStorage.setItem('werkbord-team-token', r.token); } catch (_) {} }
          state.secret = { kind: 'token', name: r.member.name, self, token: r.token }; }) }, 'New token') : '',
    (can('members.manage') && m.role !== 'owner')
      ? h('button', { class: 'danger', onclick: () => confirm('Remove ' + m.name + ' from the workspace? Anything they are working on goes back on the board.') && act(() => api('DELETE', '/members/' + m.id)) }, 'Remove') : ''));
  if (can('admins.manage')) members.forEach((m, i) => { if (m.role !== 'owner') rows[i].append(h('button', { class: 'plain', onclick: () => act(() => api('PUT', '/members/' + m.id + '/role', { role: m.role === 'admin' ? 'member' : 'admin' })) }, m.role === 'admin' ? 'Remove admin role' : 'Make admin')); });
  const panels = [h('div', { class: 'panel' }, h('h2', {}, 'Members (' + members.length + ')'), rows)];
  if (can('members.manage') && !state.desktop) {
    const name = h('input', { name: 'm-name', required: true, maxlength: 80 });
    const email = h('input', { name: 'm-email', type: 'email', maxlength: 254 });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'Add a member'),
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
    rows.length ? rows : h('p', { class: 'muted' }, can('projects.view_all') ? 'No projects yet.' : 'You are not on any project yet. Ask for an invite code.'))];
  const code = h('input', { name: 'join-code', required: true, maxlength: 200, autocomplete: 'off', placeholder: 'wbi_…' });
  panels.push(h('div', { class: 'panel' }, h('h2', {}, 'Join a project with a code'),
    h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => {
        const j = await api('POST', '/invites/join', { code: code.value.trim() });
        code.value = ''; state.projectId = j.project.id; state.tab = 'board'; state.data = null; remember(); state.info = 'You joined ' + j.project.name + '.'; }); } },
      field('Invite code', code), h('button', { class: 'primary' }, 'Join'))));
  if (can('projects.create')) {
    const name = h('input', { name: 'p-name', required: true, maxlength: 80 });
    const desc = h('input', { name: 'p-desc', maxlength: 2000 });
    const repo = h('input', { name: 'p-repo', placeholder: 'https://github.com/owner/repo (optional)', maxlength: 2048 });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'New project'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { const p = await api('POST', '/projects', { name: name.value, description: desc.value, repository: repo.value });
          name.value = ''; desc.value = ''; repo.value = ''; state.projectId = p.id; state.tab = 'board'; state.data = null; remember(); }); } },
        field('Name', name), field('Description (optional)', desc), field('Repository', repo), h('button', { class: 'primary' }, 'Create'))));
  }
  return h('div', { class: 'project-grid' }, panels[0], h('div', { class: 'project-forms' }, panels.slice(1)));
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
    h('p', { class: 'muted' }, can('projects.create') ? 'Create a project under Projects to get started.' : 'You are not on any project yet. Ask for an invite code.'));
  const d = await loadProject();
  const p = d.board.project;
  const switcher = h('select', { name: 'project-switch', 'aria-label': 'Project', onchange: (e) => openProject(e.target.value, state.tab) },
    state.ov.projects.map((x) => h('option', { value: x.project.id, selected: x.project.id === p.id }, x.project.name + (x.project.archived ? ' (archived)' : ''))));
  const content = state.tab === 'repository' ? repositoryTab(d) : state.tab === 'activity' ? activityTab(d) : state.tab === 'people' ? peopleTab(d) : boardTab(d);
  return h('div', { class: 'project-view' },
    state.tab === 'people' ? h('p', {}, h('button', { class: 'link', onclick: () => go('projects') }, '← Projects')) : '',
    h('div', { class: 'project-head' },
      h('div', { class: 'project-bar' }, h('label', { class: 'inline' }, 'Project ', switcher),
        state.tab === 'board' ? h('button', { class: 'plain small', onclick: () => openProject(p.id, 'people') }, 'People & invites') : '',
        h('details', { class: 'project-details' }, h('summary', {}, 'Project details'),
          h('div', { class: 'project-detail-content' },
            p.description ? h('p', {}, p.description) : '',
            p.repository ? h('p', {}, isHTTPS(p.repository) ? extLink(p.repository, p.repository) : p.repository) : '',
            h('span', { class: 'muted' }, d.board.member ? 'Your role: ' + d.board.role : 'You are not a project member.'),
            can('projects.manage') ? h('button', { class: 'danger small', onclick: () => act(() => api('PATCH', '/projects/' + p.id, { archived: !p.archived })) }, p.archived ? 'Unarchive project' : 'Archive project') : '')))),
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
    : h('p', { class: 'muted' }, 'No branches reported.');

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

function personName(d, id) {
  if (!id) return '';
  const p = d.board.people.find((x) => x.id === id);
  return p ? p.name : 'a former member';
}

let draggedTicket = null;
const pendingTickets = new Set();
function dropAction(d, k, to) {
  if (!k || k.archivedAt || pendingTickets.has(k.id) || k.status === to) return null;
  const creator = k.creatorId === state.me.member.id || pcan('tickets.edit');
  const holder = k.assigneeId === state.me.member.id || pcan('tickets.assign');
  if ((k.status === 'backlog' && to === 'available' || k.status === 'available' && to === 'backlog') && creator) return ['move', { status: to }];
  if (to === 'in_progress' && k.status === 'available' && d.board.member && pcan('tickets.claim')) return ['claim'];
  if (to === 'in_progress' && k.status === 'backlog' && d.board.member && pcan('tickets.assign')) return ['assign', { memberId: state.me.member.id }];
  if (k.status === 'in_progress' && to === 'available' && holder) return ['release'];
  if (k.status === 'in_progress' && to === 'review' && holder) return ['submit', {}];
  if (k.status === 'review' && to === 'done' && pcan('tickets.review') && !d.board.completion[k.id]) return ['complete'];
  if (k.status === 'done' && to === 'available' && pcan('tickets.reopen')) return ['move', { status: to }];
  return null;
}
function boardTab(d) {
  const b = d.board;
  const toolbar = h('div', { class: 'actions board-tools' },
    h('button', { class: 'plain', 'aria-pressed': state.showArchived, onclick: () => { state.showArchived = !state.showArchived; render(); } }, state.showArchived ? 'Back to board' : 'Archive (' + (b.archived || []).length + ')'),
    pcan('tickets.reopen') ? h('button', { class: 'plain', disabled: !b.tickets.some(k => k.status === 'done'), onclick: () => act(async () => { const result = await api('POST', '/projects/' + d.id + '/tickets/archive-done'); state.info = plural(result.archived, 'ticket') + ' moved to the archive.'; }) }, 'Clear Done') : '',
    pcan('tickets.create') && !b.project.archived ? h('button', { class: 'primary', title: state.newTicketOpen ? 'Cancel new ticket' : 'New ticket (N)', onclick: () => { state.newTicketOpen = !state.newTicketOpen; render(); } }, state.newTicketOpen ? 'Cancel new ticket' : 'New ticket') : '');
  const cols = b.statuses.map((s) => {
    const items = b.tickets.filter((k) => k.status === s.status);
    return h('section', { class: 'col', 'aria-label': s.label, 'data-active': String(state.column === s.status),
      ondragover: e => { const k = b.tickets.find(t => t.id === draggedTicket); if (!k || !dropAction(d, k, s.status)) return; e.preventDefault(); e.dataTransfer.dropEffect = 'move'; e.currentTarget.classList.add('drop-over'); },
      ondragleave: e => { if (!e.currentTarget.contains(e.relatedTarget)) e.currentTarget.classList.remove('drop-over'); },
      ondrop: e => { e.preventDefault(); e.currentTarget.classList.remove('drop-over'); const k = b.tickets.find(t => t.id === draggedTicket); if (!k) return; const action = dropAction(d, k, s.status); draggedTicket = null; if (!action) return; pendingTickets.add(k.id); toast('Updating ' + k.key + '…'); act(async () => { try { await api('POST', projectPath(k) + '/' + action[0], action[1]); } finally { pendingTickets.delete(k.id); } }); }
    },
      h('h3', {}, s.label, ' ', h('span', { class: 'badge' }, String(items.length))),
      h('div', { class: 'column-cards', 'data-scroll-key': 'column-' + s.status }, items.map((k) => card(d, k))));
  });
  let open = null;
  if (state.ticketId) {
    const k = [...b.tickets, ...(b.archived || [])].find((x) => x.id === state.ticketId);
    if (k) open = ticketPanel(d, d.ticket || k); else state.ticketId = null;
  }
  const columns = h('div', { class: 'column-pills' }, sectionButtons('column', b.statuses.map(s => [s.status, s.label, b.tickets.filter(k => k.status === s.status).length]), 'Board columns'));
  return h('div', { class: 'board-view' }, toolbar,
    state.newTicketOpen ? newTicketForm(d) : '',
    !state.showArchived ? columns : '',
    h('div', { class: 'tboard' + (open ? ' with-ticket' : '') }, state.showArchived ? archiveTab(d) : h('div', { class: 'board' }, cols), open || ''));
}
function archiveTab(d) {
  const rows = (d.board.archived || []).slice().reverse().map(k => h('div', { class: 'row', 'data-search': (k.title + ' ' + k.description + ' ' + k.key).toLowerCase() },
    h('div', { class: 'grow' }, h('button', { class: 'link', onclick: () => openTicket(d.id, k.id) }, k.key + ' · ' + k.title), h('p', { class: 'muted' }, statusName(k.status) + ' · Archived ' + ago(k.archivedAt))),
    canArchiveTicket(k) ? h('button', { class: 'plain small', onclick: () => act(() => api('POST', projectPath(k) + '/archive', { version: k.version, archived: false })) }, 'Restore') : ''));
  const list = h('div', { class: 'archive-list' }, rows.length ? rows : h('p', { class: 'muted' }, 'No archived work yet. Clear Done or close a ticket to keep it here.'));
  const search = h('input', { name: 'archive-search', type: 'search', placeholder: 'Search title, ticket key or details', oninput: e => { for (const row of list.querySelectorAll('[data-search]')) row.hidden = !row.dataset.search.includes(e.target.value.toLowerCase()); } });
  return h('section', { class: 'panel archive-panel' }, h('h2', {}, 'Archived work'), field('Search archive', search), list);
}
function canArchiveTicket(k) {
  if (k.status === 'done') return pcan('tickets.reopen');
  if (['in_progress', 'review'].includes(k.status)) return k.assigneeId === state.me.member.id || pcan('tickets.assign');
  return k.creatorId === state.me.member.id || pcan('tickets.edit');
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
  return h('article', { class: 'card' + (mine ? ' mine' : '') + (k.id === state.ticketId ? ' open' : ''), draggable: !pendingTickets.has(k.id), 'aria-busy': pendingTickets.has(k.id), ondragstart: e => { draggedTicket = k.id; e.dataTransfer.setData('text/plain', k.id); e.dataTransfer.effectAllowed = 'move'; }, ondragend: () => { draggedTicket = null; document.querySelectorAll('.drop-over').forEach(el => el.classList.remove('drop-over')); } },
    h('div', { class: 'muted small' }, k.key),
    h('button', { class: 'link title', onclick: () => { state.ticketId = k.id === state.ticketId ? null : k.id; state.handoff = null; remember(); render(); } }, k.title),
    k.assigneeId ? h('div', { class: 'owner' }, initial(mine ? state.me.member.name : personName(d, k.assigneeId)), mine ? 'You' : personName(d, k.assigneeId), k.status === 'in_progress' ? ' · active' : '') : '',
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
        await api('POST', '/projects/' + state.projectId + '/tickets', { title: title.value, description: desc.value, requirements: reqs.value, status: ready.checked ? 'available' : 'backlog' }); state.newTicketOpen = false;
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
  const canEditText = !k.archivedAt && (isCreator || pcan('tickets.edit')) && k.status !== 'done';

  if (!k.archivedAt) {

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

  }
  if (canArchiveTicket(k)) actions.push(h('button', { class: 'plain', onclick: () => act(() => api('POST', path + '/archive', { version: k.version, archived: !k.archivedAt })) }, k.archivedAt ? 'Restore ticket' : 'Close ticket'));
  if (!k.archivedAt && ['in_progress', 'review'].includes(k.status)) actions.push(h('p', { class: 'muted small' }, 'Closing archives the ticket in Team. Stop any running agent in your own Werkbord.'));

  const facts = h('dl', { class: 'facts' },
    h('dt', {}, 'Status'), h('dd', {}, (k.archivedAt ? 'Archived · ' : '') + statusLabel(b, k.status)),
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
    : h('p', { class: 'muted' }, 'No commits reported.');

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
        !k.archivedAt && (mine || pcan('git.report_any')) && (k.status === 'in_progress' || k.status === 'review') ? gitForm(k, path) : '')));
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
  if (state.desktop) return runnerHandoff(hf);
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
      h('p', { class: 'muted' }, 'This creates the task in the Werkbord running on your own computer (a localhost address only), using WERKBORD_TEAM_TOKEN and WERKBORD_TOKEN from your environment. Set both first. Run it again with --report after work, or add --watch to keep metadata synchronized from your computer. Repeated imports reuse your task.'),
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
    sectionButtons('repoSection', [['attention', 'Needs attention', r.attention.length], ['prs', 'Pull requests', r.pullRequests.length], ['branches', 'Branches', r.branches.length]], 'Repository sections'),
    state.repoSection === 'attention' ? h('div', { class: 'panel' }, h('h2', {}, 'Needs attention'), attention) : '',
    state.repoSection === 'prs' ? h('div', { class: 'panel' }, h('h2', {}, 'Pull requests'), prs) : '',
    state.repoSection === 'branches' ? h('div', { class: 'panel scroll' }, h('h2', {}, 'Branches'), branches,
      h('p', { class: 'muted small' }, 'A branch is stale after ' + r.staleAfterDays + ' days without activity.')) : '');
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
      h('p', { class: 'muted' }, 'The code lets someone who is already in this workspace join this project. To bring in a new person or computer, invite them from Members first. Send the code privately; it cannot be shown again.'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => {
          const r = await api('POST', '/projects/' + pid + '/invites', { role: role.value, maxUses: Number(uses.value) || 1, expiresInHours: Number(hours.value) || 168 });
          state.secret = { kind: 'invite', name: d.board.project.name + ' (' + r.invite.role + ')', token: r.code }; }); } },
        field('They join as', role), field('Can be used', uses), field('Valid for (hours)', hours), h('button', { class: 'primary' }, 'Create invite code')),
      list.length ? h('div', {}, h('h3', {}, 'Invites'), list) : ''));
  }
  return h('div', {}, panels);
}

function readLocation() {
 const q = new URLSearchParams(location.search);
 const tab = q.get('tab');
 state.tab = [...TABS, ...ADMIN_TABS].some(x => x[0] === tab) || tab === 'people' ? tab : 'workspace';
 state.projectId = q.get('project') || null;
 state.ticketId = q.get('ticket') || null;
}
readLocation();
window.addEventListener('popstate', () => { captureDrafts(); readLocation(); focusMain = true; state.data = null; state.handoff = null; render(); });
window.addEventListener('keydown', e => {
  if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || e.target.closest('input, textarea, select, [contenteditable="true"]')) return;
  if (e.key === 'Escape' && state.ticketId) { e.preventDefault(); state.ticketId = null; state.handoff = null; remember(); render(); }
  if (e.key.toLowerCase() === 'n' && state.tab === 'board' && pcan('tickets.create') && !state.data.board.project.archived) {
    e.preventDefault(); state.newTicketOpen = true; render().then(() => app.querySelector('[name="t-title"]')?.focus());
  }
});
window.addEventListener('load', () => render());
