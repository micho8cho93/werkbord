// A short first-visit tour of the Team console, shown once per device and workspace. It is orientation only: it points at nothing
// that is not already on the page and shows no sample data. Plain text throughout (textContent), like the rest of the console.
// Press ? to see it again.
'use strict';

(() => {
  // A page the window shows only as a section of Settings has no tour: the workspace beside it does.
  try { if (window.parent !== window && new URLSearchParams(location.search).get('hosted') === '1') return; } catch (_) { /* a tour is optional */ }
  const BASE = (/^\/w\/[a-z0-9_]{1,24}(?=\/|$)/.exec(location.pathname) || [''])[0];
  const KEY = 'werkbord-team-tour' + (BASE ? ':' + BASE.slice(3) : '');
  const TIPS = [
    ['Shared with your team', 'Projects, boards and tickets here are shared with the people on each project. Team coordinates the work; it never runs agents or reaches into anyone’s computer.'],
    ['Workspace and My Work', 'Workspace collects what is waiting for you. My Work is your own tickets, and Reviews is work waiting for your verdict.'],
    ['Projects and invitations', 'Open Projects to see who is on each project and to invite people with a link.'],
    ['Run it on your own computer', 'Open a ticket in Werkbord to do the work on your own computer, with your own agents and sign-ins. Press ? to see these tips again.'],
  ];

  const seen = () => { try { return localStorage.getItem(KEY) !== null; } catch (_) { return true; } };
  const remember = () => { try { localStorage.setItem(KEY, new Date().toISOString()); } catch (_) { /* shown again next time; harmless */ } };

  let card = null;
  let index = 0;

  function el(tag, attrs, ...kids) {
    const n = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) { if (k === 'onclick') n.addEventListener('click', v); else n.setAttribute(k, v); }
    for (const kid of kids) n.append(kid);
    return n;
  }

  function close() {
    if (card) card.remove();
    card = null;
    remember();
  }

  function show() {
    if (card) card.remove();
    const [title, body] = TIPS[index];
    const last = index === TIPS.length - 1;
    const row = el('div', { class: 'tour-row' }, el('button', { type: 'button', class: 'plain', onclick: close }, 'Skip tour'), el('span', { class: 'tour-grow' }));
    if (index > 0) row.append(el('button', { type: 'button', class: 'plain', onclick: () => { index -= 1; show(); } }, 'Back'));
    const next = el('button', { type: 'button', class: 'primary', onclick: () => { if (last) close(); else { index += 1; show(); } } }, last ? 'Done' : 'Next');
    row.append(next);
    card = el('div', { class: 'team-tour', role: 'dialog', 'aria-label': 'Quick tour', 'aria-live': 'polite' },
      el('p', { class: 'tour-count' }, (index + 1) + ' of ' + TIPS.length), el('h2', {}, title), el('p', { class: 'tour-body' }, body), row);
    document.body.append(card);
    next.focus({ preventScroll: true });
  }

  function start() { index = 0; show(); }

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && card) { close(); return; }
    const t = e.target;
    if (e.key === '?' && !e.metaKey && !e.ctrlKey && !e.altKey && !(t instanceof HTMLElement && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName)))) start();
  });

  // The tour waits for the signed-in console (the setup and sign-in screens have no .app-shell), and for the page to settle.
  if (seen()) return;
  const app = document.getElementById('app');
  if (!app) return;
  let timer = 0;
  const watch = new MutationObserver(() => {
    if (!app.querySelector('.app-shell')) { clearTimeout(timer); timer = 0; return; }
    if (timer || card) return;
    timer = setTimeout(() => {
      timer = 0;
      if (seen() || card || !app.querySelector('.app-shell')) return;
      watch.disconnect();
      start();
    }, 1200);
  });
  watch.observe(app, { childList: true });
})();
