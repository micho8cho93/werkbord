<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';

  // A bottom sheet on a phone, a centred dialog on a wide screen. Every action that changes
  // something outside the app (a merge, a push, a new task) is confirmed in one of these, with
  // its checks shown, one click from where you are and never a page away.

  let {
    title,
    onclose,
    children,
    footer,
    width = '34rem',
  }: { title: string; onclose: () => void; children: Snippet; footer?: Snippet; width?: string } = $props();

  let panel: HTMLDivElement | undefined = $state();

  $effect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const first = panel?.querySelector<HTMLElement>('[data-autofocus]');
    (first ?? panel)?.focus();
    return () => previous?.focus?.();
  });

  function onkey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onclose();
    }
  }
</script>

<svelte:window onkeydown={onkey} />

<div class="backdrop" role="presentation" onclick={onclose}></div>
<div class="sheet" role="dialog" aria-modal="true" aria-label={title} tabindex="-1" bind:this={panel} style:--w={width}>
  <header>
    <h2>{title}</h2>
    <button class="btn quiet small icon" onclick={onclose} aria-label="Close"><Icon name="close" /></button>
  </header>
  <div class="body">{@render children()}</div>
  {#if footer}<footer>{@render footer()}</footer>{/if}
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 50;
    background: var(--scrim);
    backdrop-filter: blur(2px);
  }

  .sheet {
    position: fixed;
    z-index: 51;
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
    font-size: 15px;
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
      animation: rise 0.18s var(--ease);
    }
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translate(-50%, -6px);
    }
  }
</style>
