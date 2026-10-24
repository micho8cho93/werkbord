// Product-neutral motion. Callers supply identity, readiness and confirmed state.
// No product actions, timers that delay mutations, or inferred success live here.
(() => {
  if (typeof window === 'undefined' || typeof document === 'undefined') return;
  const active = new Set();
  const reduced = () => document.hidden || window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  function animate(node, frames, duration = 180) {
    if (reduced() || !node?.isConnected || typeof node.animate !== 'function') return null;
    const animation = node.animate(frames, { duration, easing: 'cubic-bezier(.16,1,.3,1)' });
    active.add(animation);
    animation.finished.catch(() => {}).finally(() => active.delete(animation));
    return animation;
  }
  function cancel() { active.forEach(a => a.cancel()); active.clear(); }
  window.matchMedia('(prefers-reduced-motion: reduce)').addEventListener('change', cancel);
  const visibility = () => { document.documentElement.toggleAttribute('data-motion-paused', document.hidden); if (document.hidden) cancel(); };
  document.addEventListener('visibilitychange', visibility); visibility();
  function snapshot(root, selector = '[data-motion-key]') {
    const items = new Map();
    const bounds = root.getBoundingClientRect?.();
    root.querySelectorAll(selector).forEach(node => {
      const rect = node.getBoundingClientRect();
      if (!node.getClientRects().length || !rect.width || !rect.height) return;
      if (rect.bottom <= 0 || rect.right <= 0 || rect.top >= window.innerHeight || rect.left >= window.innerWidth) return;
      if (bounds && (rect.bottom <= bounds.top || rect.top >= bounds.bottom || rect.right <= bounds.left || rect.left >= bounds.right)) return;
      const key = node.dataset.motionKey || node.dataset.boardItem || node.dataset.planItem;
      if (key) items.set(key, { node, rect, parent: node.parentElement, scroll: node.parentElement?.scrollLeft + ':' + node.parentElement?.scrollTop });
    });
    return { items, width: root.clientWidth, scroll: root.scrollLeft + ':' + root.scrollTop + ':' + window.scrollX + ':' + window.scrollY };
  }
  function ghost(item) {
    const node = item.node.cloneNode(true);
    node.removeAttribute('id'); node.removeAttribute('data-board-item'); node.removeAttribute('data-plan-item');
    node.querySelectorAll('[id]').forEach(child => child.removeAttribute('id'));
    node.setAttribute('aria-hidden', 'true'); node.inert = true;
    Object.assign(node.style, { position: 'fixed', margin: '0', left: item.rect.left + 'px', top: item.rect.top + 'px', width: item.rect.width + 'px', height: item.rect.height + 'px', boxSizing: 'border-box', pointerEvents: 'none', zIndex: '19' });
    document.body.append(node);
    return node;
  }
  const moving = new WeakMap();
  function dispose(root, before) {
    const jobs = moving.get(root);
    jobs?.forEach((job, key) => {
      const old = before?.items.get(key);
      if (old && job.copy.isConnected) old.rect = job.copy.getBoundingClientRect();
      job.movement.cancel(); job.cover?.cancel(); job.copy.remove();
    });
    jobs?.clear();
  }
  function layout(root, before, selector, entries = false) {
    dispose(root, before);
    const jobs = new Map(); moving.set(root, jobs);
    const after = snapshot(root, selector);
    if (!before || reduced() || Math.abs(before.width - after.width) > 1 || before.scroll !== after.scroll) return after;
    if ([...before.items.values()].some(item => item.scroll !== item.parent?.scrollLeft + ':' + item.parent?.scrollTop)) return after;
    after.items.forEach((item, key) => {
      const old = before.items.get(key);
      if (!old) {
        if (entries) animate(item.node, [{ opacity: 0, transform: 'translateY(6px)' }, { opacity: 1, transform: 'none' }]);
        return;
      }
      const dx = old.rect.left - item.rect.left, dy = old.rect.top - item.rect.top;
      const resizing = item.node.hasAttribute('data-plan-item') && Math.abs(old.rect.width-item.rect.width) > 1;
      if (Math.abs(dx) + Math.abs(dy) < 1 && !resizing) return;
      if (old.parent !== item.parent) {
        const copy = ghost(old);
        const movement = animate(copy, [{ transform: 'none' }, { transform: `translate(${ -dx }px,${ -dy }px)` }], 240);
        const cover = animate(item.node, [{ opacity: 0 }, { opacity: 0 }], 240);
        if (movement) {
          const job = { copy, movement, cover }; jobs.set(key, job);
          movement.finished.catch(() => {}).finally(() => { copy.remove(); cover?.cancel(); if (jobs.get(key) === job) jobs.delete(key); });
        } else copy.remove();
      } else {
        animate(item.node, [{ transformOrigin: 'left center', transform: `translate(${dx}px,${dy}px) scaleX(${resizing ? old.rect.width/item.rect.width : 1})` }, { transformOrigin: 'left center', transform: 'none' }], resizing ? 220 : 200);
      }
    });
    if (entries) before.items.forEach((item, key) => {
      if (after.items.has(key)) return;
      const copy = ghost(item);
      const exit = animate(copy, [{ opacity: 1 }, { opacity: 0 }], 160);
      if (exit) exit.finished.catch(() => {}).finally(() => copy.remove()); else copy.remove();
    });
    return after;
  }
  function chartSnapshot(root) {
    return new Map([...root.querySelectorAll('[data-chart-key]')].map(node => [node.dataset.chartKey, { value: Number(node.dataset.chartValue), scope: node.dataset.chartScope }]));
  }
  let chartsObserver;
  function charts(root, before) {
    chartsObserver?.disconnect();
    chartsObserver = new IntersectionObserver(entries => entries.forEach(entry => {
      if (!entry.isIntersecting) return;
      const node = entry.target, value = Number(node.dataset.chartValue), previous = before?.get(node.dataset.chartKey);
      const vertical = node.dataset.chartAxis === 'y';
      if (previous) animate(node, [{ opacity: 0.35 }, { opacity: 1 }], 200);
      else animate(node, [{ transform: `scale${vertical ? 'Y' : 'X'}(0)` }, { transform: 'none' }], 320);
      chartsObserver.unobserve(node);
    }));
    root.querySelectorAll('[data-chart-key]').forEach(node => {
      const previous = before?.get(node.dataset.chartKey);
      if (!previous || previous.scope !== node.dataset.chartScope) chartsObserver.observe(node);
    });
  }
  window.WerkbordMotion = { reduced, animate, snapshot, layout, dispose, cancel, chartSnapshot, charts };
})();
