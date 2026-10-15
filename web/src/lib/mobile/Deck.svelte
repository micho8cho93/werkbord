<script lang="ts">
  import { agentName, runElapsed } from '../format';
  import Icon from '../Icon.svelte';
  import Mark from '../Mark.svelte';
  import RunBadge from '../RunBadge.svelte';
  import { globalHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import { activeRunners, attentionLine, connectionWord, deckCounts, deckKeys } from './deck';
  import Keycap from './Keycap.svelte';

  // The phone's home. A header that says whether the controller is reachable, one line on what needs the person, the nine keys, and
  // what the agents are doing now. It only reads what the app already holds (the Control Center overview and the runners); the keys
  // are links to pages, so nothing here can start, stop or change anything.

  const counts = $derived(deckCounts(app.overview, app.needsYou));
  const keys = $derived(deckKeys(counts));
  const attention = $derived(attentionLine(counts.needsYou, counts.review));
  const link = $derived(connectionWord(app.connection));
  const runners = $derived(activeRunners(app.runners));
  const loaded = $derived(app.overview !== null);
  const working = $derived((app.overview?.runs ?? []).filter((x) => x.run.state === 'running' || x.run.state === 'starting'));
  const shown = $derived(working.slice(0, 3));
</script>

<div class="deck-page">
  <header class="top">
    <a class="brand" href={globalHref('deck')} aria-label="Werkbord, Control Deck">
      <Mark height={20} />
      <span class="wm">werkbord</span>
    </a>
    <div class="status">
      {#if app.connection === 'offline'}
        <button class="chip link" data-tone={link.tone} type="button" onclick={() => app.reconnect()} aria-label="Offline. Reconnect" data-testid="deck-connection">
          <span class="dot" aria-hidden="true"></span>{link.word}
        </button>
      {:else}
        <span class="chip link" data-tone={link.tone} role="status" data-testid="deck-connection"><span class="dot" aria-hidden="true"></span>{link.word}</span>
      {/if}
      <a class="btn icon" href={globalHref('settings')} aria-label="Settings" title="Settings"><Icon name="settings" /></a>
    </div>
  </header>

  <p class="fleet mm" data-testid="deck-fleet">
    {#if !loaded}Looking…{:else}{runners.length} {runners.length === 1 ? 'runner' : 'runners'} online · {counts.running} {counts.running === 1 ? 'agent' : 'agents'} running{/if}
  </p>

  <a class="attention card" class:waiting={attention.waiting} href={globalHref('control')} data-testid="deck-attention">
    <span class="signal" aria-hidden="true"></span>
    <span class="say">{loaded ? attention.text : 'Looking…'}</span>
    <Icon name="right" size={16} />
  </a>

  <section class="housing" aria-label="Control Deck">
    <div class="grid">
      {#each keys as key (key.id)}
        <Keycap {key} />
      {/each}
    </div>
  </section>

  <a class="activity card" href={globalHref('agents')} data-testid="deck-activity" aria-label="Agents. {working.length ? `${working.length} running` : 'None running'}">
    <div class="head">
      <span class="lab">Running now</span>
      <Icon name="right" size={16} />
    </div>
    {#if !loaded}
      <p class="none">Looking…</p>
    {:else if shown.length === 0}
      <p class="none">No agents are running.</p>
    {:else}
      <ul>
        {#each shown as x (x.run.id)}
          <li>
            <span class="who">
              <span class="task">{x.taskTitle}</span>
              <span class="meta mm">{x.projectName} · {agentName(app.agents, x.run.agentId)} · {runElapsed(x.run, app.now)}</span>
            </span>
            <RunBadge run={x.run} />
          </li>
        {/each}
      </ul>
      {#if working.length > shown.length}<p class="more mm">and {working.length - shown.length} more</p>{/if}
    {/if}
  </a>
</div>

<style>
  .deck-page {
    --deck-edge-light: light-dark(var(--surface), color-mix(in srgb, var(--text) 9%, transparent));
    --deck-wall: light-dark(color-mix(in srgb, var(--text) 20%, var(--border)), color-mix(in srgb, var(--ground) 72%, var(--border)));
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: calc(10px + env(safe-area-inset-top)) 16px 20px;
    max-width: 440px;
    margin: 0 auto;
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 44px;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text);
    text-decoration: none;
  }

  .wm {
    font-family: var(--mono);
    font-weight: 600;
    font-size: 17px;
    letter-spacing: -0.06em;
    line-height: 1;
  }

  .status {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .chip.link {
    height: 28px;
    border: 0;
    font: inherit;
    font-family: var(--mono);
    font-weight: 500;
    font-size: 11.5px;
    color: var(--text);
  }

  button.chip.link {
    min-height: 44px;
    padding-inline: 12px;
    background: transparent;
  }

  .chip .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--text-2);
  }

  .chip[data-tone='ok'] .dot {
    background: var(--ok);
  }

  .chip[data-tone='warn'] .dot {
    background: var(--warn);
  }

  .chip[data-tone='bad'] .dot {
    background: var(--danger);
  }

  .btn.icon {
    width: 44px;
    height: 44px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    text-decoration: none;
  }

  .fleet {
    margin: -4px 2px 0;
  }

  .attention {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 52px;
    padding: 0 14px;
    color: var(--text);
    text-decoration: none;
    font-weight: 500;
    font-size: 14px;
  }

  .attention :global(svg:last-child) {
    margin-left: auto;
    color: var(--text-2);
  }

  .signal {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--ok);
    flex: none;
  }

  .attention.waiting {
    border-color: var(--amber);
    box-shadow:
      var(--card-sh),
      0 0 0 3px var(--amber-ring);
  }

  .attention.waiting .signal {
    background: var(--amber);
  }

  /* The keyboard. A thick matte housing in the tray's paper, with a little grain, a lit top edge and a dark lower one; the keys
     do not stretch to fill the screen, the material around them is part of it. */
  .housing {
    padding: 18px;
    border-radius: 26px;
    border: 1px solid var(--border);
    background:
      radial-gradient(color-mix(in srgb, var(--text) 6%, transparent) 0.6px, transparent 0.7px) 0 0 / 3px 3px,
      linear-gradient(180deg, color-mix(in srgb, var(--surface-2) 70%, var(--surface)), var(--surface-2) 55%, color-mix(in srgb, var(--surface-2) 86%, var(--ground)));
    box-shadow:
      inset 0 2px 0 var(--deck-edge-light),
      inset 0 -3px 0 color-mix(in srgb, var(--text) 10%, transparent),
      inset 0 0 0 5px color-mix(in srgb, var(--text) 3%, transparent),
      0 1px 1px color-mix(in srgb, var(--text) 14%, transparent),
      0 14px 24px -14px color-mix(in srgb, var(--text) 40%, transparent);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 12px;
  }

  .activity {
    display: block;
    padding: 12px 14px 14px;
    color: var(--text);
    text-decoration: none;
  }

  .activity .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
    color: var(--text-2);
  }

  .activity ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }

  .activity li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }

  .who {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .task {
    font-size: 14px;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .none,
  .more {
    margin: 0;
    font-size: 13px;
    color: var(--text-2);
  }

  .more {
    margin-top: 8px;
  }
</style>
