<script lang="ts">
  let { state, direction = 'down' }: { state: 'pending' | 'done'; direction?: 'up' | 'down' } = $props();
</script>

<span class="transfer-status" data-state={state} data-direction={direction} aria-hidden="true">
  {#if state === 'done'}
    <svg viewBox="0 0 16 16"><path class="check" d="m2 8 4 4 8-9" pathLength="1" /></svg>
  {:else}
    <svg class="arrow" viewBox="0 0 16 16"><path d={direction === 'down' ? 'M8 2v12m-5-5 5 5 5-5' : 'M8 14V2m-5 5 5-5 5 5'} /></svg>
  {/if}
</span>

<style>
  .transfer-status { display: inline-grid; place-items: center; flex: none; width: 16px; height: 18px; overflow: hidden; color: var(--accent); }
  .transfer-status[data-state='done'] { color: var(--ok-text); }
  svg { width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
  @media (prefers-reduced-motion: no-preference) {
    .arrow { animation: transfer-down 1.05s linear infinite; }
    [data-direction='up'] .arrow { animation-name: transfer-up; }
    .check { animation: transfer-check 160ms ease-out; }
  }
  @keyframes transfer-down { from { transform: translateY(-18px); } to { transform: translateY(18px); } }
  @keyframes transfer-up { from { transform: translateY(18px); } to { transform: translateY(-18px); } }
  @keyframes transfer-check { from { stroke-dasharray: 1; stroke-dashoffset: 1; } to { stroke-dasharray: 1; stroke-dashoffset: 0; } }
</style>
