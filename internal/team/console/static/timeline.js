// The Team console's timeline arithmetic and layout, on wall dates ("2026-10-05"): planned work is in whole days, so no
// time zone is ever applied. Pure functions, no DOM, so they can be tested on their own (web/src/lib/timeline.parity.test.ts
// runs the same cases against this and against the individual app's web/src/lib/timeline.ts, which it mirrors).
// What is wrong with the dependencies is worked out by the server (internal/planning); this only places things.
'use strict';
(function (root) {
  const DAY = 86400000;
  const at = (d) => Date.parse(d + 'T12:00:00Z');
  const iso = (ms) => new Date(ms).toISOString().slice(0, 10);
  const addDays = (d, n) => iso(at(d) + n * DAY);
  const daysBetween = (a, b) => Math.round((at(b) - at(a)) / DAY);
  const isDay = (s) => typeof s === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(s) && Number.isFinite(at(s));

  // Where a ticket sits in time, or null when it has no usable dates. Only a start is a one-day item, only an end is
  // something due that day, a milestone is its single date.
  function spanOf(t) {
    const p = t && t.plan;
    if (!p) return null;
    const start = isDay(p.start) ? p.start : undefined;
    const end = isDay(p.end) ? p.end : undefined;
    const first = start || end;
    if (!first) return null;
    if (p.milestone) return { start: first, end: first, milestone: true };
    const last = end || first;
    if (daysBetween(first, last) < 0) return null;
    return { start: first, end: last, milestone: false };
  }

  const PX_PER_DAY = { week: 40, month: 16, quarter: 6 };
  const ROW_HEIGHT = 36;

  function viewRange(spans, today, pad = 7, min = 28) {
    let start = today, end = today;
    for (const s of spans) {
      if (daysBetween(s.start, start) > 0) start = s.start;
      if (daysBetween(end, s.end) > 0) end = s.end;
    }
    start = addDays(start, -pad);
    end = addDays(end, pad);
    const short = min - (daysBetween(start, end) + 1);
    if (short > 0) end = addDays(end, short);
    return { start, end };
  }

  const MONTH = (d) => new Intl.DateTimeFormat('en', { month: 'short', timeZone: 'UTC' }).format(new Date(at(d)));

  function ticks(range, zoom) {
    const out = [];
    const total = daysBetween(range.start, range.end) + 1;
    const px = PX_PER_DAY[zoom];
    for (let i = 0; i < total; i++) {
      const day = addDays(range.start, i);
      const dom = Number(day.slice(8));
      const monday = new Date(at(day)).getUTCDay() === 1;
      if (zoom === 'week') out.push({ day, x: i * px, label: dom === 1 ? MONTH(day) + ' 1' : String(dom), major: dom === 1 || monday });
      else if (zoom === 'month' && monday) out.push({ day, x: i * px, label: dom === 1 ? MONTH(day) : String(dom), major: dom <= 7 });
      else if (zoom === 'quarter' && dom === 1) out.push({ day, x: i * px, label: MONTH(day), major: true });
    }
    return out;
  }

  function barFor(id, span, range, zoom) {
    const px = PX_PER_DAY[zoom];
    return { taskId: id, x: daysBetween(range.start, span.start) * px, width: (daysBetween(span.start, span.end) + 1) * px, milestone: span.milestone, span };
  }

  const STATUS = { backlog: 'Backlog', available: 'Available', in_progress: 'In progress', review: 'In review', done: 'Done' };
  const STATUS_ORDER = ['backlog', 'available', 'in_progress', 'review', 'done'];
  const MODE = { human: 'Human', agent: 'Agent', hybrid: 'Hybrid' };
  const MODE_ORDER = ['human', 'agent', 'hybrid'];
  const modeOf = (t) => t.workMode || 'agent';
  const statusOf = (t) => t.status || t.state;

  // Groups for the rows. By label, a ticket with several labels is listed under each, and one with none under "No label".
  function groupTasks(tasks, labels, by) {
    const order = (a, b) => {
      const sa = spanOf(a), sb = spanOf(b);
      if (sa && sb) return daysBetween(sb.start, sa.start) || daysBetween(sb.end, sa.end) || (a.number || a.position || 0) - (b.number || b.position || 0);
      if (sa) return -1;
      if (sb) return 1;
      return (a.number || a.position || 0) - (b.number || b.position || 0);
    };
    const sorted = [...tasks].sort(order);
    if (by === 'none') return [{ key: 'all', title: 'All work', tasks: sorted }];
    const groups = new Map();
    const put = (key, title, t, color) => {
      let g = groups.get(key);
      if (!g) groups.set(key, (g = { key, title, color, tasks: [] }));
      g.tasks.push(t);
    };
    const byId = new Map((labels || []).map((l) => [l.id, l]));
    for (const t of sorted) {
      if (by === 'status') put(statusOf(t), STATUS[statusOf(t)] || statusOf(t), t);
      else if (by === 'mode') put(modeOf(t), MODE[modeOf(t)], t);
      else {
        const ls = (t.labelIds || []).map((id) => byId.get(id)).filter(Boolean);
        if (!ls.length) put('~none', 'No label', t);
        for (const l of ls) put(l.id, l.name, t, l.color);
      }
    }
    const list = [...groups.values()];
    if (by === 'status') list.sort((a, b) => STATUS_ORDER.indexOf(a.key) - STATUS_ORDER.indexOf(b.key));
    else if (by === 'mode') list.sort((a, b) => MODE_ORDER.indexOf(a.key) - MODE_ORDER.indexOf(b.key));
    else list.sort((a, b) => (a.key === '~none' ? 1 : b.key === '~none' ? -1 : a.title.localeCompare(b.title, undefined, { sensitivity: 'base' })));
    return list;
  }

  function barsFor(tasks, range, zoom) {
    const out = new Map();
    for (const t of tasks) {
      const s = spanOf(t);
      if (s) out.set(t.id, barFor(t.id, s, range, zoom));
    }
    return out;
  }

  function firstRows(keys) {
    const out = new Map();
    keys.forEach((id, i) => { if (id !== null && !out.has(id)) out.set(id, i); });
    return out;
  }

  function dependencyMap(tasks) {
    const out = new Map();
    for (const t of tasks) {
      const deps = t.dependencies || (t.orchestration && t.orchestration.dependencies) || [];
      if (deps.length) out.set(t.id, deps);
    }
    return out;
  }

  function arrows(bars, rowOf, deps) {
    const out = [];
    for (const [to, froms] of deps) {
      const b = bars.get(to), rb = rowOf.get(to);
      if (!b || rb === undefined) continue;
      for (const from of froms) {
        const a = bars.get(from), ra = rowOf.get(from);
        if (!a || ra === undefined) continue;
        const x1 = a.x + a.width, y1 = ra * ROW_HEIGHT + ROW_HEIGHT / 2;
        const x2 = b.x, y2 = rb * ROW_HEIGHT + ROW_HEIGHT / 2;
        const d = x2 >= x1 + 16 ? 'M' + x1 + ' ' + y1 + ' H' + (x1 + 8) + ' V' + y2 + ' H' + x2
          : 'M' + x1 + ' ' + y1 + ' H' + (x1 + 8) + ' V' + (y1 + y2) / 2 + ' H' + (x2 - 8) + ' V' + y2 + ' H' + x2;
        out.push({ from, to, d, conflict: daysBetween(b.span.start, a.span.end) > 0 });
      }
    }
    return out;
  }

  function warningsByTask(warnings) {
    const out = new Map();
    for (const w of warnings) for (const id of new Set([w.itemId, ...(w.cycle || [])])) out.set(id, [...(out.get(id) || []), w]);
    return out;
  }

  function worst(ws) {
    if (!ws || !ws.length) return '';
    return ws.some((w) => w.severity === 'error') ? 'error' : ws.some((w) => w.severity === 'warning') ? 'warning' : 'info';
  }

  function spanTitle(span, thisYear) {
    const f = (d, year) => new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', year: year ? 'numeric' : undefined, timeZone: 'UTC' }).format(new Date(at(d)));
    const y = (d) => Number(d.slice(0, 4)) !== thisYear;
    if (span.start === span.end) return f(span.start, y(span.start));
    const sameMonth = span.start.slice(0, 7) === span.end.slice(0, 7);
    const tail = sameMonth ? String(Number(span.end.slice(8))) : f(span.end, false);
    return f(span.start, false) + ' – ' + tail + (y(span.end) ? ', ' + span.end.slice(0, 4) : '');
  }

  function localToday(now) {
    const d = new Date(now);
    const p = (n) => String(n).padStart(2, '0');
    return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate());
  }

  // A label's colour is #rrggbb when it comes from the server; anything else is not used.
  const safeColor = (c) => (typeof c === 'string' && /^#[0-9a-f]{6}$/i.test(c) ? c : '#6f6e69');
  // Text colour for a background: the one with the better contrast.
  function inkFor(bg) {
    const m = /^#([0-9a-f]{6})$/i.exec(bg);
    if (!m) return '#14130f';
    const [r, g, b] = [0, 2, 4].map((i) => {
      const c = parseInt(m[1].slice(i, i + 2), 16) / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    });
    const lum = 0.2126 * r + 0.7152 * g + 0.0722 * b;
    return 1.05 / (lum + 0.05) >= (lum + 0.05) / 0.0565 ? '#ffffff' : '#14130f';
  }

  const api = { addDays, daysBetween, isDay, spanOf, PX_PER_DAY, ROW_HEIGHT, viewRange, ticks, barFor, barsFor, groupTasks, firstRows, dependencyMap, arrows,
    warningsByTask, worst, spanTitle, localToday, safeColor, inkFor };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.WerkbordTimeline = api;
})(typeof window !== 'undefined' ? window : globalThis);
