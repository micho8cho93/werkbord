<script lang="ts">
  // Pages: a tree of notes, each a document of its own, linked to each other with [[Page title]]. A small knowledge base.
  import { tick } from 'svelte';
  import Icon from '../../Icon.svelte';
  import { workspaceSession } from '../session.svelte';
  import { matchesSavedFilters, type Collection, type PersonalRecord, type View } from '../schema';
  import DocEditor from './DocEditor.svelte';
  import { buildTree, descendantIds, pageTitle, pagesLinkingTo, type TreeNode } from './pages';
  import { excerpt, newRecordValues, titleOf, untitled } from './shared';

  let { view, collection }: { view: View; collection: Collection } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const parentKey = $derived(view.parentField ?? '');
  const bodyKey = $derived(view.bodyField ?? '');
  const canCreate = $derived((view.operations?.includes('create') ?? false) && !session.readOnly);
  const canUpdate = $derived((view.operations?.includes('update') ?? false) && !session.readOnly);
  const tagField = $derived(view.fields.map((key) => collection.fields.find((f) => f.key === key)).find((f) => f?.type === 'multiselect' || f?.type === 'select'));
  const all = $derived((session.records[view.collectionId] ?? []).filter((r) => matchesSavedFilters(r.values, view.filters)));
  const byId = $derived(new Map(all.map((r) => [r.id, r])));
  const tree = $derived(buildTree(all, parentKey, (r) => titleOf(view, r)));

  let search = $state('');
  let tag = $state('');
  let chosen = $state('');
  let collapsed = $state<Record<string, boolean>>({});
  let listOpen = $state(false);
  let missing = $state('');
  let editor: ReturnType<typeof DocEditor> | undefined = $state();

  const matches = $derived.by(() => {
    const needle = search.trim().toLocaleLowerCase();
    return all.filter((r) => {
      if (tag && tagField) {
        const value = r.values[tagField.key];
        if (!(Array.isArray(value) ? value.includes(tag) : value === tag)) return false;
      }
      return !needle || titleOf(view, r).toLocaleLowerCase().includes(needle) || String(r.values[bodyKey] ?? '').toLocaleLowerCase().includes(needle);
    });
  });
  const filtering = $derived(!!search.trim() || !!tag);
  const current = $derived<PersonalRecord | undefined>(byId.get(chosen) ?? tree.roots[0]?.record);
  const node = $derived(current ? tree.nodes.get(current.id) : undefined);
  const trail = $derived(current ? tree.trail(current.id) : []);
  const children = $derived(node?.children ?? []);
  const linked = $derived(current ? pagesLinkingTo(all, bodyKey, titleOf(view, current), current.id) : []);
  const titles = $derived(new Set(all.map((r) => titleOf(view, r).toLocaleLowerCase())));
  const titlesKey = $derived([...titles].sort().join('\u0000'));
  const parents = $derived(current ? all.filter((r) => r.id !== current.id && !descendantIds(tree, current.id).has(r.id)).sort((a, b) => titleOf(view, a).localeCompare(titleOf(view, b))) : []);

  $effect(() => { void titlesKey; editor?.refreshLinks(); });

  async function open(id: string) {
    missing = '';
    if (current && current.id === id) { listOpen = false; return; }
    await editor?.flush();
    chosen = id;
    listOpen = false;
    // Show where the page sits in the tree.
    for (const ancestor of tree.trail(id)) collapsed[ancestor.id] = false;
  }
  async function create(parentId = '', title = untitled) {
    await editor?.flush();
    const given: Record<string, string> = { [view.fields[0]]: title };
    if (parentId && parentKey) given[parentKey] = parentId;
    if (!await session.mutate(view.id, 'create', undefined, newRecordValues(collection, view, given)) || !session.lastSaved) return false;
    chosen = session.lastSaved.id; search = ''; tag = ''; missing = ''; listOpen = false;
    if (parentId) collapsed[parentId] = false;
    await tick();
    if (title === untitled) editor?.focusTitle();
    return true;
  }
  async function remove() {
    if (!current) return;
    const gone = current;
    const up = (parentKey && typeof gone.values[parentKey] === 'string' ? (gone.values[parentKey] as string) : '');
    const fallback = byId.has(up) ? up : '';
    // Its sub-pages move up one level, so deleting a page never deletes what was written beneath it.
    for (const child of children) {
      if (!await session.mutate(view.id, 'update', child.record.id, fallback ? { [parentKey]: fallback } : {}, fallback ? [] : [parentKey])) return;
    }
    if (await session.mutate(view.id, 'delete', gone.id)) chosen = fallback || tree.roots.find((n) => n.record.id !== gone.id)?.record.id || '';
  }
  async function move(event: Event & { currentTarget: HTMLSelectElement }) {
    if (!current || !parentKey) return;
    const value = event.currentTarget.value;
    await editor?.flush();
    await session.mutate(view.id, 'update', current.id, value ? { [parentKey]: value } : {}, value ? [] : [parentKey]);
  }
  /** A [[link]] was followed: go to the page it names, or offer to write it. */
  function follow(title: string) {
    const target = all.find((r) => titleOf(view, r).toLocaleLowerCase() === title.toLocaleLowerCase());
    if (target) void open(target.id); else missing = title;
  }
</script>

<section class="pages" aria-label={view.name} data-view-kind="pages">
  <div class="shell">
    <aside class="side" class:open={listOpen} aria-label={`${view.name}: page tree`}>
      <div class="head">
        <h3>{view.name}</h3>
        {#if canCreate}<button class="btn primary" type="button" disabled={session.busy} onclick={() => create()} data-testid="page-new"><Icon name="plus" size={14} />New page</button>{/if}
      </div>
      <label class="search" for={`${uid}-search`}><span class="visually-hidden">Search pages</span>
        <input id={`${uid}-search`} type="search" placeholder="Search pages" bind:value={search} /></label>
      {#if tagField}
        <div class="tags" role="group" aria-label={`Filter by ${tagField.name}`}>
          <button type="button" class="tag" class:on={!tag} aria-pressed={!tag} onclick={() => tag = ''}>All</button>
          {#each tagField.options ?? [] as option (option)}<button type="button" class="tag" class:on={tag === option} aria-pressed={tag === option} onclick={() => tag = tag === option ? '' : option}>{option}</button>{/each}
        </div>
      {/if}
      {#if filtering}
        <ul class="results">
          {#each matches.sort((a, b) => titleOf(view, a).localeCompare(titleOf(view, b))) as page (page.id)}
            <li><button type="button" class="row flat" class:current={page.id === current?.id} onclick={() => open(page.id)}><strong>{titleOf(view, page)}</strong><span>{excerpt(page.values[bodyKey], 70)}</span></button></li>
          {:else}<li class="none">No pages match.</li>{/each}
        </ul>
      {:else}
        <ul class="tree" role="tree" aria-label="Pages">
          {#each tree.roots as root (root.record.id)}{@render branch(root, 0)}{:else}<li class="none">No pages yet.</li>{/each}
        </ul>
      {/if}
    </aside>

    <div class="main">
      <button class="btn list-toggle" type="button" aria-expanded={listOpen} onclick={() => listOpen = !listOpen}><Icon name="left" size={14} />All pages ({all.length})</button>
      {#if missing}
        <div class="missing" role="status">There is no page called “{missing}”.
          {#if canCreate}<button class="btn primary" type="button" onclick={() => create('', missing)}>Create it</button>{/if}
          <button class="btn" type="button" onclick={() => missing = ''}>Dismiss</button></div>
      {/if}
      {#if current}
        {#key current.id}
          <DocEditor bind:this={editor} {view} {collection} record={current} ondelete={remove} onwiki={follow} wikiExists={(title) => titles.has(title.toLocaleLowerCase())}
            deleteNote={children.length ? `Its ${children.length} sub-${children.length === 1 ? 'page moves' : 'pages move'} up one level.` : ''}>
            {#snippet before()}
              {#if trail.length > 1 || parentKey}
                <nav class="crumbs" aria-label="Where this page sits">
                  {#each trail.slice(0, -1) as ancestor (ancestor.id)}<button type="button" onclick={() => open(ancestor.id)}>{titleOf(view, ancestor)}</button><span aria-hidden="true">/</span>{/each}
                  <span class="here">{pageTitle(current, view.fields[0])}</span>
                  {#if parentKey && canUpdate}
                    <label class="parent">Move under
                      <select value={trail.length > 1 ? trail[trail.length - 2].id : ''} disabled={session.busy} onchange={move}>
                        <option value="">Top level</option>
                        {#each parents as page (page.id)}<option value={page.id}>{titleOf(view, page)}</option>{/each}
                      </select></label>
                  {/if}
                </nav>
              {/if}
            {/snippet}
            {#snippet after()}
              {#if canCreate && parentKey}<div><button class="btn add" type="button" disabled={session.busy} onclick={() => create(current.id)}><Icon name="plus" size={14} />Add a sub-page</button></div>{/if}
              {#if children.length}
                <section class="related" aria-label="Sub-pages"><h4>Sub-pages</h4>
                  <ul>{#each children as child (child.record.id)}<li><button type="button" onclick={() => open(child.record.id)}>{titleOf(view, child.record)}</button></li>{/each}</ul></section>
              {/if}
              {#if linked.length}
                <section class="related" aria-label="Linked from"><h4>Linked from</h4>
                  <ul>{#each linked as page (page.id)}<li><button type="button" onclick={() => open(page.id)}>{titleOf(view, page)}</button></li>{/each}</ul></section>
              {/if}
            {/snippet}
          </DocEditor>
        {/key}
      {:else}
        <div class="empty"><h4>Start a knowledge base</h4><p>Pages can sit inside pages, carry tags, and link to each other with <code>[[Page title]]</code>.</p>{#if canCreate}<button class="btn primary" type="button" onclick={() => create()}><Icon name="plus" size={14} />New page</button>{/if}</div>
      {/if}
    </div>
  </div>
</section>

{#snippet branch(item: TreeNode, depth: number)}
  {@const open_ = !collapsed[item.record.id]}
  <li role="treeitem" aria-expanded={item.children.length ? open_ : undefined} aria-selected={item.record.id === current?.id}>
    <div class="row" class:current={item.record.id === current?.id} style:padding-left={`${6 + depth * 14}px`}>
      {#if item.children.length}
        <button type="button" class="twirl" aria-label={open_ ? `Collapse ${titleOf(view, item.record)}` : `Expand ${titleOf(view, item.record)}`} onclick={() => collapsed[item.record.id] = open_}><span class:open={open_}><Icon name="right" size={12} /></span></button>
      {:else}<span class="twirl blank"></span>{/if}
      <button type="button" class="name" onclick={() => open(item.record.id)}>{titleOf(view, item.record)}</button>
      {#if canCreate && parentKey}<button type="button" class="add" aria-label={`Add a sub-page to ${titleOf(view, item.record)}`} title="Add a sub-page" disabled={session.busy} onclick={() => create(item.record.id)}><Icon name="plus" size={12} /></button>{/if}
    </div>
    {#if item.children.length && open_}<ul role="group">{#each item.children as child (child.record.id)}{@render branch(child, depth + 1)}{/each}</ul>{/if}
  </li>
{/snippet}

<style>
  .pages { min-width: 0; }
  .shell { display: grid; grid-template-columns: minmax(220px, 280px) minmax(0, 1fr); gap: 20px; align-items: start; }
  .side { display: grid; gap: 10px; min-width: 0; position: sticky; top: 12px; max-height: calc(100vh - 40px); overflow: auto; padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  h3 { margin: 0; font-size: 14px; font-weight: 600; overflow-wrap: anywhere; }
  .search input { width: 100%; min-width: 0; min-height: 34px; padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; font-size: 13px; }
  .search input:focus-visible, select:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .tags { display: flex; flex-wrap: wrap; gap: 5px; }
  .tag { padding: 2px 9px; min-height: 24px; border: 1px solid var(--border); border-radius: 999px; background: transparent; color: var(--text-2); font: inherit; font-size: 12px; cursor: pointer; }
  .tag.on { background: var(--tint); border-color: var(--accent); color: var(--accent); font-weight: 600; }
  ul { margin: 0; padding: 0; list-style: none; }
  .row { display: flex; align-items: center; gap: 2px; min-height: 30px; padding-right: 4px; border-radius: var(--radius-sm); }
  .row:hover { background: var(--surface-2); }
  .row.current { background: var(--tint); box-shadow: inset 2px 0 0 var(--accent); }
  .twirl { display: inline-grid; place-items: center; flex: none; width: 22px; height: 22px; padding: 0; border: 0; background: transparent; color: var(--text-2); cursor: pointer; }
  .twirl.blank { cursor: default; }
  .twirl span { display: inline-grid; transition: transform 0.12s var(--ease); }
  .twirl span.open { transform: rotate(90deg); }
  .name { flex: 1; min-width: 0; padding: 4px 4px; border: 0; background: transparent; color: var(--text); font: inherit; font-size: 13px; text-align: left; cursor: pointer; overflow-wrap: anywhere; }
  .row.current .name { font-weight: 600; }
  .add { display: inline-grid; place-items: center; flex: none; width: 24px; height: 24px; padding: 0; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); cursor: pointer; opacity: 0; }
  .row:hover .add, .row:focus-within .add, .add:focus-visible { opacity: 1; }
  .add:hover { background: var(--surface); color: var(--accent); }
  .name:focus-visible, .twirl:focus-visible, .add:focus-visible, .flat:focus-visible { outline: 2px solid var(--accent); outline-offset: -1px; }
  .results { display: grid; gap: 2px; }
  .flat { display: grid; gap: 2px; width: 100%; padding: 7px 8px; border: 0; background: transparent; color: var(--text); font: inherit; text-align: left; cursor: pointer; border-radius: var(--radius-sm); }
  .flat:hover { background: var(--surface-2); }
  .flat span { color: var(--text-2); font-size: 12px; }
  .none { padding: 12px 6px; color: var(--text-2); font-size: 13px; }
  .main { display: grid; gap: 12px; min-width: 0; align-content: start; }
  .list-toggle { display: none; justify-self: start; }
  .crumbs { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 6px; color: var(--text-2); font-size: 13px; }
  .crumbs button { padding: 0; border: 0; background: none; color: var(--text-2); font: inherit; cursor: pointer; text-decoration: none; }
  .crumbs button:hover { color: var(--accent); }
  .here { color: var(--text); font-weight: 600; }
  .parent { display: inline-flex; align-items: center; gap: 6px; margin-left: auto; }
  .parent select { min-height: 28px; max-width: 220px; padding: 2px 6px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; font-size: 13px; }
  .missing { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--tint); font-size: 13px; }
  .related { display: grid; gap: 6px; padding-top: 10px; border-top: 1px solid var(--border); }
  .related h4 { margin: 0; color: var(--text-2); font-size: 12px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; }
  .related ul { display: flex; flex-wrap: wrap; gap: 6px; }
  .related button { padding: 3px 10px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-2); color: var(--accent); font: inherit; font-size: 13px; cursor: pointer; }
  .related button:hover { border-color: var(--accent); }
  .empty { display: grid; justify-items: start; gap: 8px; padding: 30px 4px; }
  .empty h4 { margin: 0; font-size: 20px; }
  .empty p { margin: 0; color: var(--text-2); }
  .visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  @media (max-width: 760px) {
    .shell { grid-template-columns: minmax(0, 1fr); gap: 10px; }
    .side { display: none; position: static; max-height: 60vh; }
    .side.open { display: grid; }
    .list-toggle { display: inline-flex; }
    .add { opacity: 1; width: 36px; height: 36px; }
    .row { min-height: 44px; }
    .parent { margin-left: 0; }
    .search input { min-height: 44px; font-size: 16px; }
  }
</style>
