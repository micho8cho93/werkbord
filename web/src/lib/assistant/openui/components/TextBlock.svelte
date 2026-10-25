<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';

  let { props }: ComponentRenderProps<{ text?: string; tone?: 'normal' | 'secondary' | 'mono' }> = $props();
</script>

<p class="text" data-tone={props.tone ?? 'normal'}>{props.text ?? ''}</p>

<style>
  .text {
    margin: 0;
    color: var(--text);
    font-size: 14px;
    line-height: 1.55;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .text[data-tone='secondary'] { color: var(--text-2); }
  .text[data-tone='mono'] {
    font-family: var(--mono);
    font-size: 12px;
  }
</style>
