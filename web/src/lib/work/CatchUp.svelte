<script lang="ts">
  import { recap } from '../../shell/catchup';
  import { catchUpSession } from '../../shell/catchup-session.svelte';
  import type { Overview } from '../../shell/types';

  // "Since you were last here", drawn once for the Mac window page and for the web app: what was completed and merged since the person's last
  // visit (kept as cursors in this browser, never as content), and a button that asks Salie to explain it. It is handed the overview and says
  // nothing about where it came from; each side says how the overview is read again, how Salie is asked, and what opening a change does.
  let { overview, waiting, refresh, latest, explain: ask, canExplain = true, open }: {
    overview: Overview | null;
    waiting: number;
    /** Reads the overview again, so nothing cached is sent to Salie; false when it could not be. */
    refresh: () => Promise<boolean>;
    /** The overview as it is now, after `refresh`. */
    latest: () => Overview | null;
    explain: (prompt: string) => Promise<void>;
    canExplain?: boolean;
    open: (workspace: string, href: string) => void;
  } = $props();
  const previous = $derived(catchUpSession.previous);
  const through = $derived(catchUpSession.through);
  let expanded = $state(false);
  let explanationError = $state('');
  const result = $derived(overview && previous ? recap(overview, previous, through) : null);
  const completed = $derived(result?.items.filter(i => i.change.kind === 'completed').length ?? 0);
  const merged = $derived(result?.items.filter(i => i.change.kind === 'pr_merged').length ?? 0);
  const elapsed = $derived(previous ? Math.max(0, Math.floor((through - previous.at) / 86_400_000)) : 0);
  async function explain() {
    // Refresh before explanation; never send cached Team content after permission changes.
    if (!await refresh()) { explanationError = 'Recent activity could not be refreshed. Retry before sharing it with Salie.'; return; }
    explanationError = '';
    const now = latest() && previous ? recap(latest()!, previous, through) : null;
    const evidence = now?.items.slice(0, 30).map(i => `- ${i.change.kind === 'completed' ? 'Completed' : 'Merged pull request'}: ${i.change.title} (${i.name}, ${new Date(i.change.at).toLocaleString()}). Source: ${i.change.href}`) ?? [];
    const coverage = [now?.incomplete ? 'Some history is outside the available coverage.' : '', now?.unavailable.length ? `Unavailable workspaces: ${now.unavailable.join(', ')}.` : '', (now?.items.length ?? 0) > evidence.length ? `Showing the first ${evidence.length} of ${now?.items.length} recorded events.` : ''].filter(Boolean).join(' ');
    await ask(`Catch me up on the work recorded since ${new Date(previous!.at).toLocaleString()}. Explain what these facts establish and link to the source records. Say where the evidence is incomplete.\n${coverage}\n\n${evidence.join('\n') || 'No completion or pull-request merge events were recorded in this interval.'}`);
  }
</script>

<section class="catchup" aria-labelledby="catchup-title">
  <div class="heading"><h2 id="catchup-title">Since you were last here</h2>{#if previous}<span class="muted">{elapsed === 0 ? 'Earlier today' : `${elapsed} ${elapsed === 1 ? 'day' : 'days'}`}</span>{/if}</div>
  {#if explanationError}<p role="alert">{explanationError}</p>{/if}
  {#if !previous}<p class="muted">Your next visit will show what changed while you were away.</p>
  {:else if result}
    <div class="facts">
      <button class="btn small quiet" onclick={() => expanded = !expanded} aria-expanded={expanded}>{merged} pull {merged === 1 ? 'request' : 'requests'} merged · {completed} {completed === 1 ? 'item' : 'items'} completed</button>
      <button class="btn small quiet" onclick={() => { const heading = document.getElementById('work-attention') as HTMLDetailsElement | null; if (heading) { heading.open = true; heading.scrollIntoView({ block: 'nearest' }); heading.querySelector('summary')?.focus(); } }}>{waiting} {waiting === 1 ? 'item needs' : 'items need'} attention now</button>
      {#if canExplain}<button class="btn small" onclick={() => { void explain(); }}>Catch me up with Salie</button>{/if}
    </div>
    {#if result.incomplete || result.unavailable.length}<p class="muted">{result.incomplete ? 'Some history is outside the available coverage. ' : ''}{result.unavailable.length ? `Unavailable: ${result.unavailable.join(', ')}.` : ''}</p>{/if}
    {#if expanded}<ul>{#each result.items as row (row.change.key)}<li><button class="btn small quiet" onclick={() => open(row.workspace, row.change.href)}>{row.change.title}</button><span class="muted">{row.name} · {row.change.kind === 'completed' ? 'completed' : 'PR merged'}</span></li>{/each}</ul>{/if}
  {:else}<p class="muted">Loading recent activity…</p>{/if}
</section>
<style>
  .catchup { padding-bottom: 12px; border-bottom: 1px solid var(--border); flex: none; }
  .heading, .facts { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 16px; }
  h2, p { margin: 0; } h2 { font-family: var(--font); font-size: 14px; font-weight: 600; letter-spacing: 0; text-transform: none; color: var(--text); } p { font-size: 13px; margin-top: 6px; }
  .facts { margin-top: 8px; }
  .facts .btn { max-width: 100%; white-space: normal; text-align: left; } .muted { color: var(--text-2); font-size: 13px; }
  ul { margin: 8px 0 0; padding: 0; list-style: none; max-height: 160px; overflow: auto; }
  li { display: flex; align-items: baseline; flex-wrap: wrap; gap: 8px; }
</style>
