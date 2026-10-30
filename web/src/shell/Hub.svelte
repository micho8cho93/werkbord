<script lang="ts">
  import Calendar from './Calendar.svelte';
  import CatchUp from './CatchUp.svelte';
  import Inbox from './Inbox.svelte';
  import { attentionGroups, tallyByKind } from './aggregate';
  import { model } from './model.svelte';
  import type { Overview } from './types';

  // Work preserves the existing attention and calendar panels within the shared project context.
  let { overview }: { overview: Overview | null } = $props();

  const groups = $derived(overview ? attentionGroups(overview) : []);
  const tally = $derived(tallyByKind(groups));
  const waiting = $derived(tally.reduce((n, t) => n + t.actions, 0));
  const project = $derived(model.selectedProject && model.contextOverview?.entries.find(e => e.workspace.id === model.selectedProject?.workspace)?.summary?.projects.find(p => p.id === model.selectedProject?.project));
</script>

<section class="hub" aria-labelledby="hub-h">
  <header class="top">
    <div class="title">
      <h1 id="hub-h">Work</h1>
      <p class="lede">{!overview ? 'Loading Personal and your teams…' : waiting ? `${waiting} ${waiting === 1 ? 'item needs' : 'items need'} attention in this project context.` : 'Nothing needs your attention in this project context.'}</p>
    </div>
    {#if overview}
      <ul class="tally" aria-label="Waiting by kind of workspace">
        {#each tally as t (t.kind)}
          <li data-kind={t.kind} data-testid="tally-{t.kind}">
            <span class="swatch" aria-hidden="true"></span>
            <span class="who">{t.label}</span>
            <span class="n">{t.actions}</span><span class="cap">{t.actions === 1 ? 'action' : 'actions'}</span>
            <span class="n">{t.warnings}</span><span class="cap">{t.warnings === 1 ? 'warning' : 'warnings'}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </header>

  {#if !model.selectedProject || overview?.entries.some(e => e.summary?.changes)}<CatchUp {overview} {waiting} />{/if}

  {#if project && model.selectedProject}<div><button class="btn" type="button" onclick={() => model.goTo(model.selectedProject!.workspace, project!.href)}>Open project</button></div>{/if}

  <div class="deck">
    <section class="panel needs" aria-labelledby="hub-needs-h">
      <h2 id="hub-needs-h" tabindex="-1">Needs attention</h2>
      <div class="body"><Inbox {overview} embedded /></div>
    </section>
    <section class="panel plan" aria-labelledby="hub-plan-h">
      <h2 id="hub-plan-h">Calendar</h2>
      <div class="body flush"><Calendar {overview} embedded /></div>
    </section>
  </div>
</section>

<style>
  .hub {
    display: flex;
    flex-direction: column;
    gap: 14px;
    height: 100%;
    min-height: 0;
    padding: 20px clamp(16px, 2vw, 32px);
    overflow: hidden;
  }
  h1 { margin: 0; }
  .top { display: flex; flex-wrap: wrap; align-items: flex-end; justify-content: space-between; gap: 12px 24px; }
  .lede { margin: 4px 0 0; color: var(--text-2); }
  .tally { display: flex; flex-wrap: wrap; gap: 6px 20px; margin: 0; padding: 0; list-style: none; }
  .tally li {
    --kind: var(--accent);
    display: flex;
    align-items: baseline;
    gap: 4px 6px;
    font-size: 13px;
  }
  .tally li[data-kind='team'] { --kind: var(--block); }
  .swatch { align-self: center; flex: none; width: 8px; height: 8px; border-radius: 50%; background: var(--kind, var(--accent)); }
  .who { font-weight: 600; font-size: 13px; }
  .n { font-family: var(--mono); font-size: 14px; font-weight: 600; }
  .cap { color: var(--text-2); margin-right: 6px; }

  .deck { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(340px, 4fr) minmax(0, 7fr); gap: 14px; }
  .panel { display: flex; flex-direction: column; min-width: 0; min-height: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); overflow: hidden; }
  .panel > h2 { display: flex; align-items: center; gap: 8px; margin: 0; padding: 12px 16px; border-bottom: 1px solid var(--border); color: var(--text); font-weight: 600; font-size: 13px; }
  .body { flex: 1; min-height: 0; padding: 14px; overflow-y: auto; }
  .body.flush { display: flex; flex-direction: column; overflow: hidden; }

  @media (max-width: 1000px) {
    .hub { height: auto; overflow: visible; }
    .deck { grid-template-columns: 1fr; }
    .panel { min-height: 420px; }
    .plan .body { overflow: auto; }
  }
</style>
