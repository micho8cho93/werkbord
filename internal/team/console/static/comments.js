// Comments on a ticket and the mentions in them: the ticket's conversation, the box to write in (with its picker of the project's
// people), and the rows that tell a person they were mentioned. Plain JavaScript, no build step, like the rest of the console, and
// like it everything from the server goes in as text, never as markup: a comment is a list of text and mention parts, shown with
// textContent, and what is written is sent as the same parts.
//
// What survives a refresh. The console redraws a screen whenever the workspace changes, so everything a person is in the middle of
// lives here and not in the elements: the draft (text and who is mentioned) in `composers`, how much of a conversation is loaded in
// `threads`. A redraw builds the box again from its draft; the console's own snapshot puts back the focus, the caret and the scroll.
// Drafts are also kept for the tab (sessionStorage) so a reload does not lose them, and are dropped when posted or on sign-out.
//
// The pure part (how text and mentions stay together, who a query means) is in mentions.js, which has its own tests.
'use strict';

const WM = window.WerkbordMentions;
const COMMENT_LIMIT = 20000;
const COMMENT_PAGE_SIZE = 50;

const threads = new Map();    // ticket ID -> what is loaded of its conversation
const composers = new Map();  // draft key -> { text, tokens, caret, clientId, digest, error, busy, active, version }
let mentionLimit = 20;        // how many mentions Needs you and the project's Overview list
let mentionCount = null;      // how many unread mentions the last redraw saw, to say when one arrives
const toldOf = new Set();     // notifications already announced in this window
let openThread = '';          // the ticket whose conversation is on screen

// ---- drafts ----

function draftStorageKey(key) { return KEY('draft:' + key); }

function draftFor(key, initial) {
  let st = composers.get(key);
  if (st) return st;
  st = { text: '', tokens: [], caret: 0, clientId: '', digest: '', error: '', conflict: false, busy: false, active: 0, version: 0 };
  let kept = null;
  try { kept = JSON.parse(sessionStorage.getItem(draftStorageKey(key)) || 'null'); } catch (_) { /* a draft is a convenience */ }
  if (kept && typeof kept.text === 'string' && Array.isArray(kept.tokens)) {
    st.text = kept.text;
    st.tokens = kept.tokens.filter((t) => t && st.text.slice(t.start, t.end) === '@' + t.name && typeof t.memberId === 'string');
    st.version = kept.version || 0;
  } else if (initial) {
    Object.assign(st, initial);
  }
  st.caret = st.text.length;
  composers.set(key, st);
  return st;
}

function keepDraft(key, st) {
  try {
    if (!st.text) sessionStorage.removeItem(draftStorageKey(key));
    else sessionStorage.setItem(draftStorageKey(key), JSON.stringify({ text: st.text, tokens: st.tokens, version: st.version }));
  } catch (_) { /* the draft is still in memory */ }
}

function dropDraft(key) {
  composers.delete(key);
  try { sessionStorage.removeItem(draftStorageKey(key)); } catch (_) {}
}

// Called when someone signs out: nothing they wrote stays for the next person at this window.
function clearCommentDrafts() {
  for (const key of [...composers.keys()]) dropDraft(key);
  threads.clear();
  mentionCount = null; toldOf.clear();
  try { for (const k of Object.keys(sessionStorage)) if (k.includes('-draft:')) sessionStorage.removeItem(k); } catch (_) {}
}

// The ticket is closed: the next time it is opened nothing in it is marked as the comment that was asked for.
function forgetOpenComment() { openThread = ''; }

// A creation ID the server uses to notice the same comment arriving twice. 128 random bits; no particular form is relied on.
function newCreationId() {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return 'c' + Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

// ---- the conversation ----

function commentsPath(projectId, ticketId) { return '/projects/' + projectId + '/tickets/' + ticketId + '/comments'; }

// Reads what is loaded of a ticket's conversation again, and makes it the current one. A first read is the latest page; "earlier"
// and "newer" pages are added by the person; opening a comment from a notification reads around it. A redraw reads the same
// stretch again from its first comment on, so an edit, a deletion or a new comment in it shows, and the person's place does not move.
async function loadComments(projectId, ticketId) {
  let th = threads.get(ticketId);
  if (!th || th.projectId !== projectId) {
    th = { projectId, ticketId, first: 0, last: 0, tail: true, around: '', highlight: '', revealed: true, editing: '', data: null, error: '' };
    threads.set(ticketId, th);
  }
  if (openThread !== ticketId && !state.focusComment) th.highlight = ''; // what a notification pointed at is marked only while that ticket is open
  openThread = ticketId;
  if (state.focusComment) {
    Object.assign(th, { around: state.focusComment, highlight: state.focusComment, revealed: false, first: 0, last: 0, tail: true });
    state.focusComment = '';
  }
  const base = commentsPath(projectId, ticketId);
  try {
    let page, comments, meta;
    if (th.around) {
      page = await api('GET', base + '?limit=' + COMMENT_PAGE_SIZE + '&around=' + encodeURIComponent(th.around));
      th.around = '';
      comments = page.comments.slice(); meta = page;
      th.first = comments.length ? comments[0].seq : 0;
      th.tail = !page.hasNewer;
      th.last = comments.length ? comments[comments.length - 1].seq : 0;
    } else if (!th.first) {
      page = await api('GET', base + '?limit=' + COMMENT_PAGE_SIZE);
      comments = page.comments.slice(); meta = page;
      th.first = comments.length ? comments[0].seq : 0;
      th.tail = true;
    } else {
      page = await api('GET', base + '?limit=100&after=' + (th.first - 1));
      comments = page.comments.slice(); meta = page;
      // Follow on to the end of what was loaded (or, for one that follows the conversation, to its end), a little at a time.
      for (let more = 0; more < 5 && page.hasNewer && comments.length && (th.tail || comments[comments.length - 1].seq < th.last); more++) {
        page = await api('GET', base + '?limit=100&after=' + comments[comments.length - 1].seq);
        comments.push(...page.comments); meta = { ...meta, hasNewer: page.hasNewer, newerAfter: page.newerAfter };
      }
      if (th.tail && meta.hasNewer) { th.tail = false; th.last = comments[comments.length - 1].seq; }
    }
    th.data = { ...meta, comments };
    th.error = '';
  } catch (e) {
    if (e instanceof TypeError) throw e; // the network: the console says so and keeps trying
    th.error = e.message;
  }
  return th;
}

async function moreComments(th, direction) {
  const base = commentsPath(th.projectId, th.ticketId);
  const data = th.data;
  try {
    if (direction === 'older') {
      const page = await api('GET', base + '?limit=' + COMMENT_PAGE_SIZE + '&before=' + data.comments[0].seq);
      th.first = page.comments.length ? page.comments[0].seq : th.first;
    } else {
      const page = await api('GET', base + '?limit=' + COMMENT_PAGE_SIZE + '&after=' + data.comments[data.comments.length - 1].seq);
      th.last = page.comments.length ? page.comments[page.comments.length - 1].seq : th.last;
      th.tail = !page.hasNewer;
    }
  } catch (e) { toast('Could not load more comments. ' + e.message); return; }
  await render();
}

function mentionText(seg) {
  return '@' + seg.name;
}

function commentBody(c, me) {
  if (c.deletedAt) return h('p', { class: 'comment-gone muted' }, c.deletedBy === 'manager' ? 'This comment was removed by a project manager' : 'This comment was deleted', ' · ', when(c.deletedAt));
  return h('div', { class: 'comment-body' }, c.segments.map((s) => s.type === 'mention'
    ? h('span', { class: 'mention' + (s.memberId === me ? ' me' : ''), 'data-member-id': s.memberId }, mentionText(s))
    : s.text));
}

async function markMentionRead(id, button) {
  if (button) { button.disabled = true; button.setAttribute('aria-busy', 'true'); }
  try { await api('POST', '/notifications/' + id + '/read'); }
  catch (e) { toast('Could not mark it read. ' + e.message); }
  await render();
}

function commentItem(d, th, k, c) {
  const me = state.me.member.id;
  const unread = c.notification && !c.notification.read;
  const mine = c.authorId === me;
  const editing = th.editing === c.id && c.canEdit;
  const classes = ['comment'];
  if (c.mentionsMe) classes.push('mentions-me', unread ? 'is-unread' : 'is-read');
  if (c.deletedAt) classes.push('is-deleted');
  if (th.highlight === c.id) classes.push('is-referenced');
  const head = h('header', { class: 'comment-head' },
    initial(c.authorName),
    h('strong', { class: 'comment-author' }, mine ? 'You' : c.authorName), c.authorFormer ? h('span', { class: 'muted small' }, ' (no longer here)') : '',
    h('span', { class: 'muted small' }, ' · ', when(c.createdAt)),
    c.editedAt && !c.deletedAt ? h('span', { class: 'muted small comment-edited', title: 'Edited ' + new Date(c.editedAt).toLocaleString() }, ' · edited') : '',
    c.mentionsMe && !c.deletedAt ? h('span', { class: 'badge ' + (unread ? 'warn' : '') }, unread ? 'Mentions you' : 'Mentioned you · read') : '');
  const actions = [];
  if (unread) actions.push(h('button', { type: 'button', class: 'plain small', name: 'mark-read-' + c.id, onclick: (e) => markMentionRead(c.notification.id, e.currentTarget) }, 'Mark read'));
  if (c.canEdit && !editing) actions.push(h('button', { type: 'button', class: 'plain small quiet', onclick: () => { th.editing = c.id; render(); } }, 'Edit'));
  if (c.canDelete && !editing) actions.push(h('button', { type: 'button', class: 'plain small quiet danger-text', onclick: () => deleteComment(th, c) }, 'Delete'));
  const body = editing ? editBox(d, th, k, c) : commentBody(c, me);
  return h('li', { class: 'comment-row' }, h('article', { class: classes.join(' '), id: 'comment-' + c.id, 'data-comment-id': c.id, tabindex: '-1',
    'aria-label': (mine ? 'Your comment' : 'Comment by ' + c.authorName) + (c.mentionsMe ? ', mentions you' : '') },
    head, body, actions.length ? h('div', { class: 'comment-actions' }, actions) : ''));
}

async function deleteComment(th, c) {
  if (!confirm('Delete this comment? It leaves a note that it was deleted, and nobody it mentioned is notified any more.')) return;
  try { await api('DELETE', commentsPath(th.projectId, th.ticketId) + '/' + c.id + '?version=' + c.version); dropDraft('edit-' + c.id); }
  catch (e) { toast('Could not delete the comment. ' + e.message); }
  await render();
}

// ---- the box ----

// A text box for a comment, with its picker. key names the draft; people are who can be mentioned; submit(segments, creationId)
// sends it and resolves when it is done. The draft lives in `composers`: this builds the box from it every time.
function commentBox({ key, label, people, submitLabel, placeholder, initial: seed, onSubmit, onCancel, onReload, staleVersion }) {
  const st = draftFor(key, seed);
  const labelsOf = WM.labels(people);
  const idBase = 'cm-' + key.replace(/[^A-Za-z0-9_-]/g, '');
  const ta = h('textarea', { name: 'comment-' + key, id: idBase, rows: 3, class: 'comment-input', placeholder: placeholder || 'Write a comment…',
    'aria-label': label, 'aria-autocomplete': 'list', 'aria-controls': idBase + '-people', 'aria-describedby': idBase + '-help', autocomplete: 'off', spellcheck: 'true' });
  ta.value = st.text;
  const list = h('div', { class: 'mention-picker', id: idBase + '-people', role: 'listbox', 'aria-label': 'People on this project', hidden: true });
  const status = h('p', { class: 'sr-only', role: 'status', 'aria-live': 'polite' });
  const chips = h('ul', { class: 'mention-chips', 'aria-label': 'People mentioned' });
  const error = h('p', { class: 'field-error', role: 'alert' });
  const count = h('span', { class: 'muted small comment-count' });
  const send = h('button', { type: 'submit', class: 'primary', name: 'send-' + key }, submitLabel);
  const cancel = onCancel ? h('button', { type: 'button', class: 'plain', onclick: onCancel }, 'Cancel') : '';
  const atButton = h('button', { type: 'button', class: 'plain small mention-button', 'aria-label': 'Mention someone', title: 'Mention someone (type @)', onclick: () => insertAt() }, '@');
  let trig = null, matches = [];

  const length = () => WM.length(st.text);
  const empty = () => !st.text.trim();
  const refreshSend = () => { send.disabled = st.busy || empty() || length() > COMMENT_LIMIT; };
  send.refreshDisabled = refreshSend;

  function paintChips() {
    const named = WM.mentioned(st.tokens);
    chips.replaceChildren(...named.map((m) => {
      const you = m.memberId === state.me.member.id;
      return h('li', {}, h('span', { class: 'mention-chip' + (you ? ' me' : ''), title: you ? 'You are not notified of your own mention.' : 'Notified when this is posted' }, '@' + m.name,
        h('button', { type: 'button', class: 'chip-x', 'aria-label': 'Remove the mention of ' + m.name, onclick: () => take(m.memberId) }, '×')));
    }));
    chips.hidden = !named.length;
  }
  function paintCount() {
    const n = length();
    count.textContent = n > COMMENT_LIMIT - 2000 ? n.toLocaleString() + ' of ' + COMMENT_LIMIT.toLocaleString() + (n > COMMENT_LIMIT ? ' · too long' : '') : '';
    count.classList.toggle('over', n > COMMENT_LIMIT);
  }
  function paint() {
    ta.disabled = st.busy; atButton.disabled = st.busy; if (cancel) cancel.disabled = st.busy;
    error.textContent = st.error;
    send.setAttribute('aria-busy', String(st.busy));
    paintChips(); paintCount(); refreshSend();
  }
  function saveCaret() { st.caret = ta.selectionStart; st.scroll = ta.scrollTop; }
  function changed() { st.clientId = ''; st.error = ''; st.conflict = false; keepDraft(key, st); }

  function closePicker() {
    trig = null; matches = [];
    list.hidden = true; list.replaceChildren();
    ta.removeAttribute('aria-activedescendant');
  }
  function paintPicker() {
    if (!trig || !matches.length) { closePicker(); return; }
    st.active = Math.max(0, Math.min(st.active, matches.length - 1));
    const opening = list.hidden;
    list.hidden = false;
    list.replaceChildren(...matches.map((p, i) => {
      const l = labelsOf.get(p.id) || { detail: '' };
      return h('div', { role: 'option', id: idBase + '-o' + i, 'aria-selected': String(i === st.active), class: 'mention-option' + (i === st.active ? ' active' : '') + (l.alike ? ' alike' : ''),
        onpointerdown: (e) => e.preventDefault(), onclick: () => choose(p) },
        initial(p.name), h('span', { class: 'mention-name' }, p.name), l.detail ? h('span', { class: 'muted small mention-detail' }, l.detail) : '');
    }));
    ta.setAttribute('aria-activedescendant', idBase + '-o' + st.active);
    status.textContent = matches.length === 1 ? '1 person matches. Press Enter to mention ' + matches[0].name + '.' : matches.length + ' people match. Use the up and down arrow keys, then Enter, to mention one.';
    if (opening) list.scrollIntoView({ block: 'nearest' }); // near the foot of the ticket, bring the list into view
    list.children[st.active]?.scrollIntoView({ block: 'nearest' });
  }
  function updatePicker() {
    if (st.busy || ta.selectionStart !== ta.selectionEnd) { closePicker(); return; }
    trig = WM.trigger(st.text, ta.selectionStart, st.tokens);
    if (trig && st.dismissed === trig.start) trig = null; // closed with Escape: it stays closed until the text changes
    matches = trig ? WM.rank(people, trig.query) : [];
    if (trig && !matches.length && /\s/.test(trig.query)) trig = null; // a sentence that happens to follow an at sign
    paintPicker();
  }
  function choose(person) {
    if (!trig) return;
    const r = WM.insert(st.text, st.tokens, trig, ta.selectionStart, person);
    st.text = r.text; st.tokens = r.tokens;
    ta.value = r.text; ta.setSelectionRange(r.caret, r.caret); st.caret = r.caret;
    closePicker(); changed(); paint();
    ta.focus({ preventScroll: true });
    status.textContent = 'Mentioning ' + person.name + '.';
  }
  function take(memberId) {
    const r = WM.removePerson(st.text, st.tokens, memberId);
    st.text = r.text; st.tokens = r.tokens;
    ta.value = r.text; changed(); paint(); updatePicker();
    ta.focus({ preventScroll: true });
  }
  function insertAt() {
    const at = ta.selectionStart;
    const before = st.text.slice(0, at);
    const lead = before && !/\s$/.test(before) ? ' ' : '';
    const next = before + lead + '@' + st.text.slice(ta.selectionEnd);
    const caret = at + lead.length + 1;
    st.tokens = WM.reconcile(st.text, next, st.tokens, caret);
    st.text = next; ta.value = next; ta.setSelectionRange(caret, caret);
    ta.focus({ preventScroll: true });
    changed(); paint(); updatePicker();
  }

  ta.addEventListener('input', () => {
    const next = ta.value;
    st.tokens = WM.reconcile(st.text, next, st.tokens, ta.selectionEnd);
    st.text = next; saveCaret();
    st.active = 0; st.dismissed = null;
    changed(); paint(); updatePicker();
  });
  ta.addEventListener('keydown', (e) => {
    if (e.isComposing || e.keyCode === 229) return;
    if (!list.hidden && matches.length) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); st.active = (st.active + (e.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length; paintPicker(); return; }
      if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); choose(matches[st.active]); return; }
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); st.dismissed = trig.start; closePicker(); status.textContent = 'Mention list closed.'; return; }
    }
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); form.requestSubmit(); }
  });
  const sync = () => { saveCaret(); updatePicker(); };
  ta.addEventListener('keyup', (e) => { if (!['ArrowDown', 'ArrowUp', 'Enter', 'Tab', 'Escape'].includes(e.key) || list.hidden) sync(); });
  ta.addEventListener('click', sync);
  ta.addEventListener('focus', sync);
  ta.addEventListener('blur', () => { setTimeout(() => { if (document.activeElement !== ta) closePicker(); }, 0); });

  const form = h('form', { class: 'comment-form', 'data-comment-box': key, onsubmit: async (e) => {
    e.preventDefault();
    if (st.busy || empty() || length() > COMMENT_LIMIT) return;
    const segments = WM.segments(st.text, st.tokens);
    const digest = JSON.stringify(segments);
    if (!st.clientId || st.digest !== digest) { st.clientId = newCreationId(); st.digest = digest; }
    st.busy = true; st.error = ''; closePicker(); paint();
    try {
      await onSubmit(segments, st.clientId);
      dropDraft(key);
      // The console puts back what a box held when it redrew; this box is now empty, and must not be put back.
      ta.value = ''; delete ta.dataset.dirty;
      await render();
      document.getElementById(idBase)?.focus({ preventScroll: true });
    } catch (err) {
      st.busy = false;
      if (err instanceof TypeError) st.error = 'Could not reach the server. Your comment is kept: press the button again to send it. It will not be posted twice.';
      else { st.error = err.message; st.conflict = err.status === 409 && key.startsWith('edit-'); }
      if (!ta.isConnected) { await render(); return; } // the page was redrawn while it was being sent: draw the box again, with the reason
      paint();
      ta.focus({ preventScroll: true });
    }
  } },
    h('div', { class: 'comment-field' }, ta, list),
    status, chips,
    staleVersion ? h('div', { role: 'alert' }, h('p', { class: 'error' }, 'Someone changed this comment while you were editing. Your draft is kept. Copy your words before loading their version and combining the changes.'),
      h('button', { type: 'button', class: 'plain', onclick: () => { dropDraft(key); if (onReload) onReload(); render(); } }, 'Load latest version')) : '',
    error,
    h('div', { class: 'comment-tools' }, atButton, h('span', { class: 'muted small', id: idBase + '-help' }, 'Type @ to mention someone on this project. Ctrl or ⌘ + Enter posts.'), count,
      h('span', { class: 'grow' }), cancel, send));
  paint();
  if (st.scroll) ta.scrollTop = st.scroll;
  // After a redraw the console puts the focus and the caret back; the list of people follows, if the caret is in an "@…".
  requestAnimationFrame(() => { if (ta.isConnected && document.activeElement === ta) updatePicker(); });
  return form;
}

function editBox(d, th, k, c) {
  const key = 'edit-' + c.id;
  const seed = WM.fromSegments(c.segments);
  const st = draftFor(key, { ...seed, version: c.version });
  if (!st.version) st.version = c.version;
  return commentBox({ key, label: 'Edit your comment', people: d.board.people, submitLabel: 'Save', staleVersion: st.version !== c.version,
    onCancel: () => { dropDraft(key); th.editing = ''; render(); },
    onReload: () => { th.editing = ''; },
    onSubmit: async (segments) => {
      await api('PATCH', commentsPath(th.projectId, th.ticketId) + '/' + c.id, { version: st.version, segments });
      th.editing = '';
    } });
}

function newCommentBox(d, th, k) {
  return commentBox({ key: 'new-' + k.id, label: 'Write a comment', people: d.board.people, submitLabel: 'Post comment',
    onSubmit: async (segments, clientId) => { await api('POST', commentsPath(th.projectId, th.ticketId), { clientId, segments }); } });
}

// The Comments section of a ticket.
function commentsSection(d, k) {
  const th = d.comments;
  if (!th) return '';
  const data = th.data;
  if (!data) return h('section', { class: 'comments', id: 'ticket-comments' }, h('h3', {}, 'Comments'), h('p', { class: 'error', role: 'alert' }, th.error || 'Comments could not be loaded.'));
  const unread = data.unreadMentions;
  const head = h('div', { class: 'comments-head' }, h('h3', { id: 'comments-title' }, 'Comments'), h('span', { class: 'badge', title: 'Comments that are not deleted' }, String(data.total)),
    unread ? h('span', { class: 'badge warn' }, plural(unread, 'unread mention')) : '');
  const older = data.hasOlder ? h('button', { type: 'button', class: 'plain small', onclick: () => moreComments(th, 'older') }, 'Load earlier comments') : '';
  const newer = data.hasNewer ? h('button', { type: 'button', class: 'plain small', onclick: () => moreComments(th, 'newer') }, 'Load newer comments') : '';
  const rows = data.comments.length
    ? h('ol', { class: 'comment-list', 'aria-labelledby': 'comments-title' }, data.comments.map((c) => commentItem(d, th, k, c)))
    : h('p', { class: 'muted comments-empty' }, 'No comments yet. Start the conversation, or mention someone to ask them something.');
  const foot = data.canPost ? newCommentBox(d, th, k)
    : h('p', { class: 'muted comments-note' }, data.readOnly ? 'This ticket is archived, so its conversation is read-only. Restore the ticket to add to it.' : 'You can read this conversation but not write in it.');
  return h('section', { class: 'comments', id: 'ticket-comments', 'aria-labelledby': 'comments-title' },
    head, th.error ? h('p', { class: 'error', role: 'alert' }, th.error) : '', older, rows, newer, foot);
}

// After a redraw: bring the comment a notification pointed at into view, once, and give it the focus.
function revealComment() {
  const th = state.ticketId && threads.get(state.ticketId);
  if (!th || th.revealed || !th.highlight) return;
  const el = document.getElementById('comment-' + th.highlight);
  if (!el) return;
  th.revealed = true;
  el.scrollIntoView({ block: 'center' });
  el.focus({ preventScroll: true });
}

// ---- mentions: the rows in Needs you and the project's Overview ----

function openMention(n) {
  state.focusComment = n.commentId;
  return openTicket(n.projectId, n.ticketId);
}

function mentionRow(n) {
  return h('div', { class: 'row mention-row', 'data-notification-id': n.id },
    initial(n.authorName),
    h('div', { class: 'grow' },
      h('div', {}, h('strong', {}, n.authorName), ' mentioned you on ', h('code', {}, n.ticketKey), h('span', { class: 'muted' }, ' · ' + n.projectName + ' · '), when(n.createdAt)),
      h('p', { class: 'mention-excerpt' }, n.excerpt)),
    h('div', { class: 'mention-actions' },
      h('button', { type: 'button', class: 'plain small', 'aria-label': 'Open comment on ' + n.ticketKey + ' by ' + n.authorName, onclick: () => openMention(n) }, 'Open comment'),
      h('button', { type: 'button', class: 'plain small', 'aria-label': 'Mark the mention on ' + n.ticketKey + ' read', onclick: (e) => markMentionRead(n.id, e.currentTarget) }, 'Mark read')));
}

// The unread mentions, newest first. list is what GET /notifications answered.
function mentionRows(list, empty) {
  if (!list) return h('p', { class: 'muted' }, 'Mentions could not be loaded.');
  if (!list.notifications.length) return empty ? h('p', { class: 'muted' }, empty) : '';
  return [list.notifications.map(mentionRow),
    list.hasMore ? h('button', { type: 'button', class: 'plain small', onclick: () => { mentionLimit = Math.min(100, mentionLimit + 20); render(); } }, 'Show more mentions') : ''];
}

function loadMentions(projectId) {
  return api('GET', '/notifications?limit=' + mentionLimit + (projectId ? '&projectId=' + encodeURIComponent(projectId) : '')).catch((e) => { if (e instanceof TypeError) throw e; return null; });
}

// When a mention arrives while the console is open, say so. The first time a redraw sees the count it only remembers it.
async function noteMentions(unread) {
  const before = mentionCount;
  mentionCount = unread;
  if (before === null || unread <= before) return;
  try {
    const list = await api('GET', '/notifications?limit=' + Math.min(3, unread - before));
    for (const n of list.notifications) {
      if (toldOf.has(n.id)) continue;
      toldOf.add(n.id);
      toast(n.authorName + ' mentioned you on ' + n.ticketKey + ' · ' + n.projectName);
    }
  } catch (_) { /* the count is already on the screen */ }
}
