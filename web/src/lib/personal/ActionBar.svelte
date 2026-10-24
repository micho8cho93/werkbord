<script lang="ts">
  import { matchesSavedFilters, type PersonalRecord, type View } from './schema';
  import { workspaceSession } from './session.svelte';
  let {view, records}: {view: View; records: PersonalRecord[]} = $props();
  const session = workspaceSession();
  let selected = $state<string[]>([]);
  let reviewing = $state('');
  let search = $state('');
  const candidates = $derived(records.filter(r => String(r.values[view.fields[0]] ?? '').toLocaleLowerCase().includes(search.toLocaleLowerCase())).slice(0,100));
  const action = $derived(view.actions?.find(a => a.id === reviewing));
  const matching = (when: View['filters']) => records.filter(r => selected.includes(r.id) && matchesSavedFilters(r.values, when));
  async function run() {
    if (!action) return;
    const ids = action.steps.some(s => s.operation !== 'create') ? matching(action.when).map(r => r.id) : [];
    if (await session.action(view.id, action.id, ids, true)) {selected = []; reviewing = '';}
  }
</script>
{#if view.actions?.length}
  <div class="actions" role="group" aria-label={`${view.name} actions`}>
    {#if view.actions.some(a => a.steps.some(s => s.operation !== 'create'))}
      <details><summary>Select records ({selected.length})</summary>
        <label>Find records<input type="search" bind:value={search} /></label>
        <label><input type="checkbox" checked={candidates.length > 0 && candidates.every(r => selected.includes(r.id))} onchange={(e) => selected = e.currentTarget.checked ? candidates.map(r => r.id) : []} disabled={session.busy || session.readOnly} />Select these {candidates.length} records</label>
        <div class="selection">{#each candidates as record (record.id)}<label><input type="checkbox" value={record.id} bind:group={selected} disabled={session.busy || session.readOnly || selected.length >= 100 && !selected.includes(record.id)} />{String(record.values[view.fields[0]] ?? 'Untitled')}</label>{/each}</div>
      </details>
    {/if}
    <div class="buttons">{#each view.actions as item (item.id)}
      <button class="btn" disabled={session.busy || session.readOnly || !!item.prompt && !session.assist || !item.prompt && item.steps.some(s => s.operation !== 'create') && !matching(item.when).length} onclick={() => item.prompt ? session.assist?.(item.prompt) : reviewing = item.id}>{item.name}</button>
    {/each}</div>
    {#if action}
      <div class="review" role="region" aria-label="Review record action">
        <strong>{action.name}</strong>
        <p>{action.steps.some(s => s.operation !== 'create') ? `${matching(action.when).length} matching selected records` : 'Create a record'} · all steps save together.</p>
        <ol>{#each action.steps as step, index (index)}<li>{step.operation}{#if step.values}: {Object.entries(step.values).map(([k,v]) => `${k} = ${Array.isArray(v) ? v.join(', ') : v}`).join('; ')}{/if}{#if step.clear?.length} · clear {step.clear.join(', ')}{/if}</li>{/each}</ol>
        <div class="buttons"><button class="btn ok" disabled={session.busy} onclick={run}>Confirm {action.name}</button><button class="btn quiet" disabled={session.busy} onclick={() => reviewing = ''}>Cancel</button></div>
      </div>
    {/if}
  </div>
{/if}
<style>
  .actions { display: grid; gap: 10px; margin-block: 12px; }
  .buttons { display: flex; flex-wrap: wrap; gap: 8px; }
  summary { cursor: pointer; color: var(--text-2); font-size: 13px; }
  label { display: flex; align-items: center; gap: 8px; min-height: 36px; font-size: 13px; overflow-wrap: anywhere; }
  input[type='search'] { min-width: 0; width: 100%; min-height: 36px; padding: 7px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; }
  input { accent-color: var(--accent); }
  .selection { max-height: 220px; overflow: auto; }
  .review { padding: 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); font-size: 13px; overflow-wrap: anywhere; }
  p { color: var(--text-2); }
  summary:focus-visible, input:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
  @media (pointer: coarse), (max-width: 600px) { input[type='search'] { min-height: 44px; font-size: 16px; } }
  @media (max-width: 600px) { label, button { min-height: 44px; } }
</style>
