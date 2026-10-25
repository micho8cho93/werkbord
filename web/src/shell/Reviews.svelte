<script lang="ts">
  import { model } from './model.svelte';
  import { wheres } from './analytics';
  import { KIND_LABEL, reviewRows, type ReviewKind } from './reviews';
  import WorkspaceTag from './WorkspaceTag.svelte';

  // One list of everything waiting on a review, from Personal and from every team. Choose where to look and which kind; each row opens
  // in the workspace it belongs to, where the review itself is done.
  const kinds: { id: 'all' | ReviewKind; label: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'to_review', label: KIND_LABEL.to_review },
    { id: 'changes', label: KIND_LABEL.changes },
    { id: 'mine', label: KIND_LABEL.mine },
  ];
  let where = $state('all');
  let kind = $state<'all' | ReviewKind>('all');

  const places = $derived(wheres(model.view?.items));
  $effect(() => {
    if (!places.some((p) => p.id === where)) where = 'all';
  });
  const all = $derived(model.overview ? reviewRows(model.overview, where) : []);
  const rows = $derived(kind === 'all' ? all : all.filter((r) => r.kind === kind));
  const countOf = (k: 'all' | ReviewKind): number => (k === 'all' ? all.length : all.filter((r) => r.kind === k).length);
  const silent = $derived((model.overview?.entries ?? []).filter((e) => (where === 'all' || e.workspace.id === where) && (e.error || !e.summary)));
</script>

<section class="reviews" aria-labelledby="reviews-h">
  <header>
    <h1 id="reviews-h">Reviews</h1>
    <p class="lede">Work waiting on a review, across Personal and your teams.</p>
  </header>

  <div class="filters">
    <label class="pick"><span>Where</span>
      <select bind:value={where} aria-label="Reviews place" data-testid="reviews-where">
        {#each places as p (p.id)}<option value={p.id}>{p.label}</option>{/each}
      </select>
    </label>
    <div class="tabs" role="group" aria-label="Kind of review">
      {#each kinds as k (k.id)}
        <button type="button" aria-pressed={kind === k.id} onclick={() => (kind = k.id)} data-kind={k.id}>{k.label}<span class="n">{countOf(k.id)}</span></button>
      {/each}
    </div>
  </div>

  {#each silent as e (e.workspace.id)}
    <p class="notice" role="status"><WorkspaceTag workspace={e.workspace} /> Current status unavailable{e.error ? `: ${e.error}` : ''}. Its reviews are left out.</p>
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
</section>

<style>
  .reviews { display: flex; flex-direction: column; gap: 16px; padding: 24px clamp(16px, 3vw, 40px) 32px; height: 100%; overflow: auto; }
  .reviews > :global(*) { flex: none; }
  h1 { margin: 0; }
  .lede { margin: 4px 0 0; color: var(--text-2); }
  .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
  .tabs { display: inline-flex; flex-wrap: wrap; margin-left: auto; padding: 2px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface-2); }
  .tabs button { display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 12px; border: 0; border-radius: 4px; background: transparent; color: var(--text-2); font: inherit; font-size: 13px; font-weight: 500; cursor: pointer; }
  .tabs button[aria-pressed='true'] { background: var(--surface); box-shadow: 0 0 0 1px var(--border); color: var(--text); }
  .n { color: var(--text-2); font-family: var(--mono); font-size: 12px; }
  .list { margin: 0; padding: 0; list-style: none; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
  .row { display: flex; align-items: center; gap: 14px; padding: 12px 14px; border-bottom: 1px solid var(--border); }
  .row:last-child { border-bottom: 0; }
  .text { display: grid; gap: 4px; min-width: 0; flex: 1; }
  .title { font-weight: 600; overflow-wrap: anywhere; }
  .context { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; color: var(--text-2); font-size: 12px; }
  .kind { padding: 0 8px; border-radius: 99px; background: var(--surface-2); }
  .detail { color: var(--text-2); font-size: 12px; }
  .notice { padding: 12px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface-2); font-size: 13px; }
  .empty { color: var(--text-2); }
  p { margin: 0; }
  @media (max-width: 899px) { .reviews { height: auto; overflow: visible; padding: 16px 16px 24px; } .tabs { margin-left: 0; } }
</style>
