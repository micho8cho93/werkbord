<script lang="ts">
  import { INTERACTION_OPTIONS, PERMISSIONS_NOTE } from './policy';
  import type { InteractionPolicy } from './types';

  let {
    value = $bindable<InteractionPolicy>('interactive'),
    name,
    disabled = false,
    legend = 'Interaction',
  }: { value?: InteractionPolicy; name: string; disabled?: boolean; legend?: string } = $props();
</script>

<fieldset class="picker" {disabled}>
  <legend>{legend}</legend>
  {#each INTERACTION_OPTIONS as o (o.value)}
    <label class="option" data-selected={value === o.value}>
      <input type="radio" {name} value={o.value} bind:group={value} />
      <span class="text">
        <span class="label">{o.label}</span>
        <span class="desc">{o.description}</span>
      </span>
    </label>
  {/each}
  <p class="note">{PERMISSIONS_NOTE}</p>
</fieldset>

<style>
  .picker {
    display: grid;
    gap: 6px;
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }

  legend {
    padding: 0;
    margin-bottom: 4px;
    font-size: 0.9rem;
    font-weight: 550;
  }

  .option {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-height: 44px;
    padding: 9px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    cursor: pointer;
  }

  .option[data-selected='true'] {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, var(--surface));
  }

  input {
    flex: none;
    width: 18px;
    height: 18px;
    margin: 2px 0 0;
  }

  .text {
    display: grid;
    gap: 1px;
    min-width: 0;
  }

  .label {
    font-weight: 550;
    font-size: 0.92rem;
  }

  .desc {
    font-size: 0.8rem;
    color: var(--text-2);
  }

  .note {
    font-size: 0.78rem;
    color: var(--text-2);
  }

  fieldset:disabled .option {
    opacity: 0.6;
    cursor: default;
  }
</style>
