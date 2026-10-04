<script lang="ts">
  import { untrack } from 'svelte';
  import { diffLineKind, statusWord } from '../gitui';
  import type { GitFileDiff } from '../types';

  // A diff, a window at a time. A large diff is never fetched or drawn whole: the first
  // window comes with the screen, and "Show more" asks for the next. The page scrolls;
  // only a long line scrolls sideways, inside the diff, so the screen never does.

  let { load, label }: { load: (offset: number) => Promise<GitFileDiff>; label: string } = $props();

  let chunks = $state<GitFileDiff[]>([]);
  let loading = $state(false);
  let error = $state('');

  const first = $derived(chunks[0]);
  const last = $derived(chunks[chunks.length - 1]);
  const lines = $derived(chunks.flatMap((c) => (c.diff === '' ? [] : c.diff.split('\n'))));

  /** Which file's diff is being loaded: an answer for another one is ignored. */
  let req = 0;

  async function more(initial = false) {
    if (loading && !initial) return;
    const id = initial ? ++req : req;
    loading = true;
    error = '';
    try {
      const offset = initial ? 0 : last ? last.offset + last.lines : 0;
      const next = await load(offset);
      if (id !== req) return;
      chunks = initial ? [next] : [...chunks, next];
    } catch (err) {
      if (id === req) error = err instanceof Error ? err.message : String(err);
    } finally {
      if (id === req) loading = false;
    }
  }

  // The diff of a different file starts again from its top.
  $effect(() => {
    void label;
    untrack(() => {
      chunks = [];
      void more(true);
    });
  });
</script>

<section class="wrap" aria-label="Diff of {label}">
  {#if first}
    <p class="stats g-small">
      {#if first.status}<span class="muted">{statusWord(first.status)}</span>{/if}
      {#if first.binary}
        <span class="muted">binary file: its contents are not shown</span>
      {:else}
        <span class="add">+{first.additions}</span> <span class="del">−{first.deletions}</span>
        <span class="muted">· {last.hasMore ? `lines 1–${last.offset + last.lines} of ${last.totalLines}${last.truncated ? '+' : ''}` : `${last.totalLines} lines`}</span>
      {/if}
    </p>
  {/if}

  {#if error}<p class="error" role="alert">{error}</p>{/if}

  {#if lines.length}
    <pre class="diff" aria-label="Unified diff">{#each lines as line, i (i)}<span class="ln" data-kind={diffLineKind(line)}>{line === '' ? ' ' : line}</span>{/each}</pre>
  {:else if first && !first.binary && !loading && !error}
    <p class="card empty">No textual changes to show{first.additions + first.deletions === 0 ? ' (the file mode or only its name changed)' : ''}.</p>
  {/if}

  {#if last?.hasMore}
    <button class="btn" onclick={() => more()} disabled={loading}>{loading ? 'Loading…' : 'Show more lines'}</button>
  {/if}
  {#if last && !last.hasMore && last.truncated}
    <p class="g-notice g-small">This diff is too large to show completely, so the rest is not available here. Review it from your terminal.</p>
  {/if}
  {#if loading && !chunks.length}<p class="muted">Loading…</p>{/if}
</section>

<style>
  .wrap {
    display: grid;
    gap: 8px;
    min-width: 0;
  }

  .stats {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
  }

  .add {
    color: var(--ok);
    font-family: var(--mono);
  }

  .del {
    color: var(--danger);
    font-family: var(--mono);
  }

  .diff {
    margin: 0;
    padding: 8px 0;
    overflow-x: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    font: 0.78rem/1.5 var(--mono);
    tab-size: 4;
  }

  .ln {
    display: block;
    padding: 0 10px;
    min-width: max-content;
    white-space: pre;
  }

  .ln[data-kind='add'] {
    background: color-mix(in srgb, var(--ok) 16%, transparent);
  }

  .ln[data-kind='del'] {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
  }

  .ln[data-kind='hunk'] {
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 9%, transparent);
  }

  .ln[data-kind='meta'] {
    color: var(--text-2);
  }
</style>
