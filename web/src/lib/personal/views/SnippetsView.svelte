<script lang="ts">
  // Snippets: commands, queries, templates and runbooks kept to copy, grouped by a category.
  import Icon from '../../Icon.svelte';
  import { copyText } from '../../clipboard';
  import RecordEditor from '../RecordEditor.svelte';
  import { workspaceSession } from '../session.svelte';
  import { matchesSavedFilters, type Collection, type PersonalRecord, type View } from '../schema';
  import { display, titleOf } from './shared';

  let { view, collection }: { view: View; collection: Collection } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const bodyKey = $derived(view.bodyField ?? '');
  const groupField = $derived(collection.fields.find((f) => f.key === view.groupBy));
  const canCreate = $derived((view.operations?.includes('create') ?? false) && !session.readOnly);
  const canUpdate = $derived((view.operations?.includes('update') ?? false) && !session.readOnly);
  const canDelete = $derived((view.operations?.includes('delete') ?? false) && !session.readOnly);
  const records = $derived((session.records[view.collectionId] ?? []).filter((r) => matchesSavedFilters(r.values, view.filters)));
  const extras = $derived(view.fields.slice(1).filter((key) => key !== view.groupBy && key !== bodyKey).map((key) => collection.fields.find((f) => f.key === key)!).filter((f) => f && f.type !== 'longtext'));
  let search = $state('');
  let group = $state('');
  let editing = $state<PersonalRecord | 'new' | null>(null);
  let deleting = $state('');
  let copied = $state('');

  const shown = $derived.by(() => {
    const needle = search.trim().toLocaleLowerCase();
    return records
      .filter((r) => (!group || r.values[view.groupBy ?? ''] === group)
        && (!needle || titleOf(view, r).toLocaleLowerCase().includes(needle) || String(r.values[bodyKey] ?? '').toLocaleLowerCase().includes(needle)))
      .sort((a, b) => titleOf(view, a).localeCompare(titleOf(view, b), undefined, { numeric: true, sensitivity: 'base' }));
  });
  async function copy(record: PersonalRecord) {
    if (await copyText(String(record.values[bodyKey] ?? ''))) { copied = record.id; setTimeout(() => { if (copied === record.id) copied = ''; }, 1600); }
  }
  async function remove(id: string) { if (await session.mutate(view.id, 'delete', id)) deleting = ''; }
</script>

<section class="snippets" aria-label={view.name} data-view-kind="snippets">
  <div class="top">
    <h3>{view.name}</h3>
    {#if canCreate}<button class="btn primary" type="button" disabled={session.busy || editing !== null} onclick={() => editing = 'new'} data-testid="snippet-new"><Icon name="plus" size={14} />New snippet</button>{/if}
  </div>
  <div class="tools">
    <label class="search" for={`${uid}-search`}><span class="visually-hidden">Search snippets</span><input id={`${uid}-search`} type="search" placeholder="Search snippets" bind:value={search} /></label>
    {#if groupField}
      <div class="groups" role="group" aria-label={`Filter by ${groupField.name}`}>
        <button type="button" class="group" class:on={!group} aria-pressed={!group} onclick={() => group = ''}>All</button>
        {#each groupField.options ?? [] as option (option)}<button type="button" class="group" class:on={group === option} aria-pressed={group === option} onclick={() => group = group === option ? '' : option}>{option}</button>{/each}
      </div>
    {/if}
  </div>
  {#if editing}{#key editing === 'new' ? 'new' : editing.id}<RecordEditor {collection} {view} record={editing === 'new' ? undefined : editing} onclose={() => editing = null} />{/key}{/if}
  <p class="count">{shown.length} of {records.length} snippets</p>
  <ul>
    {#each shown as snippet (snippet.id)}
      <li class="snippet">
        <header>
          <strong>{titleOf(view, snippet)}</strong>
          {#if view.groupBy && typeof snippet.values[view.groupBy] === 'string'}<span class="lang">{snippet.values[view.groupBy]}</span>{/if}
          <button class="btn" type="button" aria-label={`Copy ${titleOf(view, snippet)}`} onclick={() => copy(snippet)}><Icon name="copy" size={14} />{copied === snippet.id ? 'Copied' : 'Copy'}</button>
        </header>
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <pre tabindex="0" aria-label={`${titleOf(view, snippet)} text`}><code>{String(snippet.values[bodyKey] ?? '')}</code></pre>
        {#if extras.some((f) => snippet.values[f.key] !== undefined)}
          <dl>{#each extras as field (field.key)}{#if snippet.values[field.key] !== undefined}
            <div><dt>{field.name}</dt><dd>{#if field.type === 'url'}<a href={String(snippet.values[field.key])} target="_blank" rel="noopener noreferrer">{String(snippet.values[field.key])}</a>{:else}{display(snippet.values[field.key])}{/if}</dd></div>{/if}{/each}</dl>
        {/if}
        {#if canUpdate || canDelete}
          <footer>
            {#if canUpdate}<button class="btn" type="button" aria-label={`Edit ${titleOf(view, snippet)}`} disabled={session.busy || editing !== null} onclick={() => { deleting = ''; editing = snippet; }}>Edit</button>{/if}
            {#if canDelete}
              {#if deleting === snippet.id}
                <span class="confirm" role="group" aria-label="Delete snippet">Delete “{titleOf(view, snippet)}”?
                  <button class="btn danger" type="button" disabled={session.busy} onclick={() => remove(snippet.id)}>Delete</button><button class="btn ok" type="button" onclick={() => deleting = ''}>Keep</button></span>
              {:else}<button class="btn danger" type="button" aria-label={`Delete ${titleOf(view, snippet)}`} disabled={session.busy || editing !== null} onclick={() => deleting = snippet.id}>Delete</button>{/if}
            {/if}
          </footer>
        {/if}
      </li>
    {:else}
      <li class="none">{records.length ? 'No snippets match.' : 'No snippets yet.'}</li>
    {/each}
  </ul>
</section>

<style>
  .snippets { display: grid; gap: 12px; min-width: 0; padding: 16px 18px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); }
  :global(.card) .snippets { padding: 0; border: 0; border-radius: 0; background: none; box-shadow: none; }
  .top, .tools { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; }
  h3 { margin: 0; font-size: 16px; font-weight: 600; }
  .search input { min-height: 34px; width: 240px; max-width: 100%; padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; font-size: 13px; }
  .search input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .groups { display: flex; flex-wrap: wrap; gap: 5px; }
  .group { padding: 2px 10px; min-height: 26px; border: 1px solid var(--border); border-radius: 999px; background: transparent; color: var(--text-2); font: inherit; font-size: 12px; cursor: pointer; }
  .group.on { background: var(--tint); border-color: var(--accent); color: var(--accent); font-weight: 600; }
  .count { margin: 0; color: var(--text-2); font-size: 12px; }
  ul { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr)); gap: 12px; margin: 0; padding: 0; list-style: none; }
  .snippet { display: grid; gap: 8px; align-content: start; min-width: 0; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); }
  header { display: flex; align-items: center; gap: 8px; }
  header strong { flex: 1; min-width: 0; font-size: 14px; font-weight: 600; overflow-wrap: anywhere; }
  .lang { padding: 1px 9px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-2); color: var(--text-2); font-family: var(--mono); font-size: 12px; }
  pre { margin: 0; max-height: 220px; overflow: auto; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); font-family: var(--mono); font-size: 13px; line-height: 1.55; tab-size: 2; white-space: pre; }
  pre:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  dl { display: flex; flex-wrap: wrap; gap: 4px 14px; margin: 0; color: var(--text-2); font-size: 12px; }
  dl div { display: flex; gap: 5px; } dt::after { content: ':'; } dd { margin: 0; overflow-wrap: anywhere; } dd a { color: var(--accent); }
  footer { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
  .confirm { display: inline-flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 13px; }
  .none { grid-column: 1 / -1; padding: 18px 0; color: var(--text-2); font-size: 13px; }
  .visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  @media (max-width: 600px) { .search input { width: 100%; min-height: 44px; font-size: 16px; } .snippet :global(.btn) { min-height: 44px; } .group { min-height: 36px; } }
</style>
