// Product-neutral ordering gesture transport for Work queues. Products decide who may reorder, validate the move and persist
// it; this file only reports where the person put an item. Lists are [data-order-list]; their items are [data-order-item]
// (an element with data-order-item="<id>"). A pointer drag starts from [data-order-handle] only, so the rest of a row still
// opens it; Alt+ArrowUp/ArrowDown on an item (or its handle) is the keyboard alternative. [data-order-locked] items never move.
// A completed move is a bubbling `werkbord-order-drop` event on the item: detail { id, before, after, from, to }, where
// before/after are the ids that will sit directly above and below it ('' at an edge) and from/to are zero-based indices.
// A list whose data-order-group names a group can send its items to another place in that group: an element with
// data-order-drop="<scope>" and the same data-order-group is a place an item can be dropped (on it, or in the list of that scope
// inside it, which may not exist yet when it is empty). That drop reports detail.toScope; Alt+Shift+ArrowUp/Down is the keyboard way.
(function (root) {
  const STEP = 1;
  // The position that puts an item between two neighbours, or null when there is no room left and the list needs spreading.
  function between(before, after) {
    const hasBefore = Number.isFinite(before), hasAfter = Number.isFinite(after);
    if (hasBefore && hasAfter) {
      const value = (before + after) / 2;
      return value > before && value < after && after - before > 1e-9 ? value : null;
    }
    if (hasBefore) return before + STEP;
    if (hasAfter) return after - STEP;
    return STEP;
  }
  // Evenly spaced positions for n items, for when between() has run out of room.
  function spread(count) { return Array.from({ length: count }, (_, i) => (i + 1) * STEP); }
  root.WerkbordWork = Object.assign(root.WerkbordWork || {}, { order: { between, spread } });
  if (typeof document === 'undefined') return;

  let gesture = null, suppress = null, live = null;
  const items = list => [...list.querySelectorAll(':scope > [data-order-item]')];
  const movable = el => !el.hasAttribute('data-order-locked') && el.getAttribute('aria-busy') !== 'true';
  function announce(text) {
    if (!live) {
      live = document.createElement('div');
      live.setAttribute('role', 'status'); live.setAttribute('aria-live', 'polite');
      live.style.cssText = 'position:fixed;width:1px;height:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap';
      document.body.append(live);
    }
    live.textContent = '';
    setTimeout(() => { if (live) live.textContent = text; }, 30);
  }
  function describe(el, to, total) { return (el.dataset.orderLabel || 'Item') + ' moved to position ' + (to + 1) + ' of ' + total; }
  function detail(el, order, from) {
    const to = order.indexOf(el);
    return { id: el.dataset.orderItem, before: order[to - 1]?.dataset.orderItem ?? '', after: order[to + 1]?.dataset.orderItem ?? '', from, to };
  }
  function clear() {
    if (!gesture) return;
    gesture.item.classList.remove('order-dragging');
    gesture.list.classList.remove('order-active');
    document.querySelectorAll('.order-drop-over').forEach(el => el.classList.remove('order-drop-over'));
    gesture.list.querySelectorAll('.order-drop-before,.order-drop-after').forEach(el => el.classList.remove('order-drop-before', 'order-drop-after'));
    gesture = null;
  }
  // The slot the pointer is over: the item it is nearest to and which side of it, among the list's other items.
  function slot(g, y) {
    const others = items(g.list).filter(el => el !== g.item);
    for (const el of others) {
      const r = el.getBoundingClientRect();
      if (y < r.top + r.height / 2) return { ref: el, side: 'before' };
    }
    return others.length ? { ref: others[others.length - 1], side: 'after' } : null;
  }
  // The place in the item's group the pointer is over, when it is not the item's own list: its element, scope and list (if any).
  function elsewhere(g, x, y) {
    if (!g.group) return null;
    const over = document.elementFromPoint(x, y)?.closest?.('[data-order-drop]');
    if (!over || over.dataset.orderGroup !== g.group || over.dataset.orderDrop === g.list.dataset.orderScope) return null;
    const scope = over.dataset.orderDrop;
    const list = [...over.querySelectorAll('[data-order-list]')].find(l => l.dataset.orderScope === scope) || null;
    return { over, scope, list };
  }
  // Where in another list an item dropped at y would sit: the item it is nearest to and which side of it.
  function slotIn(list, y) {
    const all = list ? items(list) : [];
    for (const el of all) { const r = el.getBoundingClientRect(); if (y < r.top + r.height / 2) return { ref: el, side: 'before' }; }
    return all.length ? { ref: all[all.length - 1], side: 'after' } : null;
  }
  document.addEventListener('pointerdown', event => {
    const handle = event.target.closest?.('[data-order-handle]');
    const item = handle?.closest('[data-order-item]');
    const list = item?.closest('[data-order-list]');
    if (!item || !list || event.button !== 0 || !movable(item)) return;
    event.preventDefault();
    gesture = { item, list, group: list.dataset.orderGroup || '', pointer: event.pointerId, x: event.clientX, y: event.clientY, moved: false, from: items(list).indexOf(item), target: null, cross: null };
    handle.setPointerCapture?.(event.pointerId);
  });
  document.addEventListener('pointermove', event => {
    const g = gesture;
    if (!g || g.pointer !== event.pointerId) return;
    if (!g.moved && Math.hypot(event.clientX - g.x, event.clientY - g.y) < 4) return;
    g.moved = true;
    g.item.classList.add('order-dragging');
    g.list.classList.add('order-active');
    document.querySelectorAll('.order-drop-before,.order-drop-after,.order-drop-over').forEach(el => el.classList.remove('order-drop-before', 'order-drop-after', 'order-drop-over'));
    g.cross = elsewhere(g, event.clientX, event.clientY);
    if (g.cross) {
      g.cross.over.classList.add('order-drop-over');
      g.target = slotIn(g.cross.list, event.clientY);
    } else g.target = slot(g, event.clientY);
    g.target?.ref.classList.add(g.target.side === 'before' ? 'order-drop-before' : 'order-drop-after');
    const scroller = g.list.closest('.work-view, .scroll, main');
    if (scroller) { const r = scroller.getBoundingClientRect(); if (event.clientY > r.bottom - 48) scroller.scrollTop += 14; if (event.clientY < r.top + 48) scroller.scrollTop -= 14; }
  });
  document.addEventListener('pointerup', event => {
    const g = gesture;
    if (!g || g.pointer !== event.pointerId) return;
    const { item, list, target, from, moved, cross } = g;
    clear();
    if (!moved) return;
    suppress = item; setTimeout(() => { suppress = null; }, 0);
    if (cross) {
      // Into another place in the group: the neighbours are those of that list (none when it is empty).
      const there = cross.list ? items(cross.list) : [];
      const at = target ? there.indexOf(target.ref) + (target.side === 'after' ? 1 : 0) : there.length;
      const d = { id: item.dataset.orderItem, before: there[at - 1]?.dataset.orderItem ?? '', after: there[at]?.dataset.orderItem ?? '', from, to: at, toScope: cross.scope };
      item.dispatchEvent(new CustomEvent('werkbord-order-drop', { bubbles: true, detail: d }));
      announce((item.dataset.orderLabel || 'Item') + ' moved to ' + (cross.over.dataset.orderName || cross.scope));
      return;
    }
    if (!target) return;
    const order = items(list).filter(el => el !== item);
    order.splice(order.indexOf(target.ref) + (target.side === 'after' ? 1 : 0), 0, item);
    if (order.indexOf(item) === from) return;
    item.dispatchEvent(new CustomEvent('werkbord-order-drop', { bubbles: true, detail: detail(item, order, from) }));
    announce(describe(item, order.indexOf(item), order.length));
  });
  document.addEventListener('pointercancel', clear);
  document.addEventListener('click', event => { if (suppress?.contains(event.target)) { event.preventDefault(); event.stopImmediatePropagation(); } }, true);
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape') { clear(); return; }
    if (!event.altKey || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) return;
    const item = event.target.closest?.('[data-order-item]');
    const list = item?.closest('[data-order-list]');
    if (!item || !list || !movable(item)) return;
    event.preventDefault();
    if (event.shiftKey) {
      // Alt+Shift+Arrow: into the previous or next place in the item's group, at its end or its start.
      const group = list.dataset.orderGroup;
      const places = group ? [...document.querySelectorAll('[data-order-drop]')].filter(el => el.dataset.orderGroup === group) : [];
      const here = places.findIndex(el => el.dataset.orderDrop === list.dataset.orderScope);
      const next = places[here + (event.key === 'ArrowDown' ? 1 : -1)];
      if (here < 0 || !next) { announce((item.dataset.orderLabel || 'Item') + ' is already in the ' + (event.key === 'ArrowDown' ? 'last' : 'first') + ' place'); return; }
      const scope = next.dataset.orderDrop;
      const there = [...next.querySelectorAll('[data-order-list]')].filter(l => l.dataset.orderScope === scope).flatMap(items);
      const up = event.key === 'ArrowUp';
      const d = { id: item.dataset.orderItem, before: up ? there[there.length - 1]?.dataset.orderItem ?? '' : '', after: up ? '' : there[0]?.dataset.orderItem ?? '', from: items(list).indexOf(item), to: up ? there.length : 0, toScope: scope };
      item.dispatchEvent(new CustomEvent('werkbord-order-drop', { bubbles: true, detail: d }));
      announce((item.dataset.orderLabel || 'Item') + ' moved to ' + (next.dataset.orderName || scope));
      return;
    }
    const order = items(list), from = order.indexOf(item), to = from + (event.key === 'ArrowDown' ? 1 : -1);
    if (to < 0 || to >= order.length) { announce((item.dataset.orderLabel || 'Item') + (to < 0 ? ' is already first' : ' is already last')); return; }
    order.splice(from, 1); order.splice(to, 0, item);
    item.dispatchEvent(new CustomEvent('werkbord-order-drop', { bubbles: true, detail: detail(item, order, from) }));
    announce(describe(item, to, order.length));
  });
})(typeof globalThis !== 'undefined' ? globalThis : window);
