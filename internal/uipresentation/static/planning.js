// Product-neutral calendar dates, planned spans and label colours. No visual layout or product behavior.
// This is a classic script for the standalone console and a side-effect import for Svelte.
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

  root.WerkbordPlanning = { addDays, daysBetween, isDay, spanOf, spanTitle, localToday, safeColor, inkFor };
})(globalThis);
