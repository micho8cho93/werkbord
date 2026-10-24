<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { api } from './api';
  import { app } from './state.svelte';
  import { LOCAL_SURFACES, TOURS, markHere, tourDue, type TourSurface } from './setup/tours';
  import { tourHost } from './setup/tourhost.svelte';
  import { embedded } from './embed';

  // In the desktop window the Help button sits in the bottom right corner of the window, so a card that is not tied to anything
  // there keeps clear of it.
  const framed = embedded();

  /** `enabled` holds a tour back while something else has the person's attention, such as first-time setup. */
  let { surface, enabled = true }: { surface: TourSurface; enabled?: boolean } = $props();

  const steps = $derived(TOURS[surface]);
  let index = $state(0);
  let open = $state(false);
  let card: HTMLDivElement | undefined = $state();
  let rect = $state<DOMRect | null>(null);
  const step = $derived(steps[index]);
  const last = $derived(index === steps.length - 1);

  // Offered once the onboarding record has loaded; closing it, however it closes, counts as seen.
  $effect(() => {
    if (open || !enabled || tourHost.open || !tourDue(app.onboarding, surface)) return;
    // A moment for the page to lay itself out, so that a tip can point at what it is about.
    const t = setTimeout(() => {
      if (tourHost.open) return;
      index = 0;
      open = true;
      tourHost.open = surface;
    }, 700);
    return () => clearTimeout(t);
  });

  function measure() {
    const el = step?.target ? document.querySelector(step.target) : null;
    rect = el && el.getClientRects().length ? el.getBoundingClientRect() : null;
  }

  $effect(() => {
    if (!open) return;
    void step;
    measure();
    void tick().then(() => card?.querySelector<HTMLElement>('[data-primary]')?.focus({ preventScroll: true }));
  });

  onMount(() => {
    const onKey = (e: KeyboardEvent) => { if (open && e.key === 'Escape') close(); };
    window.addEventListener('keydown', onKey);
    window.addEventListener('resize', measure);
    return () => { window.removeEventListener('keydown', onKey); window.removeEventListener('resize', measure); };
  });

  // Leaving the page with a tip showing frees the slot; the tour was not finished, so it comes back next visit.
  onDestroy(() => { if (tourHost.open === surface) tourHost.open = ''; });

  async function close() {
    open = false;
    if (tourHost.open === surface) tourHost.open = '';
    if (LOCAL_SURFACES.includes(surface)) { markHere(surface); return; }
    try { app.onboarding = await api.markTourSeen(surface); } catch { /* shown again next time; harmless */ }
  }

  const below = $derived(rect ? rect.bottom + 12 : 0);
  const left = $derived(rect ? Math.max(12, Math.min(rect.left, window.innerWidth - 332)) : 0);
</script>

{#if open && step}
  {#if rect}<div class="ring" style="top:{rect.top - 4}px;left:{rect.left - 4}px;width:{rect.width + 8}px;height:{rect.height + 8}px" aria-hidden="true"></div>{/if}
  <div bind:this={card} class="tour" class:anchored={!!rect} class:framed style={rect ? `top:${below}px;left:${left}px` : undefined} role="dialog" aria-label="Quick tour" aria-live="polite">
    <p class="count">{index + 1} of {steps.length}</p>
    <h3>{step.title}</h3>
    <p class="body">{step.body}</p>
    <div class="row">
      <button class="btn warn" onclick={close}>Skip tour</button>
      <span class="grow"></span>
      {#if index > 0}<button class="btn" onclick={() => (index -= 1)}>Back</button>{/if}
      <button class="btn primary" data-primary onclick={() => (last ? close() : (index += 1))}>{last ? 'Done' : 'Next'}</button>
    </div>
  </div>
{/if}

<style>
  .tour {
    position: fixed;
    z-index: 60;
    width: min(320px, calc(100vw - 24px));
    display: grid;
    gap: 6px;
    padding: 14px;
    background: var(--surface, var(--bg));
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 8px 28px rgb(0 0 0 / 0.18);
  }
  .tour:not(.anchored) { bottom: 16px; right: 16px; }
  .tour.framed:not(.anchored) { bottom: 60px; }
  @media (max-width: 640px) { .tour:not(.anchored) { left: 12px; right: 12px; width: auto; bottom: calc(12px + env(safe-area-inset-bottom)); } }
  .ring { position: fixed; z-index: 59; border-radius: 8px; box-shadow: 0 0 0 2px var(--accent, currentColor); pointer-events: none; }
  .count { font-size: 12px; color: var(--text-2); margin: 0; }
  h3 { margin: 0; font-size: 15px; font-weight: 600; text-transform: none; letter-spacing: 0; color: var(--text); }
  .body { margin: 0; color: var(--text-2); font-size: 13px; }
  .row { display: flex; align-items: center; gap: 6px; margin-top: 6px; }
  .grow { flex: 1; }
  .row :global(.btn) { min-height: 36px; }
</style>
