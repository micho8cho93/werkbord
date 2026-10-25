<script lang="ts">
  import { model } from './model.svelte';
  import { KIND_LABEL, reviewRows, type ReviewKind } from './reviews';
  import FilterBar from './FilterBar.svelte';
  import Notice from './Notice.svelte';
  import PageFrame from './PageFrame.svelte';
  import WherePicker from './WherePicker.svelte';
  import WorkspaceTag from './WorkspaceTag.svelte';
  import Segmented from '../lib/Segmented.svelte';

  // One list of everything waiting on a review, from Personal and from every team. Choose where to look and which kind; each row opens
  // in the workspace it belongs to, where the review itself is done.
  const kindIds: ('all' | ReviewKind)[] = ['all', 'to_review', 'changes', 'mine'];
  let where = $state('all');
  let kind = $state<'all' | ReviewKind>('all');

  const all = $derived(model.overview ? reviewRows(model.overview, where) : []);
  const rows = $derived(kind === 'all' ? all : all.filter((r) => r.kind === kind));
  const countOf = (k: 'all' | ReviewKind): number => (k === 'all' ? all.length : all.filter((r) => r.kind === k).length);
  const kinds = $derived(kindIds.map((id) => ({ id, label: id === 'all' ? 'All' : KIND_LABEL[id], count: countOf(id) })));
  const silent = $derived((model.overview?.entries ?? []).filter((e) => (where === 'all' || e.workspace.id === where) && (e.error || !e.summary)));
</script>

<PageFrame id="reviews-h" title="Reviews" lede="Work waiting on a review, across Personal and your teams.">
  <FilterBar>
    <WherePicker bind:where label="Reviews place" testid="reviews-where" />
    <Segmented compact options={kinds} value={kind} onchange={(id) => (kind = id)} label="Kind of review" />
  </FilterBar>

  {#each silent as e (e.workspace.id)}
    <Notice><WorkspaceTag workspace={e.workspace} /> Current status unavailable{e.error ? `: ${e.error}` : ''}. Its reviews are left out.</Notice>
  {/each}

  {#if !model.overview}
    <p class="empty" role="status">Looking…</p>
  {:else if rows.length === 0}
    <p class="empty" data-testid="reviews-empty">{kind === 'all' ? 'Nothing is waiting on a review.' : `Nothing in “${KIND_LABEL[kind]}”.`}</p>
  {:else}
    <ul class="list">
      {#each rows as r (r.workspace.id + r.id)}
        <li class="row" data-kind={r.kind} data-testid="review-item">
          <span class="text">
            <span class="title">{r.title}</span>
            <span class="context"><WorkspaceTag workspace={r.workspace} />{#if r.project}<span>{r.project}</span>{/if}<span class="kind">{KIND_LABEL[r.kind]}</span></span>
            {#if r.detail}<span class="detail">{r.detail}</span>{/if}
          </span>
          <button class="btn" class:primary={r.kind === 'to_review'} type="button" onclick={() => model.goTo(r.workspace.id, r.href)} aria-label="Open: {r.title}">Open</button>
        </li>
      {/each}
    </ul>
  {/if}
</PageFrame>

<style>
  .empty { margin: 0; color: var(--text-2); }
  .list { margin: 0; padding: 0; list-style: none; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
  .row { display: flex; align-items: center; gap: 14px; padding: 12px 14px; border-bottom: 1px solid var(--border); }
  .row:last-child { border-bottom: 0; }
  .text { display: grid; gap: 4px; min-width: 0; flex: 1; }
  .title { font-weight: 600; overflow-wrap: anywhere; }
  .context { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; color: var(--text-2); font-size: 12px; }
  .kind { padding: 0 8px; border-radius: 99px; background: var(--surface-2); }
  .detail { color: var(--text-2); font-size: 12px; }
</style>
