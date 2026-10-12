<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';

  let { props }: ComponentRenderProps<{ text?: string; level?: 1 | 2 | 3 }> = $props();
  const tag = $derived(props.level === 1 ? 'h2' : props.level === 3 ? 'h4' : 'h3');
</script>

<svelte:element this={tag} class="heading" data-level={props.level ?? 2}>{props.text ?? ''}</svelte:element>

<style>
  .heading {
    margin: 8px 0 2px;
    color: var(--text);
    font-size: 16px;
    font-weight: 600;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }

  .heading[data-level='1'] { font-size: 19px; }
  .heading[data-level='3'] { font-size: 14px; }
</style>
