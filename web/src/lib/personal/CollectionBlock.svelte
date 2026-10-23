<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import Icon from '../Icon.svelte';
  import ProgressBlock from '../assistant/openui/components/ProgressBlock.svelte';
  import BarChartBlock from '../assistant/openui/components/BarChartBlock.svelte';
  import RecordEditor from './RecordEditor.svelte';
  import { workspaceSession } from './session.svelte';
  import { matchesSavedFilters, type PersonalRecord } from './schema';
  let { props }: ComponentRenderProps<{ viewId: string }> = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const view = $derived(session.workspace!.definition.views.find(v => v.id === props.viewId)!);
  const collection = $derived(session.collections.find(c => c.id === view.collectionId)!);
  const fields = $derived(view.fields.map(key => collection.fields.find(f => f.key === key)!));
  const records = $derived((session.records[view.collectionId] ?? []).filter(r => matchesSavedFilters(r.values, view.filters)));
  const completed = $derived(records.filter(r => r.values[view.checkedField ?? ''] === true).length);
  const editable = $derived(view.operations?.includes('update') ?? false);
  let search = $state('');
  let filters = $state<Record<string, string>>({});
  let sortKey = $state('');
  let ascending = $state(true);
  let editing = $state<PersonalRecord | 'new' | null>(null);
  let deleting = $state<string | null>(null);
  const display = (value: unknown) => value === undefined ? '—' : Array.isArray(value) ? value.join(', ') : typeof value === 'boolean' ? value ? 'Yes' : 'No' : String(value);
  const visible = $derived(records.filter(r => fields.some(f => display(r.values[f.key]).toLocaleLowerCase().includes(search.toLocaleLowerCase())) && Object.entries(filters).every(([key, value]) => !value || String(r.values[key]) === value)).sort((a, b) => {
    if (!sortKey) return a.id.localeCompare(b.id);
    const av = a.values[sortKey], bv = b.values[sortKey];
    const result = typeof av === 'number' && typeof bv === 'number' ? av - bv : display(av).localeCompare(display(bv), undefined, { numeric: true });
    return ascending ? result : -result;
  }));
  const chartField = $derived(collection.fields.find(f => f.key === view.groupBy));
  const categories = $derived(chartField?.options ?? []);
  function sort(key: string) { ascending = sortKey === key ? !ascending : true; sortKey = key; }
  function title(r: PersonalRecord) { return display(r.values[view.fields[0]]); }
  async function remove(id: string) { if (await session.mutate(view.id, 'delete', id)) deleting = null; }
</script>

<section class="collection" aria-label={view.name}>
  {#if view.kind === 'progress'}
    <ProgressBlock renderNode={emptyNode} props={{ label: view.name, value: completed, total: records.length }} />
  {:else if view.kind === 'status'}
    <div class="status-summary"><h3>{view.name}</h3><p><span class="complete">{completed} completed</span> · {records.length - completed} remaining</p></div>
  {:else if view.kind === 'chart'}
    <BarChartBlock renderNode={emptyNode} props={{ title: view.name, categories, values: categories.map(option => records.filter(r => r.values[view.groupBy!] === option).length), note: `${records.length} records. Unassigned: ${records.filter(r => r.values[view.groupBy!] === undefined).length}.` }} />
  {:else}
    <div class="view-heading"><h3>{view.name}</h3>{#if view.operations?.includes('create')}<button class="btn" disabled={session.busy || editing !== null} onclick={() => editing = 'new'}><Icon name="plus" size={14} />Add record</button>{/if}</div>
    <div class="filters">
      <label class="search" for={`${uid}-search`}>Filter records<input id={`${uid}-search`} type="search" bind:value={search} placeholder="Search this view" /></label>
      {#each fields.filter(f => f.type === 'select' || f.type === 'boolean') as field (field.key)}
        <label for={`${uid}-filter-${field.key}`}>{field.name}<select id={`${uid}-filter-${field.key}`} value={filters[field.key] ?? ''} onchange={(e) => filters[field.key] = e.currentTarget.value}>
          <option value="">All</option>
          {#each field.type === 'boolean' ? ['true', 'false'] : field.options ?? [] as option (option)}<option value={option}>{option === 'true' ? 'Yes' : option === 'false' ? 'No' : option}</option>{/each}
        </select></label>
      {/each}
      {#if view.kind !== 'table'}<label for={`${uid}-sort`}>Sort by<select id={`${uid}-sort`} bind:value={sortKey}><option value="">Record order</option>{#each fields as field (field.key)}<option value={field.key}>{field.name}</option>{/each}</select></label>{/if}
    </div>
    <p class="count">{visible.length} of {records.length} records</p>
    {#if editing}
      {#key editing === 'new' ? 'new' : editing.id}<RecordEditor {collection} {view} record={editing === 'new' ? undefined : editing} onclose={() => editing = null} />{/key}
    {/if}
    {#if deleting}
      <div class="delete-confirm" role="group" aria-label="Delete record"><p>Delete “{title(records.find(r => r.id === deleting)!)}”?</p><button class="btn danger" disabled={session.busy} onclick={() => remove(deleting!)}>Delete record</button><button class="btn" disabled={session.busy} onclick={() => deleting = null}>Keep record</button></div>
    {/if}
    {#if visible.length === 0}<p class="empty">{records.length ? 'No records match these filters.' : 'No records yet. Add your first record to get started.'}</p>
    {:else if view.kind === 'table'}
      <!-- Scrollable tables need keyboard access. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="table-scroll" role="region" aria-label={`${view.name} table`} tabindex="0"><table>
        <thead><tr>{#each fields as field (field.key)}<th scope="col" aria-sort={sortKey === field.key ? ascending ? 'ascending' : 'descending' : 'none'}><button type="button" onclick={() => sort(field.key)}>{field.name}<Icon name="down" size={12} /></button></th>{/each}{#if editable || view.operations?.includes('delete')}<th scope="col">Actions</th>{/if}</tr></thead>
        <tbody>{#each visible as record (record.id)}<tr>{#each fields as field (field.key)}<td>{display(record.values[field.key])}</td>{/each}{#if editable || view.operations?.includes('delete')}<td>{@render actions(record)}</td>{/if}</tr>{/each}</tbody>
      </table></div>
    {:else}
      <ul class="records">{#each visible as record (record.id)}<li>
        {#if view.kind === 'checklist'}<label class="done-target"><input class="done" type="checkbox" checked={record.values[view.checkedField!] === true} disabled={!editable || session.busy} aria-label={`Complete ${title(record)}`} onchange={(e) => session.mutate(view.id, 'update', record.id, { [view.checkedField!]: e.currentTarget.checked })} /></label>{/if}
        <div class="record-content"><strong class:completed={view.kind === 'checklist' && record.values[view.checkedField!] === true}>{title(record)}</strong><dl>{#each fields.slice(1).filter(f => f.key !== view.checkedField) as field (field.key)}<div><dt>{field.name}</dt><dd>{display(record.values[field.key])}</dd></div>{/each}</dl></div>
        {@render actions(record)}
      </li>{/each}</ul>
    {/if}
  {/if}
</section>

{#snippet emptyNode()}{/snippet}

{#snippet actions(record: PersonalRecord)}
  <div class="record-actions">
    {#if editable}<button class="btn" disabled={session.busy || editing !== null} aria-label={`Edit ${title(record)}`} onclick={() => { deleting = null; editing = record; }}>Edit</button>{/if}
    {#if view.operations?.includes('delete')}<button class="btn" disabled={session.busy || editing !== null} aria-label={`Delete ${title(record)}`} onclick={() => deleting = record.id}>Delete</button>{/if}
  </div>
{/snippet}

<style>
  .collection { display: grid; gap: 14px; min-width: 0; padding: 16px 18px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); }
  /* Inside a Card the card is already the panel: no second frame. */
  :global(.card) .collection { padding: 0; border: 0; border-radius: 0; background: none; box-shadow: none; }
  .view-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding-bottom: 12px; border-bottom: 1px solid var(--border); }
  h3 { margin: 0; font-size: 15px; font-weight: 600; overflow-wrap: anywhere; }
  .filters { display: flex; flex-wrap: wrap; align-items: end; gap: 10px 12px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); }
  .filters label { display: grid; gap: 5px; min-width: 0; color: var(--text-2); font-size: 12px; }
  .search { flex: 1 1 180px; }
  input[type='search'], select { min-width: 0; width: 100%; min-height: 36px; padding: 7px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; }
  input:focus-visible, select:focus-visible, .table-scroll:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .count, .empty, .status-summary p { margin: 0; color: var(--text-2); font-size: 12px; }
  .empty { padding: 20px 0; }
  .records { margin: 0; padding: 0; overflow: hidden; list-style: none; border: 1px solid var(--border); border-radius: var(--radius-sm); }
  .records li { display: flex; align-items: start; gap: 12px; padding: 12px 14px; border-bottom: 1px solid var(--border); background: var(--surface); }
  .records li:last-child { border-bottom: 0; }
  .records li:hover { background: var(--surface-2); }
  .done-target { display: grid; place-items: center; flex: 0 0 44px; min-height: 44px; margin-block: -10px; cursor: pointer; }
  .done { width: 20px; height: 24px; flex-shrink: 0; accent-color: var(--accent); }
  .record-content { min-width: 0; flex: 1; }
  strong { font-size: 13px; font-weight: 500; overflow-wrap: anywhere; }
  .completed { text-decoration: line-through; color: var(--text-2); }
  dl { display: flex; flex-wrap: wrap; gap: 6px 16px; margin: 5px 0 0; color: var(--text-2); font-size: 12px; }
  dl div { display: flex; flex-wrap: wrap; gap: 5px; min-width: 0; }
  dt::after { content: ':'; }
  dd { margin: 0; overflow-wrap: anywhere; }
  .record-actions { display: flex; gap: 6px; flex-shrink: 0; }
  .table-scroll { max-width: 100%; overflow-x: auto; border: 1px solid var(--border); border-radius: var(--radius-sm); }
  table { width: 100%; min-width: 760px; border-collapse: collapse; font-size: 13px; text-align: left; }
  th, td { min-width: 110px; padding: 9px 10px; border-bottom: 1px solid var(--border); max-width: 320px; overflow-wrap: anywhere; }
  th { background: var(--surface-2); border-bottom: 2px solid var(--border); }
  tbody tr:last-child td { border-bottom: 0; }
  tbody tr:hover td { background: var(--surface-2); }
  th button { white-space: nowrap; display: flex; align-items: center; gap: 6px; min-height: 36px; border: 0; background: transparent; color: var(--text-2); font: inherit; cursor: pointer; }
  .delete-confirm { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
  .delete-confirm p { width: 100%; margin: 0; overflow-wrap: anywhere; }
  .complete { color: var(--ok-text); }
  .status-summary { display: grid; gap: 8px; }
  @media (pointer: coarse), (max-width: 600px) { .filters input, .filters select { font-size: 16px; } }
  @media (max-width: 600px) { .filters input, select, button, th button { min-height: 44px; } .records li { flex-wrap: wrap; } .record-content { flex-basis: calc(100% - 56px); } .record-actions { margin-left: auto; } }
</style>
