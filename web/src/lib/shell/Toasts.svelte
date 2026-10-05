<script lang="ts">
  import Icon from '../Icon.svelte';
  import { app } from '../state.svelte';

  // Messages that outlive what raised them ("answered on another device", "started"), and
  // errors from the controller. They float at the bottom, never shifting the page.
</script>

<div class="toasts" aria-live="polite">
  {#if app.error}
    <div class="toast bad" role="alert">
      <span>{app.error}</span>
      <button class="x" onclick={() => (app.error = '')} aria-label="Dismiss"><Icon name="close" size={14} /></button>
    </div>
  {/if}
  {#if app.notice}
    <div class="toast" role="status">
      <span>{app.notice}</span>
      <button class="x" onclick={() => app.dismissNotice()} aria-label="Dismiss"><Icon name="close" size={14} /></button>
    </div>
  {/if}
</div>

<style>
  .toasts {
    position: fixed;
    left: calc(50% + var(--rail-w) / 2);
    bottom: 20px;
    transform: translateX(-50%);
    z-index: 60;
    display: grid;
    gap: 8px;
    width: max-content;
    max-width: min(36rem, calc(100vw - var(--rail-w) - 32px));
    pointer-events: none;
  }

  .toast {
    pointer-events: auto;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 9px 8px 9px 14px;
    border-radius: 6px;
    background: var(--text);
    color: var(--bg);
    font-size: 13px;
    font-weight: 500;
    line-height: 1.4;
    box-shadow: 0 10px 30px -8px rgba(0, 0, 0, 0.5);
    animation: up 0.25s var(--ease);
    overflow-wrap: anywhere;
  }

  .toast.bad {
    background: var(--danger);
    color: #fff;
  }

  .x {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    padding: 0;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: inherit;
    opacity: 0.7;
  }

  .x:hover {
    opacity: 1;
    background: rgba(127, 127, 127, 0.25);
  }

  @keyframes up {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }

  @media (max-width: 899px) {
    .toasts {
      left: 50%;
      max-width: calc(100vw - 32px);
      bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 12px);
    }
  }
</style>
