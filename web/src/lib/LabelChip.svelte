<script lang="ts">
  import { inkFor } from './labels';
  import type { Label } from './types';

  // A label as the person made it: their words on their colour. The text colour is chosen for contrast.

  let { label, small = false }: { label: Pick<Label, 'name' | 'color'>; small?: boolean } = $props();
</script>

<span class="lbl" class:small style:background={label.color} style:color={inkFor(label.color)} title={label.name}>{label.name}</span>

<style>
  .lbl {
    display: inline-flex;
    align-items: center;
    max-width: 100%;
    min-height: 20px;
    padding: 0 8px;
    border-radius: 999px;
    font-family: var(--font);
    font-size: 12px;
    font-weight: 500;
    line-height: 1;
    letter-spacing: 0;
    text-transform: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.12);
  }

  .small {
    min-height: 17px;
    padding: 0 6px;
    font-size: 11px;
  }
</style>
