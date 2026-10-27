<script lang="ts">
  import Icon from '../lib/Icon.svelte';
  import WorkspaceIcon from '../lib/personal/WorkspaceIcon.svelte';
  import Calendar from './Calendar.svelte';
  import Inbox from './Inbox.svelte';
  import { attentionGroups, standing, tallyByKind } from './aggregate';
  import { model } from './model.svelte';
  import type { Item, Overview } from './types';

  // Home is everything that spans the whole of Werkbord on one page. At the top, how much waits in Personal and in your teams;
  // then a shelf of every place to go (Personal, each team, the workspaces
  // Salie made, the Data library); then what needs you beside the calendar. The panels scroll inside themselves, so the page
  // itself uses the whole window and does not scroll on a screen that is large enough.
  let { overview }: { overview: Overview | null } = $props();

  const groups = $derived(overview ? attentionGroups(overview) : []);
  const tally = $derived(tallyByKind(groups));
  const waiting = $derived(tally.reduce((n, t) => n + t.actions, 0));
  const places = $derived((model.view?.items ?? []).filter((i) => i.state !== 'setup' && i.state !== 'leaving'));
  const entry = (i: Item) => overview?.entries.find((e) => e.workspace.id === i.id);
  const needs = (i: Item) => (entry(i)?.summary?.attention ?? []).filter((a) => a.severity !== 'info').length;
  const detail = (i: Item): string => {
    const e = entry(i);
    if (!e) return i.state === 'ready' ? 'Working' : i.detail || i.state;
    return standing(e);
  };
</script>

<section class="hub" aria-labelledby="hub-h">
  <header class="top">
    <div class="title">
      <h1 id="hub-h">Home</h1>
      <p class="lede">{!overview ? 'Loading Personal and your teams…' : waiting ? `${waiting} ${waiting === 1 ? 'item needs' : 'items need'} attention across Personal and your teams.` : 'Nothing needs your attention across Personal and your teams.'}</p>
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

  <div class="places-heading"><h2>Places</h2>{#if !model.remotePersonal}<button class="btn small add" type="button" onclick={() => { void model.customizeGenerated(); }}><Icon name="plus" size={14} />Create with Salie</button>{/if}</div>
  <ul class="shelf" aria-label="Places to go">
    {#each places as i (i.id)}
      <li>
        <button class="tile" type="button" data-kind={i.kind} onclick={() => model.open(i.id)} data-testid="hub-workspace-{i.id}">
          <span class="swatch" aria-hidden="true"></span>
          <span class="tx"><strong>{i.kind === 'personal' ? 'Personal' : i.name}</strong><small>{detail(i)}</small></span>
          {#if needs(i)}<span class="num" aria-label="{needs(i)} waiting for you">{needs(i)}</span>{/if}
        </button>
      </li>
    {/each}
    {#each model.visibleGenerated as w (w.id)}
      <li>
        <button class="tile" type="button" data-kind="generated" onclick={() => model.openGenerated(w.id)} data-testid="hub-generated-{w.id}">
          <span class="gi"><WorkspaceIcon icon={w.icon} name={w.name} /></span>
          <span class="tx"><strong>{w.name}</strong><small>Made by Salie</small></span>
        </button>
      </li>
    {/each}
    {#if !model.remotePersonal}<li>
      <button class="tile" type="button" data-kind="library" onclick={() => model.go('personal-library')} data-testid="hub-library">
        <span class="gi"><Icon name="database" size={18} /></span>
        <span class="tx"><strong>Data library</strong><small>Collections your workspaces share</small></span>
      </button>
    </li>{/if}
  </ul>

  {#if model.generatedError}<p class="error" role="status">Saved workspaces are unavailable. {model.generatedError} <button class="btn small quiet" type="button" onclick={() => model.refreshGenerated()}>Retry saved workspaces</button></p>{/if}

  <div class="deck">
    <section class="panel needs" aria-labelledby="hub-needs-h">
      <h2 id="hub-needs-h">Needs attention</h2>
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
  .error { margin: 0; color: var(--danger-text); }
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

  .places-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 4px; }
  .places-heading h2 { margin: 0; }
  .shelf { max-height: min(28vh, 240px); overflow-y: auto; display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 230px), 1fr)); gap: 10px; margin: -6px; padding: 6px; list-style: none; flex: none; }
  .tile {
    --kind: var(--accent);
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 58px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--card-sh);
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
    transition: border-color 0.14s, background 0.14s;
  }
  .tile[data-kind='team'] { --kind: var(--block); }
  .tile[data-kind='generated'],
  .tile[data-kind='library'] { --kind: var(--text-2); }
  .tile:hover { border-color: color-mix(in srgb, var(--text-2) 50%, var(--border)); background: var(--surface-2); }
  .tile:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .gi { display: inline-flex; flex: none; align-items: center; justify-content: center; width: 30px; height: 30px; border-radius: var(--radius-sm); background: var(--surface-2); color: var(--text); }
  .tx { display: flex; flex-direction: column; min-width: 0; gap: 2px; }
  .tx strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; font-weight: 500; }
  .tx small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-2); font-size: 12px; }
  .num { margin-left: auto; min-width: 20px; padding: 0 6px; border-radius: 10px; background: var(--amber); color: #0a0a0a; font-family: var(--mono); font-size: 12px; line-height: 20px; text-align: center; }

  .deck { flex: 1; min-height: 0; display: grid; grid-template-columns: minmax(340px, 4fr) minmax(0, 7fr); gap: 14px; }
  .panel { display: flex; flex-direction: column; min-width: 0; min-height: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); overflow: hidden; }
  .panel > h2 { display: flex; align-items: center; gap: 8px; margin: 0; padding: 12px 16px; border-bottom: 1px solid var(--border); color: var(--text); font-weight: 600; font-size: 13px; }
  .body { flex: 1; min-height: 0; padding: 14px; overflow-y: auto; }
  .body.flush { display: flex; flex-direction: column; overflow: hidden; }

  @media (max-width: 1000px) {
    .hub { height: auto; overflow: visible; }
    .shelf { max-height: none; overflow: visible; }
    .deck { grid-template-columns: 1fr; }
    .panel { min-height: 420px; }
    .plan .body { overflow: auto; }
  }
</style>
