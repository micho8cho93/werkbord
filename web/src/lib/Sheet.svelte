<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import { surface } from './motion';

  // A bottom sheet on a phone, a centred dialog on a wide screen. Every action that changes
  // something outside the app (a merge, a push, a new task) is confirmed in one of these, with
  // its checks shown, one click from where you are and never a page away.

  let {
    title,
    onclose,
    children,
    footer,
    width = '34rem',
    side = false,
    layer = 50,
  }: { title: string; onclose: () => void; children: Snippet; footer?: Snippet; width?: string; side?: boolean; layer?: number } = $props();

  let panel: HTMLDivElement | undefined = $state();
  let trapping = false;
  const FOCUSABLE = 'a[href], button, input:not([type="hidden"]), select, textarea, summary, [tabindex]';

  function isTopSheet() {
    const sheets = document.querySelectorAll('.sheet[aria-modal="true"]');
    return trapping && panel && sheets[sheets.length - 1] === panel;
  }

  function focusable() {
    return [...(panel?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])].filter((el) =>
      el.tabIndex >= 0 && !el.matches(':disabled') && !el.closest('[inert]') &&
      el.getClientRects().length > 0 && getComputedStyle(el).visibility !== 'hidden');
  }

  $effect(() => {
    const previous = document.activeElement as HTMLElement | null;
    trapping = true;
    const first = panel?.querySelector<HTMLElement>('[data-autofocus]');
    (first ?? panel)?.focus();
    return () => {
      trapping = false;
      if (previous?.isConnected) previous.focus();
    };
  });

  function onkey(e: KeyboardEvent) {
    if (e.defaultPrevented || !isTopSheet()) return;
    if (e.key === 'Tab') {
      const items = focusable();
      const first = items[0];
      const last = items[items.length - 1];
      const active = document.activeElement;
      if (!first || !active || !panel?.contains(active) || active === panel ||
          (e.shiftKey ? active === first : active === last)) {
        e.preventDefault();
        (e.shiftKey ? last : first)?.focus();
        if (!first) panel?.focus();
      }
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onclose();
    }
  }

  function onfocus(e: FocusEvent) {
    if (isTopSheet() && e.target instanceof Node && !panel?.contains(e.target)) {
      (focusable()[0] ?? panel)?.focus();
    }
  }
</script>

<svelte:window onkeydown={onkey} onfocusin={onfocus} />

<div style:--sheet-layer={layer} class="backdrop" class:side transition:surface={{ y: 0, duration: 140 }} role="presentation" onclick={onclose}></div>
<div style:--sheet-layer={layer} class="sheet" class:side transition:surface={{ x: side ? 20 : 0, y: side ? 0 : 16 }} role="dialog" aria-modal="true" aria-label={title} tabindex="-1" bind:this={panel} style:--w={width}>
  <header>
    <h2>{title}</h2>
    <button class="btn quiet small icon" onclick={onclose} aria-label="Close"><Icon name="close" /></button>
  </header>
  <div class="body">{@render children()}</div>
  {#if footer}<footer>{@render footer()}</footer>{/if}
</div>

<style>
  .backdrop.side { background: color-mix(in srgb, var(--bg) 12%, transparent); backdrop-filter: none; }
  .sheet.side { top: 0; bottom: 0; left: auto; right: 0; width: min(var(--w), 100vw); max-height: 100dvh; border-radius: 0; border: 0; border-left: 1px solid var(--border); transform: none; animation: none; }
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: var(--sheet-layer, 50);
    background: var(--scrim);
    backdrop-filter: blur(2px);
  }

  .sheet {
    position: fixed;
    z-index: calc(var(--sheet-layer, 50) + 1);
    left: 0;
    right: 0;
    bottom: 0;
    max-height: 88dvh;
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto;
    background: var(--surface);
    border: 1px solid var(--border);
    border-bottom: 0;
    border-radius: 12px 12px 0 0;
    box-shadow: var(--win-sh);
    outline: none;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 12px 8px 18px;
  }

  header h2 {
    font-family: var(--font);
    font-size: 16px;
    font-weight: 600;
    text-transform: none;
    letter-spacing: -0.01em;
    color: var(--text);
    overflow-wrap: anywhere;
  }

  .body {
    overflow-y: auto;
    padding: 4px 18px 16px;
    display: grid;
    gap: 12px;
    align-content: start;
    overscroll-behavior: contain;
    font-size: 13px;
  }

  footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 18px calc(12px + env(safe-area-inset-bottom));
    border-top: 1px solid var(--border);
    background: var(--bg);
    border-radius: 0 0 10px 10px;
  }

  @media (max-width: 719px) {
    footer :global(.btn) {
      flex: 1 1 auto;
    }
  }

  @media (min-width: 720px) {
    .sheet {
      left: 50%;
      right: auto;
      bottom: auto;
      top: 12vh;
      width: min(var(--w), 92vw);
      max-height: 76vh;
      transform: translateX(-50%);
      border-radius: 10px;
      border-bottom: 1px solid var(--border);

    }
  }


</style>
