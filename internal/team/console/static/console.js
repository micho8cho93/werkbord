// The Team console. Plain JavaScript, no build step. Every piece of text from the
// server goes in with textContent, never as HTML.
'use strict';

const app = document.getElementById('app');
const state = { token: null, me: null, tab: 'projects', projectId: null, secret: null, error: '' };

try {
  const m = /(?:^|[#&])token=([^&]+)/.exec(location.hash);
  if (m) {
    sessionStorage.setItem('werkbord-team-token', decodeURIComponent(m[1]));
    history.replaceState(null, '', location.pathname); // the token never stays in the address bar
  }
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

async function api(method, path, body) {
  const res = await fetch('/api/team/v1' + path, {
    method,
    headers: { Authorization: 'Bearer ' + state.token, ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({}));
  if (res.status === 401) { signOut(); throw new Error('Your token is not valid any more. Sign in again.'); }
  if (!res.ok) throw new Error(data.error ? data.error.message : 'Request failed (' + res.status + ')');
  return data;
}

function can(permission) { return state.me && state.me.permissions.includes(permission); }

function signOut() {
  try { sessionStorage.removeItem('werkbord-team-token'); } catch (_) {}
  state.token = null; state.me = null; state.secret = null; render();
}

async function act(fn) {
  state.error = '';
  try { await fn(); } catch (e) { state.error = e.message; }
  await render();
}

async function render() {
  if (!state.token) { app.replaceChildren(signIn()); return; }
  try { state.me = await api('GET', '/me'); } catch (e) { if (state.token) state.error = e.message; render_(signIn()); return; }
  let body;
  try { body = state.tab === 'members' ? await membersView() : await projectsView(); }
  catch (e) { state.error = e.message; body = h('p', { class: 'error' }, e.message); }
  render_(h('div', {},
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
    secretBox(),
    body));
}

function render_(node) { app.replaceChildren(node); }

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
  const link = location.origin + '/#token=' + s.token;
  return h('div', { class: 'secret', role: 'status' },
    h('strong', {}, 'Token for ' + s.name),
    h('p', { class: 'muted' }, 'Shown once; it is not stored and cannot be shown again. Send it to them privately.'),
    h('code', {}, s.token),
    h('span', { class: 'muted' }, 'Or a link that signs them in: '),
    h('code', {}, link),
    h('button', { class: 'plain', onclick: () => { state.secret = null; render(); } }, 'Done'));
}

// ---- members ----

async function membersView() {
  const members = await api('GET', '/members');
  const rows = members.map((m) => h('div', { class: 'row' },
    h('div', { class: 'grow' }, h('strong', {}, m.name), ' ', h('span', { class: 'muted' }, m.email || '')),
    h('span', { class: 'badge' }, m.role),
    (m.id === state.me.member.id || can('members.manage'))
      ? h('button', { class: 'plain', onclick: () => act(async () => { const r = await api('POST', '/members/' + m.id + '/token'); state.secret = { name: r.member.name, token: r.token }; }) }, 'New token') : '',
    (can('members.manage') && m.role !== 'owner')
      ? h('button', { class: 'danger', onclick: () => confirm('Remove ' + m.name + ' from the workspace?') && act(() => api('DELETE', '/members/' + m.id)) }, 'Remove') : ''));
  const panels = [h('div', { class: 'panel' }, h('h2', {}, 'Members (' + members.length + ')'), rows)];
  if (can('members.manage')) {
    const name = h('input', { required: true, maxlength: 80 });
    const email = h('input', { type: 'email', maxlength: 254 });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'Add a member'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { const r = await api('POST', '/members', { name: name.value, email: email.value });
          state.secret = { name: r.member.name, token: r.token }; }); } },
        h('label', {}, 'Name', name), h('label', {}, 'Email (optional)', email), h('button', { class: 'primary' }, 'Add'))));
  }
  return h('div', {}, panels);
}

// ---- projects ----

async function projectsView() {
  if (state.projectId) return projectView(state.projectId);
  const projects = await api('GET', '/projects');
  const rows = projects.map((p) => h('div', { class: 'row' },
    h('div', { class: 'grow' }, h('button', { class: 'link', onclick: () => { state.projectId = p.id; state.secret = null; render(); } }, p.name),
      p.repository ? h('div', { class: 'muted' }, p.repository) : ''),
    p.archived ? h('span', { class: 'badge' }, 'archived') : ''));
  const panels = [h('div', { class: 'panel' }, h('h2', {}, 'Projects (' + projects.length + ')'),
    rows.length ? rows : h('p', { class: 'muted' }, can('projects.view_all') ? 'No projects yet.' : 'You are not on any project yet.'))];
  if (can('projects.create')) {
    const name = h('input', { required: true, maxlength: 80 });
    const repo = h('input', { placeholder: 'https://github.com/owner/repo (optional)', maxlength: 2048 });
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'New project'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(async () => { await api('POST', '/projects', { name: name.value, repository: repo.value }); }); } },
        h('label', {}, 'Name', name), h('label', {}, 'Repository', repo), h('button', { class: 'primary' }, 'Create'))));
  }
  return h('div', {}, panels);
}

async function projectView(id) {
  const [p, onProject, everyone] = await Promise.all([
    api('GET', '/projects/' + id), api('GET', '/projects/' + id + '/members'), api('GET', '/members')]);
  const manage = can('projects.manage'), people = can('project_members.manage');
  const ids = new Set(onProject.map((m) => m.id));
  const rows = onProject.map((m) => h('div', { class: 'row' },
    h('div', { class: 'grow' }, h('strong', {}, m.name)), h('span', { class: 'badge' }, m.role),
    people ? h('button', { class: 'danger', onclick: () => act(() => api('DELETE', '/projects/' + id + '/members/' + m.id)) }, 'Remove') : ''));
  const panels = [h('p', {}, h('button', { class: 'link', onclick: () => { state.projectId = null; state.secret = null; render(); } }, '← Projects')),
    h('div', { class: 'panel' }, h('h2', {}, p.name, p.archived ? ' (archived)' : ''),
      p.description ? h('p', {}, p.description) : '', p.repository ? h('p', { class: 'muted' }, p.repository) : '',
      manage ? h('button', { class: 'plain', onclick: () => act(() => api('PATCH', '/projects/' + id, { archived: !p.archived })) }, p.archived ? 'Unarchive' : 'Archive') : ''),
    h('div', { class: 'panel' }, h('h2', {}, 'People on this project (' + onProject.length + ')'), rows)];
  const others = everyone.filter((m) => !ids.has(m.id));
  if (people && others.length) {
    const pick = h('select', { 'aria-label': 'Member' }, others.map((m) => h('option', { value: m.id }, m.name)));
    panels.push(h('div', { class: 'panel' }, h('h2', {}, 'Add someone'),
      h('form', { onsubmit: (e) => { e.preventDefault(); act(() => api('PUT', '/projects/' + id + '/members/' + pick.value)); } },
        h('label', {}, 'Member', pick), h('button', { class: 'primary' }, 'Add'))));
  }
  return h('div', {}, panels);
}

render();
