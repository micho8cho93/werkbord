import { markFrame, type MarkMotion } from './mark-motion';

/** An isolated 48-cell loop, capped at 30 fps. Static marks allocate no observers or frames. */
export function markActivity(node: SVGSVGElement, initial: MarkMotion | undefined) {
  const cells = [...node.querySelectorAll('rect')];
  let dispose: (() => void) | undefined;
  function start(initialMotion: MarkMotion) {
    const media = window.matchMedia('(prefers-reduced-motion: reduce)');
    let motion = initialMotion, active = true, visible = false, frame = 0, last = 0, seconds = 0, amount = 0;
    const originals = cells.map(cell => ({ x: Number(cell.getAttribute('x')) + 5, y: Number(cell.getAttribute('y')) + 5 }));
    function reset() {
      cells.forEach(cell => { cell.removeAttribute('transform'); cell.style.removeProperty('opacity'); node.append(cell); });
      node.dataset.motionRunning = 'false';
    }
    function stop() { cancelAnimationFrame(frame); frame = 0; last = 0; node.dataset.motionRunning = 'false'; }
    function allowed() { return !document.hidden && visible && !media.matches; }
    function tick(now: number) {
      frame = 0;
      if (!allowed()) return;
      if (last && now - last < 1000 / 30) { frame = requestAnimationFrame(tick); return; }
      const delta = last ? Math.min((now - last) / 1000, .1) : 0;
      last = now; seconds += delta;
      amount = Math.max(0, Math.min(1, amount + (active ? 1 : -1) * delta / .72));
      const blend = amount * amount * (3 - 2 * amount);
      for (const [order, cell] of markFrame(motion, seconds, blend).entries()) {
        const el = cells[cell.index], origin = originals[cell.index];
        const a = Math.cos(cell.angle) * cell.width / 10, b = Math.sin(cell.angle) * cell.width / 10;
        const c = -Math.sin(cell.angle) * cell.height / 10, d = Math.cos(cell.angle) * cell.height / 10;
        const values = [a, b, c, d, cell.x - a * origin.x - c * origin.y, cell.y - b * origin.x - d * origin.y];
        el.setAttribute('transform', `matrix(${values.map(v => v.toFixed(3)).join(' ')})`);
        el.style.opacity = cell.opacity.toFixed(3);
        if (node.children[order] !== el) node.insertBefore(el, node.children[order] ?? null);
      }
      node.dataset.motionRunning = String(active);
      if (active || amount > 0) frame = requestAnimationFrame(tick); else reset();
    }
    function sync() {
      stop();
      if (media.matches) { amount = 0; reset(); }
      if (allowed() && (active || amount > 0)) frame = requestAnimationFrame(tick);
      else if (!active) reset();
    }
    const observer = new IntersectionObserver(entries => { visible = entries.some(e => e.isIntersecting); sync(); });
    observer.observe(node);
    media.addEventListener('change', sync); document.addEventListener('visibilitychange', sync);
    dispose = () => { stop(); observer.disconnect(); media.removeEventListener('change', sync); document.removeEventListener('visibilitychange', sync); reset(); };
    return (next: MarkMotion | undefined) => { active = !!next; if (next) motion = next; sync(); };
  }
  let updateLoop = initial ? start(initial) : undefined;
  return {
    update(next: MarkMotion | undefined) { if (updateLoop) updateLoop(next); else if (next) updateLoop = start(next); },
    destroy() { dispose?.(); },
  };
}
