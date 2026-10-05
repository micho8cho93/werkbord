<script lang="ts">
  import { timeAgo } from '../format';
  import { headlineTone, housekeepingLine, splitFindings } from '../health';
  import { app } from '../state.svelte';
  import type { Project } from '../types';
  import FindingCard from './FindingCard.svelte';
  import type { GitStore } from './store.svelte';

  // The first thing on the Git screen: is anything wrong with the Git state the agents have produced,
  // and if so what. A headline says Healthy or what is wrong; the findings say why and what to do.
  // The score is a footnote: a number cannot say what is wrong.

  let { project, store }: { project: Project; store: GitStore } = $props();

  const h = $derived(store.health);
  const split = $derived(splitFindings(h));
  const tone = $derived(headlineTone(h));
  const tidy = $derived(housekeepingLine(split.housekeeping.length));
</script>

<section class="card g-card health" aria-label="Repository health" data-state={h?.summary.state ?? 'unknown'}>
  <header class="head">
    <div class="g-wrap">
      <h2>Repository health</h2>
      {#if h}
        <p class="headline" data-tone={tone}>{h.summary.headline}</p>
      {:else if store.healthError}
        <p class="headline" data-tone="neutral">Could not be checked</p>
      {:else}
        <p class="headline muted">Checking…</p>
      {/if}
    </div>
    <div class="g-row">
      <button class="btn small" onclick={() => store.refreshHealth()} disabled={store.healthLoading} title="Look at the repository again. Git metadata only: no network, no AI, nothing changed.">
        {store.healthLoading ? 'Checking…' : 'Check now'}
      </button>
    </div>
  </header>

  {#if store.healthError}<p class="error" role="alert">{store.healthError}</p>{/if}

  {#if h}
    <p class="meta g-small muted">
      {#if h.check}Checked {timeAgo(h.check.checkedAt, app.now)}{/if}
      {#if tidy}<span> · {tidy}</span>{/if}
      <span class="score" title="A rough number, derived from the findings. The findings are what matters."> · score {h.summary.score}</span>
    </p>

    {#if h.check?.error}
      <p class="g-notice" data-tone="bad">The repository could not be read at the last check, so what follows is as of before that: {h.check.error}</p>
    {/if}

    {#if split.needs.length}
      <ul class="g-list">
        {#each split.needs as f (f.id)}<FindingCard finding={f} projectId={project.id} {store} />{/each}
      </ul>
    {:else if h.summary.state === 'healthy'}
      <p class="calm g-small">Nothing needs you. Branches that are finished, pushed and waiting for review are normal and are not reported here.</p>
    {/if}

    {#if split.housekeeping.length}
      <details class="fold">
        <summary>Housekeeping ({split.housekeeping.length})</summary>
        <p class="g-small muted">Tidying that can wait: nothing is at risk.</p>
        <ul class="g-list">
          {#each split.housekeeping as f (f.id)}<FindingCard finding={f} projectId={project.id} {store} compact />{/each}
        </ul>
      </details>
    {/if}

    {#if h.dismissed.length}
      <details class="fold">
        <summary>You know about these ({h.dismissed.length})</summary>
        <p class="g-small muted">Hidden until they get worse, or go away and come back.</p>
        <ul class="g-list">
          {#each h.dismissed as f (f.id)}<FindingCard finding={f} projectId={project.id} {store} compact />{/each}
        </ul>
      </details>
    {/if}

    {#if h.resolved.length}
      <details class="fold">
        <summary>Fixed in the last day ({h.resolved.length})</summary>
        <ul class="g-bullets">
          {#each h.resolved as f (f.id)}
            <li class="g-small g-wrap">{f.title}{#if f.resolvedAt} <span class="muted">· {timeAgo(f.resolvedAt, app.now)}</span>{/if}</li>
          {/each}
        </ul>
      </details>
    {/if}

    <details class="fold how">
      <summary>How this is worked out</summary>
      <p class="g-small">
        From Git metadata and Werkbord’s own records only: no AI is asked and nothing goes over the network. It is looked at again when something
        changes here (a run, a task, a Git action), when you press Check now, and when this screen is a few minutes old.
        Werkbord never acts on a finding: a button only opens the same confirmation as anywhere else.
      </p>
      <p class="g-small">
        <strong>Git says so</strong> is a fact about the repository as of the last check. <strong>A guess</strong> is a pattern, such as two
        branches changing the same files, and is worded as a possibility.
      </p>
    </details>
  {/if}
</section>

<style>
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: 8px 12px;
  }

  h2 {
    font-size: 0.72rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-2);
  }

  .headline {
    margin: 2px 0 0;
    font-size: 1.25rem;
    font-weight: 700;
  }

  .headline[data-tone='ok'] {
    color: var(--ok);
  }
  .headline[data-tone='ask'] {
    color: var(--warn);
  }
  .headline[data-tone='block'] {
    color: var(--block);
  }
  .headline[data-tone='bad'] {
    color: var(--danger);
  }

  .health {
    display: grid;
    gap: 10px;
    border-left: 4px solid var(--ok);
  }

  .health[data-state='attention'] {
    border-left-color: var(--warn);
  }
  .health[data-state='risk'] {
    border-left-color: var(--block);
  }
  .health[data-state='critical'] {
    border-left-color: var(--danger);
  }
  .health[data-state='unknown'] {
    border-left-color: var(--border);
  }

  .meta,
  .calm {
    margin: 0;
  }

  .score {
    font-variant-numeric: tabular-nums;
  }

  .fold summary {
    cursor: pointer;
    padding: 6px 0;
    font-weight: 600;
    color: var(--text-2);
  }

  .fold[open] > summary {
    margin-bottom: 4px;
  }
</style>
