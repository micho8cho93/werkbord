// Product-neutral date gesture transport. Products validate and persist each completed gesture.
(function () {
  let gesture = null, suppress = null;
  const dayAt = d => Date.parse(d + 'T12:00:00Z');
  const add = (d, n) => new Date(dayAt(d) + n * 86400000).toISOString().slice(0, 10);
  function target(g, x, y) {
    const day = document.elementFromPoint(x, y)?.closest('[data-plan-day]');
    const grid = g.item.closest('[data-plan-grid]');
    let delta;
    if (grid) delta = Math.round((x - g.x + grid.scrollLeft - g.scroll) / Number(grid.dataset.planPx));
    else if (day && day.closest('[data-plan-surface]') === g.item.closest('[data-plan-surface]')) delta = Math.round((dayAt(day.dataset.planDay) - dayAt(g.start)) / 86400000) - g.offset;
    else return null;
    let start = g.edge === 'end' ? g.start : add(g.start, delta);
    let end = g.edge === 'start' ? g.end : add(g.end, delta);
    if (start > end) { if (g.edge === 'start') start = end; else end = start; }
    let minute;
    if (day?.dataset.planMinuteScale) minute = Math.max(0, Math.min(1425, Math.round((y - day.getBoundingClientRect().top - g.yOffset) / Number(day.dataset.planMinuteScale) / 15) * 15));
    return { id: g.item.dataset.planItem, start, end, minute };
  }
  function clear() {
    if (!gesture) return;
    gesture.item.classList.remove('planning-dragging');
    gesture.item.style.removeProperty('--plan-delta');
    gesture.preview.remove();
    document.querySelectorAll('.planning-over').forEach(el => el.classList.remove('planning-over'));
    gesture = null;
  }
  document.addEventListener('pointerdown', event => {
    const item = event.target.closest('[data-plan-item]');
    if (!item || item.closest('[aria-busy="true"]') || item.disabled || event.button !== 0) return;
    event.preventDefault();
    item.focus({ preventScroll: true });
    const grid = item.closest('[data-plan-grid]'), rect = item.getBoundingClientRect();
    const preview = document.createElement('div');
    preview.className = 'planning-preview'; preview.setAttribute('role', 'status');
    const day = item.closest('[data-plan-day]')?.dataset.planDay;
    gesture = { item, preview, x: event.clientX, y: event.clientY, scroll: grid?.scrollLeft || 0, start: item.dataset.planStart, end: item.dataset.planEnd || item.dataset.planStart,
      offset: day ? Math.round((dayAt(day) - dayAt(item.dataset.planStart)) / 86400000) : 0,
      yOffset: item.hasAttribute('data-plan-scheduled') && item.closest('[data-plan-minute-scale]') ? event.clientY - rect.top : 0,
      edge: event.target.closest('[data-plan-edge]')?.dataset.planEdge, pointer: event.pointerId, moved: false };
    item.setPointerCapture(event.pointerId);
  });
  document.addEventListener('pointermove', event => {
    const g = gesture;
    if (!g || g.pointer !== event.pointerId) return;
    if (!g.moved && Math.hypot(event.clientX - g.x, event.clientY - g.y) < 5) return;
    g.moved = true;
    g.item.classList.add('planning-dragging');
    const next = target(g, event.clientX, event.clientY);
    if (!next) return;
    g.next = next;
    if (!g.preview.isConnected) document.body.append(g.preview);
    g.preview.textContent = next.start + (next.end === next.start ? '' : ' – ' + next.end) + (next.minute === undefined ? '' : ' · ' + Math.floor(next.minute / 60) + ':' + String(next.minute % 60).padStart(2, '0'));
    g.preview.style.left = Math.max(8, Math.min(innerWidth - 260, event.clientX + 12)) + 'px';
    g.preview.style.top = Math.min(innerHeight - 50, event.clientY + 18) + 'px';
    document.querySelectorAll('.planning-over').forEach(el => el.classList.remove('planning-over'));
    document.elementFromPoint(event.clientX, event.clientY)?.closest('[data-plan-day]')?.classList.add('planning-over');
    const scroll = g.item.closest('[data-plan-grid]') || g.item.closest('.scroll, .grid');
    if (scroll) { const r = scroll.getBoundingClientRect(); if (event.clientX > r.right - 36) scroll.scrollLeft += 16; if (event.clientX < r.left + 36) scroll.scrollLeft -= 16; }
  });
  document.addEventListener('pointerup', event => {
    const g = gesture;
    if (!g || g.pointer !== event.pointerId) return;
    const next = target(g, event.clientX, event.clientY);
    clear();
    if (g.moved) {
      suppress = g.item; setTimeout(() => { suppress = null; }, 0);
      if (next && (next.start !== g.start || next.end !== g.end || next.minute !== undefined)) g.item.dispatchEvent(new CustomEvent('werkbord-plan-drop', { bubbles: true, detail: next }));
    }
  });
  document.addEventListener('pointercancel', clear);
  document.addEventListener('click', event => { if (suppress?.contains(event.target)) { event.preventDefault(); event.stopImmediatePropagation(); } }, true);
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape') { clear(); return; }
    const item = event.target.closest('[data-plan-item]');
    if (!item || !event.altKey || !['ArrowLeft', 'ArrowRight'].includes(event.key) || item.disabled || item.getAttribute('aria-busy') === 'true') return;
    event.preventDefault();
    const delta = event.key === 'ArrowRight' ? 1 : -1;
    const start = item.dataset.planStart, end = item.dataset.planEnd || start;
    item.dispatchEvent(new CustomEvent('werkbord-plan-drop', { bubbles: true, detail: { id: item.dataset.planItem, start: event.shiftKey ? start : add(start, delta), end: event.shiftKey && add(end, delta) < start ? start : add(end, delta) } }));
  });
})();
