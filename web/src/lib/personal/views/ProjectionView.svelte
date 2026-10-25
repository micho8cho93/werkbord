<script lang="ts">
  import { onMount } from 'svelte';
  import { countdown, dateInstant, hierarchyRows, metricValue } from '../runtime';
  import type { PersonalRecord, View } from '../schema';
  let {view, records}: {view: View; records: PersonalRecord[]} = $props();
  let now = $state(Date.now());
  onMount(() => {const timer = setInterval(() => now = Date.now(), 60000); return () => clearInterval(timer);});
  const value = $derived(metricValue(view, records));
  const dated = $derived(records.filter(r => dateInstant(r.values[view.dateField ?? '']) !== undefined).sort((a,b) => dateInstant(a.values[view.dateField!])! - dateInstant(b.values[view.dateField!])!));
  const title = (r: PersonalRecord) => String(r.values[view.fields[0]] ?? 'Untitled');
  const start = (r: PersonalRecord) => dateInstant(r.values[view.dateField!])!;
  const end = (r: PersonalRecord) => Math.max(start(r), dateInstant(r.values[view.endDateField ?? '']) ?? start(r));
  const rangeStart = $derived(dated.length ? Math.min(...dated.map(start)) : 0);
  const rangeEnd = $derived(dated.length ? Math.max(...dated.map(end)) : 0);
  const span = $derived(Math.max(86400000, rangeEnd - rangeStart));
  let collapsed = $state<string[]>([]);
  const tree = $derived(hierarchyRows(records, view.parentField ?? ''));
  const shown = $derived.by(() => {const rows: typeof tree = []; let hiddenBelow = 100; for (const row of tree) {if (row.depth <= hiddenBelow) hiddenBelow = 100; if (row.depth > hiddenBelow) continue; rows.push(row); if (collapsed.includes(row.record.id)) hiddenBelow = row.depth;} return rows;});
</script>
<section aria-label={view.name} class="projection">
  <h3>{view.name}</h3>
  {#if view.kind === 'metric'}
    <output class="metric">{value === undefined ? '—' : value.toLocaleString(undefined, {maximumFractionDigits: 2})}</output><p>{records.length} records · {view.metric?.operation}{view.metric?.field ? ` of ${view.metric.field}` : ''}</p>
  {:else if view.kind === 'hierarchy'}
    {#each shown as row (row.record.id)}<div class="tree-row" style:padding-inline-start={`${row.depth * 16}px`}><button class="btn quiet" aria-expanded={!collapsed.includes(row.record.id)} onclick={() => collapsed = collapsed.includes(row.record.id) ? collapsed.filter(id => id !== row.record.id) : [...collapsed, row.record.id]}>{title(row.record)}</button></div>{/each}
  {:else}
    {#each dated as record (record.id)}
      <div class="date-row"><strong>{title(record)}</strong><time datetime={String(record.values[view.dateField!])}>{String(record.values[view.dateField!])}{view.endDateField && record.values[view.endDateField] ? ` – ${record.values[view.endDateField]}` : ''}</time>
        {#if view.kind === 'countdown'}<span>{countdown(record.values[view.dateField!], now)}</span>
        {:else}<div class="track" aria-label="Planned date range"><div class="bar" style:margin-inline-start={`${(start(record) - rangeStart) / span * 90}%`} style:width={`${Math.max(1, (end(record) - start(record)) / span * 90)}%`}></div></div>{/if}
      </div>
    {/each}
    {#if records.length > dated.length}<p>{records.length - dated.length} records have no date.</p>{/if}
  {/if}
  {#if !records.length}<p>No records yet. Add a record in an editable view.</p>{/if}
</section>
<style>
  .projection { display: grid; gap: 12px; padding: 16px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); min-width: 0; }
  :global(.card) .projection { border: 0; padding: 0; background: none; }
  h3 { margin: 0; font-size: 16px; font-weight: 600; overflow-wrap: anywhere; }
  .metric { font-family: var(--mono); font-size: 24px; font-variant-numeric: tabular-nums; }
  p, time { margin: 0; color: var(--text-2); font-size: 12px; }
  .date-row { display: grid; gap: 6px; padding-block: 10px; border-top: 1px solid var(--border); font-size: 13px; overflow-wrap: anywhere; }
  .track { height: 8px; background: var(--surface-2); border-radius: 4px; overflow: hidden; }
  .bar { min-height: 8px; background: var(--accent); border-radius: 4px; }
  .tree-row { min-width: 0; border-top: 1px solid var(--border); padding-top: 6px; }
  button { max-width: 100%; overflow-wrap: anywhere; text-align: start; }
</style>
