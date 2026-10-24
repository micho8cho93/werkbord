<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import { model } from './model.svelte';

  // Salie's button. It floats over the window, bottom and centre to begin with, and can be picked up and put anywhere in it; where
  // it was left is remembered. Pressing it opens Salie, who grows out of wherever the button is and folds back into it.
  const SIZE = 52;
  const MARGIN = 12;
  const KEY = 'werkbord.shell.salie';

  let button: HTMLButtonElement | undefined = $state();
  let width = $state(typeof innerWidth === 'number' ? innerWidth : 1200);
  let height = $state(typeof innerHeight === 'number' ? innerHeight : 800);
  // Where the person left the button, as a share of the room it can move in; null until it has been moved.
  let saved = $state<{ fx: number; fy: number } | null>(read());
  let drag = $state<{ id: number; dx: number; dy: number; x: number; y: number; moved: boolean } | null>(null);
  let held = false;

  function read(): { fx: number; fy: number } | null {
    try {
      const v = JSON.parse(localStorage.getItem(KEY) ?? 'null');
      return v && Number.isFinite(v.fx) && Number.isFinite(v.fy) ? { fx: clamp(v.fx, 0, 1), fy: clamp(v.fy, 0, 1) } : null;
    } catch {
      return null;
    }
  }
  function write(v: { fx: number; fy: number } | null): void {
    try {
      if (v) localStorage.setItem(KEY, JSON.stringify(v));
      else localStorage.removeItem(KEY);
    } catch {
      // Remembering is a convenience; the button still works.
    }
  }
  function clamp(n: number, lo: number, hi: number): number {
    return Math.min(hi, Math.max(lo, n));
  }
  const room = $derived({ w: Math.max(0, width - SIZE - 2 * MARGIN), h: Math.max(0, height - SIZE - 2 * MARGIN) });
  const home = $derived({ x: MARGIN + room.w / 2, y: MARGIN + room.h });
  const spot = $derived(drag ? { x: drag.x, y: drag.y } : saved ? { x: MARGIN + saved.fx * room.w, y: MARGIN + saved.fy * room.h } : home);

  const reachable = $derived(model.view?.items.find((i) => i.id === 'personal')?.state !== 'unavailable');
  const label = $derived(
    !reachable
      ? 'Salie, unavailable: Cannot reach Werkbord'
      : model.assistant.waiting > 0
        ? `Salie, ${model.assistant.waiting} ${model.assistant.waiting === 1 ? 'change is' : 'changes are'} waiting for you`
        : model.assistant.running
          ? 'Salie, working'
          : 'Salie',
  );

  $effect(() => {
    model.assistantButton = button;
    return () => { if (model.assistantButton === button) model.assistantButton = undefined; };
  });

  function open(): void {
    const r = button?.getBoundingClientRect();
    if (r) void model.toggleAssistant({ x: r.left, y: r.top, width: r.width, height: r.height });
  }
  function down(e: PointerEvent): void {
    if (e.button !== 0 || !button) return;
    const r = button.getBoundingClientRect();
    drag = { id: e.pointerId, dx: e.clientX - r.left, dy: e.clientY - r.top, x: r.left, y: r.top, moved: false };
    held = false;
    button.setPointerCapture(e.pointerId);
  }
  function move(e: PointerEvent): void {
    if (!drag || e.pointerId !== drag.id) return;
    const x = clamp(e.clientX - drag.dx, MARGIN, MARGIN + room.w);
    const y = clamp(e.clientY - drag.dy, MARGIN, MARGIN + room.h);
    const moved = drag.moved || Math.hypot(x - drag.x, y - drag.y) > 4;
    drag = { ...drag, x: moved ? x : drag.x, y: moved ? y : drag.y, moved };
  }
  function up(e: PointerEvent): void {
    if (!drag || e.pointerId !== drag.id) return;
    const done = drag;
    drag = null;
    button?.releasePointerCapture?.(e.pointerId);
    if (!done.moved) return;
    held = true;
    saved = { fx: room.w ? clamp((done.x - MARGIN) / room.w, 0, 1) : 0.5, fy: room.h ? clamp((done.y - MARGIN) / room.h, 0, 1) : 1 };
    write(saved);
  }
  function cancel(): void {
    drag = null;
    held = false;
  }
  function click(): void {
    // The click that ends a drag is the button being put down, not pressed.
    if (held) { held = false; return; }
    open();
  }
  function keys(e: KeyboardEvent): void {
    const step = e.shiftKey ? 48 : 16;
    const by = e.altKey ? { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }[e.key] : undefined;
    if (e.key === 'Home' && e.altKey) {
      e.preventDefault();
      saved = null;
      write(null);
      return;
    }
    if (!by) return;
    e.preventDefault();
    const x = clamp(spot.x + by[0], MARGIN, MARGIN + room.w);
    const y = clamp(spot.y + by[1], MARGIN, MARGIN + room.h);
    saved = { fx: room.w ? (x - MARGIN) / room.w : 0.5, fy: room.h ? (y - MARGIN) / room.h : 1 };
    write(saved);
  }
</script>

<svelte:window bind:innerWidth={width} bind:innerHeight={height} />

<button
  bind:this={button}
  class="salie"
  class:dragging={drag?.moved}
  class:running={model.assistant.running}
  type="button"
  style:left="{spot.x}px"
  style:top="{spot.y}px"
  style:width="{SIZE}px"
  style:height="{SIZE}px"
  aria-haspopup="dialog"
  aria-expanded={model.assistant.open}
  aria-label={label}
  aria-keyshortcuts="Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown Alt+Home"
  title={reachable ? 'Salie. Drag to move; Alt and the arrow keys move it from the keyboard.' : 'Cannot reach Werkbord'}
  disabled={!reachable}
  onpointerdown={down}
  onpointermove={move}
  onpointerup={up}
  onpointercancel={cancel}
  onlostpointercapture={() => { drag = null; }}
  onclick={click}
  onkeydown={keys}
  data-page="assistant"
  data-testid="salie-button"
>
  <Icon name="assistant" size={24} />
  {#if model.assistant.waiting > 0}<span class="num" aria-hidden="true">{model.assistant.waiting}</span>{/if}
</button>

<style>
  .salie {
    position: fixed;
    z-index: 60;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: 1px solid var(--pri-bd);
    border-radius: 50%;
    background: var(--pri-bg);
    color: var(--accent-text);
    box-shadow: var(--pri-sh), 0 10px 24px -8px color-mix(in srgb, var(--accent) 55%, transparent);
    cursor: grab;
    touch-action: none;
    user-select: none;
    transition: transform 0.14s var(--ease), box-shadow 0.14s;
  }
  .salie:hover:not(:disabled) { box-shadow: var(--pri-sh), 0 12px 28px -6px color-mix(in srgb, var(--accent) 65%, transparent); }
  .salie:active:not(:disabled), .salie.dragging { cursor: grabbing; }
  .salie.dragging { box-shadow: var(--pri-sh), 0 18px 34px -10px color-mix(in srgb, var(--accent) 65%, transparent); transition: none; }
  .salie:focus-visible { outline: 2px solid var(--text); outline-offset: 3px; }
  .salie:disabled { opacity: 0.5; cursor: not-allowed; }
  .salie.running::after {
    content: '';
    position: absolute;
    inset: -5px;
    border-radius: 50%;
    border: 2px solid var(--accent);
    opacity: 0.5;
    animation: salie-pulse 1.6s ease-in-out infinite;
    pointer-events: none;
  }
  @keyframes salie-pulse { 0%, 100% { transform: scale(0.92); opacity: 0.55; } 50% { transform: scale(1.06); opacity: 0.1; } }
  .num {
    position: absolute;
    top: -4px;
    right: -4px;
    min-width: 20px;
    padding: 0 5px;
    border-radius: 10px;
    background: var(--amber);
    color: #0a0a0a;
    font-family: var(--mono);
    font-size: 11.5px;
    line-height: 20px;
    text-align: center;
  }
  @media (prefers-reduced-motion: reduce) {
    .salie, .salie.running::after { transition: none; animation: none; }
  }
</style>
