// A focused, read-only jump to visible projects, tickets and sections. Kept
// outside #app so live workspace updates never replace the search field.
'use strict';

let searchSession = null;
const searchShortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ K' : 'Ctrl K';

function searchButton() {
  return h('button', { class: 'search-trigger', type: 'button', name: 'open-search', 'aria-label': 'Search', 'aria-keyshortcuts': 'Meta+K Control+K', onclick: openSearch },
    icon('search'), h('span', {}, 'Search'), h('kbd', { 'aria-hidden': 'true' }, searchShortcut));
}

function openSearch() {
  if (!state.token || !state.me || !app.querySelector('[name="open-search"]') || actionBusy) return;
  if (searchSession) { searchSession.input.focus(); return; }
  const previous = document.activeElement;
  const session = {
    owner: state.me.workspace.id + ':' + state.me.member.id,
    previous, focusName: previous?.getAttribute('name'), focusID: previous?.id,
    query: '', results: null, loading: false, error: '', items: [], active: 0, timer: null, controller: null,
  };
  session.input = h('input', { type: 'text', inputmode: 'search', role: 'combobox', 'aria-label': 'Search projects, tickets and sections',
    'aria-autocomplete': 'list', 'aria-expanded': 'true', 'aria-controls': 'team-search-results', 'aria-describedby': 'team-search-status',
    placeholder: 'Search projects, tickets, sections…', maxlength: 160, autocomplete: 'off', autocapitalize: 'off', spellcheck: 'false',
    oninput: () => querySearch(session),
    onkeydown: e => {
      if (e.isComposing) return;
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault(); activateSearch(session, session.active + (e.key === 'ArrowDown' ? 1 : -1), true);
      } else if (e.key === 'Enter') {
        e.preventDefault(); chooseSearch(session, session.active);
      }
    },
  });
  session.list = h('div', { class: 'search-results', id: 'team-search-results', role: 'listbox', 'aria-label': 'Search results' });
  session.status = h('span', { id: 'team-search-status', role: 'status', 'aria-live': 'polite' });
  session.dialog = h('dialog', { class: 'jump-search', 'aria-label': 'Search Werkbord Team',
    oncancel: e => { e.preventDefault(); closeSearch(); },
    onkeydown: e => {
      // Close this layer before a background keyboard shortcut can handle Escape.
      if (e.key === 'Escape') { e.preventDefault(); closeSearch(); return; }
      if (e.key !== 'Tab') return;
      const controls = [...session.dialog.querySelectorAll('input:not(:disabled), button:not(:disabled):not([tabindex="-1"])')];
      const first = controls[0], last = controls[controls.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
    },
    onclick: e => { if (e.target === session.dialog) { const r = session.dialog.getBoundingClientRect(); if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) closeSearch(); } },
  },
    h('div', { class: 'search-field' }, icon('search'), session.input,
      h('button', { type: 'button', class: 'search-close', 'aria-label': 'Close search', onclick: () => closeSearch() }, icon('close'))),
    session.list,
    h('div', { class: 'search-foot' }, session.status, h('span', { class: 'search-keys', 'aria-hidden': 'true' }, h('kbd', {}, '↑ ↓'), ' Navigate', h('kbd', {}, '↵'), ' Open', h('kbd', {}, 'Esc'), ' Close')));
  searchSession = session;
  document.body.append(session.dialog);
  paintSearch(session);
  session.dialog.showModal();
  session.input.focus();
}

function closeSearch(restoreFocus = true) {
  const session = searchSession;
  if (!session) return;
  searchSession = null;
  clearTimeout(session.timer);
  session.controller?.abort();
  session.dialog.close();
  session.dialog.remove();
  if (restoreFocus) {
    const fallback = session.focusName ? app.querySelector('[name="' + CSS.escape(session.focusName) + '"]') : session.focusID ? document.getElementById(session.focusID) : null;
    const target = session.previous?.isConnected ? session.previous : fallback || app.querySelector('[name="open-search"]');
    target?.focus({ preventScroll: true });
  }
}

function refreshSearch() {
  const session = searchSession;
  if (!session) return;
  if (!state.me || session.owner !== state.me.workspace.id + ':' + state.me.member.id) { closeSearch(false); return; }
  // Membership and ticket changes are re-read, even while the palette is open.
  if (session.query) querySearch(session); else paintSearch(session);
}

function querySearch(session) {
  clearTimeout(session.timer);
  session.controller?.abort();
  session.query = session.input.value.trim();
  session.active = 0; session.results = null; session.error = ''; session.loading = !!session.query;
  paintSearch(session);
  if (session.query) session.timer = setTimeout(() => fetchSearch(session), 160);
}

async function fetchSearch(session) {
  const query = session.query;
  session.controller?.abort();
  const controller = new AbortController(); session.controller = controller;
  session.loading = true; session.error = ''; paintSearch(session);
  try {
    const results = await api('GET', '/search?q=' + encodeURIComponent(query), null, { signal: controller.signal });
    if (controller.signal.aborted || searchSession !== session || session.query !== query) return;
    session.results = results;
  } catch (e) {
    if (controller.signal.aborted || searchSession !== session) return;
    session.error = e instanceof TypeError ? 'Search unavailable. Check your connection and try again.' : e.message;
  }
  if (searchSession !== session) return;
  session.loading = false; paintSearch(session);
}

function paintSearch(session) {
  const terms = session.query.toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const matches = text => terms.every(term => text.toLocaleLowerCase().includes(term));
  const aliases = { workspace: 'overview', mywork: 'my tasks assigned', repository: 'repository branches pull requests', members: 'people organization company teammates' };
  const sections = [...TABS, ...availableSettings()].filter(([id, label]) => matches(label + ' ' + id + ' ' + (aliases[id] || '')))
    .filter(([id]) => session.query || !ADMIN_TABS.some(([tab]) => tab === id) || id === 'settings')
    .map(([id, label]) => ({ key: 'section:' + id, title: id === 'settings' ? 'Settings' : label, detail: ADMIN_TABS.some(([tab]) => tab === id) && id !== 'settings' ? 'Settings' : '', symbol: ADMIN_TABS.some(([tab]) => tab === id) ? 'settings' : id, open: () => go(id) }));
  const projects = (session.results?.projects || state.ov.projects.map(p => p.project).filter(p => matches(p.name + ' ' + (p.repository || ''))))
    .slice(0, session.query ? 20 : 6).map(p => ({ key: 'project:' + p.id, title: p.name, detail: p.archived ? 'Archived project' : 'Project', symbol: 'projects', open: () => openProject(p.id, 'board') }));
  const tickets = (session.results?.tickets || []).map(k => ({ key: 'ticket:' + k.id, title: k.title, ticketKey: k.key,
    detail: k.project.name + ' · ' + (k.archived ? 'Archived' : statusName(k.status)) + (k.project.archived ? ' · Archived project' : ''), symbol: 'board',
    open: () => { if (k.archived) state.showArchived = true; state.column = k.status; openTicket(k.project.id, k.id); } }));
  const groups = session.query ? [['Tickets', tickets], ['Projects', projects], ['Sections', sections]] : [['Sections', sections], ['Projects', projects]];
  const selected = session.items[session.active]?.key;
  session.items = groups.flatMap(([, items]) => items);
  const preserved = session.items.findIndex(item => item.key === selected);
  session.active = preserved >= 0 ? preserved : 0;
  session.list.replaceChildren(...groups.filter(([, items]) => items.length).map(([label, items]) => h('div', { role: 'group', 'aria-label': label },
    h('h2', { class: 'search-group-title', 'aria-hidden': 'true' }, label),
    items.map(item => {
      const index = session.items.indexOf(item);
      return h('button', { type: 'button', role: 'option', tabindex: '-1', id: 'team-search-result-' + index,
        'aria-selected': false, onclick: () => chooseSearch(session, index), onpointermove: () => activateSearch(session, index, false) },
        icon(item.symbol), h('span', { class: 'search-result-copy' }, h('span', { class: 'search-result-title' }, item.ticketKey ? [h('code', {}, item.ticketKey), ' '] : '', item.title),
          item.detail ? h('span', { class: 'muted' }, item.detail) : ''));
    }))));
  if (session.error) session.list.append(h('div', { class: 'search-message' }, h('p', { class: 'error' }, session.error), h('button', { class: 'plain', type: 'button', onclick: () => fetchSearch(session) }, 'Try again')));
  else if (!session.items.length) session.list.append(h('p', { class: 'search-message muted' }, session.loading ? 'Searching…' : 'No results for “' + session.query + '”.'));
  session.list.setAttribute('aria-busy', String(session.loading));
  const more = session.results?.moreProjects || session.results?.moreTickets;
  session.status.textContent = session.loading ? 'Searching…' : session.error ? 'Search unavailable' : plural(session.items.length, 'result') + (more ? '+ · Refine your search' : '');
  activateSearch(session, session.active, false);
}

function activateSearch(session, index, scroll) {
  if (!session.items.length) { session.input.removeAttribute('aria-activedescendant'); return; }
  session.active = (index + session.items.length) % session.items.length;
  session.list.querySelectorAll('[role="option"]').forEach((option, i) => option.setAttribute('aria-selected', String(i === session.active)));
  const id = 'team-search-result-' + session.active;
  session.input.setAttribute('aria-activedescendant', id);
  if (scroll) document.getElementById(id)?.scrollIntoView({ block: 'nearest' });
}

function chooseSearch(session, index) {
  const item = session.items[index];
  if (!item) return;
  closeSearch(false);
  item.open();
}

window.addEventListener('keydown', e => {
  if (!e.defaultPrevented && !e.isComposing && (e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k') {
    if (!state.me || !app.classList.contains('signed-in')) return;
    e.preventDefault();
    if (searchSession) closeSearch(); else openSearch();
  }
});
