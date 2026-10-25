<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  type Filter = { column: string; label: string; options: string[] };
  let { props }: ComponentRenderProps<{ caption?: string; columns?: string[]; rows?: string[][]; asOf?: string; filterable?: boolean; filters?: Filter[] }> = $props();
  let query = $state('');
  let sortColumn = $state(-1);
  let sortDirection = $state<'asc' | 'desc'>('asc');
  let selected = $state<Record<string, string>>({});

  const columns = $derived(props.columns ?? []);
  const rows = $derived(props.rows ?? []);
  const filters = $derived((props.filters ?? []).slice(0, 4).flatMap((filter) => {
    const columnIndex = columns.indexOf(filter.column);
    if (columnIndex < 0) return [];
    const values = [...new Set(filter.options.filter((option) => rows.some((row) => row[columnIndex] === option)))].slice(0, 12);
    return values.length ? [{ ...filter, columnIndex, options: values }] : [];
  }));
  const visibleRows = $derived.by(() => {
    const needle = query.trim().toLocaleLowerCase();
    let result = rows.filter((row) => (!needle || row.some((cell) => (cell ?? '').toLocaleLowerCase().includes(needle))) &&
      filters.every((filter) => !selected[filter.column] || row[filter.columnIndex] === selected[filter.column]));
    if (sortColumn >= 0) {
      const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
      result = [...result].sort((a, b) => collator.compare(a[sortColumn] ?? '', b[sortColumn] ?? '') * (sortDirection === 'asc' ? 1 : -1));
    }
    return result;
  });

  function sortBy(index: number) {
    if (sortColumn === index) sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
    else { sortColumn = index; sortDirection = 'asc'; }
  }
</script>

<section class="data-table">
  <div class="table-head">
    {#if props.caption}<h3>{props.caption}</h3>{/if}
    {#if (props.filterable !== false && rows.length > 0) || filters.length > 0}
      <div class="filters">
        {#if props.filterable !== false && rows.length > 0}<label class="filter"><span>Filter rows</span><input type="search" bind:value={query} aria-label={`Filter ${props.caption ?? 'table'}`} /></label>{/if}
        {#each filters as filter (filter.column)}
          <label class="filter"><span>{filter.label}</span>
            <select aria-label={filter.label} value={selected[filter.column] ?? ''} onchange={(event) => (selected[filter.column] = event.currentTarget.value)}>
              <option value="">All</option>
              {#each filter.options as option (option)}<option value={option}>{option}</option>{/each}
            </select>
          </label>
        {/each}
      </div>
    {/if}
  </div>
  <SnapshotStamp asOf={props.asOf} />
  {#if rows.length === 0}
    <p class="empty">No records are available in this snapshot.</p>
  {:else if visibleRows.length === 0}
    <p class="empty">No rows match the current filters. Clear a filter to see more rows.</p>
  {:else}
    <!-- svelte-ignore a11y_no_noninteractive_tabindex (The table region must be keyboard reachable when it overflows on phones.) -->
    <div class="table-scroll" role="region" aria-label={props.caption ?? 'Data table'} tabindex="0">
      <table>
        <thead><tr>{#each columns as column, i (i)}
          <th scope="col" aria-sort={sortColumn === i ? (sortDirection === 'asc' ? 'ascending' : 'descending') : 'none'}>
            <button type="button" onclick={() => sortBy(i)} aria-label={`Sort by ${column}${sortColumn === i ? `, currently ${sortDirection}ending` : ''}`}>
              {column}<span class="sort" aria-hidden="true" data-active={sortColumn === i} data-direction={sortDirection}></span>
            </button>
          </th>
        {/each}</tr></thead>
        <tbody>{#each visibleRows as row, i (`${i}-${row[0] ?? ''}`)}
          <tr>{#each columns as column, j (`${i}-${j}`)}<td aria-label={`${column}: ${row?.[j] ?? 'Not reported'}`}>{row?.[j] ?? 'Not reported'}</td>{/each}</tr>
        {/each}</tbody>
      </table>
    </div>
  {/if}
</section>

<style>
  .data-table { display: grid; gap: 8px; min-width: 0; }
  .table-head { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px 14px; }
  .filters { display: flex; flex-wrap: wrap; align-items: end; gap: 8px 14px; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .filter { display: flex; align-items: center; gap: 8px; color: var(--text-2); font-size: 12px; }
  input, select { width: min(100%, 220px); min-height: 36px; padding: 6px 9px; color: var(--text); border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); font: inherit; }
  input:focus-visible, select:focus-visible, button:focus-visible, .table-scroll:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .table-scroll { max-width: 100%; overflow: auto; border: 1px solid var(--border); border-radius: var(--radius-sm); }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { min-width: 96px; padding: 0; border-bottom: 1px solid var(--border); text-align: left; vertical-align: top; }
  th { position: sticky; top: 0; z-index: 1; background: var(--surface-2); font-weight: 600; }
  th button { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; min-height: 42px; padding: 7px 10px; border: 0; background: transparent; color: inherit; text-align: left; font: inherit; cursor: pointer; }
  td { padding: 8px 10px; background: var(--surface); overflow-wrap: anywhere; }
  tbody tr:last-child td { border-bottom: 0; }
  tbody tr:hover td { background: var(--surface-2); }
  .sort { position: relative; flex: 0 0 9px; width: 9px; height: 12px; opacity: .45; }
  .sort::before, .sort::after { position: absolute; left: 1px; width: 0; height: 0; border-right: 4px solid transparent; border-left: 4px solid transparent; content: ''; }
  .sort::before { top: 0; border-bottom: 4px solid currentColor; }
  .sort::after { bottom: 0; border-top: 4px solid currentColor; }
  .sort[data-active='true'] { opacity: 1; color: var(--accent); }
  .sort[data-active='true'][data-direction='asc']::after, .sort[data-active='true'][data-direction='desc']::before { opacity: .22; }
  .empty { margin: 0; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-2); font-size: 13px; }
  @media (max-width: 600px) {
    .filters, .filter { flex: 1 1 100%; align-items: stretch; flex-direction: column; }
    input, select { width: 100%; min-height: 44px; }
    th button { min-height: 44px; }
  }
</style>
