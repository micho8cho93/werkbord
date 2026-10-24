<script lang="ts">
  // A board: one column for each choice of a select field, and cards that move between them.
  import Icon from '../../Icon.svelte';
  import RecordEditor from '../RecordEditor.svelte';
  import { workspaceSession } from '../session.svelte';
  import { matchesSavedFilters, type Collection, type PersonalRecord, type View } from '../schema';
  import { autofocus, display, newRecordValues, titleOf } from './shared';

  let { view, collection }: { view: View; collection: Collection } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const groupKey = $derived(view.groupBy ?? '');
  const groupField = $derived(collection.fields.find((f) => f.key === groupKey));
  const options = $derived(groupField?.options ?? []);
  const canCreate = $derived((view.operations?.includes('create') ?? false) && !session.readOnly);
  const canUpdate = $derived((view.operations?.includes('update') ?? false) && !session.readOnly);
  const canDelete = $derived((view.operations?.includes('delete') ?? false) && !session.readOnly);
  const records = $derived((session.records[view.collectionId] ?? []).filter((r) => matchesSavedFilters(r.values, view.filters)));
  let search = $state('');
  const shown = $derived.by(() => {
    const needle = search.trim().toLocaleLowerCase();
    return needle ? records.filter((r) => view.fields.some((key) => display(r.values[key]).toLocaleLowerCase().includes(needle))) : records;
  });
  const unassigned = $derived(shown.filter((r) => typeof r.values[groupKey] !== 'string' || !options.includes(r.values[groupKey] as string)));
  const columns = $derived([
    ...options.map((option) => ({ id: option, label: option, cards: shown.filter((r) => r.values[groupKey] === option) })),
    ...(unassigned.length ? [{ id: '', label: `No ${groupField?.name.toLocaleLowerCase() ?? 'column'}`, cards: unassigned }] : []),
  ]);
  const details = $derived(view.fields.slice(1).filter((key) => key !== groupKey).map((key) => collection.fields.find((f) => f.key === key)!).filter((f) => f && f.type !== 'longtext'));

  const listOf = (value: unknown): string[] => (Array.isArray(value) ? value : []);
  let dragging = $state('');
  let over = $state<string | null>(null);
  let adding = $state<string | null>(null);
  let draftTitle = $state('');
  let editing = $state<PersonalRecord | null>(null);
  let deleting = $state('');
  let announce = $state('');

  async function moveTo(record: PersonalRecord, column: string) {
    if ((record.values[groupKey] ?? '') === column || !canUpdate) return;
    await session.idle();
    const ok = await session.mutate(view.id, 'update', record.id, column ? { [groupKey]: column } : {}, column ? [] : [groupKey]);
    if (ok) announce = `${titleOf(view, record)} moved to ${column || 'no column'}.`;
  }
  async function step(record: PersonalRecord, delta: number) {
    const at = options.indexOf(record.values[groupKey] as string);
    const next = options[Math.min(options.length - 1, Math.max(0, (at === -1 ? (delta > 0 ? -1 : options.length) : at) + delta))];
    if (next) await moveTo(record, next);
  }
  async function add(event: SubmitEvent, column: string) {
    event.preventDefault();
    const title = draftTitle.trim();
    if (!title) return;
    const given: Record<string, string> = { [view.fields[0]]: title };
    if (column) given[groupKey] = column;
    if (await session.mutate(view.id, 'create', undefined, newRecordValues(collection, view, given))) { draftTitle = ''; adding = null; }
  }
  async function remove(id: string) { if (await session.mutate(view.id, 'delete', id)) deleting = ''; }
  function drop(event: DragEvent, column: string) {
    event.preventDefault();
    over = null;
    const record = records.find((r) => r.id === (event.dataTransfer?.getData('text/plain') || dragging));
    dragging = '';
    if (record) void moveTo(record, column);
  }
</script>

<section class="board" aria-label={view.name} data-view-kind="board">
  <div class="top">
    <h3>{view.name}</h3>
    <label class="search"><span class="visually-hidden">Search cards</span><input type="search" placeholder="Search cards" bind:value={search} /></label>
  </div>
  {#if editing}{#key editing.id}<RecordEditor {collection} {view} record={editing} onclose={() => editing = null} />{/key}{/if}
  <div class="columns">
    {#each columns as column (column.id)}
      <div class="column" class:over={over === column.id} role="group" aria-label={`${column.label}, ${column.cards.length} ${column.cards.length === 1 ? 'card' : 'cards'}`}
        ondragover={(e) => { if (canUpdate && dragging) { e.preventDefault(); over = column.id; } }} ondragleave={() => { if (over === column.id) over = null; }} ondrop={(e) => drop(e, column.id)}>
        <header><h4>{column.label}</h4><span class="count">{column.cards.length}</span></header>
        <ul>
          {#each column.cards as card (card.id)}
            <li class="card" class:dragging={dragging === card.id} draggable={canUpdate} ondragstart={(e) => { dragging = card.id; e.dataTransfer?.setData('text/plain', card.id); if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'; }} ondragend={() => { dragging = ''; over = null; }}>
              <div class="card-title">{titleOf(view, card)}</div>
              {#if details.length}
                <dl>{#each details as field (field.key)}{#if card.values[field.key] !== undefined}
                  <div><dt>{field.name}</dt><dd>
                    {#if field.type === 'url'}<a href={String(card.values[field.key])} target="_blank" rel="noopener noreferrer">{String(card.values[field.key])}</a>
                    {:else if Array.isArray(card.values[field.key])}{#each listOf(card.values[field.key]) as tag (tag)}<span class="chip">{tag}</span>{/each}
                    {:else}{display(card.values[field.key])}{/if}
                  </dd></div>{/if}{/each}</dl>
              {/if}
              {#if canUpdate || canDelete}
                <div class="actions">
                  {#if canUpdate && options.length > 1}
                    <button type="button" class="mini" aria-label={`Move ${titleOf(view, card)} to the previous column`} disabled={session.busy || options.indexOf(card.values[groupKey] as string) === 0} onclick={() => step(card, -1)}><Icon name="left" size={12} /></button>
                    <button type="button" class="mini" aria-label={`Move ${titleOf(view, card)} to the next column`} disabled={session.busy || options.indexOf(card.values[groupKey] as string) === options.length - 1} onclick={() => step(card, 1)}><Icon name="right" size={12} /></button>
                  {/if}
                  <span class="gap"></span>
                  {#if canUpdate}<button type="button" class="mini text" aria-label={`Edit ${titleOf(view, card)}`} disabled={session.busy || !!editing} onclick={() => { deleting = ''; editing = card; }}>Edit</button>{/if}
                  {#if canDelete}<button type="button" class="mini text" aria-label={`Delete ${titleOf(view, card)}`} disabled={session.busy || !!editing} onclick={() => deleting = card.id}>Delete</button>{/if}
                </div>
              {/if}
              {#if deleting === card.id}
                <div class="confirm" role="group" aria-label="Delete card">Delete “{titleOf(view, card)}”?
                  <button class="btn danger" type="button" disabled={session.busy} onclick={() => remove(card.id)}>Delete</button><button class="btn ok" type="button" onclick={() => deleting = ''}>Keep</button></div>
              {/if}
            </li>
          {:else}<li class="blank">No cards</li>{/each}
        </ul>
        {#if canCreate && column.id !== ''}
          {#if adding === column.id}
            <form class="adder" onsubmit={(e) => add(e, column.id)}>
              <label class="visually-hidden" for={`${uid}-${column.id}`}>Card title</label>
              <input id={`${uid}-${column.id}`} type="text" use:autofocus maxlength="300" placeholder="Card title" bind:value={draftTitle} onkeydown={(e) => { if (e.key === 'Escape') { adding = null; draftTitle = ''; } }} />
              <button class="btn primary" type="submit" disabled={session.busy || !draftTitle.trim()}>Add</button><button class="btn" type="button" onclick={() => { adding = null; draftTitle = ''; }}>Cancel</button>
            </form>
          {:else}<button type="button" class="add" disabled={session.busy} onclick={() => { adding = column.id; draftTitle = ''; }}><Icon name="plus" size={12} />Add a card</button>{/if}
        {/if}
      </div>
    {/each}
  </div>
  <p class="visually-hidden" role="status" aria-live="polite">{announce}</p>
</section>

<style>
  .board { display: grid; gap: 12px; min-width: 0; padding: 16px 18px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); }
  :global(.card) .board { padding: 0; border: 0; border-radius: 0; background: none; box-shadow: none; }
  .top { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; }
  h3 { margin: 0; font-size: 15px; font-weight: 600; }
  .search input { min-height: 34px; width: 210px; max-width: 100%; padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; font-size: 13px; }
  .search input:focus-visible, .adder input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .columns { display: grid; grid-auto-flow: column; grid-auto-columns: minmax(230px, 1fr); gap: 12px; overflow-x: auto; padding-bottom: 6px; align-items: start; }
  .column { display: grid; gap: 8px; align-content: start; min-width: 0; padding: 10px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface-2); box-shadow: var(--tray-sh); }
  .column.over { border-color: var(--accent); box-shadow: 0 0 0 2px var(--tint), var(--tray-sh); }
  header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  h4 { margin: 0; font-size: 12.5px; font-weight: 650; letter-spacing: 0.01em; overflow-wrap: anywhere; }
  .count { min-width: 22px; padding: 0 7px; border-radius: 999px; background: var(--surface); color: var(--text-2); font-size: 12px; text-align: center; border: 1px solid var(--border); }
  ul { display: grid; gap: 8px; margin: 0; padding: 0; list-style: none; }
  .card { display: grid; gap: 6px; padding: 10px 11px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); box-shadow: var(--card-sh); }
  .card[draggable='true'] { cursor: grab; }
  .card.dragging { opacity: 0.45; }
  .card-title { font-size: 13.5px; font-weight: 550; overflow-wrap: anywhere; }
  dl { display: grid; gap: 3px; margin: 0; font-size: 12px; color: var(--text-2); }
  dl div { display: flex; flex-wrap: wrap; gap: 4px 6px; min-width: 0; }
  dt::after { content: ':'; }
  dd { margin: 0; overflow-wrap: anywhere; }
  dd a { color: var(--accent); }
  .chip { padding: 0 8px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-2); font-size: 11.5px; }
  .actions { display: flex; align-items: center; gap: 4px; margin-top: 2px; }
  .gap { flex: 1; }
  .mini { display: inline-grid; place-items: center; min-width: 26px; height: 26px; padding: 0 4px; border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); font: inherit; font-size: 12px; cursor: pointer; }
  .mini.text { padding: 0 8px; }
  .mini:hover:not(:disabled) { border-color: var(--border); background: var(--surface-2); color: var(--text); }
  .mini:disabled { opacity: 0.35; cursor: default; }
  .mini:focus-visible, .add:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .confirm { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 12.5px; }
  .blank { padding: 8px; color: var(--text-2); font-size: 12.5px; text-align: center; border: 1px dashed var(--border); border-radius: var(--radius-sm); }
  .add { display: inline-flex; align-items: center; gap: 6px; padding: 6px 8px; border: 1px dashed var(--border); border-radius: var(--radius-sm); background: transparent; color: var(--text-2); font: inherit; font-size: 12.5px; cursor: pointer; }
  .add:hover:not(:disabled) { border-color: var(--accent); color: var(--accent); }
  .adder { display: grid; gap: 6px; }
  .adder input { min-width: 0; min-height: 34px; padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; font-size: 13px; }
  .adder .btn { justify-self: start; }
  .visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  @media (max-width: 600px) {
    .columns { grid-auto-flow: row; grid-auto-columns: auto; overflow-x: visible; }
    .mini { min-width: 44px; height: 44px; } .add { min-height: 44px; } .adder input, .search input { min-height: 44px; font-size: 16px; } .search input { width: 100%; }
  }
</style>
