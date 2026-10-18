// How a comment's text and its mentions are kept together while someone writes it. Pure functions, no DOM, so they are tested
// on their own (scripts/team-mentions.test.cjs).
//
// A comment is written in a plain text box. A mention of a person is the text "@Name" in that box plus a token that says which
// member it is: { start, end, memberId, name }, end exclusive, and text.slice(start, end) is "@" + name. The text is only what the
// person sees; who is mentioned is the token's member ID, never the name, so two people called alike are told apart, and a
// mention is never made by typing a name that happens to match. Whatever is done to the text (typing, deleting, pasting), a
// token whose text is touched stops being a mention and is left as the words it is, and one that is not touched moves with its
// text. What is sent is a list of text and mention segments, in order.
'use strict';
(function (root) {
  const MAX_QUERY = 40;
  const LIMIT = 8;

  const norm = (s) => String(s || '').toLocaleLowerCase().replace(/\s+/g, ' ').trim();
  const chars = (s) => Array.from(s).length;

  // The tokens after the text changed from prev to next. caret is where the caret is in next afterwards, when known: it says
  // where the edit ended, which tells apart edits that otherwise look alike (a character typed in a run of the same character).
  function reconcile(prev, next, tokens, caret) {
    if (!tokens.length || prev === next) return tokens.slice();
    let tail = -1;
    if (Number.isInteger(caret) && caret >= 0 && caret <= next.length && prev.endsWith(next.slice(caret))) tail = next.length - caret;
    let head = 0;
    const room = (n) => Math.min(prev.length, next.length) - n;
    if (tail >= 0) {
      const before = Math.min(prev.length - tail, caret);
      while (head < before && prev[head] === next[head]) head++;
    } else {
      while (head < Math.min(prev.length, next.length) && prev[head] === next[head]) head++;
      tail = 0;
      while (tail < room(head) && prev[prev.length - 1 - tail] === next[next.length - 1 - tail]) tail++;
    }
    const prevEnd = prev.length - tail;
    const delta = next.length - prev.length;
    const out = [];
    for (const t of tokens) {
      if (t.end <= head) out.push({ ...t });
      else if (t.start >= prevEnd) out.push({ ...t, start: t.start + delta, end: t.end + delta });
      // otherwise the edit reached into it: it is words now
    }
    return out.filter((t) => next.slice(t.start, t.end) === '@' + t.name);
  }

  // What to send: text and mention parts in order, with no empty text.
  function segments(text, tokens) {
    const out = [];
    let at = 0;
    for (const t of tokens.slice().sort((a, b) => a.start - b.start)) {
      if (t.start < at || text.slice(t.start, t.end) !== '@' + t.name) continue;
      if (t.start > at) out.push({ type: 'text', text: text.slice(at, t.start) });
      out.push({ type: 'mention', memberId: t.memberId });
      at = t.end;
    }
    if (at < text.length) out.push({ type: 'text', text: text.slice(at) });
    return out;
  }

  // The box's content for a comment that is being edited.
  function fromSegments(segs) {
    let text = '';
    const tokens = [];
    for (const s of segs || []) {
      if (s.type === 'mention') {
        const name = s.name || 'former member';
        tokens.push({ start: text.length, end: text.length + 1 + name.length, memberId: s.memberId, name });
        text += '@' + name;
      } else text += s.text || '';
    }
    return { text, tokens };
  }

  // The people the tokens name, once each, in order.
  function mentioned(tokens) {
    const seen = new Set();
    const out = [];
    for (const t of tokens.slice().sort((a, b) => a.start - b.start)) {
      if (!seen.has(t.memberId)) { seen.add(t.memberId); out.push({ memberId: t.memberId, name: t.name }); }
    }
    return out;
  }

  // Whether the caret is in the middle of writing a mention ("@" at the start of a word, then what has been typed of a name),
  // and what has been typed.
  function trigger(text, caret, tokens) {
    if (!Number.isInteger(caret) || caret < 1 || caret > text.length) return null;
    if (tokens.some((t) => caret > t.start && caret <= t.end)) return null;
    const floor = Math.max(0, caret - MAX_QUERY - 1);
    for (let i = caret - 1; i >= floor; i--) {
      const c = text[i];
      if (c === '\n') return null;
      if (c !== '@') continue;
      const before = i === 0 ? ' ' : text[i - 1];
      if (!/[\s([{"'“‘]/.test(before)) return null; // an address (a@b), not a mention
      if (tokens.some((t) => i >= t.start && i < t.end)) return null;
      const query = text.slice(i + 1, caret);
      if (/^\s/.test(query)) return null; // "@ " is just an at sign
      return { start: i, query };
    }
    return null;
  }

  // The people a query could mean, best first: the start of the name, the start of a word of it, anywhere in it, the email.
  function rank(people, query) {
    const q = norm(query);
    const scored = [];
    for (const p of people) {
      const name = norm(p.name), email = norm(p.email);
      let score;
      if (!q) score = 0;
      else if (name.startsWith(q)) score = 0;
      else if (name.split(' ').some((w) => w.startsWith(q))) score = 1;
      else if (name.includes(q)) score = 2;
      else if (email && email.startsWith(q)) score = 3;
      else if (email && email.includes(q)) score = 4;
      else continue;
      scored.push({ p, score });
    }
    scored.sort((a, b) => a.score - b.score || a.p.name.localeCompare(b.p.name) || a.p.id.localeCompare(b.p.id));
    return scored.slice(0, LIMIT).map((s) => s.p);
  }

  // What to show beside each person's name so that two with the same name can be told apart: their email, their role on the
  // project, and, if that is not enough, the end of their ID. Nothing is added for a name that is only one person's.
  function labels(people) {
    const groups = new Map();
    for (const p of people) {
      const key = norm(p.name);
      groups.set(key, [...(groups.get(key) || []), p]);
    }
    const out = new Map();
    for (const group of groups.values()) {
      const alike = group.length > 1;
      const detail = (p) => [p.email, p.projectRole || p.role].filter(Boolean).join(' · ');
      const seen = new Map();
      for (const p of group) seen.set(detail(p), (seen.get(detail(p)) || 0) + 1);
      for (const p of group) {
        let d = detail(p);
        if (alike && (!d || seen.get(d) > 1)) d = (d ? d + ' · ' : '') + 'ID …' + String(p.id).slice(-4);
        out.set(p.id, { name: p.name, detail: alike ? d : (p.projectRole || p.role || ''), alike });
      }
    }
    return out;
  }

  // Put a person where the "@query" was: text, tokens and caret afterwards.
  function insert(text, tokens, trig, caret, person) {
    const mention = '@' + person.name;
    const nextChar = text[caret];
    const gap = nextChar === undefined || !/[\s.,;:!?)\]}]/.test(nextChar) ? ' ' : '';
    const replacement = mention + gap;
    const next = text.slice(0, trig.start) + replacement + text.slice(caret);
    const delta = replacement.length - (caret - trig.start);
    const kept = [];
    for (const t of tokens) {
      if (t.end <= trig.start) kept.push({ ...t });
      else if (t.start >= caret) kept.push({ ...t, start: t.start + delta, end: t.end + delta });
    }
    kept.push({ start: trig.start, end: trig.start + mention.length, memberId: person.id, name: person.name });
    kept.sort((a, b) => a.start - b.start);
    return { text: next, tokens: kept, caret: trig.start + replacement.length };
  }

  // Take every mention of one person out of the text (the words "@Name" go with them).
  function removePerson(text, tokens, memberId) {
    let out = text;
    let rest = tokens.map((t) => ({ ...t }));
    for (const t of tokens.filter((x) => x.memberId === memberId).sort((a, b) => b.start - a.start)) {
      const cut = t.end - t.start;
      out = out.slice(0, t.start) + out.slice(t.end);
      rest = rest.filter((x) => x.start !== t.start).map((x) => (x.start > t.start ? { ...x, start: x.start - cut, end: x.end - cut } : x));
    }
    return { text: out, tokens: rest };
  }

  // How long the comment reads, as the server counts it.
  function length(text) { return chars(text); }

  const api = { reconcile, segments, fromSegments, mentioned, trigger, rank, labels, insert, removePerson, length, MAX_QUERY, LIMIT };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.WerkbordMentions = api;
})(typeof window !== 'undefined' ? window : globalThis);
