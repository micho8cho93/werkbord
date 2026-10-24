<script lang="ts">
  // The fields of a document, page, snippet or card, beside its text: each is changed where it is, and saved at once.
  import { workspaceSession } from '../session.svelte';
  import type { Collection, Field, PersonalRecord, View } from '../schema';
  import { display, fromLocalInput, propertyFields, toLocalInput } from './shared';

  let { view, collection, record, fields: only }: { view: View; collection: Collection; record: PersonalRecord; fields?: Field[] } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const fields = $derived(only ?? propertyFields(collection, view));
  const editable = $derived((view.operations?.includes('update') ?? false) && !session.readOnly);

  async function save(key: string, value: string | number | boolean | string[] | undefined) {
    const empty = value === undefined || value === '' || (typeof value === 'number' && Number.isNaN(value));
    await session.mutate(view.id, 'update', record.id, empty ? {} : { [key]: value }, empty ? [key] : []);
  }
  function toggle(field: Field, option: string) {
    const current = Array.isArray(record.values[field.key]) ? (record.values[field.key] as string[]) : [];
    void save(field.key, current.includes(option) ? current.filter((o) => o !== option) : [...current, option]);
  }
</script>

{#if fields.length}
  <div class="props" role="group" aria-label="Properties">
    {#each fields as field (field.key)}
      {@const value = record.values[field.key]}
      <div class="prop" class:wide={field.type === 'multiselect'}>
        <label for={`${uid}-${field.key}`} class="name">{field.name}</label>
        {#if !editable}
          {#if field.type === 'url' && typeof value === 'string'}<a href={value} target="_blank" rel="noopener noreferrer">{value}</a>
          {:else if field.type === 'multiselect' && Array.isArray(value)}<span class="chips">{#each value as option (option)}<span class="chip">{option}</span>{/each}</span>
          {:else}<span class="value">{display(value) || '—'}</span>{/if}
        {:else if field.type === 'select'}
          <select id={`${uid}-${field.key}`} value={typeof value === 'string' ? value : ''} disabled={session.busy} onchange={(e) => save(field.key, e.currentTarget.value)}>
            <option value="">—</option>
            {#each field.options ?? [] as option (option)}<option value={option}>{option}</option>{/each}
          </select>
        {:else if field.type === 'multiselect'}
          <span class="chips" role="group" id={`${uid}-${field.key}`} aria-label={field.name}>
            {#each field.options ?? [] as option (option)}
              {@const on = Array.isArray(value) && value.includes(option)}
              <button type="button" class="chip pick" class:on aria-pressed={on} disabled={session.busy} onclick={() => toggle(field, option)}>{option}</button>
            {/each}
          </span>
        {:else if field.type === 'boolean'}
          <input id={`${uid}-${field.key}`} type="checkbox" checked={value === true} disabled={session.busy} onchange={(e) => save(field.key, e.currentTarget.checked)} />
        {:else if field.type === 'date'}
          <input id={`${uid}-${field.key}`} type="date" value={typeof value === 'string' ? value : ''} disabled={session.busy} onchange={(e) => save(field.key, e.currentTarget.value)} />
        {:else if field.type === 'datetime'}
          <input id={`${uid}-${field.key}`} type="datetime-local" value={toLocalInput(value)} disabled={session.busy} onchange={(e) => save(field.key, fromLocalInput(e.currentTarget.value))} />
        {:else if field.type === 'number'}
          <input id={`${uid}-${field.key}`} type="number" step="any" value={typeof value === 'number' ? value : ''} disabled={session.busy} onchange={(e) => save(field.key, e.currentTarget.value === '' ? undefined : Number(e.currentTarget.value))} />
        {:else}
          <input id={`${uid}-${field.key}`} type={field.type === 'url' ? 'url' : 'text'} maxlength={field.type === 'url' ? 2000 : 16000} placeholder={field.type === 'url' ? 'https://' : ''} value={typeof value === 'string' ? value : ''} disabled={session.busy} onchange={(e) => save(field.key, e.currentTarget.value.trim())} />
          {#if field.type === 'url' && typeof value === 'string' && value}<a class="open" href={value} target="_blank" rel="noopener noreferrer" aria-label={`Open ${field.name}`}>Open</a>{/if}
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .props { display: flex; flex-wrap: wrap; gap: 10px 22px; align-items: center; padding: 8px 0; }
  .prop { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .prop.wide { flex-basis: 100%; }
  .name { color: var(--text-2); font-size: 12px; white-space: nowrap; }
  .value { font-size: 13px; }
  select, input:not([type='checkbox']) { min-width: 0; min-height: 32px; padding: 4px 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; font-size: 13px; }
  input[type='checkbox'] { width: 18px; height: 18px; accent-color: var(--accent); }
  input:focus-visible, select:focus-visible, .chip:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .chip { padding: 2px 10px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-2); color: var(--text-2); font: inherit; font-size: 12px; }
  .chip.pick { cursor: pointer; min-height: 26px; }
  .chip.pick:hover:not(:disabled) { border-color: var(--text-2); color: var(--text); }
  .chip.on { background: var(--tint); border-color: var(--accent); color: var(--accent); font-weight: 600; }
  a { color: var(--accent); font-size: 13px; overflow-wrap: anywhere; }
  .open { white-space: nowrap; }
  @media (max-width: 600px) { select, input:not([type='checkbox']) { min-height: 44px; font-size: 16px; } .chip.pick { min-height: 36px; } .prop { flex-wrap: wrap; } }
</style>
