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
//   People     the people in your organization
//
// Where the person is has two levels. The window's sidebar (or, outside it, the rail) lists the major parts: the team, and each of
// its projects. Inside a major part one row of tabs lists its own sub-areas: a project has Board, Git, People…, the team has
// Workspace, Projects, People. Both rows are drawn by tabNav, so they look and behave the same.
'use strict';

const app = document.getElementById('app');

// A device service that holds several Team workspaces serves each under /w/<slot>/. Everything this page asks for is under
// the same prefix, and what it remembers is its own workspace's alone: two workspaces open side by side in one window
// share an origin, and so would otherwise share a token.
const BASE = (/^\/w\/[a-z0-9_]{1,24}(?=\/|$)/.exec(location.pathname) || [''])[0];
const SLOT = BASE.slice(3) || 'main';
const KEY = (name) => 'werkbord-team-' + name + (BASE ? ':' + SLOT : '');

// Inside the desktop app this page is one workspace beside the person's other workspaces, and the window's own sidebar is the
// navigation (the shell asks this page to go to a place with a query such as ?tab=board&project=…). Only a place in the
// person's own Werkbord ("Open in Werkbord") is ever sent back to the shell.
const FRAMED = window.parent !== window;
// The window also frames this page only to show a section of Settings beside the workspace the person is in (it loads the page
// with ?hosted=1): such a page is the section and nothing around it. Decided now, before the address is rewritten as the person
// moves about.
const HOSTED = FRAMED && new URLSearchParams(location.search).get('hosted') === '1';

const TABS = [['workspace', 'Workspace'], ['projects', 'Projects'], ['initiatives', 'Initiatives'], ['overview', 'Overview'], ['board', 'Board'], ['calendar', 'Calendar'], ['timeline', 'Timeline'], ['plans', 'Plans'], ['mywork', 'My Work'], ['reviews', 'Reviews'], ['repository', 'Git'], ['activity', 'Runs'], ['analytics', 'Analytics'], ['members', 'People'], ['people', 'Project people']];
const ADVANCED_TABS = new Set(['hosts', 'connectivity']);
const ADMIN_TABS = [['settings', 'This computer'], ['mcp', 'MCP clients'], ['labels', 'Labels'], ['devices', 'Devices'], ['hosts', 'Workspace Hosts'], ['connectivity', 'Connectivity'], ['backups', 'Backups']];
const PROJECT_TABS = new Set(['overview', 'board', 'calendar', 'timeline', 'repository', 'activity', 'people', 'project-settings']);
// The team's own sub-areas, in the order of their tabs. They are the team as a major part; a project's are PROJECT_TABS.
const WORKSPACE_TABS = ['workspace', 'projects', 'initiatives', 'members'];
const SETTINGS_PARTS = new Set(['computer', 'phones', 'memory', 'history']);

const state = {
  token: null, me: null, tab: 'workspace', projectId: null, ticketId: null, secret: null, error: '', info: '',
  invite: null,   // an invite code from the address, waiting to be used
  ov: null,       // the workspace overview: the projects, and the counts the navigation shows
  data: null,     // the open project: its board and what the open tab shows
  handoff: null,  // a handoff the member just opened
  showArchived: false, newTicketOpen: false, repoSection: 'attention', wsSegment: 'all',
  advancedSettings: false,
  settingsPart: '', // which part of This computer's page a section of Settings shows: computer, phones or memory
  filter: { labels: [], match: 'any' }, // the board's and the timeline's label filter: a way of looking, never stored
  calendarMode: 'calendar', calendarAnchor: null,
  tlZoom: 'week', tlGroup: 'none', tlCollapsed: {},
  online: true,   // whether the live connection to the server is up
  desktop: null, device: null, joinLink: null,
  memberQuery: '', validationIssues: [], focusId: '',
  focusComment: '', // a comment to open the next ticket at (from a notification, or a ?comment= address)
};

try {
  const tok = /(?:^|[#&])token=([^&]+)/.exec(location.hash);
  const inv = /(?:^|[#&])invite=([^&]+)/.exec(location.hash);
  const join = /(?:^|[#&])join=([^&]+)/.exec(location.hash);
  if (tok) sessionStorage.setItem(KEY('token'), decodeURIComponent(tok[1]));
  if (inv) state.invite = decodeURIComponent(inv[1]);
  if (join) state.joinLink = decodeURIComponent(join[1]);
  if (tok || inv || join) history.replaceState(null, '', location.pathname + location.search); // tokens and codes never stay in the address bar
  state.token = sessionStorage.getItem(KEY('token'));
  state.projectId = sessionStorage.getItem(KEY('project'));
  const t = sessionStorage.getItem(KEY('tab'));
  if (t && (TABS.some((x) => x[0] === t) || ADMIN_TABS.some((x) => x[0] === t) || t === 'project-settings')) state.tab = t === 'settings' && FRAMED && !HOSTED ? 'workspace' : t;
  if (FRAMED && !HOSTED && ADMIN_TABS.some((x) => x[0] === state.tab)) state.tab = 'workspace'; // the window's Settings has these
} catch (_) { /* storage can be unavailable; the sign-in form still works for the session */ }

function remember() {
  captureDrafts();
  const q = new URLSearchParams(); q.set("tab", state.tab);
  if (state.projectId) q.set("project", state.projectId);
  if (state.ticketId) q.set("ticket", state.ticketId);
  if (state.tab === 'settings' && state.settingsPart) q.set("part", state.settingsPart);
  history.pushState(null, "", location.pathname + "?" + q.toString());
  reportPlace();
  try {
    sessionStorage.setItem(KEY('tab'), state.tab);
    if (state.projectId) sessionStorage.setItem(KEY('project'), state.projectId);
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

// A save becomes useful only when its fields differ from the accepted values. Recompute
// after draft restoration too, so live updates cannot strand a changed form.
function changedSubmit(label, fields, saved = fields.map(fieldValue), className = 'plain') {
  const button = h('button', { class: className, disabled: true }, label);
  button.refreshDisabled = () => { button.disabled = actionBusy || fields.every((el, i) => fieldValue(el) === saved[i]); };
  fields.forEach(el => { el.addEventListener('input', button.refreshDisabled); el.addEventListener('change', button.refreshDisabled); });
  button.refreshDisabled();
  return button;
}
function fieldValue(el) { return el.type === 'checkbox' ? el.checked : el.value; }

// The Werkbord mark and wordmark, and "team": the head of every screen.
function brand() {
  return h('div', { class: 'brand' }, h('img', { src: teamTheme.dark ? 'mark-dark.svg' : 'mark.svg', alt: '', width: 30, height: 22 }), h('span', { class: 'wm' }, 'werkbord'), h('span', { class: 'prod' }, 'team'));
}
// The same icons as the individual app (Lucide, ISC licence: see LICENSE-LUCIDE.txt), one for each meaning and never the same twice.
// Each is a list of [tag, attributes] drawn on Lucide's 24-unit grid, in a line that stays the same weight at every size.
function icon(name) {
  const shapes = {
    workspace: [['rect',{width:'7',height:'7',x:'3',y:'3',rx:'1'}],['rect',{width:'7',height:'7',x:'14',y:'3',rx:'1'}],['rect',{width:'7',height:'7',x:'14',y:'14',rx:'1'}],['rect',{width:'7',height:'7',x:'3',y:'14',rx:'1'}]],
    projects: [['path',{d:'M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z'}],['path',{d:'M8 10v4'}],['path',{d:'M12 10v2'}],['path',{d:'M16 10v6'}]],
    board: [['rect',{width:'18',height:'18',x:'3',y:'3',rx:'2'}],['path',{d:'M8 7v7'}],['path',{d:'M12 7v4'}],['path',{d:'M16 7v9'}]],
    timeline: [['path',{d:'M10 6h8'}],['path',{d:'M12 16h6'}],['path',{d:'M3 3v16a2 2 0 0 0 2 2h16'}],['path',{d:'M8 11h7'}]],
    plans: [['path',{d:'M13 5h8'}],['path',{d:'M13 12h8'}],['path',{d:'M13 19h8'}],['path',{d:'m3 17 2 2 4-4'}],['rect',{x:'3',y:'4',width:'6',height:'6',rx:'1'}]],
    mywork: [['rect',{width:'8',height:'4',x:'8',y:'2',rx:'1',ry:'1'}],['path',{d:'M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2'}],['path',{d:'m9 14 2 2 4-4'}]],
    reviews: [['circle',{cx:'18',cy:'18',r:'3'}],['circle',{cx:'6',cy:'6',r:'3'}],['path',{d:'M6 21V9a9 9 0 0 0 9 9'}]],
    repository: [['circle',{cx:'12',cy:'18',r:'3'}],['circle',{cx:'6',cy:'6',r:'3'}],['circle',{cx:'18',cy:'6',r:'3'}],['path',{d:'M18 9v2c0 .6-.4 1-1 1H7c-.6 0-1-.4-1-1V9'}],['path',{d:'M12 12v3'}]],
    activity: [['path',{d:'M19.07 4.93A10 10 0 0 0 6.99 3.34'}],['path',{d:'M4 6h.01'}],['path',{d:'M2.29 9.62A10 10 0 1 0 21.31 8.35'}],['path',{d:'M16.24 7.76A6 6 0 1 0 8.23 16.67'}],['path',{d:'M12 18h.01'}],['path',{d:'M17.99 11.66A6 6 0 0 1 15.77 16.67'}],['circle',{cx:'12',cy:'12',r:'2'}],['path',{d:'m13.41 10.59 5.66-5.66'}]],
    analytics: [['path',{d:'M3 3v16a2 2 0 0 0 2 2h16'}],['path',{d:'M18 17V9'}],['path',{d:'M13 17V5'}],['path',{d:'M8 17v-3'}]],
    members: [['path',{d:'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2'}],['path',{d:'M16 3.128a4 4 0 0 1 0 7.744'}],['path',{d:'M22 21v-2a4 4 0 0 0-3-3.87'}],['circle',{cx:'9',cy:'7',r:'4'}]],
    search: [['path',{d:'m21 21-4.34-4.34'}],['circle',{cx:'11',cy:'11',r:'8'}]],
    sun: [['circle',{cx:'12',cy:'12',r:'4'}],['path',{d:'M12 2v2'}],['path',{d:'M12 20v2'}],['path',{d:'m4.93 4.93 1.41 1.41'}],['path',{d:'m17.66 17.66 1.41 1.41'}],['path',{d:'M2 12h2'}],['path',{d:'M20 12h2'}],['path',{d:'m6.34 17.66-1.41 1.41'}],['path',{d:'m19.07 4.93-1.41 1.41'}]],
    moon: [['path',{d:'M20.985 12.486a9 9 0 1 1-9.473-9.472c.405-.022.617.46.402.803a6 6 0 0 0 8.268 8.268c.344-.215.825-.004.803.401'}]],
    close: [['path',{d:'M18 6 6 18'}],['path',{d:'m6 6 12 12'}]],
    more: [['circle',{cx:'12',cy:'12',r:'1'}],['circle',{cx:'19',cy:'12',r:'1'}],['circle',{cx:'5',cy:'12',r:'1'}]],
    defaults: [['path',{d:'M10 5H3'}],['path',{d:'M12 19H3'}],['path',{d:'M14 3v4'}],['path',{d:'M16 17v4'}],['path',{d:'M21 12h-9'}],['path',{d:'M21 19h-5'}],['path',{d:'M21 5h-7'}],['path',{d:'M8 10v4'}],['path',{d:'M8 12H3'}]],
    settings: [['path',{d:'M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915'}],['circle',{cx:'12',cy:'12',r:'3'}]],
  };
  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');
  for (const [key, value] of Object.entries({ viewBox: '0 0 24 24', width: '18', height: '18', fill: 'none', stroke: 'currentColor', 'stroke-width': '2.1', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true', focusable: 'false' })) svg.setAttribute(key, value);
  for (const [tag, attributes] of shapes[name] || shapes.projects) {
    const shape = document.createElementNS(ns, tag);
    for (const [key, value] of Object.entries(attributes)) shape.setAttribute(key, value);
    svg.append(shape);
  }
  return svg;
}

function themeButton() {
  const button = h('button', { class: 'theme-toggle', type: 'button', 'data-theme-toggle': true, onclick: () => teamTheme.toggle() });
  updateThemeButton(button);
  return button;
}
function updateThemeButton(button) {
  const label = teamTheme.dark ? 'Light mode' : 'Dark mode';
  button.setAttribute('aria-label', 'Switch to ' + label.toLowerCase());
  button.title = 'Switch to ' + label.toLowerCase();
  button.replaceChildren(icon(teamTheme.dark ? 'sun' : 'moon'), h('span', {}, label));
}
// The style (Paper, Graphite, Classic) is chosen next to the theme wherever this page offers the theme. In the desktop window it is
// chosen in the window's own Settings and told to this page, so only a browser tab shows this.
const STYLE_CHOICES = [['paper', 'Paper'], ['graphite', 'Graphite'], ['classic', 'Classic']];
function styleChoice() {
  return h('div', { class: 'style-choice', role: 'group', 'aria-label': 'Style', 'data-style-choice': true },
    h('span', { class: 'muted small' }, 'Style'),
    STYLE_CHOICES.map(([id, label]) => h('button', { type: 'button', class: 'plain small', 'data-style-id': id, 'aria-pressed': teamStyle.current === id ? 'true' : 'false', onclick: () => teamStyle.set(id) }, label)));
}
window.addEventListener('teamstylechange', () => {
  document.querySelectorAll('[data-style-choice] [data-style-id]').forEach(b => b.setAttribute('aria-pressed', b.dataset.styleId === teamStyle.current ? 'true' : 'false'));
});
window.addEventListener('teamthemechange', () => {
  document.querySelectorAll('[data-theme-toggle]').forEach(updateThemeButton);
  document.querySelectorAll('.brand img').forEach(img => { img.src = teamTheme.dark ? 'mark-dark.svg' : 'mark.svg'; });
});

function settingsTab() { return ADMIN_TABS.some(([id]) => id === state.tab); }
function availableSettings() { return ADMIN_TABS.filter(([id]) => id === 'labels' ? can('labels.manage') : !['hosts', 'connectivity', 'backups'].includes(id) || can('devices.view_all')); }

function navigation(counts) {
  if (FRAMED) return '';
  const projects = state.ov.projects.filter(p => !p.project.archived);
  return h('aside', { class: 'rail', 'aria-label': 'Werkbord Team navigation' },
    h('button', { class: 'brand-home', type: 'button', onclick: () => go('workspace'), 'aria-label': 'Werkbord Team workspace' }, brand()),
    h('div', { class: 'workspace-name', title: state.me.workspace.name }, state.me.workspace.name),
    h('nav', { class: 'rail-primary', 'aria-label': 'Werkbord Team' }, TABS.filter(([id]) => ['workspace', 'projects', 'initiatives', 'reviews', 'members', 'analytics'].includes(id)).map(([id, label]) =>
      h('button', { name: 'nav-' + id, type: 'button', 'aria-label': label, 'aria-description': counts[id] ? plural(counts[id], 'ticket') : null, 'aria-current': navTab() === id ? 'page' : null, onclick: () => go(id) }, icon(id), h('span', { class: 'nav-label' }, label),
        badgeOn(counts[id], id === 'repository' ? 'bad' : id === 'reviews' ? '' : 'quiet')))),
    projects.length ? h('nav', { class: 'project-links', 'aria-label': 'Projects' },
      h('span', { class: 'lab' }, 'Projects'),
      projects.map(p => h('button', { type: 'button', 'aria-label': 'Open project ' + p.project.name, 'aria-current': PROJECT_TABS.has(state.tab) && state.projectId === p.project.id ? 'true' : null, title: p.project.name, onclick: () => openProject(p.project.id, PROJECT_TABS.has(state.tab) ? state.tab : 'board') },
        h('span', { class: 'project-dot', 'aria-hidden': 'true' }), h('span', { class: 'nav-label' }, p.project.name), badgeOn(p.toReview)))) : '',
    h('div', { class: 'rail-foot' },
      h('nav', { class: 'rail-settings', 'aria-label': 'Settings' }, h('button', { name: 'nav-settings', type: 'button', 'aria-label': 'Settings', 'aria-current': settingsTab() ? 'page' : null, onclick: () => go('settings') }, icon('settings'), h('span', {}, 'Settings'))),
      h('div', { class: 'profile' }, initial(state.me.member.name), h('div', { class: 'grow' }, h('strong', {}, state.me.member.name), h('span', { class: 'muted small' }, state.me.member.role)),
        state.desktop ? '' : h('button', { class: 'link small', onclick: signOut }, 'Sign out'))));
}

function settingsLayout(body) {
  // A hosted section uses the window's navigation. Direct links reveal Advanced automatically.
  const choices = availableSettings();
  const button = ([id, label]) => h('button', { type: 'button', 'aria-current': state.tab === id ? 'page' : null, onclick: () => go(id) }, label);
  const advanced = choices.filter(([id]) => ADVANCED_TABS.has(id));
  return h('div', { class: 'settings-layout' + (HOSTED ? ' hosted' : '') },
    HOSTED ? '' : h('nav', { class: 'settings-sections', 'aria-label': 'Team administration' },
      choices.filter(([id]) => !ADVANCED_TABS.has(id)).map(button),
      advanced.length ? h('details', { class: 'advanced-settings', open: state.advancedSettings || ADVANCED_TABS.has(state.tab), ontoggle: e => { state.advancedSettings = e.currentTarget.open; } },
        h('summary', {}, 'Advanced'), advanced.map(button)) : ''),
    h('div', { class: 'settings-content' }, body));
}

function sectionButtons(key, choices, label) {
  return h('nav', { class: 'section-controls', 'aria-label': label, 'data-scroll-key': 'section-' + key }, choices.map(([id, title, count]) =>
    h('button', { type: 'button', 'aria-current': state[key] === id ? 'page' : null, onclick: () => { state[key] = id; render(); } }, title,
      count == null ? '' : h('span', { class: 'badge' }, String(count)))));
}
function revealActiveNavigation() {
  const active = app.querySelector('.rail-primary [aria-current="page"]');
  if (matchMedia('(max-width: 899px)').matches) {
    const navigation = app.querySelector('.rail-primary');
    if (navigation) navigation.scrollLeft = active ? Math.max(0, active.offsetLeft - 16) : 0;
  }
}
matchMedia('(max-width: 899px)').addEventListener('change', revealActiveNavigation);
let navigationResize = 0;
window.addEventListener('resize', () => {
  cancelAnimationFrame(navigationResize);
  navigationResize = requestAnimationFrame(() => { revealActiveNavigation(); centerTimeline(); });
});
function initial(name) { return h('span', { class: 'av', 'aria-hidden': 'true' }, (String(name || '?').trim()[0] || '?').toUpperCase()); }

const HOST_OUTDATED = 'This workspace’s host runs an older Werkbord than this window and cannot do this yet. An administrator updates the host.';
async function api(method, path, body, opts) {
  const credential = state.token;
  const res = await fetch(BASE + '/api/team/v1' + path, {
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
    // A Workspace Host answers an address it has no route for with 404 "no such endpoint", the same words for a typo and for
    // a feature that came after it. This window and its host are updated separately, so that answer almost always means the
    // host is older than the window: say so, and let the screen explain what to do instead of printing the host's words.
    const outdated = res.status === 404 && data.error && data.error.code === 'not_found' && /no such endpoint/i.test(data.error.message || '');
    const err = new Error(outdated ? HOST_OUTDATED : data.error ? data.error.message : 'Werkbord Team could not complete this request. Check the current state before repeating an action.');
    err.status = res.status;
    if (outdated) err.code = 'host_outdated';
    if (res.status === 402 && data.error && data.error.code === 'premium_required') err.code = 'premium_required';
    err.issues = data.issues || [];
    throw err;
  }
  return data;
}

function can(permission) { return state.me && state.me.permissions.includes(permission); }
function pcan(permission) { return !!(state.data && state.data.board.can.includes(permission)); }

function signOut() {
  closeSearch(false);
  try { sessionStorage.removeItem(KEY('token')); } catch (_) {}
  stopSync();
  drafts.clear(); draftVersions.clear(); renderedScope = "";
  if (typeof clearCommentDrafts === 'function') clearCommentDrafts();
  if (typeof executionViews !== 'undefined') executionViews.clear();
  Object.assign(state, { token: null, me: null, ov: null, secret: null, data: null, ticketId: null, handoff: null, memberQuery: '' });
  render();
}

let actionBusy = false;
async function act(fn) {
  if (actionBusy) return;
  actionBusy = true;
  state.error = ''; state.info = ''; state.validationIssues = []; state.focusId = '';
  const controls = [...app.querySelectorAll('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled)')];
  controls.forEach(el => { el.disabled = true; });
  app.setAttribute('aria-busy', 'true');
  try { await fn(); } catch (e) {
    state.validationIssues = e.issues || [];
    if (e.code === 'premium_required') { state.upgrade = e.message; state.error = ''; }
    else state.error = state.validationIssues.length ? 'Fix the highlighted planning issues.' : e.message;
  }
  finally { actionBusy = false; controls.forEach(el => { el.disabled = false; }); app.removeAttribute('aria-busy'); }
  await render();
  if (state.validationIssues.length) app.querySelector('.plan-fields [aria-invalid="true"]')?.focus();
  else if (state.focusId) { document.getElementById(state.focusId)?.focus(); state.focusId = ''; }
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

function field(label, control, ...extra) { return h('label', {}, label, control, ...extra); }
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
let motionResync = true;
let motionFilter = '';
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
function captureDrafts() {
 if (!renderedScope) return;
 const next = snapshot(app), before = drafts.get(renderedScope);
 // A ticket being made lives in a dialog that Escape puts away without losing what was typed, so a draft whose fields are
 // not on the page now is kept until the ticket is created or the dialog is cancelled.
 if (before) for (const [name, v] of Object.entries(before.fields)) if (/^t-(title|desc|reqs)$|^p-.+-new$/.test(name) && !(name in next.fields) && !app.querySelector('[name="' + name + '"]')) next.fields[name] = v;
 drafts.set(renderedScope, next);
}
function restore(root, keep, focus) {
 if (!keep) return;
 for (const el of root.querySelectorAll('[name]')) {
  const k = keep.fields[el.name];
  if (k && el.type !== 'file') { if (el.type === 'checkbox') el.checked = k.v; else el.value = k.v; el.dataset.dirty = '1'; }
 }
 for (const button of root.querySelectorAll('button')) button.refreshDisabled?.();
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
  if (on && !state.online) motionResync = true;
  state.online = on; banner.hidden = on;
  if (!on) {
    app.querySelectorAll('.member-presence').forEach(el => { el.textContent = 'Runner connection unknown'; });
    app.querySelectorAll('.member-phone').forEach(el => { el.textContent = 'Phone connection unknown'; });
  }
}

// When the workspace's storage has no quorum of Workspace Hosts, reading works and every change is refused until enough of them
// are back; the server says so in /health, and this says it to the person, so that a refused change is not a mystery.
const readOnly = h('p', { class: 'banner', role: 'status', hidden: true });
document.body.prepend(readOnly);
async function checkReadOnly() {
  try {
    const res = await fetch(BASE + '/api/team/v1/health', { headers: { Accept: 'application/json', ...(state.token ? { Authorization: 'Bearer ' + state.token } : {}) } });
    const j = await res.json();
    const st = j && j.storage;
    const previouslyReadOnly = !readOnly.hidden;
    readOnly.hidden = !(st && st.readOnly);
    if (state.desktop && state.me && !actionBusy && previouslyReadOnly !== !readOnly.hidden) await render();
    if (state.me && !actionBusy) await refreshTeamStart();
    const panel = app.querySelector('.resilience');
    if (panel && !actionBusy && state.me) panel.replaceWith(await resiliencePanel(panel.classList.contains('compact-health')));
    if (!readOnly.hidden) readOnly.textContent = state.desktop ? 'This workspace is read-only for now. You can keep reading. Keep the Workspace Hosts online so Team can safely save changes again.' : 'This workspace is read-only for now: ' + (st.reason || 'its storage has no quorum') + '. You can read; changes are refused until it is back.';
  } catch (e) { /* the connection banner says when the server cannot be reached */ }
}
checkReadOnly();
setInterval(checkReadOnly, 10000);
setInterval(() => { if (state.tab === 'members' && state.me && !actionBusy && document.visibilityState === 'visible') render(); }, 15000);
function toast(message) {
  const t = h('div', { class: 'toast', role: 'status' }, message);
  toasts.append(t);
  while (toasts.children.length > 4) toasts.firstChild.remove();
  setTimeout(() => t.remove(), 7000);
}

// ---- a host older than this window ----

let hostVersion = '';
const WHAT_THE_HOST_LACKS = { calendar: 'Calendar', timeline: 'Timeline', analytics: 'Analytics', mcp: 'MCP clients', plans: 'Plans', labels: 'Labels', initiatives: 'Initiatives', reviews: 'Reviews', overview: 'Overview' };
async function hostOutdatedView() {
  if (!hostVersion) hostVersion = await api('GET', '/health').then((r) => String(r.version || '').replace(/^v/, '').replace(/-\d+-g[0-9a-f]+$/, '')).catch(() => '');
  const what = WHAT_THE_HOST_LACKS[state.tab] || ([...TABS, ...ADMIN_TABS].find(([id]) => id === state.tab) || [])[1] || 'This screen';
  return h('section', { class: 'panel host-outdated', role: 'status' },
    h('h2', {}, what + ' needs a newer Workspace Host'),
    h('p', {}, 'This workspace’s host is running ', hostVersion ? 'Werkbord ' + hostVersion : 'an older Werkbord', ', which came before ' + what + '. Everything the host does know keeps working.'),
    h('p', { class: 'muted' }, 'A host that holds a workspace is never replaced automatically. An administrator updates it in a planned maintenance window, after a verified backup.'),
    h('div', { class: 'actions' }, h('button', { class: 'plain open', onclick: () => go('hosts') }, 'Open Workspace Hosts'), h('button', { class: 'plain', onclick: () => { hostVersion = ''; render(); } }, 'Check again')));
}

// ---- navigation ----

function navTab() { return state.tab === 'project-settings' ? 'projects' : state.tab; }
let focusMain = false;
// In the window the Settings screen is the window's own: a link from this page to a section of it asks the window.
function askForSettings(tab, part) {
  const place = '?tab=' + tab + (tab === 'settings' && part ? '&part=' + part : '');
  window.parent.postMessage({ type: 'werkbord.frame', event: 'settings', place }, '*');
}
function go(tab, part) { if (FRAMED && !HOSTED && ADMIN_TABS.some(([id]) => id === tab)) return askForSettings(tab, part); focusMain = true; if (tab === 'settings') state.settingsPart = SETTINGS_PARTS.has(part) ? part : state.settingsPart; state.tab = tab; state.ticketId = null; state.handoff = null; state.secret = null; state.error = ''; state.validationIssues = []; state.info = ''; remember(); return render(); }
function openProject(id, tab) { focusMain = true; state.projectId = id; state.tab = tab || 'board'; state.ticketId = null; state.handoff = null; state.data = null; state.secret = null; state.validationIssues = []; remember(); return render(); }
function openTicket(projectId, ticketId) { const keepCalendar = state.projectId === projectId && ['calendar', 'timeline'].includes(state.tab); state.projectId = projectId; if (!keepCalendar) state.tab = 'board'; state.ticketId = ticketId; state.handoff = null; state.data = null; state.validationIssues = []; remember(); return render(); }
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

let errorScope = '';
let rendering = Promise.resolve();
function render() { rendering = rendering.then(renderNow, renderNow); return rendering; }

async function renderNow() {
  if (draggedTicket || document.querySelector('.pointer-dragging, .planning-dragging')) return;
  const requestedScope = screenScope();
  // An error belongs to the screen it happened on. It does not follow the person to the next one.
  if (state.error && errorScope && errorScope !== requestedScope) state.error = '';
  errorScope = state.error ? errorScope || requestedScope : '';
  if (typeof desktopGate === 'function' && await desktopGate()) { closeSearch(false); app.classList.remove('signed-in'); return; }
  if (!state.token) { closeSearch(false); stopSync(); app.classList.remove('signed-in'); app.replaceChildren(state.invite ? joinScreen() : signIn()); return; }
  try {
    // The plan is asked for too, but a Workspace Host from before the plan existed has no such route: that is no plan to show.
    [state.me, state.ov, state.plan] = await Promise.all([api('GET', '/me'), api('GET', '/overview'), api('GET', '/plan').catch(() => null)]);
  } catch (e) {
    if (state.desktop) { closeSearch(false); state.error = e.message; app.replaceChildren(desktopConnecting()); scheduleDesktopRefresh(); return; }
    if (e instanceof TypeError) { // the network, not the token: keep what is on screen and keep trying
      setOnline(false);
      if (!app.firstChild || app.querySelector('.loading')) app.replaceChildren(unreachable());
      startSync();
      return;
    }
    if (state.token) state.error = e.message;
    closeSearch(false);
    app.classList.remove('signed-in'); app.replaceChildren(signIn());
    return;
  }
  if (state.invite) { closeSearch(false); app.classList.remove('signed-in'); app.replaceChildren(joinScreen()); return; }
  setOnline(true);
  let body;
  try {
    switch (state.tab) {
      case 'workspace': body = await workspaceView(); break;
      case 'projects': body = await projectsView(); break;
      case 'initiatives': body = await initiativesView(); break;
      case 'mywork': body = await myWorkView(); break;
      case 'reviews': body = await reviewsView(); break;
      case 'members': body = await membersView(); break;
      case 'analytics': body = await analyticsView(); break;
      case 'plans': body = await mcpPlansView(); break;
      case 'devices': case 'hosts': case 'connectivity': case 'backups': case 'settings': case 'mcp': case 'labels': body = await administrationView(state.tab); break;
      default: body = await projectScopedView();
    }
  } catch (e) {
    if (e instanceof TypeError) { setOnline(false); startSync(); return; }
    // Said once, where the screen would be: not also in the banner above every other screen.
    body = e.code === 'host_outdated' ? await hostOutdatedView() : h('p', { class: 'error', role: 'alert' }, e.message);
  }
  startSync();
  if (requestedScope !== screenScope() && requestedScope.split(":")[1]) return;
  const previousScope = renderedScope;
  captureDrafts();
  const keep = drafts.get(screenScope());
  app.classList.add('signed-in');
  const ov = state.ov;
  const counts = {
    workspace: ov.mentions || 0,
    board: sum(ov.projects, (p) => p.counts.available || 0),
    plans: 0,
    mywork: sum(ov.projects, (p) => p.mine),
    reviews: sum(ov.projects, (p) => p.toReview),
    repository: sum(ov.projects, (p) => p.problems),
  };
  const projectScreen = PROJECT_TABS.has(state.tab) && !!state.data?.board;
  const title = projectScreen ? state.data.board.project.name : settingsTab() ? 'Settings' : TABS.find(([id]) => id === navTab())?.[1] || 'Projects';
  if (draggedTicket || document.querySelector('.pointer-dragging, .planning-dragging')) return;
  const motion = window.WerkbordMotion;
  const motionBefore = motion?.snapshot(app, '[data-board-item], [data-plan-item]');
  const chartsBefore = motion?.chartSnapshot(app);
  const nextMotionFilter = JSON.stringify([state.filter, state.showArchived, state.ownTickets, state.calendarMode, state.calendarAnchor, state.tlZoom, state.tlGroup, state.tlCollapsed]);
  const animateSaved = renderedScope === screenScope() && state.online && !motionResync && motionFilter === nextMotionFilter;
  motionFilter = nextMotionFilter;
  app.replaceChildren(h('div', { class: 'app-shell' + (state.tab === 'board' || state.tab === 'timeline' ? ' is-board' : '') + (FRAMED ? ' framed' : '') + (HOSTED ? ' hosted' : '') },
    h('button', { class: 'skip-link', onclick: () => app.querySelector('#main-title').focus() }, 'Skip to content'),
    navigation(counts),
    h('main', { class: 'main-pane' },
      projectScreen || (HOSTED && settingsTab()) ? '' : h('header', { class: 'top page-head' }, h('h1', { id: 'main-title', tabindex: '-1' }, title),
        state.tab === 'workspace' ? h('span', { class: 'workspace-context' }, state.me.workspace.name) : '', searchButton()),
      h('div', { class: 'main-content', 'data-scroll-key': 'main' },
        workspaceTabs(),
        planBanner(),
        state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
        state.info ? h('p', { class: 'ok', role: 'status' }, state.info) : '',
        secretBox(), settingsTab() ? settingsLayout(body) : body)),
    upgradeDialog()));
  for (const dlg of app.querySelectorAll('dialog.ticket-dialog, dialog.upgrade-dialog')) if (!dlg.open) dlg.showModal();
  restore(app, keep, previousScope === screenScope());
  if (animateSaved) motion?.layout(app, motionBefore, '[data-board-item], [data-plan-item]', true);
  motion?.charts(app, renderedScope === screenScope() ? chartsBefore : undefined);
  motionResync = false;
  revealComment();
  noteMentions(ov.mentions || 0);
  revealActiveNavigation();
  if (focusMain) { app.querySelector('#main-title')?.focus({ preventScroll: true }); focusMain = false; }
  renderedScope = screenScope();
  refreshSearch();
}

function unreachable() {
  return h('div', { class: 'panel' }, h('h2', {}, 'Cannot reach the Team server'),
    h('p', { class: 'muted' }, 'Check your connection. This page keeps trying and will load by itself.'),
    h('button', { class: 'primary', onclick: () => render() }, 'Try now'));
}

function signIn() {
  const input = h('input', { type: 'password', autocomplete: 'off', placeholder: 'wbt_…', required: true, 'aria-label': 'Token' });
  return h('div', { class: 'gate' },
    h('header', { class: 'top' }, brand(), themeButton()),
    h('div', { class: 'panel' },
      h('h2', {}, 'Sign in'),
      state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
      h('p', { class: 'muted' }, 'Paste your token. The person who runs the server gave it to you.'),
      h('form', { onsubmit: (e) => { e.preventDefault(); state.token = input.value.trim(); state.error = '';
          try { sessionStorage.setItem(KEY('token'), state.token); } catch (_) {} render(); } },
        h('label', {}, 'Token', input), h('button', { class: 'primary' }, 'Sign in'))));
}

function secretBox() {
  if (!state.secret) return '';
  const s = state.secret;
  if (s.kind === 'phone') return h('div', { class: 'secret', role: 'status' },
    h('strong', {}, 'Pair your Team phone'),
    h('p', { class: 'muted' }, 'Open this private link on your phone. Anyone with it can read your Team summary until you revoke it.'),
    h('code', {}, s.token), extLink(s.token, 'Open phone link'),
    h('div', { class: 'actions' }, copy(s.token, 'Copy phone link'), h('button', { class: 'plain', onclick: () => { state.secret = null; render(); } }, 'Done')));
  const invite = s.kind === 'invite';
  // A project invite is a code and nothing else. A link would carry this window's own address, which is this computer's
  // (the Team app's window is a page of a service on 127.0.0.1) and means nothing on anyone else's.
  const link = invite ? '' : location.origin + BASE + '/#token=' + s.token;
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
          try { sessionStorage.setItem(KEY('token'), j.token); } catch (_) {}
          state.token = j.token; state.tab = 'board'; state.projectId = j.project.id; remember();
          state.secret = { kind: 'token', name: j.member.name, self: true, token: j.token };
          state.info = 'Welcome to ' + j.project.name + '. Keep your token somewhere safe: it is how you sign in again.';
        }); } },
        field('Your name', name), field('Email (optional)', email), h('button', { class: 'primary' }, 'Join')),
      h('p', { class: 'muted' }, 'Already have a token? ', h('button', { class: 'link', onclick: () => { state.error = ''; render_signin(); } }, 'Sign in first'), '.'));
  }
  return h('div', { class: 'gate' }, h('header', { class: 'top' }, brand(), themeButton()), h('div', { class: 'panel' }, content));
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
          analyticsState.report = null;
          if (r.reset || r.truncated) toast('You were away for a while, so everything was reloaded.');
          else announce(r.events || []);
          await render();
        } else if (state.ticketId && state.data?.progress?.some(p => !p.stale && Date.now() - new Date(p.reportedAt).getTime() > 120000)) {
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
  'project.archived': 'archived the project', 'project.restored': 'restored the project',
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

// The Workspace page is laid out as Individual's is: a bar to choose what to look at, what needs the person as cards in a column, and
// what is going on beside it. Projects are listed by the window's sidebar, so they are not repeated here.
const ACTION_WORDS = { changes_requested: 'Changes requested', conflict: 'Conflict', behind: 'Behind', submit: 'Ready to submit', review_requested: 'Review requested', ready_to_complete: 'Ready to complete', merged: 'Merged', closed: 'Closed', stale: 'Stale' };

async function workspaceView() {
  const ov = state.ov, me = state.me.member.id;
  const [w, mentions, start] = await Promise.all([api('GET', '/my-work'), loadMentions(), teamStart()]);
  const mine = sum(ov.projects, (p) => p.mine), toReview = sum(ov.projects, (p) => p.toReview);
  const best = ov.projects.filter((p) => !p.project.archived).sort((a, b) => (b.counts.available || 0) - (a.counts.available || 0))[0];
  const unread = mentions ? mentions.unread : 0;
  const projectName = (id) => (ov.projects.find((x) => x.project.id === id) || { project: { name: '' } }).project.name;

  const segments = [['all', 'All', w.needsAction.length + (toReview ? 1 : 0) + (mentions && mentions.notifications.length ? 1 : 0)], ['needs', 'Needs you', w.needsAction.length], ['review', 'To review', toReview], ['mentions', 'Mentions', unread]];
  if (!segments.some(([id, , n]) => id === state.wsSegment && (id === 'all' || n > 0))) state.wsSegment = 'all';
  const show = (id) => state.wsSegment === 'all' || state.wsSegment === id;
  const shownSegments = segments.filter(([id, , n]) => id === 'all' || n > 0);
  const bar = shownSegments.length < 2 ? '' : h('div', { class: 'cc-bar' }, h('div', { class: 'seg', role: 'group', 'aria-label': 'What to show' },
    shownSegments.map(([id, label, n]) => h('button', { type: 'button', name: 'ws-segment-' + id, 'aria-pressed': String(state.wsSegment === id), 'aria-label': label + ': ' + plural(n, 'item'), onclick: () => { state.wsSegment = id; render(); } }, label, h('span', { class: 'n', 'aria-hidden': 'true' }, String(n))))));

  const cards = [];
  if (show('needs')) for (const a of w.needsAction) {
    cards.push(h('article', { class: 'cc-card tone-' + a.level },
      h('p', { class: 'cc-kind' }, h('span', { class: 'cc-dot' }), h('b', {}, ACTION_WORDS[a.kind] || a.kind.replaceAll('_', ' ')), h('span', {}, projectName(a.projectId))),
      h('button', { type: 'button', class: 'link cc-title', onclick: () => openTicket(a.projectId, a.ticketId) }, a.message),
      h('div', { class: 'cc-acts' }, h('button', { type: 'button', class: 'plain small', onclick: () => openTicket(a.projectId, a.ticketId) }, 'Open ' + a.ticketKey))));
  }
  if (show('review') && toReview) cards.push(h('article', { class: 'cc-card tone-info' },
    h('p', { class: 'cc-kind' }, h('span', { class: 'cc-dot' }), h('b', {}, 'To review')),
    h('p', { class: 'cc-title' }, plural(toReview, 'ticket') + ' waiting for a review you can do'),
    h('div', { class: 'cc-acts' }, h('button', { type: 'button', class: 'plain open small', onclick: () => go('reviews') }, 'Open Reviews'))));
  if (show('mentions') && mentions && mentions.notifications.length) cards.push(h('article', { class: 'cc-card mentions-block', 'aria-label': 'Mentions' },
    h('p', { class: 'cc-kind' }, h('span', { class: 'cc-dot' }), h('b', {}, 'Mentions'), unread ? h('span', { class: 'cc-badge', title: 'Unread mentions' }, unread + ' unread') : ''), mentionRows(mentions)));

  const feed = cards.length ? h('div', { class: 'cc-feed' }, cards)
    : h('div', { class: 'cc-clear' }, h('span', { class: 'cc-dot ok' }), h('div', {}, h('p', { class: 'cc-clear-title' }, 'Nothing needs you.'),
        h('p', { class: 'muted' }, 'Review requests, conflicts and mentions appear here the moment they happen.')));

  const working = ov.working.length
    ? ov.working.map((it) => h('div', { class: 'cc-run' },
        h('button', { type: 'button', class: 'link', onclick: () => openTicket(it.project.id, it.ticket.id) }, it.ticket.key + ' · ' + it.ticket.title),
        h('p', { class: 'muted small' }, it.project.name + ' · ' + (it.ticket.assigneeId === me ? 'You' : it.author || 'a former member')),
        h('div', { class: 'cc-run-meta' }, h('span', { class: 'badge' }, statusName(it.ticket.status)), mergeBadge(it), when(it.ticket.updatedAt))))
    : h('div', { class: 'empty-state' }, h('p', { class: 'muted' }, 'No work in progress.'), h('button', { class: 'plain small', onclick: () => best ? openProject(best.project.id) : go('projects') }, best ? 'Open board' : can('projects.create') ? 'Create a project' : 'Join a project'));
  const shortcut = (label, count, click, tone) => h('button', { type: 'button', class: 'cc-shortcut', onclick: click }, label, h('span', { class: 'badge' + (tone ? ' ' + tone : '') }, String(count)));

  const totals = [['Available', ov.available], ['In progress', sum(ov.projects, p => p.counts.in_progress || 0)], ['To review', toReview], ['Your tickets', mine]];
  return h('div', { class: 'cc' }, start, h('dl', { class: 'workspace-statistics' }, totals.map(([label, value]) => h('div', {}, h('dt', {}, label), h('dd', {}, String(value))))), bar, h('div', { class: 'cc-split' },
    h('section', { class: 'cc-pane', 'aria-label': 'Workspace' }, h('h2', {}, 'Needs you'), feed),
    h('aside', { class: 'cc-pane cc-side', 'aria-label': 'What is going on' },
      h('section', { class: 'cc-panel', id: 'working-now', 'aria-label': 'Working now' }, h('div', { class: 'cc-ph' }, 'Working now', h('span', { class: 'cc-chip' }, String(ov.working.length))), working),
      h('section', { class: 'cc-panel', 'aria-label': 'Your queue' }, h('div', { class: 'cc-ph' }, 'Your queue'),
        shortcut('My Work', mine, () => go('mywork')), shortcut('To review', toReview, () => go('reviews'), toReview ? 'warn' : ''),
        shortcut('Available to claim', ov.available, () => best ? openProject(best.project.id) : go('projects'))),
      state.desktop && !state.device.runner.configured ? h('button', { class: 'plain add', onclick: () => go('settings', 'computer') }, 'Connect your runner') : '')));
}

function mergeBadge(it) {
  const m = it.merge;
  if (!m || m === 'none') return '';
  const text = { mergeable: 'PR can merge', conflicting: 'PR has conflicts', unknown: 'PR open', merged: 'PR merged', closed: 'PR closed' }[m] || m;
  return h('span', { class: 'badge' + (m === 'conflicting' ? ' bad' : m === 'closed' ? ' warn' : '') }, text);
}

function membersPanels(members) {
  const ordered = [...members].sort((a, b) => a.name.localeCompare(b.name));
  const rows = ordered.map(m => {
    const self = m.id === state.me.member.id;
    const c = m.connections;
    const runnerParts = [];
    if (c?.runnersConnected) runnerParts.push(plural(c.runnersConnected, 'runner') + ' connected');
    if (c?.runnersDisconnected) runnerParts.push(plural(c.runnersDisconnected, 'runner') + ' disconnected');
    if (c?.runnersUnknown) runnerParts.push(plural(c.runnersUnknown, 'runner') + ' · connection unknown');
    const runners = !state.online || !c ? 'Runner connection unknown' : runnerParts.join(' · ') || 'No runners connected';
    const phones = !state.online || !c ? 'Phone connection unknown'
      : c.phonesConnected ? plural(c.phonesConnected, 'phone') + ' connected' + (c.phonesUnknown ? ' · other phone connections unknown' : '')
      : c.phonesUnknown ? 'Phone connection unknown' : c.phonesPaired ? 'Phone disconnected · access paired' : 'No phone paired';
    const actions = [];
    if (!state.desktop && (m.id === state.me.member.id || can('members.manage'))) actions.push(
      h('button', { class: 'plain add', type: 'button', onclick: () => act(async () => {
        const r = await api('POST', '/members/' + m.id + '/token'); const self = r.member.id === state.me.member.id;
        if (self) { stopSync(); state.token = r.token; try { sessionStorage.setItem(KEY('token'), r.token); } catch (_) {} }
        state.secret = { kind: 'token', name: r.member.name, self, token: r.token };
      }) }, 'New token'));
    if (can('admins.manage') && m.role !== 'owner') actions.push(h('button', { class: 'plain', type: 'button', onclick: () => act(() => api('PUT', '/members/' + m.id + '/role', { role: m.role === 'admin' ? 'member' : 'admin' })) }, m.role === 'admin' ? 'Remove admin role' : 'Make admin'));
    if (can('members.manage') && m.role !== 'owner') actions.push(h('button', { class: 'danger', type: 'button', onclick: () => confirm('Remove ' + m.name + ' from the workspace? Anything they are working on goes back on the board.') && act(() => api('DELETE', '/members/' + m.id)) }, 'Remove'));
    return h('div', { class: 'row member-row', 'data-member-id': m.id, 'data-self': String(self) }, h('span', { class: 'av', 'aria-hidden': 'true' }, m.name.trim().split(/\s+/).slice(0, 2).map(n => n[0]).join('').toUpperCase()),
      h('div', { class: 'grow' }, h('strong', {}, m.name), self ? h('span', { class: 'muted small member-you' }, '(you)') : '', h('div', { class: 'muted member-presence' }, runners), h('div', { class: 'muted member-phone' }, phones), m.email ? h('div', { class: 'muted member-email' }, m.email) : ''),
      h('span', { class: 'badge member-role' }, m.role[0].toUpperCase() + m.role.slice(1)),
      actions.length ? h('details', { class: 'member-manage', ontoggle: e => {
        if (e.currentTarget.open) app.querySelectorAll('.member-manage[open]').forEach(other => { if (other !== e.currentTarget) other.open = false; });
      } }, h('summary', { 'aria-label': 'Manage ' + m.name, title: 'Manage ' + m.name }, icon('more'), h('span', { class: 'sr-only' }, m.name)), h('div', { class: 'member-actions' }, actions)) : '');
  });
  const input = h('input', { name: 'member-filter', type: 'search', 'aria-label': 'Find a member', placeholder: 'Find a member…', value: state.memberQuery, autocomplete: 'off', oninput: () => { state.memberQuery = input.value; filter(); } });
  const status = h('p', { class: 'muted member-filter-status', role: 'status', hidden: true });
  const filter = () => {
    const terms = state.memberQuery.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
    let shown = 0;
    rows.forEach((row, i) => {
      const m = ordered[i], text = (m.name + ' ' + m.email + ' ' + m.role).toLocaleLowerCase();
      row.hidden = !terms.every(term => text.includes(term));
      if (!row.hidden) shown++; else { const menu = row.querySelector('details'); if (menu) menu.open = false; }
    });
    status.hidden = !terms.length;
    status.textContent = shown ? plural(shown, 'member') + ' found' : 'No members found.';
  };
  filter();
  const panels = [h('section', { class: 'panel member-directory', 'aria-label': 'Organization members' },
    h('div', { class: 'section-head' }, h('h2', {}, state.me.workspace.name), h('span', { class: 'badge' }, plural(members.length, 'member'))),
    h('p', { class: 'muted member-intro' }, 'Each person keeps their own computer, runner and credentials.'),
    input, status, h('div', { class: 'member-list' }, rows))];
  if (can('members.manage') && !state.desktop) {
    const name = h('input', { name: 'm-name', required: true, maxlength: 80 });
    const email = h('input', { name: 'm-email', type: 'email', maxlength: 254 });
    panels.push(h('aside', { class: 'member-tools' }, h('div', { class: 'panel' }, h('h2', {}, 'Add a member'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { const r = await api('POST', '/members', { name: name.value, email: email.value });
          state.secret = { kind: 'token', name: r.member.name, token: r.token }; name.value = ''; email.value = ''; }); } },
        field('Name', name), field('Email (optional)', email), h('button', { class: 'primary' }, 'Add')))));
  }
  return h('div', { class: 'members-layout' }, panels);
}

// ---- projects ----
function projectHistoryPanel() {
  const projects = state.ov.projects.filter(({ project }) => project.archived);
  return h('section', { class: 'panel' }, h('h2', {}, 'Project history'),
    h('p', { class: 'muted' }, 'Archived projects keep tickets, completed work, activity and memory. Restore a project to continue where the team left off.'),
    projects.length ? projects.map(({ project: p }) => h('div', { class: 'row' },
      h('div', { class: 'grow' }, h('button', { class: 'link', onclick: () => openProject(p.id, 'overview') }, p.name),
        h('p', { class: 'muted' }, (p.archived ? 'Archived' : 'Active') + ' · ' + new Date(p.updatedAt).toLocaleDateString())),
      can('projects.manage') ? h('button', { class: 'plain', onclick: () => (p.archived || confirm('Archive ' + p.name + '? Its tickets and memory will be kept in Project history.')) && act(() => api('PATCH', '/projects/' + p.id, { archived: !p.archived })) }, p.archived ? 'Restore project' : 'Archive project') : '')) : h('p', { class: 'muted' }, 'No archived projects yet. Archive a project in Project settings to keep it here.'));
}


async function projectsView() {
  const projects = state.ov.projects.filter(({ project }) => !project.archived);
  const rows = projects.map(p => h('article', { class: 'project-card' },
    h('header', {}, h('button', { class: 'link project-card-title', onclick: () => openProject(p.project.id, 'board') }, p.project.name),
      h('span', { class: 'badge' }, p.member ? p.role : 'not a member')),
    p.project.repository ? h('p', { class: 'project-card-repository' }, icon('repository'), p.project.repository) : '',
    p.project.description ? h('p', { class: 'project-card-description' }, p.project.description) : '',
    h('dl', { class: 'project-card-counts' }, ['available', 'in_progress', 'review', 'done'].map(status => h('div', {}, h('dt', {}, statusName(status)), h('dd', {}, String(p.counts[status] || 0))))),
    h('footer', { class: 'actions' }, h('button', { class: 'plain open small', onclick: () => openProject(p.project.id, 'board') }, 'Open board'),
      p.toReview ? h('span', { class: 'badge warn' }, plural(p.toReview, 'review') + ' for you') : '')));
  const list = h('section', { class: 'projects-list', 'aria-label': 'Active projects' },
    h('h2', {}, 'Projects', h('span', { class: 'badge' }, String(projects.length))),
    rows.length ? h('div', { class: 'project-cards' }, rows) : h('p', { class: 'empty-state muted' }, can('projects.view_all') ? 'No projects yet.' : 'You are not on any project yet. Ask for an invite code.'));
  const panels = [];
  const code = h('input', { name: 'join-code', required: true, maxlength: 200, autocomplete: 'off', placeholder: 'wbi_…' });
  panels.push(h('details', { class: 'panel project-tool' }, h('summary', {}, 'Join a project with a code'),
    h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => {
        const j = await api('POST', '/invites/join', { code: code.value.trim() });
        code.value = ''; state.projectId = j.project.id; state.tab = 'board'; state.data = null; remember(); state.info = 'You joined ' + j.project.name + '.'; }); } },
      field('Invite code', code), h('button', { class: 'primary' }, 'Join'))));
  if (can('projects.create')) {
    const name = h('input', { name: 'p-name', required: true, maxlength: 80 });
    const desc = h('input', { name: 'p-desc', maxlength: 2000 });
    const repo = h('input', { name: 'p-repo', placeholder: 'https://github.com/owner/repo (optional)', maxlength: 2048 });
    panels.unshift(h('details', { class: 'panel project-tool', open: !projects.length }, h('summary', {}, 'New project'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { const p = await api('POST', '/projects', { name: name.value, description: desc.value, repository: repo.value });
          name.value = ''; desc.value = ''; repo.value = ''; state.projectId = p.id; state.tab = 'board'; state.data = null; remember(); }); } },
        field('Name', name), field('Description (optional)', desc), field('Repository', repo), h('button', { class: 'primary' }, 'Create'))));
  }
  return h('div', { class: 'projects-page' }, list, h('aside', { class: 'project-tools', 'aria-label': 'Manage projects' }, panels, h('details', { class: 'project-history' }, h('summary', {}, 'Project history'), projectHistoryPanel())));
}

// ---- one project: board, repository, activity, people ----

// Its board is loaded with everything the other project tabs need.
async function loadProject() {
  const id = state.projectId;
  const board = await api('GET', '/projects/' + id + '/board');
  const d = { id, board, tab: state.tab };
  if (state.ticketId) {
    d.ticket = await api('GET', '/projects/' + id + '/tickets/' + state.ticketId);
    d.progress = await api('GET', '/projects/' + id + '/tickets/' + state.ticketId + '/progress').catch(() => []);
    d.comments = await loadComments(id, state.ticketId);
  } else forgetOpenComment();
  if (state.tab === 'overview') d.mentions = await loadMentions(id);
  if (state.tab === 'timeline' || state.tab === 'calendar') {
    // A host from before the Calendar cannot answer: the tab says so, and the project's other tabs stay one click away.
    d.timeline = await api('GET', '/projects/' + id + '/timeline').catch((e) => { if (e.code === 'host_outdated') return null; throw e; });
    // Which tickets have a shared request for agent work: their dependencies gate its start. Only a marking, so it is optional.
    d.schedules = await api('GET', '/projects/' + id + '/schedules').catch(() => []);
  }
  if (state.tab === 'repository') d.repo = await api('GET', '/projects/' + id + '/repository');
  if (state.tab === 'activity' || state.tab === 'board') {
    d.progressReports = await Promise.all(board.tickets.filter(k => k.assigneeId === state.me.member.id).map(async k => ({ ticket: k, progress: await api('GET', '/projects/' + id + '/tickets/' + k.id + '/progress').catch(() => []) })));
  }
  if (state.tab === 'people' || state.tab === 'project-settings') {
    d.people = board.people;
    d.invites = board.can.includes('invites.manage') ? await api('GET', '/projects/' + id + '/invites') : [];
    d.everyone = board.can.includes('members.manage') ? await api('GET', '/members') : [];
  }
  state.data = d;
  return d;
}

function relationshipSettings(project) {
  const body = h('div', {}, h('p', { class: 'muted' }, 'Load approved links visible to you. Links suggest compatibility checks; they do not prove breakage.'));
  const button = h('button', { class: 'plain small', onclick: async () => {
    button.disabled = true;
    body.replaceChildren(h('p', { role: 'status' }, 'Loading relationships…'));
    try {
      const rows = await api('GET', '/projects/' + project.id + '/relationships');
      body.replaceChildren(...(rows.length ? rows.map(row => h('div', {},
        h('p', {}, row.relationship.source.repository + ' → ' + row.relationship.target.repository),
        h('p', { class: 'muted' }, row.relationship.type.replaceAll('_', ' ') + ' · ' + row.status + ' · Evidence: ' + row.relationship.evidence),
        h('p', {}, row.recommendation))) : [h('p', { class: 'muted' }, 'No visible approved relationships. Propose, approve and publish links through the project memory API.')]));
    } catch (error) { body.replaceChildren(h('p', { role: 'alert' }, error.message)); }
    finally { button.disabled = false; }
  } }, 'Load relationships');
  return h('section', { 'aria-label': 'Repository relationships' }, h('h3', {}, 'Repository relationships'), button, body);
}

async function projectScopedView() {
  const cur = currentProject();
  if (!cur) return h('div', { class: 'panel' }, h('h2', {}, 'No project yet'),
    h('p', { class: 'muted' }, can('projects.create') ? 'Create a project under Projects to get started.' : 'You are not on any project yet. Ask for an invite code.'));
  const d = await loadProject();
  const p = d.board.project;
  const switcher = h('select', { name: 'project-switch', 'aria-label': 'Project', onchange: (e) => openProject(e.target.value, state.tab) },
    state.ov.projects.map((x) => h('option', { value: x.project.id, selected: x.project.id === p.id }, x.project.name + (x.project.archived ? ' (archived)' : ''))));
  const mine = state.ov.projects.find((x) => x.project.id === p.id);
  const tabs = projectTabs(p.id, [['overview', 'Overview', mine?.mentions || 0], ['board', 'Board', mine?.counts.available || 0], ['calendar', 'Calendar', 0], ['repository', 'Git', mine?.problems || 0], ['activity', 'Runs', 0]]);
  const calendarLacking = ['calendar', 'timeline'].includes(state.tab) && d.timeline === null ? await hostOutdatedView() : null;
  const content = state.tab === 'overview' ? projectOverviewTab(d) : state.tab === 'calendar' || state.tab === 'timeline' ? calendarLacking || calendarTab(d) : state.tab === 'repository' ? repositoryTab(d) : state.tab === 'activity' ? activityTab(d) : state.tab === 'people' ? h('div', { class: 'project-settings' }, peopleTab(d)) : state.tab === 'project-settings' ? projectSettingsTab(d) : boardTab(d);
  const gear = h('button', { class: 'plain icon-button', type: 'button', name: 'project-settings', 'aria-label': 'Project settings: ' + p.name, title: 'Project settings', 'aria-current': ['project-settings', 'people'].includes(state.tab) ? 'page' : null, onclick: () => openProject(p.id, 'project-settings') }, icon('defaults'));
  const newTicket = pcan('tickets.create') && !p.archived ? h('button', { class: 'primary', title: 'New ticket (N)', onclick: () => { state.newTicketOpen = true; render(); } }, 'New ticket') : '';
  let view = content;
  if (['calendar', 'timeline'].includes(state.tab) && state.ticketId) {
    const k = d.ticket || d.board.tickets.find(k => k.id === state.ticketId);
    if (k) view = h('div', { class: 'calendar-with-ticket' }, content, ticketPanel(d, k));
  }
  return h('div', { class: 'project-view' },
    h('div', { class: 'project-head' },
      h('div', { class: 'project-title-row' },
        h('div', { class: 'project-where' },
          p.repository ? h('div', { class: 'project-meta' }, icon('repository'), isHTTPS(p.repository) ? extLink(p.repository, p.repository) : p.repository) : '',
          h('h1', { id: 'main-title', tabindex: '-1' }, p.name)),
        h('div', { class: 'actions' }, searchButton(), gear, newTicket)),
      FRAMED ? '' : h('label', { class: 'project-chooser' }, 'Project ', switcher)),
    tabs, state.newTicketOpen ? newTicketForm(d) : '', view);

}

// Project administration: details, relationships, people and archiving. Existing People deep links still work.
function projectSettingsTab(d) {
  const p = d.board.project;
  const details = h('section', { class: 'panel', 'aria-labelledby': 'project-details-h' }, h('h2', { id: 'project-details-h' }, 'Details'),
    p.description ? h('p', {}, p.description) : h('p', { class: 'muted' }, 'No description.'),
    p.repository ? h('p', {}, isHTTPS(p.repository) ? extLink(p.repository, p.repository) : p.repository) : '',
    h('p', { class: 'muted' }, d.board.member ? 'Your role: ' + d.board.role : 'You are not a project member.'));
  const archive = can('projects.manage') ? h('section', { class: 'panel', 'aria-labelledby': 'project-archive-h' }, h('h2', { id: 'project-archive-h' }, p.archived ? 'Unarchive project' : 'Archive project'),
    h('p', { class: 'muted' }, p.archived ? 'Put this project back among the active ones. Its tickets are as they were.' : 'An archived project leaves the sidebar and cannot take new tickets. Nothing is deleted: you can unarchive it.'),
    h('button', { class: 'danger', onclick: () => (p.archived || confirm('Archive ' + p.name + '? It leaves the sidebar and takes no new tickets until you unarchive it.')) && act(() => api('PATCH', '/projects/' + p.id, { archived: !p.archived })) }, p.archived ? 'Unarchive project' : 'Archive project')) : '';
  return h('div', { class: 'project-settings' }, details, h('section', { class: 'panel' }, relationshipSettings(p)), peopleTab(d), archive);
}

// One row of tabs for the sub-areas of a major part. The sidebar lists the major parts; this is the only other navigation, and the
// project and the team both use it. choices are [id, title, count]; `prefix` names each button for tests and scripts.
function tabNav(label, prefix, choices, isCurrent, open) {
  return h('nav', { class: 'sub-tabs section-tabs', 'aria-label': label }, choices.map(([id, title, count]) =>
    h('button', { name: prefix + id, type: 'button', 'aria-current': isCurrent(id) ? 'page' : null, onclick: () => open(id) }, title,
      count > 0 ? h('span', { class: 'count' + (id === 'repository' ? ' bad' : ' quiet') }, String(count)) : '')));
}

// A project's own sections, where the window's sidebar (which lists the project) does not: Board, Git, Runs, People.
function projectTabs(projectId, choices) {
  return tabNav('Project sections', 'project-tab-', choices, (id) => state.tab === id || (id === 'calendar' && state.tab === 'timeline'), (id) => openProject(projectId, id));
}

// One choice among a few, as a row of buttons that say which is on: a way of looking at the page, not a place to go (that is tabNav).
// choices are [id, title]; `cls` is the row's class, and `small` the buttons' size.
function choiceGroup(label, choices, isOn, pick, cls = 'inline', small = true) {
  return h('div', { class: cls + ' seg', role: 'group', 'aria-label': label }, choices.map(([id, title]) =>
    h('button', { type: 'button', class: small ? 'small' : '', 'aria-pressed': isOn(id), onclick: () => pick(id) }, title)));
}

// The team's own sub-areas, under its entry in the window's sidebar. Outside the window the rail lists them, so there is no second row.
function workspaceTabs() {
  if (!FRAMED || !WORKSPACE_TABS.includes(state.tab)) return '';
  return tabNav('Workspace sections', 'workspace-tab-', WORKSPACE_TABS.map((id) => [id, TABS.find(([t]) => t === id)[1], 0]), (id) => state.tab === id, (id) => go(id));
}

function projectOverviewTab(d) {
  const b = d.board;
  return h('section', { class: 'panel' }, h('h2', {}, b.project.name), b.project.description ? h('p', {}, b.project.description) : '',
    h('div', { class: 'overview-counts' }, b.statuses.map(s => h('p', {}, h('strong', {}, String(b.tickets.filter(k => k.status === s.status).length)), ' ' + s.label))),
    h('p', { class: 'muted' }, 'Work is coordinated here. Git and execution evidence is provided by each member’s own Werkbord.'),
    h('button', { class: 'primary', onclick: () => openProject(d.id, 'board') }, 'Open board'),
    h('section', { class: 'mentions-block overview-mentions', 'aria-label': 'Mentions in this project' }, h('div', { class: 'section-head' }, h('h3', {}, 'Mentions'),
      d.mentions && d.mentions.unread ? h('span', { class: 'badge warn', title: 'Unread mentions in this project' }, String(d.mentions.unread)) : ''),
      mentionRows(d.mentions, 'No unread mentions in this project.')));
}
function calendarTab(d) {
  const toggle = choiceGroup('Calendar presentation', [['calendar', 'Calendar'], ['timeline', 'Timeline']], mode => state.calendarMode === mode || state.tab === 'timeline' && mode === 'timeline',
    mode => { state.calendarMode = mode; if (state.tab === 'timeline') state.tab = 'calendar'; render(); }, 'actions calendar-switch');
  if (state.calendarMode === 'timeline' || state.tab === 'timeline') return h('div', {}, toggle, timelineTab(d));
  const anchor = state.calendarAnchor || TL.localToday(Date.now());
  const day = new Date(anchor + 'T12:00:00'); day.setDate(day.getDate() - (day.getDay() + 6) % 7);
  const dates = Array.from({ length: 7 }, (_, i) => { const t = new Date(day); t.setDate(t.getDate() + i); return TL.localToday(t.getTime()); });
  const change = n => { const t = new Date(anchor + 'T12:00:00'); t.setDate(t.getDate()+n); state.calendarAnchor = TL.localToday(t.getTime()); render(); };
  const requests = (d.schedules || []).filter(s => !['canceled', 'completed'].includes(s.state));
  const week = h('div', { class: 'calendar-week' }, dates.map(date => h('section', { class: 'calendar-day', 'data-plan-day': date }, h('h3', {}, new Date(date + 'T12:00:00').toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })),
    d.board.tickets.filter(k => { const span = TL.spanOf(k); return span && span.start <= date && span.end >= date; }).map(k => h('button', { class: 'calendar-event', 'data-status': k.status, ...planAttrs(k), onclick: () => openTicket(d.id, k.id) }, k.title, h('small', {}, statusName(k.status) + ' · Planned work'))),
    requests.filter(s => TL.localToday(new Date(s.at).getTime()) === date).map(s => { const k = d.board.tickets.find(k => k.id === s.ticketId); return h('button', { class: 'calendar-event scheduled', ...(k ? planAttrs(k, s) : {}), onclick: () => k && openTicket(d.id, k.id) }, k?.title || 'Scheduled work', h('small', {}, new Date(s.at).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }) + ' · ' + s.state)); }))));
  return h('div', { class: 'team-calendar', 'data-plan-surface': '' }, toggle, h('div', { class: 'actions' }, h('button', { class: 'plain small', onclick: () => change(-7) }, 'Previous week'), h('button', { class: 'plain small', onclick: () => { state.calendarAnchor = null; render(); } }, 'Today'), h('button', { class: 'plain small', onclick: () => change(7) }, 'Next week')), h('p', { class: 'muted small' }, 'Planned dates and shared schedule requests. Requests wait for the owner’s own runner and approval.'), h('div', { class: 'scroll' }, week));
}

// ---- My Work ----

async function myWorkView() {
  const w = await api('GET', '/my-work');
  const attn = w.needsAction.length
    ? w.needsAction.map((a) => h('div', { class: 'row attn ' + a.level },
        h('span', { class: 'badge ' + (a.level === 'problem' ? 'bad' : a.level === 'warning' ? 'warn' : '') }, a.level === 'info' ? 'next' : a.level),
        h('div', { class: 'grow' }, a.message),
        a.kind === 'review_requested' || a.kind === 'ready_to_complete'
          ? h('button', { class: 'plain open small', onclick: () => go('reviews') }, 'Open Reviews')
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
    (w.ready || []).length ? h('div', { class: 'panel' }, h('h2', {}, 'Claimed · ready to start (' + w.ready.length + ')'), list(w.ready, '')) : '',
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
  const foot = [extra || '', state.handoff && state.handoff.ticket.id === k.id ? handoffBox(state.handoff) : '',
    mine && (k.workMode || 'agent') !== 'human' && ['available', 'in_progress', 'review'].includes(k.status)
      ? (() => {
          const individual = typeof individualActions === 'function' ? individualActions(it.project.id, k.id, true) : '';
          return h('div', { class: 'actions' }, individual, h('button', { class: (individual ? 'plain' : 'primary') + ' small', onclick: () => openInRunner(it) }, 'Open in my runner'),
            h('button', { class: 'plain open small', onclick: () => openTicket(it.project.id, k.id) }, 'Open ticket'));
        })()
      : ''];
  return h('article', { class: 'card item' + (mine ? ' mine' : '') },
    h('header', { class: 'item-head' },
      h('div', { class: 'muted small item-meta' }, k.key, ' · ', it.project.name, ' · ', statusName(k.status), it.assignedBy ? ' · assigned to you by ' + it.assignedBy : (it.author && !mine ? ' · by ' + it.author : '')),
      h('button', { class: 'link title', onclick: () => openTicket(it.project.id, k.id) }, k.title),
      h('div', { class: 'flags' }, mergeBadge(it), pr && pr.state === 'open' && pr.behind > 0 ? h('span', { class: 'badge warn' }, pr.behind + ' behind ' + (pr.baseBranch || 'base')) : '', pr && pr.draft ? h('span', { class: 'badge' }, 'draft') : '')),
    k.branch || links.length || k.commits && k.commits.length ? h('div', { class: 'item-body' },
      k.branch ? h('div', {}, 'Branch ', h('code', {}, k.branch), ' ', copy(k.branch, 'Copy')) : '',
      links.length ? h('div', { class: 'links' }, links.map((l, i) => [i ? ' · ' : '', l])) : '',
      commits) : '',
    h('div', { class: 'item-foot' }, foot));
}

// ---- MCP batch proposals and client grants ----

const teamMCPPlanDrafts = new Map();
let teamMCPIssuedToken = '';
let teamMCPUseStdio = false;
let teamMCPEndpoint = '';
function teamMCPSetup(endpoint) {
  const address = h('input', { type: 'url', value: teamMCPEndpoint || endpoint, 'aria-label': 'MCP server address', onchange: e => { teamMCPEndpoint = e.currentTarget.value; render(); } });
  let config;
  try {
    const url = new URL(teamMCPEndpoint || endpoint);
    if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) throw Error('Enter an HTTP or HTTPS address without embedded credentials.');
    config = JSON.stringify({ mcpServers: { 'werkbord-team': teamMCPUseStdio
      ? { command: 'werkbord-team', args: ['mcp', '--url', url.href], env: { WERKBORD_TEAM_MCP_TOKEN: teamMCPIssuedToken } }
      : { url: url.href, headers: { Authorization: 'Bearer ' + teamMCPIssuedToken } } } }, null, 2);
  } catch { config = ''; }
  return h('div', { class: 'stack' }, field('Address reachable from your client', address),
    h('p', { class: 'muted' }, 'For a client on another computer, use this workspace’s private host address ending in /mcp. localhost means the client’s own computer.'),
    h('label', { class: 'check-row' }, h('input', { type: 'checkbox', checked: teamMCPUseStdio, onchange: e => { teamMCPUseStdio = e.currentTarget.checked; render(); } }), 'My client needs a local process (stdio)'),
    teamMCPUseStdio ? h('p', { class: 'muted' }, 'Install werkbord-team on the client’s computer to bridge to this workspace.') : '',
    config ? [h('pre', { class: 'mcp-config' }, config), copy(config, 'Copy connection settings')] : h('p', { class: 'error' }, 'Enter a valid HTTP or HTTPS server address.'),
    h('p', { class: 'muted' }, 'Paste these settings into your MCP client and reconnect it. For URL-based setup, enter this address and an Authorization header with Bearer followed by your credential. Ask the client to list projects to check the connection.'),
    h('button', { class: 'plain', onclick: () => { teamMCPIssuedToken = ''; render(); } }, 'I’ve saved it — hide credential'));
}
let teamMCPPlanCounter = 0;

function discardMCPPlanDraft(id) {
  teamMCPPlanDrafts.delete(id);
  for (const keep of drafts.values()) for (const name of Object.keys(keep.fields)) if (name.includes(id)) delete keep.fields[name];
  for (const el of app.querySelectorAll('[name]')) if (el.name.includes(id)) delete el.dataset.dirty;
}

function addTeamMCPPlanTask(id) {
  const plan = teamMCPPlanDrafts.get(id);
  if (!plan || plan.tasks.length >= 50) return;
  plan.tasks.push({ tempId: 'tmp_review_' + Date.now() + '_' + teamMCPPlanCounter++, title: 'New task', description: '', dependencies: [] });
  render();
}

function removeTeamMCPPlanTask(id, taskKey) {
  const plan = teamMCPPlanDrafts.get(id);
  if (!plan) return;
  plan.tasks = plan.tasks.filter(task => (task.tempId || task.taskId) !== taskKey);
  render();
}

async function mcpPlansView() {
  const { proposals = [] } = await api('GET', '/mcp/proposals');
  return h('div', {},
    h('section', { class: 'panel' }, h('h2', {}, 'External task plans'),
      h('p', { class: 'muted' }, 'Review one project batch at a time. Saving replaces the pending proposal; approval checks current permissions, task versions and dependencies before applying all changes together.'),
      h('button', { class: 'plain small', onclick: () => render() }, 'Refresh'),
      proposals.length ? proposals.map(mcpPlanProposalView) : h('p', { class: 'muted' }, 'No external task plans are waiting for a decision.')));
}

function mcpPlanProposalView(proposal) {
  const draft = teamMCPPlanDrafts.get(proposal.id) || structuredClone(proposal.plan);
  teamMCPPlanDrafts.set(proposal.id, draft);
  const title = state.ov.projects.find(p => p.project.id === proposal.projectId)?.project.name || proposal.projectId;
  const unsaved = JSON.stringify(draft) !== JSON.stringify(proposal.plan);
  const items = draft.tasks.map((task, index) => {
    const taskKey = task.tempId || task.taskId;
    const edit = patch => { draft.tasks[index] = { ...draft.tasks[index], ...patch }; };
    const changeDate = (fieldName, value) => { edit({ plan: { ...(task.plan || {}), [fieldName]: value } }); };
    const dependencies = h('input', { name: 'mcp-deps-' + proposal.id + '-' + taskKey, value: (task.dependencies || []).join(', '), oninput: e => edit({ dependencies: [...new Set(e.currentTarget.value.split(/[\s,]+/).map(x => x.trim()).filter(Boolean))] }) });
    return h('fieldset', { class: 'mcp-plan-task' },
      h('legend', {}, task.tempId ? 'New task · ' + task.tempId : 'Update task · ' + task.taskId + ' · v' + task.version),
      field('Title', h('input', { name: 'mcp-title-' + proposal.id + '-' + taskKey, value: task.title || '', maxlength: 200, oninput: e => edit({ title: e.currentTarget.value }) })),
      field('Description', h('textarea', { name: 'mcp-description-' + proposal.id + '-' + taskKey, rows: 3, maxlength: 20000, oninput: e => edit({ description: e.currentTarget.value }) }, task.description || '')),
      h('div', { class: 'inline mcp-plan-dates' },
        field('Start', h('input', { type: 'date', name: 'mcp-start-' + proposal.id + '-' + taskKey, value: task.plan?.start || '', oninput: e => changeDate('start', e.currentTarget.value) })),
        field('End', h('input', { type: 'date', name: 'mcp-end-' + proposal.id + '-' + taskKey, value: task.plan?.end || '', oninput: e => changeDate('end', e.currentTarget.value) })),
        field('Milestone', h('input', { type: 'checkbox', name: 'mcp-milestone-' + proposal.id + '-' + taskKey, checked: !!task.plan?.milestone, onchange: e => changeDate('milestone', e.currentTarget.checked) }))),
      field('Dependencies · task IDs or temporary IDs, separated by commas', dependencies),
      field('Source reference', h('input', { name: 'mcp-source-' + proposal.id + '-' + taskKey, value: task.sourceRef || '', maxlength: 2000, oninput: e => edit({ sourceRef: e.currentTarget.value }) })),
      field('Requirements', h('textarea', { name: 'mcp-requirements-' + proposal.id + '-' + taskKey, rows: 2, maxlength: 20000, oninput: e => edit({ requirements: e.currentTarget.value }) }, task.requirements || '')),
      h('button', { type: 'button', class: 'plain danger small', onclick: () => removeTeamMCPPlanTask(proposal.id, taskKey) }, 'Remove task'));
  });
  const warnings = proposal.warnings?.length ? h('ul', { class: 'mcp-plan-warnings' }, proposal.warnings.map(w => h('li', { class: 'severity-' + w.severity }, h('strong', {}, w.severity), ' · ', w.message))) : '';
  return h('article', { class: 'panel mcp-plan-proposal' },
    h('div', { class: 'row' }, h('div', { class: 'grow' }, h('h3', {}, proposal.client || 'External client'), h('p', { class: 'muted' }, title + ' · ' + draft.tasks.length + ' proposed changes · ' + proposal.id)), h('span', { class: 'badge warn' }, 'Pending')),
    proposal.validationError ? h('p', { class: 'advice', role: 'alert' }, proposal.validationError) : '', warnings,
    h('div', { class: 'mcp-plan-task-list' }, items),
    h('button', { class: 'plain add small', disabled: draft.tasks.length >= 50, onclick: () => addTeamMCPPlanTask(proposal.id) }, 'Add task'),
    unsaved ? h('p', { class: 'muted small' }, 'Save your edits before approving.') : '',
    h('div', { class: 'actions' },
      h('button', { class: 'plain', onclick: () => act(async () => { await api('PUT', '/mcp/proposals/' + proposal.id, draft); discardMCPPlanDraft(proposal.id); state.info = 'Plan saved and checked again.'; }) }, 'Save edited plan'),
      h('button', { class: 'primary', disabled: !!proposal.validationError || unsaved || !draft.tasks.length, onclick: () => act(async () => { const result = await api('POST', '/mcp/proposals/' + proposal.id + '/approve'); discardMCPPlanDraft(proposal.id); state.info = plural(result.result?.created?.length || 0, 'task') + ' created and ' + plural(result.result?.updated?.length || 0, 'task') + ' updated.'; }) }, 'Approve plan'),
      h('button', { class: 'danger', onclick: () => confirm('Decline this task plan? No tasks will be changed.') && act(async () => { await api('POST', '/mcp/proposals/' + proposal.id + '/decline'); discardMCPPlanDraft(proposal.id); state.info = 'Plan declined. No tasks were changed.'; }) }, 'Decline')));
}

async function mcpClientsView() {
  const [operationData, grantData] = await Promise.all([api('GET', '/mcp/operations'), api('GET', '/mcp/grants')]);
  const operations = operationData.operations || [];
  const grants = grantData.grants || [];
  const projects = state.ov.projects.filter(p => !p.project.archived);
  const selectedProjects = new Set();
  const selectedOperations = new Set();
  const name = h('input', { name: 'mcp-client-name', maxlength: 80, required: true, placeholder: 'For example, meeting-notes agent' });
  const endpoint = location.origin + BASE + '/mcp';
  const projectChoices = projects.map(p => h('label', { class: 'check-row' }, h('input', { type: 'checkbox', onchange: e => e.currentTarget.checked ? selectedProjects.add(p.project.id) : selectedProjects.delete(p.project.id) }), p.project.name));
  const operationChoices = operations.map(op => {
    const label = ({ propose_task_plan: 'Propose task plans', list_pending_proposals: 'List pending proposals', get_proposal_status: 'Check proposal status' })[op] || op.replaceAll('_', ' ');
    return h('label', { class: 'check-row' }, h('input', { type: 'checkbox', onchange: e => e.currentTarget.checked ? selectedOperations.add(op) : selectedOperations.delete(op) }), label);
  });
  const form = h('form', { class: 'stack', onsubmit: e => { e.preventDefault(); if (!selectedProjects.size || !selectedOperations.size) { state.error = 'Choose at least one project and one operation for this client.'; render(); return; } act(async () => {
    const created = await api('POST', '/mcp/grants', { name: name.value.trim(), projects: [...selectedProjects], operations: [...selectedOperations] });
    teamMCPIssuedToken = created.token;
    state.info = 'Client created. Copy the credential now; it will only be shown once.';
  }); } },
    field('Client name', name),
    h('fieldset', { class: 'mcp-scope' }, h('legend', {}, 'Projects'), projects.length ? projectChoices : h('p', { class: 'muted' }, 'Create a project before making a scoped client.')),
    h('fieldset', { class: 'mcp-scope' }, h('legend', {}, 'Operations'), operationChoices),
    h('button', { class: 'primary', disabled: !projects.length }, 'Create scoped client'));
  return h('div', {},
    h('section', { class: 'panel' }, h('h2', {}, 'MCP clients'),
      h('details', {}, h('summary', {}, 'Setting up an MCP server: how does this work?'), h('p', {}, 'This workspace already has an MCP server. Any compatible client can use it; no specific integration is required. Name your client and choose its projects and operations below. Create a credential, then copy the generated connection settings into your client. Task plans wait for your decision in Plans; create and update ticket permissions allow direct changes. If a connection fails, check the address, private network access and credential. A credential belongs to this workspace only.')),
      h('p', { class: 'muted' }, 'Create a separate revocable credential scoped to chosen projects and operations. Team stores only its hash. External clients can propose task batches, but only a member can edit or approve them.'),
      h('p', { class: 'muted' }, 'Streamable HTTP endpoint: ', h('code', {}, endpoint)),
      teamMCPIssuedToken ? h('div', { class: 'invite-result' }, h('strong', {}, 'Copy this credential now'), h('p', { class: 'muted' }, 'It is shown once. Keep it in the MCP client’s secret environment as WERKBORD_TEAM_MCP_TOKEN.'), h('code', {}, teamMCPIssuedToken), copy(teamMCPIssuedToken, 'Copy credential'), teamMCPSetup(endpoint)) : '',
      form),
    h('section', { class: 'panel' }, h('h2', {}, 'Issued credentials'), grants.length ? grants.map(g => h('div', { class: 'row mcp-grant-row' },
      h('div', { class: 'grow' }, h('strong', {}, g.name), h('p', { class: 'muted' }, (g.projects || []).length + ' projects · ' + (g.operations || []).join(', '), g.revokedAt ? ' · revoked' : '')),
      !g.revokedAt ? h('button', { class: 'danger', onclick: () => confirm('Revoke ' + g.name + '? Its client will no longer be able to connect.') && act(async () => { await api('DELETE', '/mcp/grants/' + g.id); state.info = g.name + ' can no longer connect.'; }) }, 'Revoke') : h('span', { class: 'badge' }, 'Revoked'))) : h('p', { class: 'muted' }, 'No MCP clients have been created.')));
}


// ---- Reviews ----

async function reviewsView() {
  const q = await api('GET', '/reviews');
  const toReview = q.items.filter((i) => i.canReview && !i.mine);
  const waiting = q.items.filter((i) => i.mine || !i.canReview);
  return h('div', { class: 'reviews-grid' },
    h('section', { class: 'review-column', 'aria-label': 'To review' }, h('h2', {}, 'To review', h('span', { class: 'badge' }, String(toReview.length))),
      toReview.length ? toReview.map(reviewItem) : h('p', { class: 'empty-state muted' }, 'Nothing is waiting for you to review.')),
    h('section', { class: 'review-column', 'aria-label': 'Waiting for review' }, h('h2', {}, 'Waiting for review', h('span', { class: 'badge' }, String(waiting.length))),
      waiting.length ? waiting.map(reviewItem) : h('p', { class: 'empty-state muted' }, 'No submitted work is waiting for a reviewer.')));
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
  const holder = k.assigneeId === state.me.member.id || pcan('tickets.assign');
  // Dropping an available ticket into In progress claims it, like the Claim button.
  if (to === 'in_progress' && k.status === 'available' && !k.assigneeId && d.board.member && pcan('tickets.claim')) return ['claim'];
  if (to === 'in_progress' && k.status === 'available' && k.assigneeId === state.me.member.id) return ['start-work']; // claimed before claiming moved a ticket
  if (k.status === 'in_progress' && to === 'available' && holder) return ['release'];
  if (k.status === 'review' && to === 'in_progress' && pcan('tickets.review') && k.assigneeId) return ['request-changes', { note: '' }];
  if (k.status === 'in_progress' && to === 'review' && holder) return ['submit', {}];
  if (k.status === 'review' && to === 'done' && pcan('tickets.review') && !d.board.completion[k.id]) return ['complete'];
  if (k.status === 'done' && to === 'available' && pcan('tickets.reopen')) return ['move', { status: to }];
  return null;
}
// The same rule as the server's: every part of the filter that is set must hold.
function matchesFilter(k) {
  const f = state.filter;
  if (state.ownTickets && k.assigneeId !== state.me.member.id) return false;
  if (!f.labels.length) return true;
  const have = new Set(k.labelIds || []);
  return f.match === 'all' ? f.labels.every((id) => have.has(id)) : f.labels.some((id) => have.has(id));
}
function filterActive() { return state.filter.labels.length > 0; }

function labelChip(l) {
  const chip = h('span', { class: 'lbl', title: l.name }, l.name);
  const color = WerkbordTimeline.safeColor(l.color);
  chip.style.background = color; // the colour is set through the style object, never built into markup
  chip.style.color = WerkbordTimeline.inkFor(color);
  return chip;
}
function labelsOf(k, labels) { const by = new Map((labels || []).map((l) => [l.id, l])); return (k.labelIds || []).map((id) => by.get(id)).filter(Boolean); }

// The label filter shared by the board and the timeline. It narrows what is shown and changes nothing.
function filterBar(d) {
  const labels = d.board.labels || [];
  const refresh = () => render();
  const picker = labels.length ? h('details', { class: 'label-filter' }, h('summary', {}, state.filter.labels.length ? plural(state.filter.labels.length, 'label') : 'Labels'),
    h('div', { class: 'label-menu' }, labels.map((l) => h('label', { class: 'check' },
      h('input', { type: 'checkbox', checked: state.filter.labels.includes(l.id), onchange: (e) => { state.filter.labels = e.target.checked ? [...state.filter.labels, l.id] : state.filter.labels.filter((x) => x !== l.id); refresh(); } }), ' ', labelChip(l))),
      state.filter.labels.length > 1 ? choiceGroup('Tickets must have', [['any', 'Any'], ['all', 'All']], m => state.filter.match === m, m => { state.filter.match = m; refresh(); }) : '')) : '';
  // A label deleted meanwhile cannot hide every ticket.
  state.filter.labels = state.filter.labels.filter((id) => labels.some((l) => l.id === id));
  return h('div', { class: 'filter-bar', role: 'group', 'aria-label': 'Filter by label' }, picker,
    filterActive() ? h('button', { class: 'plain small', type: 'button', onclick: () => { state.filter = { labels: [], match: 'any' }; refresh(); } }, 'Clear filter') : '');
}

function boardTab(d) {
  const b = d.board;
  const toolbar = h('div', { class: 'actions board-tools' },
    choiceGroup('Whose tickets', [['all', 'All tickets'], ['own', 'My tickets (' + b.tickets.filter(k => k.assigneeId === state.me.member.id).length + ')']], w => (w === 'own') === !!state.ownTickets, w => { state.ownTickets = w === 'own'; render(); }, 'actions', false), filterBar(d),
    h('button', { class: 'plain', 'aria-pressed': state.showArchived, onclick: () => { state.showArchived = !state.showArchived; render(); } }, state.showArchived ? 'Back to board' : 'Archive (' + (b.archived || []).length + ')'),
    '');
  const cols = b.statuses.map((s) => {
    const items = b.tickets.filter((k) => k.status === s.status && matchesFilter(k));
    return h('section', { class: 'col', 'aria-label': s.label, 'data-board-column': s.status,
      ondragover: e => { const k = b.tickets.find(t => t.id === draggedTicket); if (!k) return; e.preventDefault(); e.dataTransfer.dropEffect = 'move'; e.currentTarget.classList.add('drop-over'); },
      ondragleave: e => { if (!e.currentTarget.contains(e.relatedTarget)) e.currentTarget.classList.remove('drop-over'); },
      ondrop: e => { e.preventDefault(); e.currentTarget.classList.remove('drop-over'); const k = b.tickets.find(t => t.id === draggedTicket || t.id === e.dataTransfer?.getData('text/plain')); draggedTicket = null; if (k) performBoardDrop(d, k, s.status); }
    },
      h('h3', {}, s.label, ' ', h('span', { class: 'badge' }, String(items.length))),
      h('div', { class: 'column-cards', 'data-scroll-key': 'column-' + s.status }, items.length ? items.map((k) => card(d, k)) : h('p', { class: 'muted small' }, 'No tickets' + (state.filter.labels.length ? ' match the filters.' : ' here yet.'))));
  });
  let open = null;
  if (state.ticketId) {
    const k = [...b.tickets, ...(b.archived || [])].find((x) => x.id === state.ticketId);
    if (k) open = ticketPanel(d, d.ticket || k); else state.ticketId = null;
  }
  return h('div', { class: 'board-view' }, toolbar,
    h('div', { class: 'tboard' + (open ? ' with-ticket' : '') }, state.showArchived ? archiveTab(d) : h('div', { class: 'board', role: 'region', 'aria-label': 'Kanban board', 'data-board': '', tabindex: '0', 'data-scroll-key': 'kanban-board' }, cols), open || ''));
}
function archiveTab(d) {
  const rows = (d.board.archived || []).slice().reverse().map(k => h('div', { class: 'row', 'data-search': (k.title + ' ' + k.description + ' ' + k.key).toLowerCase() },
    h('div', { class: 'grow' }, h('button', { class: 'link', onclick: () => openTicket(d.id, k.id) }, k.key + ' · ' + k.title), h('p', { class: 'muted' }, statusName(k.status) + ' · Archived ' + ago(k.archivedAt))),
    canArchiveTicket(k) ? h('button', { class: 'plain ok small', onclick: () => act(() => api('POST', projectPath(k) + '/archive', { version: k.version, archived: false })) }, 'Restore') : ''));
  const list = h('div', { class: 'archive-list' }, rows.length ? rows : h('p', { class: 'muted' }, 'No archived work yet. Use Archive on a Done card or in a ticket’s details to keep it here.'));
  const search = h('input', { name: 'archive-search', type: 'search', placeholder: 'Search title, ticket key or details', oninput: e => { for (const row of list.querySelectorAll('[data-search]')) row.hidden = !row.dataset.search.includes(e.target.value.toLowerCase()); } });
  return h('section', { class: 'panel archive-panel' }, h('h2', {}, 'Archived work'), field('Search archive', search), list);
}
function canArchiveTicket(k) {
  if (k.status === 'done') return pcan('tickets.reopen');
  if (k.assigneeId && ['available', 'in_progress', 'review'].includes(k.status)) return k.assigneeId === state.me.member.id || pcan('tickets.assign');
  return k.creatorId === state.me.member.id || pcan('tickets.edit');
}

// What the person holding a ticket sees of their own agent on the board. Everyone else sees only whether it is active.
const AGENT_STATE = { queued: 'Agent starting', running: 'Agent running', needs_input: 'Agent needs your answer', blocked: 'Agent blocked', completed: 'Agent finished', failed: 'Agent failed', canceled: 'Agent stopped' };
// Whether the owner's agent is working on a ticket: a yes or a no, drawn the same for everyone.
function presence(active) {
  return h('span', { class: 'presence' + (active ? ' on' : ''), title: active ? 'Their agent is working on it right now.' : 'Their agent is not working on it right now.' },
    h('span', { class: 'dot', 'aria-hidden': 'true' }), active ? 'Active now' : 'Not active');
}
function ownAgentRun(d, k) {
  const records = d.progressReports?.find(r => r.ticket.id === k.id)?.progress || (d.ticket && d.ticket.id === k.id ? d.progress || [] : []);
  const progress = records.slice().sort((a, b) => Number(a.stale) - Number(b.stale) || new Date(b.reportedAt) - new Date(a.reportedAt))[0];
  const run = progress && !progress.stale && progress.execution.runId ? progress.execution.state : '';
  return { progress, run, running: ['queued', 'running', 'needs_input'].includes(run) };
}
// Opens the ticket at the agent controls with the policy to review already laid out: one more press starts it.
function startAgent(k) {
  focusMain = false; state.ticketId = k.id; state.agentFocus = k.id; state.handoff = null; remember();
  return render();
}

function card(d, k) {
  const mine = k.assigneeId === state.me.member.id;
  const pr = k.pullRequest;
  const agentWork = (k.workMode || 'agent') !== 'human';
  const { progress, run, running } = mine ? ownAgentRun(d, k) : { progress: null, run: '', running: false };
  const active = k.status === 'in_progress' && (mine ? running : !!(d.board.active || {})[k.id]);
  const flags = [];
  if (pr) {
    flags.push(h('span', { class: 'badge' + (pr.mergeable === 'conflicting' && pr.state === 'open' ? ' bad' : '') },
      'PR' + (pr.number ? ' #' + pr.number : '') + ' ' + pr.state + (pr.state === 'open' && pr.mergeable === 'conflicting' ? ' · conflicts' : '')));
    if (pr.state === 'open' && pr.behind > 0) flags.push(h('span', { class: 'badge warn' }, pr.behind + ' behind'));
  }
  // In progress covers a claimed ticket nobody has started and one with an agent running, so say which, to its owner only.
  const status = pendingTickets.has(k.id) ? 'Updating ticket…'
    : k.status === 'in_progress' && mine ? (AGENT_STATE[run] || (agentWork ? 'Claimed · waiting to start' : 'Claimed · yours to do'))
    : k.status === 'available' && k.assigneeId ? 'Claimed · ready to start' : statusName(k.status);
  const canStartAgent = mine && state.desktop && agentWork && k.status === 'in_progress' && !running && run !== 'blocked' && !k.archivedAt;
  const open = () => openTicket(d.id, k.id);
  return h('article', { class: 'card work-card' + (mine ? ' mine' : '') + (k.id === state.ticketId ? ' open' : ''), 'data-board-item': k.id, 'data-work-state': k.status, draggable: !pendingTickets.has(k.id), 'aria-busy': pendingTickets.has(k.id), ondragstart: e => { draggedTicket = k.id; e.dataTransfer.setData('text/plain', k.id); e.dataTransfer.effectAllowed = 'move'; }, ondragend: () => { draggedTicket = null; document.querySelectorAll('.drop-over').forEach(el => el.classList.remove('drop-over')); void render(); } },
    h('button', { type: 'button', 'data-drag-handle': '', draggable: false, 'aria-label': 'Move ticket: ' + k.title, title: 'Drag to a column, or use Left and Right arrows' }, icon('board')),
    h('div', { class: 'muted small' }, k.key),
    h('button', { class: 'link title', onclick: () => { state.ticketId = k.id === state.ticketId ? null : k.id; state.handoff = null; remember(); render(); } }, k.title),
    h('p', { class: 'work-status' }, status),
    progress?.execution.runId ? h('p', { class: 'work-detail' }, (progress.stale ? 'Last reported ' : 'Reported ') + ago(progress.reportedAt)) : '',
    k.description ? h('p', { class: 'work-detail' }, k.description.length > 180 ? k.description.slice(0, 177) + '…' : k.description) : '',
    k.branch ? h('p', { class: 'work-detail' }, k.branch) : '',
    k.plan && (k.plan.start || k.plan.end) ? h('p', { class: 'work-detail' }, 'Planned ' + (k.plan.start || k.plan.end) + (k.plan.end && k.plan.end !== k.plan.start ? ' – ' + k.plan.end : '')) : '',
    labelsOf(k, d.board.labels).length ? h('div', { class: 'lbls', 'aria-label': 'Labels' }, labelsOf(k, d.board.labels).map(labelChip)) : '',
    k.assigneeId ? h('div', { class: 'owner' }, initial(mine ? state.me.member.name : personName(d, k.assigneeId)), mine ? 'You' : personName(d, k.assigneeId),
      k.status === 'in_progress' && agentWork ? presence(active) : '') : '',
    k.assigneeId && k.claimedAt ? h('p', { class: 'work-detail' }, 'Claimed ' + ago(k.claimedAt)) : '',
    flags.length ? h('div', { class: 'flags' }, flags) : '',
    k.status === 'review' ? h('button', { class: 'plain open small', onclick: open }, 'Review work') : '',
    canStartAgent ? h('button', { class: 'primary small', 'data-start-agent': k.id, onclick: () => startAgent(k) }, run === 'completed' || run === 'failed' || run === 'canceled' ? 'Start agent again' : 'Start agent') : '',
    ['available', 'in_progress'].includes(k.status) && mine ? h('button', { class: 'plain open small', onclick: open }, 'Open work') : '',
    (k.status === 'available' && !k.assigneeId && d.board.member && pcan('tickets.claim'))
      ? h('button', { class: 'primary small', onclick: () => act(() => api('POST', projectPath(k) + '/claim')) }, 'Claim') : '',
    k.status === 'done' && !k.archivedAt && canArchiveTicket(k)
      ? h('button', { class: 'plain open small', onclick: () => act(() => api('POST', projectPath(k) + '/archive', { version: k.version, archived: true })) }, 'Archive') : '');
}

function projectPath(k) { return '/projects/' + state.projectId + '/tickets/' + k.id; }

// The planning side of a ticket: its labels, when it is planned for and what it waits for. For a new ticket
// k is null. read() gives what the person chose, in the shape the API takes.
function planFields(d, k, scope) {
  const sfx = (k ? k.id : 'new') + (scope ? '-' + scope : '');
  const b = d.board;
  const chosen = new Set(k ? k.labelIds || [] : []);
  const deps = new Set(k ? k.dependencies || [] : []);
  const labelBoxes = (b.labels || []).map((l) => ({ l, box: h('input', { type: 'checkbox', name: 'p-label_' + l.id + '-' + sfx, checked: chosen.has(l.id) }) }));
  const plan = (k && k.plan) || {};
  const start = h('input', { type: 'date', name: 'p-start-' + sfx, value: plan.start || '' });
  const end = h('input', { type: 'date', name: 'p-end-' + sfx, value: plan.end || '' });
  const milestone = h('input', { type: 'checkbox', name: 'p-milestone-' + sfx, checked: !!plan.milestone });
  const others = [...b.tickets, ...(b.archived || [])].filter((t) => !k || t.id !== k.id).filter((t) => !t.archivedAt || deps.has(t.id));
  const depBoxes = others.map((t) => ({ t, box: h('input', { type: 'checkbox', name: 'p-dep_' + t.id + '-' + sfx, checked: deps.has(t.id) }) }));
  const startError = h('small', { class: 'field-error' });
  const endError = h('small', { class: 'field-error' });
  const dependencyError = h('p', { class: 'field-error', role: 'alert' });
  const summaryError = h('p', { class: 'field-error plan-summary-error', role: 'alert' });
  const savedIssues = state.validationIssues.filter((issue) => !k || issue.itemId === k.id || issue.otherId === k.id);
  const applyIssues = (issues) => {
    const startIssue = issues.find((issue) => issue.field === 'plan.start');
    const endIssue = issues.find((issue) => issue.field === 'plan.end');
    const dependencyIssue = issues.find((issue) => issue.field === 'dependencies');
    start.classList.toggle('invalid', !!startIssue); start.setAttribute('aria-invalid', String(!!startIssue)); startError.textContent = startIssue?.message || '';
    end.classList.toggle('invalid', !!endIssue); end.setAttribute('aria-invalid', String(!!endIssue)); endError.textContent = endIssue?.message || '';
    dependencyError.textContent = dependencyIssue?.message || '';
    for (const { box } of depBoxes) { box.classList.toggle('invalid', !!dependencyIssue); box.setAttribute('aria-invalid', String(!!dependencyIssue)); }
    summaryError.textContent = issues.length ? 'Fix the highlighted planning issues.' : '';
  };
  applyIssues(savedIssues);
  const clearIssues = () => { state.validationIssues = []; state.error = ''; applyIssues([]); };
  for (const control of [start, end, milestone, ...depBoxes.map((x) => x.box)]) control.addEventListener('input', clearIssues);
  // Who does the work is no longer asked: a new ticket follows its project (an agent where there is a repository), and an
  // edit leaves what the ticket already has.
  const read = () => ({
    ...(k ? {} : { workMode: b.project.repository ? 'agent' : 'human' }),
    labelIds: labelBoxes.filter((x) => x.box.checked).map((x) => x.l.id),
    plan: milestone.checked ? { start: start.value || end.value, milestone: true } : { start: start.value, end: end.value },
    dependencies: depBoxes.filter((x) => x.box.checked).map((x) => x.t.id),
  });
  const day = (value) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null;
    const date = Date.parse(value + 'T00:00:00Z');
    return Number.isFinite(date) && new Date(date).toISOString().slice(0, 10) === value ? date / 86400000 : null;
  };
  const conflictScores = (tickets) => {
    const byId = new Map(tickets.map((t) => [t.id, t]));
    const out = new Map();
    for (const t of tickets) {
      if (t.archivedAt) continue;
      const startDay = day(t.plan?.start || t.plan?.end || '');
      if (startDay == null) continue;
      for (const id of t.dependencies || []) {
        const dep = byId.get(id);
        if (!dep || dep.status === 'done') continue;
        const depStart = dep.plan?.start || dep.plan?.end || '';
        const depEnd = dep.plan?.milestone ? depStart : dep.plan?.end || depStart;
        const endDay = day(depEnd);
        if (endDay != null && startDay < endDay) out.set(t.id + '\0' + dep.id, endDay - startDay);
      }
    }
    return out;
  };
  const validate = () => {
    const draft = read(), issues = [];
    const startDay = draft.plan.start ? day(draft.plan.start) : null;
    const endDay = draft.plan.end ? day(draft.plan.end) : null;
    if (milestone.checked && !draft.plan.start) issues.push({ field: 'plan.start', message: 'Choose a date for this milestone.' });
    if (draft.plan.start && startDay == null) issues.push({ field: 'plan.start', message: 'Enter a valid date.' });
    if (!milestone.checked && draft.plan.end && endDay == null) issues.push({ field: 'plan.end', message: 'Enter a valid date.' });
    if (!milestone.checked && startDay != null && endDay != null && endDay < startDay) issues.push({ field: 'plan.end', message: 'The end is before the start.' });
    const oldDependencies = new Set(k?.dependencies || []);
    for (const id of draft.dependencies) {
      const dep = others.find((x) => x.id === id);
      if (dep?.archivedAt && dep.status !== 'done' && (!k || !oldDependencies.has(id))) issues.push({ field: 'dependencies', message: 'Remove the unfinished archived prerequisite.' });
      const startDate = startDay;
      const depStart = dep?.plan?.start || dep?.plan?.end || '';
      const depEnd = dep?.plan?.milestone ? depStart : dep?.plan?.end || depStart;
      const endDate = day(depEnd);
      if (startDate != null && endDate != null && dep?.status !== 'done' && startDate < endDate) {
        issues.push({ field: 'plan.start', message: `Start is before ${dep.key || 'the prerequisite'} ends.` });
      }
    }
    if (k) {
      const all = [...b.tickets, ...(b.archived || [])];
      const updated = { ...k, plan: draft.plan, dependencies: draft.dependencies };
      const before = conflictScores(all), after = conflictScores(all.map((t) => t.id === k.id ? updated : t));
      for (const [key, amount] of after) if (amount > (before.get(key) || 0)) issues.push({ field: key.startsWith(k.id + '\0') ? 'plan.start' : draft.plan.milestone ? 'plan.start' : 'plan.end', message: 'This date conflicts with a task that depends on this work.' });
      const reaches = (from, target, seen = new Set()) => {
        if (from === target) return true;
        if (seen.has(from)) return false;
        seen.add(from);
        const task = all.find((x) => x.id === from);
        return (task?.dependencies || []).some((id) => reaches(id, target, seen));
      };
      const beforeCycle = (k.dependencies || []).some((id) => reaches(id, k.id));
      const afterCycle = draft.dependencies.some((id) => reaches(id, k.id));
      if (afterCycle && !beforeCycle) issues.push({ field: 'dependencies', message: 'These dependencies create a cycle.' });
    }
    applyIssues(issues);
    if (issues.length) {
      const first = issues[0].field;
      (first === 'plan.start' ? start : first === 'plan.end' ? end : depBoxes.find(({ box }) => box.checked)?.box)?.focus();
    }
    return issues.length === 0;
  };
  const dates = h('fieldset', {}, h('legend', {}, 'Planned for ', h('span', { class: 'muted small' }, 'optional')),
    h('div', { class: 'inline dates' }, field('Start date', start, startError), field('Due date', end, endError), h('label', { class: 'check' }, milestone, ' Milestone')));
  const node = h('div', { class: 'stack plan-fields' },
    h('fieldset', {}, h('legend', {}, 'Labels'),
      labelBoxes.length ? h('div', { class: 'label-menu' }, labelBoxes.map(({ l, box }) => h('label', { class: 'check' }, box, ' ', labelChip(l))))
        : h('p', { class: 'muted small' }, can('labels.manage') ? 'No labels yet. Make some under Settings → Labels.' : 'No labels yet. A workspace owner or admin makes them.')),
    dates,
    depBoxes.length ? h('fieldset', {}, h('legend', {}, 'Depends on ', h('span', { class: 'muted small' }, 'must finish first')),
      h('div', { class: 'dep-list' }, depBoxes.map(({ t, box }) => h('label', { class: 'check' }, box, ' ', t.key + ' · ' + t.title)), dependencyError)) : '', summaryError);
  return { node, read, validate, dates, summaryError, controls: [start, end, milestone] };
}

// A ticket is made in a dialog over the page, not in the page: the board stays where it was, and Escape or Cancel puts it away.
function newTicketForm(d) {
  const title = h('input', { name: 't-title', required: true, maxlength: 200, autocomplete: 'off' });
  const desc = h('textarea', { name: 't-desc', rows: 3, maxlength: 20000 });
  const reqs = h('textarea', { name: 't-reqs', rows: 3, maxlength: 20000, placeholder: 'Acceptance criteria, constraints, links, anything the person doing it needs to know' });
  const plan = planFields(d, null);
  const close = () => { state.newTicketOpen = false; render(); };
  const cancel = () => { title.value = ''; desc.value = ''; reqs.value = ''; close(); };
  const section = (heading, ...content) => h('section', { class: 'dlg-section' }, h('h3', {}, heading), ...content);
  const dialog = h('dialog', { class: 'ticket-dialog', 'aria-labelledby': 'new-ticket-h', oncancel: (e) => { e.preventDefault(); close(); },
      onclick: (e) => { if (e.target === dialog) close(); } },
    h('form', { class: 'dlg-form', onsubmit: (e) => { e.preventDefault(); if (!plan.validate()) return; act(async () => {
        await api('POST', '/projects/' + state.projectId + '/tickets', { title: title.value, description: desc.value, requirements: reqs.value, status: 'available', ...plan.read() }); state.newTicketOpen = false;
        title.value = ''; desc.value = ''; reqs.value = ''; }); } },
      h('header', { class: 'dlg-head' }, h('div', {}, h('h2', { id: 'new-ticket-h' }, 'New ticket'), h('p', { class: 'muted small' }, 'It starts in Available, where anyone on the project can claim it.')),
        h('button', { type: 'button', class: 'plain icon-button', 'aria-label': 'Close', title: 'Close', onclick: cancel }, icon('close'))),
      h('div', { class: 'dlg-body' },
        state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
        section('Ticket', field('Title', title), field('Description', desc), field('Requirements and context', reqs)),
        plan.node),
      h('footer', { class: 'dlg-foot' }, h('button', { type: 'button', class: 'plain', onclick: cancel }, 'Cancel'), h('button', { class: 'primary' }, 'Create ticket'))));
  return dialog;
}

// ---- one ticket ----

// One idea on the ticket, in a box of its own.
function tcard(title, ...content) {
  return h('section', { class: 'tcard', 'aria-label': title || undefined }, title ? h('h3', {}, title) : '', ...content);
}

function ticketPanel(d, k) {
  const b = d.board, me = state.me.member.id;
  const mine = k.assigneeId === me;
  const pr = k.pullRequest;
  const path = projectPath(k);
  const agentWork = (k.workMode || 'agent') !== 'human'; // no agent is asked to do human work: nothing to open in a runner
  const actions = [];
  const run = (method, p, body) => () => act(() => api(method, path + p, body));
  const isCreator = k.creatorId === me;
  const canEditText = !k.archivedAt && (isCreator || pcan('tickets.edit')) && k.status !== 'done';
  const active = k.status === 'in_progress' && (mine ? ownAgentRun(d, k).running : !!(b.active || {})[k.id]);

  if (!k.archivedAt) {

  if (k.status === 'available' && !k.assigneeId && b.member && pcan('tickets.claim')) actions.push(h('button', { class: 'primary', onclick: run('POST', '/claim') }, 'Claim this ticket'));
  // A ticket claimed before claiming moved it to In progress (a host that is not updated yet, or an old claim) still waits in Available.
  if (k.status === 'backlog' && (isCreator || pcan('tickets.edit'))) actions.push(h('button', { class: 'plain ok', onclick: run('POST', '/move', { status: 'available' }) }, 'Make available'));
  if (k.status === 'available' && mine) actions.push(h('button', { class: agentWork && state.desktop ? 'plain' : 'primary', onclick: run('POST', '/start-work') }, 'Start work'));
  const individual = mine && agentWork && ['available', 'in_progress', 'review'].includes(k.status) && typeof individualActions === 'function' ? individualActions(k.projectId, k.id) : '';
  if (individual) actions.push(individual);
  if (['available', 'in_progress', 'review'].includes(k.status) && mine && agentWork && pcan('handoff.own'))
    actions.push(h('button', { class: individual ? 'plain' : 'primary', onclick: () => act(async () => { state.handoff = await api('POST', path + '/handoff'); }) }, 'Open in my runner'));
  // Work with no agent (done by hand, or a person's to finish) is handed in from here; an agent's finished run does it by itself.
  if (k.status === 'in_progress' && mine && (!agentWork || k.workMode === 'hybrid' || !active)) actions.push(h('button', { class: 'primary', onclick: run('POST', '/submit', {}) }, 'Submit for review'));
  if (['available', 'in_progress'].includes(k.status) && k.assigneeId && (mine || pcan('tickets.assign'))) actions.push(h('button', { class: 'plain', onclick: run('POST', '/release') }, mine ? 'Release' : 'Take it back'));
  if (k.status === 'review' && pcan('tickets.review')) {
    const note = h('input', { name: 'rc-note-' + k.id, placeholder: 'What should change? (optional)', maxlength: 1000 });
    const blocker = b.completion[k.id];
    if (blocker) actions.push(h('p', { class: 'muted completion-blocker', role: 'status' }, blocker));
    actions.push(h('button', { class: 'primary', disabled: !!blocker, onclick: run('POST', '/complete') }, 'Mark done'),
      h('span', { class: 'inline' }, note, h('button', { class: 'plain', onclick: () => act(() => api('POST', path + '/request-changes', { note: note.value })) }, 'Request changes')));
  }
  if (k.status === 'done' && pcan('tickets.reopen')) actions.push(h('button', { class: 'plain ok', onclick: run('POST', '/move', { status: 'available' }) }, 'Reopen'));
  if (pcan('tickets.assign') && ['available', 'in_progress'].includes(k.status)) {
    const who = b.people.filter((p) => p.projectRole && p.id !== k.assigneeId);
    if (who.length) {
      const pick = h('select', { name: 'assign-pick', 'aria-label': 'Assign to' }, who.map((p) => h('option', { value: p.id }, p.name)));
      actions.push(h('span', { class: 'inline' }, pick, h('button', { class: 'plain', onclick: () => act(() => api('POST', path + '/assign', { memberId: pick.value })) }, k.assigneeId ? 'Reassign' : 'Assign')));
    }
  }

  }
  if (canArchiveTicket(k)) actions.push(h('button', { class: 'plain', onclick: () => act(() => api('POST', path + '/archive', { version: k.version, archived: !k.archivedAt })) }, k.archivedAt ? 'Restore ticket' : 'Archive ticket'));
  if (!k.archivedAt) actions.push(h('p', { class: 'muted small' }, 'Archiving moves this ticket out of the active board; it does not delete it.' + (['in_progress', 'review'].includes(k.status) ? ' It does not stop an agent running in your Werkbord; stop that run there if needed.' : '')));

  const mainAction = a => a.classList.contains('primary') || a.classList.contains('completion-blocker') || ['Make available', 'Open in Werkbord', 'Restore ticket', 'Reopen'].includes(a.textContent);
  const owner = k.assigneeId ? (mine ? 'You' : personName(d, k.assigneeId)) : 'Nobody yet';
  const facts = h('dl', { class: 'facts' },
    h('dt', {}, 'Status'), h('dd', {}, (k.archivedAt ? 'Archived · ' : '') + statusLabel(b, k.status)),
    h('dt', {}, 'Owner'), h('dd', {}, owner, k.status === 'in_progress' && agentWork ? [' ', presence(active)] : ''),
    h('dt', {}, 'Created by'), h('dd', {}, personName(d, k.creatorId), ' ', when(k.createdAt)),
    k.claimedAt && k.assigneeId ? [h('dt', {}, 'Claimed'), h('dd', {}, when(k.claimedAt))] : '',
    k.submittedAt ? [h('dt', {}, 'Submitted'), h('dd', {}, when(k.submittedAt), k.reviewerId ? ' · reviewer ' + personName(d, k.reviewerId) : '')] : '',
    k.completedAt ? [h('dt', {}, 'Completed'), h('dd', {}, when(k.completedAt))] : '',
    h('dt', {}, 'Updated'), h('dd', {}, when(k.updatedAt)),
    labelsOf(k, b.labels).length ? [h('dt', {}, 'Labels'), h('dd', { class: 'lbls' }, labelsOf(k, b.labels).map(labelChip))] : '',
    (k.dependencies || []).length ? [h('dt', {}, 'Waits for'), h('dd', {}, k.dependencies.map((id) => { const t = [...b.tickets, ...(b.archived || [])].find((x) => x.id === id); return t ? t.key + ' · ' + t.title : 'a ticket that is not here'; }).join(', '))] : '');

  const git = h('dl', { class: 'facts' },
    h('dt', {}, 'Branch'), h('dd', {}, k.branch ? [h('code', {}, k.branch), ' ', copy(k.branch, 'Copy')] : h('span', { class: 'muted' }, 'chosen when someone claims it')),
    h('dt', {}, 'Pull request'), h('dd', {}, pr ? prLine(pr) : h('span', { class: 'muted' }, 'none reported')));
  const commits = k.commits && k.commits.length
    ? h('ul', { class: 'commits' }, k.commits.map((c) => h('li', {}, h('code', {}, c.sha.slice(0, 8)), ' ', c.subject, c.author ? h('span', { class: 'muted' }, ' · ' + c.author) : '')))
    : h('p', { class: 'muted small' }, 'No commits reported.');

  // Only the owner sees what their agent does. Everyone else sees that the work is theirs and whether it is active.
  // The owner's agent has two views, as a task has in Individual: the ticket's Details (with the agent's choices and Start) and Chat.
  const ownAgent = mine && state.desktop && !k.archivedAt && ['available', 'in_progress', 'review'].includes(k.status) && agentWork;
  // A ticket opens on Details, or on Chat once its agent is known to be running or to be waiting for an answer (desktop.js moves it then),
  // unless the person chose a tab or came in through Start agent on the board, which is for its policy.
  if (state.ticketTabFor !== k.id) { state.ticketTabFor = k.id; state.ticketTabChosen = state.agentFocus === k.id; state.ticketTab = !state.ticketTabChosen && ownAgent && ownAgentPending(k) ? 'chat' : 'details'; }
  if (!ownAgent) state.ticketTab = 'details';
  const agentCard = ownAgent ? ownExecutionPanel(k, 'controls')
    : k.assigneeId && agentWork ? tcard(mine ? '' : 'Agent', mine ? executionProgress(d.progress || []) : h('p', { class: 'muted' }, personName(d, k.assigneeId) + ' owns this work, and their agent is ' + (active ? 'active right now' : 'not active right now') + '. What it does, asks and writes stays private to them.'))
    : !k.assigneeId && agentWork && !k.archivedAt && k.status === 'available' ? tcard('Agent', h('p', { class: 'muted' }, 'Claim this ticket to prepare your own agent. It moves to In progress, and you start the agent from there.'))
    : '';

  return h('div', { class: 'panel ticket', 'data-scroll-key': 'ticket-' + k.id },
    h('div', { class: 'ticket-head' },
      h('h2', {}, k.key + ' · ' + k.title),
      h('button', { class: 'plain', onclick: () => { state.ticketId = null; state.handoff = null; remember(); render(); } }, 'Close')),
    h('div', { class: 'actions' }, actions.filter(mainAction)),
    actions.some(a => !mainAction(a)) ? h('details', { class: 'ticket-more' }, h('summary', {}, 'More actions'), h('div', { class: 'actions' }, actions.filter(a => !mainAction(a)))) : '',
    state.handoff && state.handoff.ticket.id === k.id ? handoffBox(state.handoff) : '',
    ownAgent ? ticketTabs(k) : '',
    ownAgent && state.ticketTab === 'chat' ? h('div', { class: 'ticket-chat', id: 'ticket-view', role: 'tabpanel', 'aria-labelledby': 'ticket-tab-chat' }, ownExecutionPanel(k, 'chat')) : h('div', { class: 'ticket-grid', id: ownAgent ? 'ticket-view' : null, role: ownAgent ? 'tabpanel' : null, 'aria-labelledby': ownAgent ? 'ticket-tab-details' : null },
      h('div', { class: 'ticket-main' },
        tcard('Description', h('p', { class: 'prose' }, k.description || '—'), canEditText ? editTicket(d, k, path) : ''),
        k.requirements ? h('details', { class: 'ticket-context' }, h('summary', {}, 'Requirements and context'), h('p', { class: 'prose' }, k.requirements)) : '',
        agentCard,
        tcard('', commentsSection(d, k)),
        !k.archivedAt ? teamSchedulePanel(k, b) : ''),
      h('div', { class: 'ticket-side' },
        tcard('Status', facts),
        datesCard(d, k, path, canEditText),
        tcard('Branch and pull request', git, h('h4', {}, 'Commits'), commits,
          !k.archivedAt && k.assigneeId && (mine || pcan('git.report_any')) && ['available', 'in_progress', 'review'].includes(k.status) ? gitForm(k, path) : ''))));
}

// Details and Chat, in the shared tab style, as a real tablist: the arrow keys move between them and a question waiting for an answer is counted.
function ticketTabs(k) {
  const pending = ownAgentPending(k);
  const tabs = [['details', 'Details'], ['chat', 'Chat']];
  const choose = (id) => { state.ticketTab = id; state.ticketTabChosen = true; render(); requestAnimationFrame(() => document.getElementById('ticket-tab-' + id)?.focus()); };
  return h('div', { class: 'tabs section-tabs ticket-tabs', role: 'tablist', 'aria-label': 'Ticket views' }, tabs.map(([id, label]) => h('button', {
    type: 'button', role: 'tab', id: 'ticket-tab-' + id, 'aria-controls': 'ticket-view', 'aria-selected': String(state.ticketTab === id), tabindex: state.ticketTab === id ? '0' : '-1',
    onclick: () => choose(id),
    onkeydown: (e) => { const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0; if (!step && e.key !== 'Home' && e.key !== 'End') return; e.preventDefault(); choose(tabs[e.key === 'Home' ? 0 : e.key === 'End' ? tabs.length - 1 : (tabs.findIndex(([t]) => t === state.ticketTab) + step + tabs.length) % tabs.length][0]); },
  }, label, id === 'chat' && pending ? h('span', { class: 'num pend', 'aria-label': pending + (pending === 1 ? ' question waiting for you' : ' questions waiting for you') }, String(pending)) : '')));
}

// When it is planned for. These dates are what the Calendar and the Timeline draw.
function datesCard(d, k, path, canEdit) {
  const plan = k.plan || {};
  const fmt = (day) => day ? new Date(day + 'T12:00:00').toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' }) : '';
  const set = !!(plan.start || plan.end);
  const view = h('dl', { class: 'facts' },
    plan.milestone ? [h('dt', {}, 'Milestone'), h('dd', {}, fmt(plan.start || plan.end))]
      : [h('dt', {}, 'Start'), h('dd', {}, plan.start ? fmt(plan.start) : h('span', { class: 'muted' }, 'not set')), h('dt', {}, 'Due'), h('dd', {}, plan.end ? fmt(plan.end) : h('span', { class: 'muted' }, 'not set'))]);
  const toCalendar = h('button', { type: 'button', class: 'link small', onclick: () => openProject(d.id, 'calendar') }, 'See it on the Calendar');
  if (!canEdit || k.archivedAt) return tcard('Dates', view, set ? toCalendar : h('p', { class: 'muted small' }, 'Not planned yet. The ticket’s creator or a project owner sets its dates.'));
  const form = planFields(d, k, 'dates');
  const draftId = k.id + '-dates';
  const version = ticketVersion({ id: draftId, version: k.version });
  return tcard('Dates',
    version !== k.version ? h('div', { role: 'alert' }, h('p', { class: 'error' }, 'This ticket changed while you were editing its dates. Your dates are kept; load the latest version before trying again.'), h('button', { class: 'plain', type: 'button', onclick: () => { discardTicketDraft(draftId); render(); } }, 'Load latest dates')) : '',
    h('form', { class: 'stack plan-fields dates-form', 'data-ticket-id': draftId, 'data-ticket-version': k.version,
      onsubmit: (e) => { e.preventDefault(); if (!form.validate()) return; const { plan: next } = form.read(); act(() => api('PATCH', path, { plan: next, version }).then(() => { discardTicketDraft(draftId); toast('Dates saved for ' + k.key + '.'); })); } },
      form.dates, form.summaryError, changedSubmit('Save dates', form.controls)),
    h('p', { class: 'muted small' }, 'The Calendar and the Timeline draw these dates. ', set ? toCalendar : ''));
}

function executionProgress(records, note = true) {
  const labels = { queued: 'Queued', running: 'Running', needs_input: 'Waiting for an answer', blocked: 'Blocked', completed: 'Completed', failed: 'Failed', canceled: 'Cancelled' };
  const rows = records.map(p => {
    const e = p.execution;
    const fact = (label, value) => h('div', { class: 'run-fact' }, h('dt', {}, label), h('dd', {}, value));
    return h('div', { class: 'execution-progress' },
      h('div', { class: 'run-state' }, h('span', { class: 'badge' + (p.stale || ['needs_input', 'blocked'].includes(e.state) ? ' warn' : e.state === 'failed' ? ' bad' : '') }, labels[e.state] || 'Unknown'), h('span', { class: 'muted small' }, 'Reported ', when(p.reportedAt)),
        records.length > 1 ? h('span', { class: 'muted small run-device' }, 'Device ', h('code', { title: p.deviceId }, p.deviceId.slice(-12))) : ''),
      p.stale ? h('p', { class: 'muted' }, 'Update overdue. Runner availability is unknown until the connector reconnects.') : h('p', { class: 'muted' }, e.runnerOnline ? (e.runnerAvailable ? 'Runner online · capacity available' : 'Runner online · no spare capacity') : 'Runner offline'),
      h('dl', { class: 'run-facts' },
        e.startedAt ? fact('Run started', when(e.startedAt)) : '',
        e.completedAt ? fact('Run finished', when(e.completedAt)) : '',
        fact(p.stale ? 'Last handoff report' : 'Handoff', e.handoffAvailable ? (p.stale ? 'Was available in the owner’s Werkbord' : 'Available in the owner’s Werkbord') : (p.stale ? 'Was not available' : 'Not available'))),
      p.gitUnavailable ? h('p', { class: 'muted small' }, 'Git metadata is unavailable; previously reported Git facts are retained.') : '');
  });
  return h('section', { 'aria-label': 'Werkbord execution' }, h('h3', {}, 'Werkbord execution'),
    rows.length ? rows : h('p', { class: 'muted' }, 'No execution reported. The owner can enable their connector to synchronize this ticket.'),
    rows.length && note ? h('p', { class: 'muted small' }, 'Completed agent-only work moves to Review. Human and hybrid work wait for their owner to submit them. Reviewing and marking work done stays with the team.') : '');
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

function editTicket(d, k, path) {
  const plan = planFields(d, k);
  const title = h('input', { name: 'e-title-' + k.id, value: k.title, maxlength: 200 });
  const desc = h('textarea', { name: 'e-desc-' + k.id, rows: 3 }); desc.value = k.description;
  const reqs = h('textarea', { name: 'e-reqs-' + k.id, rows: 3 }); reqs.value = k.requirements;
  return h('details', { open: ['calendar', 'timeline'].includes(state.tab) || state.validationIssues.some((issue) => issue.itemId === k.id || issue.otherId === k.id) }, h('summary', {}, 'Edit this ticket'),
    ticketVersion(k) !== k.version ? h('div', { role: 'alert' }, h('p', { class: 'error' }, 'Someone changed this ticket while you were editing. Your draft is kept. Copy your text before loading their version and combining the changes.'), h('button', { class: 'plain', type: 'button', onclick: () => { discardTicketDraft(k.id); render(); } }, 'Load latest version')) : '',
    h('form', { class: 'stack', 'data-ticket-id': k.id, 'data-ticket-version': k.version, onsubmit: (e) => { e.preventDefault(); if (!plan.validate()) return; act(() => api('PATCH', path, { title: title.value, description: desc.value, requirements: reqs.value, ...plan.read(), version: ticketVersion(k) }).then(() => discardTicketDraft(k.id)));  } },
      field('Title', title), field('Description', desc), field('Requirements and context', reqs), plan.node, changedSubmit(['calendar', 'timeline'].includes(state.tab) ? 'Save changes' : 'Save', [title, desc, reqs, ...plan.node.querySelectorAll('input, select, textarea')])));
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
      changedSubmit('Save', [branch, url, num, stateSel, mergeSel, behind])),
    k.status === 'in_progress' && pr.url ? h('button', { class: 'primary', onclick: () => act(() => api('POST', path + '/submit', {})) }, 'Submit for review') : '',
    k.status === 'in_progress' && !pr.url ? h('p', { class: 'muted' }, 'To submit for review: record the pull request above, or submit without one below.') : '',
    k.status === 'in_progress' && !pr.url ? h('button', { class: 'plain ok', onclick: () => act(() => api('POST', path + '/submit', {})) }, 'Submit for review without a pull request') : '');
}

function handoffBox(hf) {
  if (state.desktop) return runnerHandoff(hf);
  const json = JSON.stringify(hf, null, 2);
  return h('div', { class: 'handoff' },
    h('strong', {}, 'Open Team ticket ' + hf.ticket.key + ' as a task in your Werkbord'),
    h('p', { class: 'muted' }, 'This opens a local task with the ticket\'s context in your Werkbord. It does not connect to anyone\'s runner, and nobody else can use it to run anything on yours. It holds no credentials and no paths.'),
    h('ol', {},
      h('li', {}, 'In your Werkbord, add a task to your checkout of ', h('code', {}, hf.git.repository || 'this project\'s repository'), ' and paste the task text below as its description.'),
      h('li', {}, 'Run it on the branch ', h('code', {}, hf.git.branch), '. Commit and push there, then open a pull request. Do not merge it: the reviewer does that.'),
      h('li', {}, 'Come back and submit the ticket for review.')),
    h('div', { class: 'actions' }, copy(hf.prompt, 'Copy task text'), copy(json, 'Copy handoff (JSON)'),
      h('button', { class: 'plain', onclick: () => {
        const a = h('a', { href: URL.createObjectURL(new Blob([json], { type: 'application/json' })), download: hf.ticket.key.toLowerCase() + '-handoff.json' });
        document.body.append(a); a.click(); a.remove(); } }, 'Download handoff'),
      h('button', { class: 'plain', onclick: () => { state.handoff = null; render(); } }, 'Hide')),
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
    sectionButtons('repoSection', [['attention', 'Needs you', r.attention.length], ['prs', 'Pull requests', r.pullRequests.length], ['branches', 'Branches', r.branches.length]], 'Repository sections'),
    state.repoSection === 'attention' ? h('div', { class: 'panel' }, h('h2', {}, 'Needs you'), attention) : '',
    state.repoSection === 'prs' ? h('div', { class: 'panel' }, h('h2', {}, 'Pull requests'), prs) : '',
    state.repoSection === 'branches' ? h('div', { class: 'panel scroll' }, h('h2', {}, 'Branches'), branches,
      h('p', { class: 'muted small' }, 'A branch is stale after ' + r.staleAfterDays + ' days without activity.')) : '');
}

// ---- activity ----

// Runs: what each member's own Werkbord reports for the work they hold, one card to a ticket.
function activityTab(d) {
  const reports = d.progressReports || [];
  return h('section', { class: 'runs-view', 'aria-label': 'Runs' },
    h('header', { class: 'runs-intro' }, h('h2', {}, 'Runs'),
      h('p', { class: 'muted' }, 'Execution reported by members’ own Werkbords. Team does not run agents; raw output and full run history stay with each owner. Completed agent-only work moves to Review; reviewing and marking work done stays with the team.')),
    reports.length ? h('div', { class: 'run-list' }, reports.map(r => h('article', { class: 'run-card' },
      h('header', { class: 'run-card-head' },
        h('div', { class: 'run-card-title' }, h('span', { class: 'run-key' }, r.ticket.key), h('button', { type: 'button', class: 'link', onclick: () => openTicket(d.id, r.ticket.id) }, r.ticket.title)),
        h('span', { class: 'badge' }, statusName(r.ticket.status))),
      r.ticket.assigneeId === state.me.member.id ? executionProgress(r.progress, false) : h('p', { class: 'muted' }, 'Owned by ' + personName(d, r.ticket.assigneeId)))))
      : h('div', { class: 'cc-clear' }, h('span', { class: 'cc-dot' }), h('div', {}, h('p', { class: 'cc-clear-title' }, 'No runs yet.'), h('p', { class: 'muted' }, 'No execution has been reported for this project.'))));
}
function peopleTab(d) {
  const pid = d.id, manage = pcan('members.manage');
  const rows = d.people.map((p) => {
    const role = h('select', { name: 'role-' + p.id, 'aria-label': 'Role of ' + p.name }, ['owner', 'reviewer', 'member'].map((r) => h('option', { value: r, selected: p.projectRole === r }, r)));
    const setRole = changedSubmit('Set role', [role], [p.projectRole]);
    setRole.type = 'button';
    setRole.addEventListener('click', () => act(() => api('PUT', '/projects/' + pid + '/members/' + p.id, { role: role.value })));
    return h('div', { class: 'row' },
      h('div', { class: 'grow' }, h('strong', {}, p.name), ' ', h('span', { class: 'muted' }, p.email || '')),
      manage ? [role, setRole,
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
      h('p', { class: 'muted' }, 'The code lets someone who is already in this workspace join this project. To bring in a new person or computer, invite them from People first. Send the code privately; it cannot be shown again.'),
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
 state.tab = [...TABS, ...ADMIN_TABS].some(x => x[0] === tab) || tab === 'project-settings' ? tab : 'workspace';
 if (FRAMED && !HOSTED && ADMIN_TABS.some(x => x[0] === state.tab)) state.tab = 'workspace'; // the window's own Settings shows these
 state.settingsPart = SETTINGS_PARTS.has(q.get('part')) ? q.get('part') : '';
 state.projectId = q.get('project') || null;
 state.ticketId = q.get('ticket') || null;
 state.focusComment = state.ticketId && /^[A-Za-z0-9_-]{1,64}$/.test(q.get('comment') || '') ? q.get('comment') : '';
}
readLocation();
window.addEventListener('popstate', () => { closeSearch(false); captureDrafts(); readLocation(); focusMain = true; state.data = null; state.handoff = null; render(); });
window.addEventListener('keydown', e => {
  if (e.defaultPrevented || searchSession) return;
  if (e.key === 'Escape') {
    const menu = app.querySelector('.member-manage[open]');
    if (menu) { e.preventDefault(); menu.open = false; menu.querySelector('summary').focus(); return; }
  }
  if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || e.target.closest('input, textarea, select, [contenteditable="true"]')) return;
  if (e.key === 'Escape' && state.ticketId) { e.preventDefault(); state.ticketId = null; state.handoff = null; remember(); render(); }
  if (e.key.toLowerCase() === 'n' && state.tab === 'board' && pcan('tickets.create') && !state.data.board.project.archived) {
    e.preventDefault(); state.newTicketOpen = true; render().then(() => app.querySelector('[name="t-title"]')?.focus());
  }
});
function openInIndividual(place) {
  if (FRAMED && /^#\/[A-Za-z0-9/_-]{0,200}$/.test(place)) window.parent.postMessage({ type: 'werkbord.frame', event: 'open', target: 'personal', place }, '*');
}
// The theme and the style are chosen in the window's sidebar and told to this page; only the window that framed it is believed.
window.addEventListener('message', e => {
  if (!FRAMED || e.source !== window.parent) return;
  const m = e.data;
  if (m && m.type === 'werkbord.style') { teamStyle.set(m.style); return; }
  if (!m || m.type !== 'werkbord.theme' || (m.theme !== 'light' && m.theme !== 'dark' && m.theme !== '')) return;
  teamTheme.set(m.theme);
});
// The desktop app's shell can ask this page, framed beside a person's other workspaces, to open a place in it (a ticket that
// needs them). Only the window that framed this page is believed, and only a place inside it: a query on this page.
window.addEventListener('message', e => {
  if (window.parent === window || e.source !== window.parent) return;
  const m = e.data;
  if (!m || m.type !== 'werkbord.navigate' || typeof m.href !== 'string' || !/^\?[A-Za-z0-9/_.:=&%?#-]{0,299}$/.test(m.href) || m.href.includes('//') || m.href.includes('..')) return;
  closeSearch(false); captureDrafts();
  history.pushState(null, '', location.pathname + m.href);
  readLocation(); reportPlace(); focusMain = true; state.data = null; state.handoff = null; render();
});
document.addEventListener('click', e => { app.querySelectorAll('.member-manage[open]').forEach(menu => { if (!menu.contains(e.target)) menu.open = false; }); });
function reportPlace() {
  if (window.parent !== window) window.parent.postMessage({ type: 'werkbord.frame', event: 'place', place: location.search || '?tab=workspace' }, '*');
}
window.addEventListener('load', () => {
  render();
  if (window.parent !== window) window.parent.postMessage({ type: 'werkbord.frame', event: 'ready' }, '*');
});
window.addEventListener('popstate', reportPlace);

const scheduleDrafts = new Map();
// Names the tickets k waits for: they are set on the ticket and nowhere else, and a request for agent work waits for them.
function waitsForText(k, b) {
  const all = [...b.tickets, ...(b.archived || [])];
  const names = (k.dependencies || []).map((id) => { const t = all.find((x) => x.id === id); return t ? t.key + ' ' + t.title + (t.archivedAt && t.status !== 'done' ? ' (archived, not finished)' : '') : 'a ticket that is not here'; });
  return names.length ? 'Waits for ' + names.join(', ') + ' (set on the ticket)' : 'Waits for nothing (dependencies are set on the ticket)';
}
// Opens the ticket's own edit form at its dependencies.
function showTicketDependencies(k) {
  const form = [...app.querySelectorAll('form[data-ticket-id]')].find((f) => f.getAttribute('data-ticket-id') === k.id);
  const details = form && form.closest('details');
  if (!details) return;
  details.open = true;
  const first = form.querySelector('.dep-list input') || form.querySelector('input');
  details.scrollIntoView({ block: 'center' });
  if (first) first.focus();
}
function teamSchedulePanel(k, b) {
  const box = h('section', { class: 'handoff' }, h('h3', {}, 'Shared schedule'), h('p', { class: 'muted' }, 'A schedule requests work. Only the holder’s local approval can authorize execution.'));
  api('GET', '/projects/' + k.projectId + '/schedules').then(schedules => {
    const current = schedules.find(s => s.ticketId === k.id);
    const live = current && !['completed', 'canceled'].includes(current.state);
    const editable = [...app.querySelectorAll('form[data-ticket-id]')].some((f) => f.getAttribute('data-ticket-id') === k.id);
    box.append(h('p', { class: 'waits-for' }, waitsForText(k, b), editable ? [' ', h('button', { type: 'button', class: 'link', onclick: () => showTicketDependencies(k) }, 'Change on the ticket')] : ''));
    if (current) box.append(h('p', { role: 'status' }, (current.stale ? 'Runner update overdue; last state: ' : '') + current.state.replaceAll('_', ' ') + (current.reason ? ' · ' + current.reason : '')), h('p', {}, new Date(current.at).toLocaleString() + ' · ' + current.timezone));
    // What the request was proposed to wait for is no longer what the ticket waits for: it is stale until it is proposed again.
    const proposed = new Set(current ? current.dependencies || [] : []), now = new Set(k.dependencies || []);
    if (live && (proposed.size !== now.size || [...now].some((id) => !proposed.has(id)))) box.append(h('p', { class: 'advice', role: 'alert' }, 'This request is stale: the ticket’s dependencies changed after it was proposed. It will not start until you propose it again below.'));
    const allowed = k.assigneeId === state.me.member.id || pcan('tickets.assign');
    if (!allowed || !k.assigneeId || !['available', 'in_progress', 'review'].includes(k.status)) return;
    const key = k.projectId + ':' + k.id;
    let draft = scheduleDrafts.get(key);
    if (!draft || draft.version !== (current?.version || 0)) {
      draft = { version: current?.version || 0, at: current?.at || '', timezone: current?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone, missed: current?.missedPolicy || 'run_late', grace: current?.graceSeconds ?? 300, order: current?.order ?? 0, priority: current?.priority ?? 1, errors: {} };
      scheduleDrafts.set(key, draft);
    }
    draft.errors ||= {};
    const edit = field => e => {
      draft[field] = e.target.value;
      delete draft.errors[field]; delete draft.errors.form;
      e.target.classList.remove('invalid'); e.target.removeAttribute('aria-invalid'); e.target.removeAttribute('aria-describedby');
      e.target.parentElement?.querySelector('.field-error')?.remove();
    };
    const issue = (name, control) => {
      if (!draft.errors[name]) return '';
      control.classList.add('invalid'); control.setAttribute('aria-invalid', 'true'); control.setAttribute('aria-describedby', control.id + '-error');
      return h('small', { class: 'field-error', id: control.id + '-error', role: 'alert' }, draft.errors[name]);
    };
    const at = h('input', { id: 'team-schedule-at-' + k.id, name: 'schedule-at-' + k.id, value: draft.at, oninput: edit('at'), placeholder: '2026-10-10T09:00:00+02:00', required: true });
    const timezone = h('input', { id: 'team-schedule-zone-' + k.id, name: 'schedule-zone-' + k.id, value: draft.timezone, oninput: edit('timezone'), required: true });
    const missed = h('select', { name: 'schedule-missed-' + k.id, onchange: edit('missed') }, [['run_late', 'Wait and run when eligible'], ['skip', 'Skip after grace window']].map(([value, label]) => h('option', { value, selected: value === draft.missed }, label)));
    const grace = h('input', { id: 'team-schedule-grace-' + k.id, name: 'schedule-grace-' + k.id, type: 'number', min: 0, max: 86400, value: draft.grace, oninput: edit('grace') });
    const order = h('input', { id: 'team-schedule-order-' + k.id, name: 'schedule-order-' + k.id, type: 'number', min: 0, value: draft.order, oninput: edit('order') });
    const priority = h('select', { id: 'team-schedule-priority-' + k.id, name: 'schedule-priority-' + k.id, onchange: edit('priority') }, [['0', 'Low'], ['1', 'Normal'], ['2', 'High']].map(([value, label]) => h('option', { value, selected: Number(value) === Number(draft.priority) }, label)));
    box.append(h('form', { onsubmit: e => { e.preventDefault(); act(async () => {
      draft.errors = {};
      try { new Intl.DateTimeFormat('en', { timeZone: timezone.value }).format(0); }
      catch (_) { draft.errors.timezone = 'Choose a valid IANA timezone.'; }
      const atYear = Number(at.value.slice(0, 4));
      if (!/(Z|[+-]\d\d:\d\d)$/.test(at.value) || !Number.isFinite(Date.parse(at.value)) || atYear < 2020 || atYear > 9999) draft.errors.at = 'Enter a valid ISO time with an explicit UTC offset.';
      if (!Number.isInteger(Number(grace.value)) || Number(grace.value) < 0 || Number(grace.value) > 86400) draft.errors.grace = 'Enter a wait from 0 to 86,400 seconds.';
      if (!Number.isInteger(Number(order.value)) || Number(order.value) < 0) draft.errors.order = 'Enter a non-negative run order.';
      if (![0, 1, 2].includes(Number(priority.value))) draft.errors.priority = 'Choose a valid priority.';
      if (Object.keys(draft.errors).length) {
        const firstField = ['at', 'timezone', 'grace', 'order', 'priority'].find(name => draft.errors[name]);
        if (firstField) state.focusId = ({ at: at.id, timezone: timezone.id, grace: grace.id, order: order.id, priority: priority.id })[firstField];
        return;
      }
      try {
        await api('PUT', projectPath(k) + '/schedule', { version: current?.version || 0, at: new Date(at.value).toISOString(), timezone: timezone.value, missedPolicy: missed.value, graceSeconds: Number(grace.value), order: Number(order.value), priority: Number(priority.value) });
        scheduleDrafts.delete(key);
        if (state.desktop) executionViews.delete(k.projectId + ':' + k.id);
      } catch (err) {
        draft.errors.form = err instanceof Error ? err.message : String(err);
      }
    }); } }, field('Start time with UTC offset', at, issue('at', at)), field('Display timezone', timezone, issue('timezone', timezone)), field('Missed execution', missed), field('Wait before marking missed (seconds)', grace, issue('grace', grace)), field('Run order', order, issue('order', order)), field('Priority', priority, issue('priority', priority)), draft.errors.form ? h('p', { class: 'field-error', role: 'alert' }, draft.errors.form) : '', h('p', { class: 'muted small' }, 'Dependencies can keep execution waiting until they finish.'), h('button', { class: 'plain' }, current ? 'Reschedule request' : 'Propose schedule')));
    if (current && !['completed', 'canceled'].includes(current.state)) box.append(h('button', { class: 'danger', onclick: () => act(async () => { await api('POST', projectPath(k) + '/schedule/cancel', { version: current.version }); if (state.desktop) executionViews.delete(k.projectId + ':' + k.id); }) }, 'Cancel request'));
  }).catch(err => box.append(h('p', { class: 'advice', role: 'alert' }, err.message)));
  return box;
}

// ---- the timeline ----
// A project's tickets placed in time: planned date ranges, milestones and the dependencies between them, grouped as the
// person chooses. It shows and never moves anything: a conflict is marked (the server finds them) and the person decides.

const TL = WerkbordTimeline;
function svgEl(name, attrs) {
  const el = document.createElementNS('http://www.w3.org/2000/svg', name);
  for (const [k, v] of Object.entries(attrs || {})) el.setAttribute(k, String(v));
  return el;
}

function timelineTab(d) {
  const b = d.board, warnings = (d.timeline && d.timeline.warnings) || [];
  const today = TL.localToday(Date.now());
  const year = Number(today.slice(0, 4));
  const px = TL.PX_PER_DAY[state.tlZoom];
  const visible = b.tickets.filter(matchesFilter);
  const dated = visible.filter((k) => TL.spanOf(k));
  const undated = visible.filter((k) => !TL.spanOf(k));
  const groups = TL.groupTasks(dated, b.labels, state.tlGroup);
  const range = TL.viewRange(dated.map((k) => TL.spanOf(k)), today);
  const width = (TL.daysBetween(range.start, range.end) + 1) * px;
  const todayX = TL.daysBetween(range.start, today) * px + px / 2;
  const byTicket = TL.warningsByTask(warnings);
  const ticketOf = (id) => [...b.tickets, ...(b.archived || [])].find((k) => k.id === id);

  const rows = [];
  for (const g of groups) {
    if (state.tlGroup !== 'none') rows.push({ kind: 'group', key: g.key, group: g });
    if (state.tlGroup !== 'none' && state.tlCollapsed[g.key]) continue;
    for (const k of g.tasks) rows.push({ kind: 'ticket', key: g.key + ':' + k.id, k });
  }
  const bars = TL.barsFor(dated, range, state.tlZoom);
  const rowOf = TL.firstRows(rows.map((r) => (r.kind === 'ticket' ? r.k.id : null)));
  const links = TL.arrows(bars, rowOf, TL.dependencyMap(dated));
  const requested = new Set((d.schedules || []).filter((v) => !['completed', 'canceled'].includes(v.state)).map((v) => v.ticketId));
  const gates = (l) => requested.has(l.to);
  const open = (k) => () => openTicket(d.id, k.id);

  const tools = h('div', { class: 'actions board-tools tl-tools' },
    choiceGroup('Zoom', ['day', 'week', 'month', 'quarter'].map((z) => [z, z[0].toUpperCase() + z.slice(1)]), z => state.tlZoom === z, z => { state.tlZoom = z; render(); }),
    h('label', { class: 'inline' }, 'Group ', h('select', { name: 'tl-group', 'aria-label': 'Group work by', onchange: (e) => { state.tlGroup = e.target.value; render(); } },
      [['none', 'Nothing'], ['label', 'Label'], ['status', 'Status']].map(([id, name]) => h('option', { value: id, selected: state.tlGroup === id }, name)))),
    h('button', { type: 'button', class: 'plain small', onclick: () => { centerTimeline(); } }, 'Today'),
    filterBar(d));

  const warnPanel = warnings.length ? h('details', { class: 'tl-warns' }, h('summary', {}, 'Planning issues ', h('span', { class: 'badge' }, String(warnings.length))),
    h('ul', {}, warnings.map((w) => { const k = ticketOf(w.itemId); return h('li', { 'data-sev': w.severity },
      h('span', { class: 'sev' }, w.severity === 'error' ? 'Problem' : w.severity === 'warning' ? 'Conflict' : 'Note'), h('span', { class: 'msg' }, w.message),
      k ? h('button', { type: 'button', class: 'plain small', onclick: open(k) }, 'Open ' + k.key) : ''); }))) : '';

  let chart;
  if (!dated.length) {
    chart = h('div', { class: 'tl-empty' }, h('p', {}, filterActive() ? 'No planned work matches the filter.' : 'Nothing is planned yet.'), h('p', { class: 'muted' }, 'Give a ticket a start and end date (or make it a milestone) and it appears here.'));
  } else {
    const axis = h('div', { class: 'tl-axis' }, TL.ticks(range, state.tlZoom).map((t) => { const el = h('span', { class: 'tl-tick' + (t.major ? ' major' : '') }, t.label); el.style.left = t.x + 'px'; return el; }));
    axis.style.width = width + 'px';
    const names = h('div', { class: 'tl-names' }, rows.map((r) => {
      if (r.kind === 'group') {
        const btn = h('button', { type: 'button', class: 'tl-group', 'aria-expanded': String(!state.tlCollapsed[r.key]), onclick: () => { state.tlCollapsed[r.key] = !state.tlCollapsed[r.key]; render(); } },
          h('span', { class: 'caret' }, state.tlCollapsed[r.key] ? '▸' : '▾'), r.group.color ? labelChip({ name: r.group.title, color: r.group.color }) : h('span', {}, r.group.title), h('span', { class: 'badge' }, String(r.group.tasks.length)));
        return btn;
      }
      const ws = byTicket.get(r.k.id), worst = TL.worst(ws);
      return h('button', { type: 'button', class: 'tl-name', 'data-status': r.k.status, title: r.k.key + ' · ' + r.k.title, onclick: open(r.k) },
        worst ? h('span', { class: 'tl-warn', 'data-sev': worst, title: ws.map((x) => x.message).join('\n'), 'aria-label': 'Has ' + plural(ws.length, 'warning') }, '!') : '',
        h('span', { class: 'nt' }, r.k.title));
    }));
    const plot = h('div', { class: 'tl-plot' });
    plot.style.width = width + 'px'; plot.style.height = rows.length * TL.ROW_HEIGHT + 'px';
    for (const t of TL.ticks(range, state.tlZoom)) { const g = h('span', { class: 'tl-grid' + (t.major ? ' major' : '') }); g.style.left = t.x + 'px'; plot.append(g); }
    const tl = h('span', { class: 'tl-today', title: 'Today' }); tl.style.left = todayX + 'px'; plot.append(tl);
    rows.forEach((r, i) => {
      if (r.kind === 'group') { const band = h('span', { class: 'tl-band' }); band.style.top = i * TL.ROW_HEIGHT + 'px'; band.style.height = TL.ROW_HEIGHT + 'px'; plot.append(band); return; }
      const bar = bars.get(r.k.id);
      if (!bar) return;
      const worst = TL.worst(byTicket.get(r.k.id)), label = r.k.key + ' · ' + r.k.title + ' · ' + TL.spanTitle(bar.span, year);
      if (bar.milestone) {
        const m = h('button', { type: 'button', class: 'tl-ms', 'data-status': r.k.status, 'data-sev': worst, 'aria-label': 'Milestone: ' + label, 'data-tooltip': label, ...planAttrs(r.k), onclick: open(r.k) }, h('span', {}));
        m.style.left = bar.x + bar.width / 2 - 8 + 'px'; m.style.top = i * TL.ROW_HEIGHT + TL.ROW_HEIGHT / 2 - 8 + 'px';
        const t = h('span', { class: 'tl-mt' }, r.k.title); t.style.left = bar.x + bar.width / 2 + 14 + 'px'; t.style.top = i * TL.ROW_HEIGHT + 9 + 'px';
        plot.append(m, t);
      } else {
        const rb = h('button', { type: 'button', class: 'tl-bar', 'data-status': r.k.status, 'data-sev': worst, 'data-tooltip': label, ...planAttrs(r.k), onclick: open(r.k) }, h('span', { 'data-plan-edge': 'start', 'aria-hidden': 'true' }), h('span', { class: 'bt' }, r.k.title), h('span', { 'data-plan-edge': 'end', 'aria-hidden': 'true' }));
        rb.style.left = bar.x + 'px'; rb.style.width = Math.max(bar.width - 2, 6) + 'px'; rb.style.top = i * TL.ROW_HEIGHT + 6 + 'px'; rb.style.height = TL.ROW_HEIGHT - 12 + 'px';
        plot.append(rb);
      }
    });
    const svg = svgEl('svg', { class: 'tl-links', width, height: rows.length * TL.ROW_HEIGHT, 'aria-hidden': 'true' });
    const defs = svgEl('defs'), marker = svgEl('marker', { id: 'tl-head', viewBox: '0 0 8 8', refX: 7, refY: 4, markerWidth: 7, markerHeight: 7, orient: 'auto' });
    marker.append(svgEl('path', { d: 'M0 0 L8 4 L0 8 z', fill: 'currentColor' })); defs.append(marker); svg.append(defs);
    for (const l of links) svg.append(svgEl('path', { d: l.d, class: 'tl-link' + (gates(l) ? ' gates' : '') + (l.conflict ? ' clash' : ''), 'marker-end': 'url(#tl-head)' }));
    plot.append(svg);
    chart = h('div', { class: 'tl-chart' }, h('div', { class: 'tl-axis-row' }, h('div', { class: 'tl-corner' }, 'Work'), axis), h('div', { class: 'tl-body' }, names, plot));
  }

  const unplanned = undated.length ? h('section', { class: 'tl-undated', 'aria-label': 'Not planned yet' }, h('h3', {}, 'Not planned yet ', h('span', { class: 'badge' }, String(undated.length))),
    undated.map((k) => h('div', { class: 'row' }, h('div', { class: 'grow' }, h('button', { type: 'button', class: 'link', onclick: open(k) }, k.key + ' · ' + k.title)),
      h('span', { class: 'lbls' }, labelsOf(k, b.labels).map(labelChip))))) : '';

  const gating = links.some(gates) ? h('p', { class: 'muted small tl-legend' }, 'Heavier arrows gate an agent start: the ticket has a shared request, and it waits for the work the arrow comes from.') : '';
  return h('div', { class: 'timeline-view', 'data-plan-surface': '' }, tools, warnPanel, h('div', { class: 'tl-scroller', 'data-scroll-key': 'timeline', 'data-today-x': todayX, 'data-plan-grid': '', 'data-plan-px': px, onwheel: e => { if (!e.ctrlKey && !e.metaKey) return; e.preventDefault(); const scales = ['day', 'week', 'month', 'quarter']; state.tlZoom = scales[Math.max(0, Math.min(3, scales.indexOf(state.tlZoom) + (e.deltaY > 0 ? 1 : -1)))]; render(); } }, chart), gating, unplanned);
}

// Read-only snapshots: live board updates do not repeat aggregation queries.
const analyticsState = { view: 'Activity', scopes: Object.fromEntries(['Activity', 'Categories', 'Completion'].map(view => [view, { project: '', days: 30, appliedProject: '', appliedDays: 30 }])), report: null, key: '', visibility: '', series: 'both', metric: 'completed', category: '' };
async function analyticsView() {
  const visibility = state.me.member.role + '/' + state.ov.projects.map(x => x.project.id + ':' + x.role + ':' + x.member).join(',');
  if (analyticsState.visibility && analyticsState.visibility !== visibility) {
    for (const scope of Object.values(analyticsState.scopes)) { scope.project = ''; scope.appliedProject = ''; }
    analyticsState.report = null;
    analyticsState.category = '';
  }
  analyticsState.visibility = visibility;
  const scope = analyticsState.scopes[analyticsState.view];
  const scopedKey = scope.appliedProject + '/' + scope.appliedDays + '/' + visibility;
  if (!analyticsState.report || analyticsState.key !== scopedKey) {
    analyticsState.report = await api('GET', '/analytics?' + new URLSearchParams({ project: scope.appliedProject, days: String(scope.appliedDays) }));
    analyticsState.key = scopedKey;
  }
  const r = analyticsState.report;
  const chooseView = view => { analyticsState.view = view; return render(); };
  const tabs = h('div', { class: 'analytics-tabs', role: 'tablist', 'aria-label': 'Analytics views' }, ['Activity', 'Categories', 'Completion'].map((view, i, views) => h('button', {
    id: 'analytics-tab-' + view, type: 'button', role: 'tab', 'aria-selected': String(analyticsState.view === view), 'aria-controls': 'analytics-panel', tabindex: analyticsState.view === view ? '0' : '-1',
    onclick: () => chooseView(view), onkeydown: e => {
      if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return;
      e.preventDefault();
      const next = views[e.key === 'Home' ? 0 : e.key === 'End' ? views.length - 1 : (i + (e.key === 'ArrowRight' ? 1 : -1) + views.length) % views.length];
      chooseView(next).then(() => document.getElementById('analytics-tab-' + next)?.focus());
    },
  }, view)));
  const project = h('select', { 'aria-label': 'Analytics project', onchange: e => { scope.project = scope.appliedProject = e.target.value; analyticsState.report = null; render(); } },
    h('option', { value: '', selected: !scope.project }, 'All projects'), (r.projects || []).map(x => h('option', { value: x.id, selected: scope.project === x.id }, x.name)));
  const range = h('select', { 'aria-label': 'Analytics time range', onchange: e => { scope.days = scope.appliedDays = Number(e.target.value); analyticsState.report = null; render(); } },
    [7, 30, 90].map(n => h('option', { value: String(n), selected: scope.days === n }, 'Last ' + n + ' days')));
  const select = (label, value, choices, change) => h('label', {}, label + ' ', h('select', { 'aria-label': label, onchange: e => { change(e.target.value); render(); } }, choices.map(([id, name]) => h('option', { value: id, selected: id === value }, name))));
  const duration = seconds => seconds == null ? 'No samples' : seconds < 60 ? Math.round(seconds) + 's' : seconds < 3600 ? (seconds / 60).toFixed(1) + 'm' : seconds < 86400 ? (seconds / 3600).toFixed(1) + 'h' : (seconds / 86400).toFixed(1) + 'd';
  const tones = ['var(--accent)', 'var(--ok)', 'var(--block)', 'var(--warn)'];
  const svgChart = (label, shapes, box = '0 0 480 180') => {
    const ns = 'http://www.w3.org/2000/svg', svg = document.createElementNS(ns, 'svg');
    for (const [key, value] of Object.entries({ viewBox: box, role: 'img', 'aria-label': label, class: 'analytics-line' })) svg.setAttribute(key, value);
    for (const [tag, attributes] of shapes) {
      const el = document.createElementNS(ns, tag);
      for (const [key, value] of Object.entries(attributes)) el.setAttribute(key, String(value));
      svg.append(el);
    }
    return svg;
  };
  const linePoints = (values, width = 456, height = 148) => {
    const peak = Math.max(1, ...values);
    return values.map((value, i) => (12 + i / Math.max(1, values.length - 1) * width) + ',' + (160 - value / peak * height)).join(' ');
  };
  const stats = rows => h('dl', { class: 'analytics-statistics' }, rows.map(([label, value], i) => {
    const tile = h('div', {}, h('dt', {}, label), h('dd', {}, String(value)));
    tile.style.setProperty('--tone', tones[i % tones.length]);
    if (analyticsState.view === 'Activity' && i < 2 && r.days.length) tile.append(svgChart(label + ' per day', [['polyline', { points: linePoints(r.days.map(day => i ? day.completed : day.created)), fill: 'none', stroke: tones[i], 'stroke-width': 3, 'vector-effect': 'non-scaling-stroke' }]]));
    return tile;
  }));
  const panel = (title, caption, tone, span, body) => {
    const el = h('section', { class: 'analytics-panel' }, h('header', {}, h('span', { class: 'analytics-swatch', 'aria-hidden': 'true' }), h('div', {}, h('h2', {}, title), h('p', {}, caption))), h('div', { class: 'analytics-panel-body' }, body));
    el.style.setProperty('--tone', tone); el.style.setProperty('--span', span);
    return el;
  };
  const table = (head, rows) => h('div', { class: 'scroll', role: 'region', 'aria-label': head.join(', '), tabindex: '0' }, h('table', {}, h('thead', {}, h('tr', {}, head.map(label => h('th', {}, label)))), h('tbody', {}, rows.map(row => h('tr', {}, row.map(cell => h('td', {}, String(cell))))))));
  const comparison = rows => {
    const max = Math.max(1, ...rows.map(row => row[1]));
    return h('div', { class: 'analytics-comparison' }, rows.map(([label, value, detail, tone]) => {
      const fill = h('span', { 'data-chart-key': 'comparison:' + label, 'data-chart-value': value / max, 'data-chart-scope': scopedKey + '/' + analyticsState.view + '/' + analyticsState.metric }); fill.style.width = value / max * 100 + '%'; fill.style.background = tone || 'var(--accent)';
      return h('div', {}, h('div', { class: 'analytics-caption' }, h('span', {}, label), h('strong', {}, String(value))), h('div', { class: 'analytics-track', 'aria-hidden': 'true' }, fill), detail ? h('p', { class: 'muted small' }, detail) : '');
    }));
  };
  let body, extras = '';
  if (analyticsState.view === 'Activity') {
    extras = select('Activity chart series', analyticsState.series, [['both', 'Created and completed'], ['created', 'Created'], ['completed', 'Completed']], value => analyticsState.series = value);
    const max = Math.max(1, ...r.days.flatMap(d => analyticsState.series === 'both' ? [d.created, d.completed] : [d[analyticsState.series]]));
    const chart = h('div', { class: 'analytics-chart', role: 'img', 'aria-label': 'Daily ticket counts. Hover a day to read it.' }, r.days.map((d, i) => {
      const created = h('span', { 'data-chart-key': d.date + ':created', 'data-chart-value': d.created / max, 'data-chart-axis': 'y', 'data-chart-scope': scopedKey + '/' + analyticsState.series, class: 'analytics-created', hidden: analyticsState.series === 'completed' }); created.style.height = d.created / max * 100 + '%';
      const done = h('span', { 'data-chart-key': d.date + ':done', 'data-chart-value': d.completed / max, 'data-chart-axis': 'y', 'data-chart-scope': scopedKey + '/' + analyticsState.series, class: 'analytics-done', hidden: analyticsState.series === 'created' }); done.style.height = d.completed / max * 100 + '%';
      return h('div', { class: 'analytics-day', title: d.date + ': ' + d.created + ' created, ' + d.completed + ' completed' }, h('div', { class: 'analytics-bars' }, created, done), h('span', {}, i % Math.ceil(r.days.length / 6) === 0 || i === r.days.length - 1 ? d.date.slice(5) : ''));
    }));
    let created = 0, completed = 0;
    const totals = r.days.map(day => ({ created: created += day.created, completed: completed += day.completed }));
    const peak = Math.max(1, ...totals.flatMap(day => [day.created, day.completed]));
    const cumulativePoints = key => totals.map((day, i) => (12 + i / Math.max(1, totals.length - 1) * 456) + ',' + (160 - day[key] / peak * 148)).join(' ');
    const lines = ['created', 'completed'].filter(key => analyticsState.series === 'both' || analyticsState.series === key).map(key => ['polyline', { points: cumulativePoints(key), fill: 'none', stroke: key === 'created' ? 'var(--accent)' : 'var(--ok)', 'stroke-width': 2.5, 'vector-effect': 'non-scaling-stroke' }]);
    const ring = h('div', { class: 'analytics-ring', role: 'img', 'aria-label': r.created ? Math.round(r.completed / r.created * 100) + '% Done against created' : 'No tickets created' }, h('div', {}, h('strong', {}, r.created ? Math.round(r.completed / r.created * 100) + '%' : '—'), h('span', {}, 'Done / created')));
    ring.style.setProperty('--share', Math.min(1, r.created ? r.completed / r.created : 0));
    const heatPeak = Math.max(1, ...r.days.map(day => day.created + day.completed));
    const firstWeekday = r.days.length ? (new Date(r.days[0].date + 'T00:00:00Z').getUTCDay() + 6) % 7 : 0;
    const heat = h('div', { class: 'analytics-heat', role: 'group', 'aria-label': 'Activity for each UTC day' },
      h('div', { class: 'analytics-heat-head', 'aria-hidden': 'true' }, ['Mon','Tue','Wed','Thu','Fri','Sat','Sun'].map(day => h('span', {}, day))),
      h('div', { class: 'analytics-heat-grid' }, r.days.map((day, i) => {
        const count = day.created + day.completed;
        const cell = h('span', { title: day.date + ': ' + count + ' ticket events', 'aria-label': day.date + ': ' + count + ' ticket events' }, day.date.slice(8));
        cell.style.gridColumn = String((i + firstWeekday) % 7 + 1);
        cell.style.setProperty('--intensity', count ? Math.round(12 + count / heatPeak * 28) + '%' : '0%');
        return cell;
      })));
    const legend = h('p', { class: 'analytics-legend muted small' }, h('span', {}, h('span', { class: 'analytics-key analytics-created' }), ' Created'), h('span', {}, h('span', { class: 'analytics-key analytics-done' }), ' Done'));
    body = [stats([['Tickets created', r.created], ['Moved to Done', r.completed], ['Median ticket duration', duration(r.medianTicketSeconds)], ['Duration samples', r.durationSamples]]),
      h('p', { class: 'muted small' }, 'Duration includes waiting, from creation to first completion. Average: ' + duration(r.averageTicketSeconds) + '.'),
      r.days.some(day => day.created || day.completed) ? h('div', { class: 'analytics-board' },
        panel('Created and completed over time', 'Tickets per day, by UTC day', 'var(--accent)', 7, [legend, h('div', { class: 'analytics-daily-scroll' }, chart)]),
        panel('Progress so far', 'Running totals within this period', 'var(--ok)', 5, [svgChart('Cumulative tickets created and moved to Done', lines), h('p', { class: 'muted small' }, r.created + ' created · ' + r.completed + ' moved to Done')]),
        panel('Done against created', 'Tickets moved to Done for each one created', 'var(--ok)', 3, [ring, h('p', { class: 'muted small analytics-tail' }, r.completed + ' done · ' + r.created + ' created' + (r.completed > r.created ? '. Some were created before this period.' : '.'))]),
        panel('Busiest days', 'Created plus moved to Done, a square for each day', 'var(--block)', 5, heat),
        panel('Work categories', 'Current labels; a ticket with several appears in each', 'var(--warn)', 4, r.categories.length ? comparison(r.categories.map(c => [c.name, c.completed, c.created + ' created · ' + c.completed + ' completed', 'var(--warn)'])) : h('p', { class: 'muted' }, 'No ticket activity matches these categories.')))
        : h('p', { class: 'empty-state muted' }, 'No ticket activity in this period. Try a longer time range or another project.')];
  } else if (analyticsState.view === 'Categories') {
    const categoryId = analyticsState.category === '__unlabelled' ? '' : analyticsState.category;
    extras = [select('Category measure', analyticsState.metric, [['completed', 'Completed'], ['created', 'Created']], value => analyticsState.metric = value),
      select('Work category', analyticsState.category, [['', 'All categories'], ...r.categories.map(c => [c.labelId || '__unlabelled', c.name]), ...(analyticsState.category && !r.categories.some(c => c.labelId === categoryId) ? [[analyticsState.category, 'Selected category (not in this snapshot)']] : [])], value => analyticsState.category = value)];
    const categories = r.categories.filter(c => !analyticsState.category || c.labelId === categoryId).sort((a, b) => b[analyticsState.metric] - a[analyticsState.metric]);
    body = [h('div', { class: 'analytics-board' }, panel('Work by category', 'Current labels; category totals are not unique ticket counts', 'var(--warn)', 12,
      categories.length ? comparison(categories.map(c => [c.name, c[analyticsState.metric], c.created + ' created · ' + c.completed + ' completed', analyticsState.metric === 'completed' ? 'var(--ok)' : 'var(--accent)']))
        : h('p', { class: 'muted' }, 'No ticket activity matches this category. Try another category, project or time range.'))),
      h('details', {}, h('summary', {}, 'Category values'), table(['Current label', 'Created', 'Completed'], categories.map(c => [c.name, c.created, c.completed])))];
  } else {
    body = [stats([['Completed tickets', r.completed], ['Human completion decisions', r.humanCompletionDecisions], ['Median duration', duration(r.medianTicketSeconds)], ['Duration samples', r.durationSamples]]),
      h('div', { class: 'analytics-board' },
        panel('Recorded completion signals', 'Separate coordination signals, rather than steps of a funnel', 'var(--ok)', 7, comparison([['Completed tickets', r.completed, 'First recorded completion in the selected period', 'var(--ok)'], ['Human completion decisions', r.humanCompletionDecisions, 'Reopened tickets count once', 'var(--accent)']])),
        panel('Completion duration', 'From creation to first completion, including waiting', 'var(--block)', 5, h('dl', { class: 'analytics-facts' }, [['Median', duration(r.medianTicketSeconds)], ['Average', duration(r.averageTicketSeconds)], ['Samples', r.durationSamples]].map(([name, value]) => h('div', {}, h('dt', {}, name), h('dd', {}, String(value))))))),
      h('details', { class: 'analytics-disclosure' }, h('summary', {}, 'Coordination details'), h('p', {}, 'Team records coordination only. Execution history, agent usage and verified local merges live in Individual analytics.'))];
  }
  return h('div', { class: 'coordination-analytics' }, tabs,
    h('div', { id: 'analytics-panel', role: 'tabpanel', 'aria-labelledby': 'analytics-tab-' + analyticsState.view, tabindex: '0' },
      h('div', { class: 'analytics-filters' }, h('label', {}, 'Project ', project), h('label', {}, 'Time range ', range), extras),
      h('p', { class: 'muted small' }, 'As of ' + new Date(r.end).toLocaleString() + ' · ' + ((r.projects || []).find(p => p.id === scope.appliedProject)?.name || 'All projects you can read') + ' · ' + scope.appliedDays + ' days.'),
      r.truncated ? h('p', { class: 'warning', role: 'status' }, 'Partial history: the bounded read omitted records or projects. Counts may be lower than the full history.') : '', body));
}

const timelineTooltip = h('div', { class: 'timeline-tooltip', role: 'tooltip', hidden: true });
document.body.append(timelineTooltip);
function showTimelineTooltip(event) {
 const target = event.target.closest?.('[data-tooltip]');
 if (!target) { timelineTooltip.hidden = true; return; }
 const rect = target.getBoundingClientRect();
 timelineTooltip.textContent = target.dataset.tooltip;
 timelineTooltip.style.left = Math.max(12, Math.min(rect.left, innerWidth - 332)) + 'px';
 timelineTooltip.style.top = Math.min(rect.bottom + 6, innerHeight - 80) + 'px';
 timelineTooltip.hidden = false;
}
document.addEventListener('pointerover', showTimelineTooltip);
document.addEventListener('focusin', showTimelineTooltip);
document.addEventListener('pointerout', () => timelineTooltip.hidden = true);
document.addEventListener('focusout', () => timelineTooltip.hidden = true);

function dropRefusal(d, k, to) {
  if (d.board.project.archived) return 'Unarchive this project before moving tickets.';
  if (to === 'done' && k.status === 'review') return d.board.completion[k.id] || 'You need review permission to complete this ticket.';
  if (to === 'review') return k.status !== 'in_progress' ? 'Claim this ticket first. When its agent finishes, it moves to Review by itself.' : 'Only its owner or a project owner can submit this ticket for review.';
  if (to === 'in_progress') return !d.board.member ? 'Join this project before claiming a ticket.' : k.status === 'available' ? 'You need permission to claim tickets on this project.' : k.status === 'review' ? 'Only a reviewer can send a ticket in Review back for changes.' : 'Reopen this ticket first.';
  if (to === 'available' && k.status === 'in_progress') return 'Only its owner or a project owner can release it.';
  return 'This move needs the ticket’s normal workflow action or your project owner’s permission. Open the ticket to see the available actions.';
}
function performBoardDrop(d, k, to) {
  if (actionBusy || pendingTickets.has(k.id) || k.status === to) return;
  const action = dropAction(d, k, to);
  if (!action) { toast('Kept in ' + statusName(k.status) + '. ' + dropRefusal(d, k, to)); return; }
  pendingTickets.add(k.id);
  act(async () => {
    try { await api('POST', projectPath(k) + '/' + action[0], action[1]); }
    catch (error) { toast('Move could not be completed. ' + error.message); throw error; }
    finally { pendingTickets.delete(k.id); }
  });
}
document.addEventListener('werkbord-board-drop', event => {
  const { id, state: to } = event.detail, d = state.data;
  const k = d?.board?.tickets.find(k => k.id === id);
  if (k) performBoardDrop(d, k, to);
});

function centerTimeline() {
 const scroller = app.querySelector('.tl-scroller');
 if (!scroller) return;
 const namesWidth = scroller.querySelector('.tl-names')?.clientWidth || 280;
 scroller.scrollLeft = Math.max(0, Number(scroller.dataset.todayX) - Math.max(0, scroller.clientWidth - namesWidth) / 2);
}

// Calendar gestures use the same versioned plan and schedule operations as the editor.
function planAttrs(k, schedule) {
  const span = schedule ? { start: TL.localToday(new Date(schedule.at).getTime()), end: TL.localToday(new Date(schedule.at).getTime()) } : TL.spanOf(k);
  return span ? { 'data-plan-item': k.id + (schedule ? '/schedule' : ''), 'data-plan-start': span.start, 'data-plan-end': span.end, 'data-plan-scheduled': schedule ? '' : null, 'aria-busy': pendingTickets.has(k.id) } : {};
}
function shiftedStart(at, day) {
  const date = new Date(at), [year, month, dom] = day.split('-').map(Number);
  date.setFullYear(year, month - 1, dom);
  return date.toISOString();
}
document.addEventListener('werkbord-plan-drop', event => {
  const drop = event.detail, d = state.data;
  const k = d?.board?.tickets.find(k => k.id === drop.id.replace(/\/schedule$/, ''));
  if (!k || actionBusy || pendingTickets.has(k.id)) return;
  pendingTickets.add(k.id);
  act(async () => {
    try {
      if (drop.id.endsWith('/schedule')) {
        const schedule = (d.schedules || []).find(s => s.ticketId === k.id && !['completed', 'canceled'].includes(s.state));
        if (!schedule) throw new Error('This schedule is no longer available. Refresh the calendar.');
        await api('PUT', projectPath(k) + '/schedule', { version: schedule.version, at: shiftedStart(schedule.at, drop.start), timezone: schedule.timezone,
          missedPolicy: schedule.missedPolicy, graceSeconds: schedule.graceSeconds, order: schedule.order, priority: schedule.priority });
      } else await api('PATCH', projectPath(k), { version: k.version, plan: { ...k.plan, start: drop.start, end: drop.end } });
      toast('Dates saved for ' + k.key + '.');
    } catch (error) { toast('Dates could not be changed. ' + error.message); throw error; }
    finally { pendingTickets.delete(k.id); }
  });
});

// Universal stays on its own calendar and asks this authenticated workspace to read/save a specific ticket.
window.addEventListener('message', async event => {
  const m = event.data;
  if (!FRAMED || event.source !== window.parent || !m || m.type !== 'werkbord.planning.request' || !/^[a-z0-9_-]{1,64}$/.test(m.id || '') ||
    !['read', 'save', 'move', 'reschedule'].includes(m.operation) || typeof m.href !== 'string' || !/^\?[A-Za-z0-9_=&%-]+$/.test(m.href) || JSON.stringify(m).length > 8192) return;
  try {
    const query = new URLSearchParams(m.href.slice(1)), project = query.get('project'), id = query.get('ticket');
    if (!/^[A-Za-z0-9_-]+$/.test(project || '') || !/^[A-Za-z0-9_-]+$/.test(id || '')) throw new Error('This ticket address is invalid.');
    const base = '/projects/' + project, path = base + '/tickets/' + id;
    const [board, ticket, requests] = await Promise.all([api('GET', base + '/board'), api('GET', path), api('GET', base + '/schedules')]);
    const actor = state.me || await api('GET', '/me');
    const schedule = requests.find(s => s.ticketId === id && !['completed', 'canceled'].includes(s.state));
    let current = ticket, currentSchedule = schedule;
    const payload = m.payload || {};
    if (m.operation !== 'read') {
      if (payload.version !== ticket.version) throw new Error('This ticket changed elsewhere. Reopen it before saving.');
      if (m.operation === 'reschedule') {
        if (!schedule || payload.scheduleVersion !== schedule.version) throw new Error('This schedule changed elsewhere. Refresh before rescheduling.');
        currentSchedule = await api('PUT', path + '/schedule', { version: schedule.version, at: payload.at || shiftedStart(schedule.at, payload.start), timezone: schedule.timezone,
          missedPolicy: schedule.missedPolicy, graceSeconds: schedule.graceSeconds, order: schedule.order, priority: schedule.priority });
      } else {
        const plan = { ...ticket.plan, start: payload.start, end: payload.end, milestone: payload.milestone ?? ticket.plan?.milestone ?? false };
        current = await api('PATCH', path, { version: ticket.version, plan, ...(m.operation === 'save' ? { title: payload.title, description: payload.description } : {}) });
      }
    }
    const span = TL.spanOf(current);
    event.source.postMessage({ type: 'werkbord.planning.result', id: m.id, ok: true, item: { title: current.title, description: current.description, version: current.version,
      start: span?.start || '', end: span?.end || '', milestone: !!span?.milestone, scheduledAt: currentSchedule?.at || '', scheduleVersion: currentSchedule?.version,
      editable: !current.archivedAt && current.status !== 'done' && (current.creatorId === actor.member.id || board.can.includes('tickets.edit')),
      canReschedule: !current.archivedAt && ['in_progress', 'review'].includes(current.status) && (current.assigneeId === actor.member.id || board.can.includes('tickets.assign')) } }, event.origin);
  } catch (error) { event.source.postMessage({ type: 'werkbord.planning.result', id: m.id, ok: false, error: error.message }, event.origin); }
});

// Initiatives coordinate references; project boards remain the place to work.
let initiativeEditor;
let initiativeEditorIdentity;
async function initiativesView() {
  const [rows, initiativePeople] = await Promise.all([api('GET', '/initiatives'), api('GET', '/members')]);
  const ownerNames = new Map(initiativePeople.map(m => [m.id, m.name]));
  const identity = state.me.workspace.id + ':' + state.me.member.id;
  if (initiativeEditorIdentity !== identity) { initiativeEditor = h('div'); initiativeEditorIdentity = identity; }
  const editor = initiativeEditor;
  if (editor.dataset.initiative && !rows.some(v => v.initiative.id === editor.dataset.initiative)) editor.replaceChildren();
  const edit = async (original) => {
    const i = original || { title: '', objective: '', status: 'active', projectIds: [], tickets: [], dependencies: [], memories: [], version: 0 };
    const title = h('input', { name: 'initiative-title', value: i.title, required: true, maxlength: 200 });
    const objective = h('textarea', { name: 'initiative-objective', required: true, maxlength: 20000 }, i.objective);
    const status = h('select', { name: 'initiative-status', 'aria-label': 'Status' }, ['planned', 'active', 'paused', 'completed', 'archived'].map(s => h('option', { value: s, selected: s === i.status }, s)));
    const people = await api('GET', '/members');
    const owner = h('select', { name: 'initiative-owner', 'aria-label': 'Owner' }, people.map(m => h('option', { value: m.id, selected: m.id === (i.ownerId || state.me.member.id) }, m.name)));
    const projects = new Set(i.projectIds), tickets = new Map(i.tickets.map(t => [t.ticketId, t]));
    const choices = h('div', { class: 'stack' });
    const dependencies = [...i.dependencies];
    const memories = new Map(i.memories.map(m => [m.memoryId, m]));
    const memoryChoices = h('div', { class: 'stack' });
    const dependencyList = h('div');
    const from = h('select', { 'aria-label': 'Prerequisite ticket' });
    const to = h('select', { 'aria-label': 'Dependent ticket' });
    const names = new Map();
    const showDependencies = () => {
      dependencyList.replaceChildren(...dependencies.map((d, n) => h('div', { class: 'row' }, h('span', { class: 'grow' }, (names.get(d.after.ticketId) || d.after.ticketId) + ' waits for ' + (names.get(d.before.ticketId) || d.before.ticketId)), h('button', { type: 'button', class: 'plain danger small', onclick: () => { dependencies.splice(n, 1); showDependencies(); } }, 'Remove dependency'))));
    };
    const available = await Promise.all(state.ov.projects.filter(p => !p.project.archived).map(async ({ project: p }) => ({ p, board: await api('GET', '/projects/' + p.id + '/board'), memory: await api('GET', '/projects/' + p.id + '/memory') })));
    for (const { p, board, memory } of available) {
      const selected = h('input', { type: 'checkbox', checked: projects.has(p.id), onchange: e => { if (e.target.checked) projects.add(p.id); else projects.delete(p.id); } });
      choices.append(field(p.name, selected));
      for (const m of memory.records.filter(m => m.state === 'published')) {
        memoryChoices.append(field(p.name + ' · ' + m.content.summary, h('input', { type: 'checkbox', checked: memories.has(m.id), onchange: e => { if (e.target.checked) { memories.set(m.id, { projectId: p.id, memoryId: m.id }); projects.add(p.id); selected.checked = true; } else memories.delete(m.id); } })));
      }
      for (const k of board.tickets) {
        names.set(k.id, k.key + ' · ' + k.title);
        const ref = { projectId: p.id, ticketId: k.id };
        choices.append(field(k.key + ' · ' + k.title, h('input', { type: 'checkbox', checked: tickets.has(k.id), onchange: e => { if (e.target.checked) { tickets.set(k.id, ref); projects.add(p.id); selected.checked = true; } else tickets.delete(k.id); } })));
        for (const control of [from, to]) control.append(h('option', { value: k.id }, k.key + ' · ' + p.name));
      }
    }
    showDependencies();
    editor.dataset.initiative = i.id || '';
    editor.replaceChildren(h('form', { class: 'panel stack', onsubmit: e => { e.preventDefault(); act(async () => {
      const body = { title: title.value, objective: objective.value, status: status.value, ownerId: owner.value, projectIds: [...projects], tickets: [...tickets.values()], dependencies, memories: [...memories.values()], version: i.version };
      await api(original ? 'PUT' : 'POST', '/initiatives' + (original ? '/' + i.id : ''), body);
      editor.replaceChildren();
      state.info = 'Initiative saved.';
    }); } }, h('h2', {}, original ? 'Edit Initiative' : 'New Initiative'), field('Title', title), field('Objective', objective), field('Status', status), field('Owner', owner), h('h3', {}, 'Link existing projects and tickets'), choices, h('h3', {}, 'Shared context and decisions'), memoryChoices,
    h('h3', {}, 'Dependencies'), h('p', { class: 'muted' }, 'Link both tickets above before adding a dependency.'), from, to,
    h('button', { type: 'button', class: 'plain add', onclick: () => { const before = tickets.get(from.value), after = tickets.get(to.value); if (before && after) { dependencies.push({ before, after }); showDependencies(); } } }, 'Add dependency'), dependencyList,
    h('div', { class: 'actions' }, h('button', { class: 'primary', type: 'submit' }, 'Save Initiative'), h('button', { class: 'plain', type: 'button', onclick: () => editor.replaceChildren() }, 'Cancel'))));
    title.focus();
  };
  return h('div', { class: 'stack' }, h('div', { class: 'section-head' }, h('p', { class: 'muted' }, 'Shared objectives across existing projects. Progress reflects stored ticket states and last-reported PR states.'), h('button', { class: 'primary', onclick: () => edit().catch(e => { state.error = e.message; render(); }) }, 'New Initiative')), editor,
    rows.length ? rows.map(v => h('section', { class: 'panel stack' }, h('div', { class: 'section-head' }, h('h2', {}, v.initiative.title), h('span', { class: 'badge' }, v.initiative.status)), h('p', {}, v.initiative.objective), h('p', { class: 'muted small' }, 'Owner · ' + (ownerNames.get(v.initiative.ownerId) || 'Former member')),
      h('div', { class: 'actions' }, v.projects.map(p => h('button', { class: 'link', onclick: () => openProject(p.id) }, p.name))),
      h('p', {}, v.done + ' of ' + v.tickets.length + ' tickets done · ' + plural(v.openPRs, 'reported open PR')),
      v.tickets.map(k => h('div', { class: 'row' }, h('button', { class: 'link grow', onclick: () => openTicket(k.projectId, k.id) }, k.key + ' · ' + k.title), h('span', { class: 'badge' }, statusName(k.status)), k.pullRequest ? extLink(k.pullRequest.url, 'PR · ' + k.pullRequest.state) : '', when(k.updatedAt))),
      h('h3', {}, 'Blockers'), v.blockers.length ? h('ul', {}, v.blockers.map(b => h('li', {}, b))) : h('p', { class: 'muted' }, 'No blockers identified in current reports.'),
      h('h3', {}, 'Next actions'), h('ul', {}, v.nextActions.map(a => h('li', {}, a))),
      v.memories.length ? h('div', {}, h('h3', {}, 'Shared context and decisions'), v.memories.map(m => h('p', {}, m.content.summary + ' · ' + (m.freshness?.status || 'uncertain')))) : '',
      h('details', {}, h('summary', {}, 'Activity and changes'), v.activity.map(a => h('p', {}, a.kind + ' · ' + a.ticketKey + ' · ', when(a.createdAt))), v.initiative.history.map(a => h('p', {}, 'Initiative ' + a.kind + ' · ', when(a.at)))),
      h('p', { class: 'muted small' }, 'Updated ', when(v.initiative.updatedAt)),
      v.initiative.ownerId === state.me.member.id || can('projects.manage') ? h('button', { class: 'plain', onclick: () => edit(v.initiative).catch(e => { state.error = e.message; render(); }) }, 'Edit Initiative') : '')) : h('p', { class: 'muted' }, 'No Initiatives to show. Create an Initiative to link work around a shared objective.'));
}
