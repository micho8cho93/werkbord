<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';

  let { props }: ComponentRenderProps<{ label?: string; value?: string; detail?: string }> = $props();
</script>

<section class="metric" aria-label={`${props.label ?? 'Metric'}: ${props.value ?? ''}`}>
  <p class="label">{props.label ?? ''}</p>
  <p class="value">{props.value ?? ''}</p>
  {#if props.detail}<p class="detail">{props.detail}</p>{/if}
</section>

<style>
  .metric {
    display: grid;
    min-width: 0;
    align-content: start;
    gap: 3px;
    padding: 11px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface-2);
  }

  p { margin: 0; overflow-wrap: anywhere; }
  .label, .detail { color: var(--text-2); font-size: 12px; }
  .value { font-size: 19px; font-weight: 600; line-height: 1.25; font-variant-numeric: tabular-nums; }
</style>
