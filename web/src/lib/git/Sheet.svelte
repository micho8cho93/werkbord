<script lang="ts">
  import type { Snippet } from 'svelte';

  // A bottom sheet on a phone, a centred dialog on a wide screen. Every Git action that
  // changes something is confirmed in one of these, with its checks shown, so that Review,
  // Merge, Push and Delete are one tap from where you are and never a screen away.

  let { title, onclose, children, footer }: { title: string; onclose: () => void; children: Snippet; footer?: Snippet } = $props();

  let panel: HTMLDivElement | undefined = $state();

  $effect(() => {
    const previous = document.activeElement as HTMLElement | null;
    panel?.focus();
    return () => previous?.focus?.();
  });

  function onkey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      onclose();
    }
  }
</script>

<svelte:window onkeydown={onkey} />

<div class="backdrop" role="presentation" onclick={onclose}></div>
<div class="sheet" role="dialog" aria-modal="true" aria-label={title} tabindex="-1" bind:this={panel}>
  <header>
    <h2>{title}</h2>
    <button class="btn small quiet" onclick={onclose} aria-label="Close">Close</button>
  </header>
  <div class="body">{@render children()}</div>
  {#if footer}<footer>{@render footer()}</footer>{/if}
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 30;
    background: rgb(0 0 0 / 0.45);
  }

  .sheet {
    position: fixed;
    z-index: 31;
    left: 0;
    right: 0;
    bottom: 0;
    max-height: 88dvh;
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto;
    background: var(--surface);
    border: 1px solid var(--border);
    border-bottom: 0;
    border-radius: 16px 16px 0 0;
    box-shadow: 0 -8px 30px rgb(0 0 0 / 0.25);
    outline: none;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 16px 8px;
  }

  header h2 {
    font-size: 1rem;
    text-transform: none;
    letter-spacing: 0;
    color: var(--text);
  }

  .body {
    overflow-y: auto;
    padding: 4px 16px 12px;
    display: grid;
    gap: 12px;
    align-content: start;
    overscroll-behavior: contain;
  }

  footer {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 10px 16px calc(12px + env(safe-area-inset-bottom));
    border-top: 1px solid var(--border);
  }

  footer :global(.btn) {
    flex: 1 1 auto;
  }

  @media (min-width: 720px) {
    .sheet {
      left: 50%;
      right: auto;
      bottom: auto;
      top: 50%;
      width: min(34rem, 92vw);
      transform: translate(-50%, -50%);
      border-radius: var(--radius);
      border-bottom: 1px solid var(--border);
    }
  }
</style>
