// Product-neutral identity: which state an agent mark shows, how old a report is, and a person's initials and avatar tone. No layout,
// no DOM and no product behaviour; identity.css draws it. This is a classic script for the standalone Team console and a side-effect
// import for Svelte, so Personal, Team and the shell resolve one state the same way.
//
// The rule that matters: only a confirmed, fresh running state animates. A live state (running, starting, waiting, blocked, queued)
// whose report is old reads "stale", and one whose freshness is not known reads "unknown". Neither animates and neither becomes
// "done" or "offline" on its own. Freshness is decided by the caller (the controller for own work, the server for a peer); this file
// never reads a clock except to word an age.
'use strict';
(function (root) {
  // Every state a caller may pass, and the ring each one draws. A live state needs a fresh report to be shown as itself.
  const STATES = {
    requested: { ring: 'queued', text: 'requested', live: false },
    queued: { ring: 'queued', text: 'queued', live: true },
    starting: { ring: 'starting', text: 'starting', live: true },
    running: { ring: 'running', text: 'running', live: true },
    needs_input: { ring: 'waiting', text: 'waiting for you', live: true },
    blocked: { ring: 'blocked', text: 'blocked', live: true },
    completed: { ring: 'completed', text: 'completed', live: false },
    failed: { ring: 'failed', text: 'failed', live: false },
    stopped: { ring: 'stopped', text: 'stopped', live: false },
  };
  const ALIASES = { waiting: 'needs_input', 'needs-input': 'needs_input', needs_you: 'needs_input', done: 'completed', complete: 'completed', error: 'failed', cancelled: 'stopped', canceled: 'stopped' };

  // "just now", "6m", "3h", "2d". Nothing for a missing or future time.
  function ageLabel(observedAt, now) {
    const then = typeof observedAt === 'number' ? observedAt : Date.parse(observedAt);
    if (!Number.isFinite(then) || !Number.isFinite(now)) return '';
    const s = Math.floor((now - then) / 1000);
    if (s < 0) return '';
    if (s < 60) return 'just now';
    if (s < 3600) return Math.floor(s / 60) + 'm';
    if (s < 86400) return Math.floor(s / 3600) + 'h';
    return Math.floor(s / 86400) + 'd';
  }

  // input: { state, fresh, observedAt, now, reducedMotion, agentName }
  // Returns { kind, ring, animate, arc, label, text }.
  //   kind     the resolved state (a STATES key, or 'stale' or 'unknown')
  //   ring     what identity.css draws (data-ring)
  //   animate  true only for a confirmed, fresh running state with motion allowed
  //   arc      a running state with motion reduced: the static 270 degree arc
  //   label    the accessible name, for example "Codex, running" or "Codex, last reported 6m ago"
  function resolveAgentState(input) {
    const i = input || {};
    const raw = typeof i.state === 'string' ? i.state.trim().toLowerCase() : '';
    const key = ALIASES[raw] || raw;
    const known = Object.prototype.hasOwnProperty.call(STATES, key) ? STATES[key] : null;
    const name = typeof i.agentName === 'string' && i.agentName.trim() ? i.agentName.trim() : 'Agent';
    let kind;
    if (!known) kind = 'unknown';
    else if (known.live && i.fresh === false) kind = 'stale';
    else if (known.live && i.fresh !== true) kind = 'unknown';
    else kind = key;
    const ring = known && kind === key ? known.ring : kind;
    const running = kind === 'running';
    const age = kind === 'stale' ? ageLabel(i.observedAt, i.now) : '';
    let text;
    if (kind === 'stale') text = age ? 'last reported ' + (age === 'just now' ? age : age + ' ago') : 'last report is out of date';
    else if (kind === 'unknown') text = 'state not known';
    else text = known.text;
    return { kind, ring, animate: running && !i.reducedMotion, arc: running && !!i.reducedMotion, label: name + ', ' + text, text };
  }

  // Up to two capital initials from a name; the first letter of an email local part when that is all there is. Never empty.
  function initials(name) {
    const words = String(name || '').trim().split(/[\s._-]+/).filter(Boolean);
    const letters = words.length > 1 ? words[0][0] + words[words.length - 1][0] : (words[0] || '').slice(0, 1);
    return (letters || '?').toLocaleUpperCase();
  }

  // The same member gets the same tone everywhere (FNV-1a over the id). The count matches the tones in identity.css.
  const TONES = 6;
  function avatarTone(id) {
    let h = 0x811c9dc5;
    const s = String(id || '');
    for (let n = 0; n < s.length; n++) { h ^= s.charCodeAt(n); h = Math.imul(h, 0x01000193) >>> 0; }
    return h % TONES;
  }

  root.WerkbordIdentity = { STATES: Object.keys(STATES), TONES, resolveAgentState, ageLabel, initials, avatarTone };
})(globalThis);
