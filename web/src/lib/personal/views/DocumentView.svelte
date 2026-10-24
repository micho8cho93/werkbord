<script lang="ts">
  // Documents: a list of what has been written, and a word processor for the one that is open.
  import { tick } from 'svelte';
  import Icon from '../../Icon.svelte';
  import { workspaceSession } from '../session.svelte';
  import { matchesSavedFilters, type Collection, type PersonalRecord, type View } from '../schema';
  import DocEditor from './DocEditor.svelte';
  import { excerpt, newRecordValues, relativeTime, titleOf, untitled } from './shared';

  let { view, collection }: { view: View; collection: Collection } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const canCreate = $derived((view.operations?.includes('create') ?? false) && !session.readOnly);
  const tagField = $derived(view.fields.map((key) => collection.fields.find((f) => f.key === key)).find((f) => f?.type === 'multiselect' || f?.type === 'select'));
  const all = $derived((session.records[view.collectionId] ?? []).filter((r) => matchesSavedFilters(r.values, view.filters)));
  let search = $state('');
  let tag = $state('');
  let chosen = $state('');
  let listOpen = $state(false);
  let editor: ReturnType<typeof DocEditor> | undefined = $state();

  const documents = $derived.by(() => {
    const needle = search.trim().toLocaleLowerCase();
    return all
      .filter((r) => {
        if (tag && tagField) {
          const value = r.values[tagField.key];
          if (!(Array.isArray(value) ? value.includes(tag) : value === tag)) return false;
        }
        return !needle || titleOf(view, r).toLocaleLowerCase().includes(needle) || String(r.values[view.bodyField ?? ''] ?? '').toLocaleLowerCase().includes(needle);
      })
      .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt) || a.id.localeCompare(b.id));
  });
  const current = $derived<PersonalRecord | undefined>(all.find((r) => r.id === chosen) ?? documents[0]);

  async function open(id: string) {
    if (current && current.id === id) { listOpen = false; return; }
    await editor?.flush();
    chosen = id;
    listOpen = false;
  }
  async function create() {
    await editor?.flush();
    const made = await session.mutate(view.id, 'create', undefined, newRecordValues(collection, view, { [view.fields[0]]: untitled }));
    if (!made || !session.lastSaved) return;
    chosen = session.lastSaved.id; search = ''; tag = ''; listOpen = false;
    await tick();
    editor?.focusTitle();
  }
  async function remove() {
    if (!current) return;
    const gone = current.id;
    const next = documents.find((r) => r.id !== gone);
    if (await session.mutate(view.id, 'delete', gone)) chosen = next?.id ?? '';
  }
</script>

<section class="docs" aria-label={view.name} data-view-kind="document">
  <div class="shell">
    <aside class="list" class:open={listOpen} aria-label={`${view.name}: documents`}>
      <div class="list-head">
        <h3>{view.name}</h3>
        {#if canCreate}<button class="btn primary" type="button" disabled={session.busy} onclick={create} data-testid="document-new"><Icon name="plus" size={14} />New</button>{/if}
      </div>
      <label class="search" for={`${uid}-search`}><span class="visually-hidden">Search documents</span>
        <input id={`${uid}-search`} type="search" placeholder="Search documents" bind:value={search} /></label>
      {#if tagField}
        <div class="tags" role="group" aria-label={`Filter by ${tagField.name}`}>
          <button type="button" class="tag" class:on={!tag} aria-pressed={!tag} onclick={() => tag = ''}>All</button>
          {#each tagField.options ?? [] as option (option)}<button type="button" class="tag" class:on={tag === option} aria-pressed={tag === option} onclick={() => tag = tag === option ? '' : option}>{option}</button>{/each}
        </div>
      {/if}
      <ul>
        {#each documents as doc (doc.id)}
          <li><button type="button" class="item" class:current={doc.id === current?.id} aria-current={doc.id === current?.id ? 'true' : undefined} onclick={() => open(doc.id)}>
            <strong>{titleOf(view, doc)}</strong>
            <span class="taste">{excerpt(doc.values[view.bodyField ?? ''], 90) || 'Empty'}</span>
            <span class="when">{relativeTime(doc.updatedAt)}</span>
          </button></li>
        {:else}
          <li class="none">{all.length ? 'No documents match.' : 'No documents yet.'}</li>
        {/each}
      </ul>
    </aside>
    <div class="page">
      <button class="btn list-toggle" type="button" aria-expanded={listOpen} onclick={() => listOpen = !listOpen}><Icon name="left" size={14} />All documents ({all.length})</button>
      {#if current}
        {#key current.id}<DocEditor bind:this={editor} {view} {collection} record={current} ondelete={remove} />{/key}
      {:else}
        <div class="empty"><h4>Write something</h4><p>Documents are saved as you type and stay on this computer.</p>{#if canCreate}<button class="btn primary" type="button" onclick={create}><Icon name="plus" size={14} />New document</button>{/if}</div>
      {/if}
    </div>
  </div>
</section>

<style>
  .docs { min-width: 0; }
  .shell { display: grid; grid-template-columns: minmax(200px, 260px) minmax(0, 1fr); gap: 20px; align-items: start; }
  .list { display: grid; gap: 10px; min-width: 0; position: sticky; top: 12px; max-height: calc(100vh - 40px); overflow: auto; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); }
  .list-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  h3 { margin: 0; font-size: 14px; font-weight: 600; overflow-wrap: anywhere; }
  .search input { width: 100%; min-width: 0; min-height: 34px; padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; font-size: 13px; }
  .search input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .tags { display: flex; flex-wrap: wrap; gap: 5px; }
  .tag { padding: 2px 9px; min-height: 24px; border: 1px solid var(--border); border-radius: 999px; background: transparent; color: var(--text-2); font: inherit; font-size: 12px; cursor: pointer; }
  .tag.on { background: var(--tint); border-color: var(--accent); color: var(--accent); font-weight: 600; }
  ul { display: grid; gap: 2px; margin: 0; padding: 0; list-style: none; }
  .item { display: grid; gap: 2px; width: 100%; padding: 8px 10px; border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: var(--text); font: inherit; text-align: left; cursor: pointer; }
  .item:hover { background: var(--surface-2); }
  .item.current { background: var(--tint); border-color: var(--accent); }
  .item:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .item strong { font-size: 13px; font-weight: 600; overflow-wrap: anywhere; }
  .taste { color: var(--text-2); font-size: 12px; line-height: 1.4; overflow-wrap: anywhere; }
  .when { color: var(--text-2); font-size: 11.5px; }
  .none { padding: 14px 6px; color: var(--text-2); font-size: 13px; }
  .page { min-width: 0; }
  .list-toggle { display: none; margin-bottom: 10px; }
  .empty { display: grid; justify-items: start; gap: 8px; padding: 30px 4px; }
  .empty h4 { margin: 0; font-size: 18px; }
  .empty p { margin: 0; color: var(--text-2); }
  .visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  @media (max-width: 760px) {
    .shell { grid-template-columns: minmax(0, 1fr); gap: 10px; }
    .list { display: none; position: static; max-height: 60vh; }
    .list.open { display: grid; }
    .list-toggle { display: inline-flex; }
    .item { min-height: 44px; }
    .search input { min-height: 44px; font-size: 16px; }
  }
</style>
