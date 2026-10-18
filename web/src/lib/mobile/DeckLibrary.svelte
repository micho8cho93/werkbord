<script lang="ts">
  import Sheet from '../Sheet.svelte';
  import Icon from '../Icon.svelte';
  import { deckKeys, type DeckKeyId } from './deck';
  import { WORKFLOWS, type ButtonId, type DeckSlot } from './layout';
  let { onboarding = false, position, slot, onclose, onchoose, onremove, onmove, onconfigure }:
    { onboarding?: boolean; position: number; slot: DeckSlot | null; onclose: () => void; onchoose: (id: ButtonId) => void; onremove: () => void; onmove: (to: number) => void; onconfigure: () => void } = $props();
  const navigation = deckKeys({ needsYou: 0, review: 0, running: 0, repoAttention: 0 });
</script>
<Sheet title="{slot ? 'Edit button' : 'Add button'} · position {position + 1}" {onclose}>
  {#if onboarding}<p>Programme the bottom three positions with navigation or workflows. You can customise all nine positions later.</p>{/if}
  {#if slot && onboarding}<button class="btn" onclick={onremove}>Remove choice</button>{/if}
  {#if slot && !onboarding}
    <div class="actions">
      {#if WORKFLOWS.some(w => w.id === slot.button)}<button class="btn" onclick={onconfigure}><Icon name="settings" />Configure workflow</button>{/if}
      <button class="btn danger" onclick={onremove}>Remove button</button>
    </div>
    <label class="move">Move to position
      <select class="select" value={String(position)} onchange={e => onmove(Number(e.currentTarget.value))}>
        {#each Array.from({ length: 9 }, (_, i) => i) as i (i)}<option value={String(i)}>{i + 1}{i === position ? ' (current)' : ''}</option>{/each}
      </select>
    </label>
  {/if}
  <h3>{slot ? 'Replace with navigation' : 'Navigation'}</h3>
  <p class="muted">Open a page. These buttons do not start agents.</p>
  <div class="library">
    {#each (onboarding ? navigation.slice(6) : [...navigation.slice(6), ...navigation.slice(0, 6)]) as key (key.id)}
      <button type="button" onclick={() => onchoose(key.id as DeckKeyId)}><Icon name={key.icon} size={20} /><span>{key.label}</span></button>
    {/each}
  </div>
  <h3>Agent workflows</h3>
  <p class="muted">Execute work on a repository using your runner and agent.</p>
  <div class="library">
    {#each WORKFLOWS as w (w.id)}
      <button type="button" onclick={() => onchoose(w.id)}><Icon name={w.icon} size={20} /><span>{w.label}<small>{w.description}</small></span></button>
    {/each}
  </div>
</Sheet>
<style>
  h3 { font-size: 14px; margin: 20px 0 8px; }
  .library { display: grid; }
  .library button { display: flex; align-items: center; gap: 12px; min-height: 48px; padding: 10px 4px; border: 0; border-bottom: 1px solid var(--border); background: none; color: var(--text); text-align: left; font: inherit; cursor: pointer; }
  .library button:hover { background: var(--surface-2); }
  .library :global(svg) { flex: none; color: var(--text-2); }
  small { display: block; margin-top: 3px; font-size: 12px; color: var(--text-2); line-height: 1.5; }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }
  .move { display: grid; gap: 6px; margin-top: 14px; }
  select { min-height: 44px; font-size: 16px; }
</style>
