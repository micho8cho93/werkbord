<script lang="ts">
  import { agentName, runStatus } from '../format';
  import Icon from '../Icon.svelte';
  import Mark from '../Mark.svelte';
  import { globalHref } from '../router.svelte';
  import { app } from '../state.svelte';
  import { activeRunners, attentionLine, connectionWord, deckCounts, deckKeys } from './deck';
  import Keycap from './Keycap.svelte';
  import Tour from '../Tour.svelte';

  // The phone's home. A header that says whether the controller is reachable, one line on what needs the person, the nine keys, and
  // what the agents are doing now. It only reads what the app already holds (the Control Center overview and the runners); the keys
  // are links to pages, so nothing here can start, stop or change anything.

  const counts = $derived(deckCounts(app.overview, app.needsYou));
  const keys = $derived(deckKeys(counts));
  const attention = $derived(attentionLine(counts.needsYou, counts.review));
  const link = $derived(connectionWord(app.connection));
  const runners = $derived(activeRunners(app.runners));
  const loaded = $derived(app.overview !== null);
  const active = $derived((app.overview?.runs ?? []).filter((x) => runStatus(x.run).active));
  const waiting = $derived(active.filter((x) => runStatus(x.run).needsInput).length);
  const working = $derived(active.filter((x) => x.run.state === 'running' || x.run.state === 'starting'));
  // "2 agents running · 1 waiting for you", and which agents they are.
  const headline = $derived(
    [working.length ? `${working.length} ${working.length === 1 ? 'agent' : 'agents'} running` : '', waiting ? `${waiting} waiting for you` : ''].filter(Boolean).join(' · ') || 'No agents are running',
  );
  const names = $derived([...new Set(active.map((x) => agentName(app.agents, x.run.agentId)))].join(', '));
</script>

<div class="deck-page">
  <Tour surface="phone" />
  <header class="top">
    <div class="id">
      <a class="brand" href={globalHref('deck')} aria-label="Werkbord, Control Deck">
        <Mark height={20} />
        <span class="wm">werkbord</span>
      </a>
      <p class="status mm" data-testid="deck-fleet">
        {#if app.connection === 'offline'}
          <button class="reconnect" data-tone={link.tone} type="button" onclick={() => app.reconnect()} aria-label="Offline. Reconnect" data-testid="deck-connection"><span class="dot" aria-hidden="true"></span>{link.word}. Reconnect</button>
        {:else}
          <span class="conn" data-tone={link.tone} role="status" data-testid="deck-connection"><span class="dot" aria-hidden="true"></span>{link.word}</span>
        {/if}
        {#if loaded} · {runners.length} {runners.length === 1 ? 'runner' : 'runners'}{counts.running ? ` · ${counts.running} ${counts.running === 1 ? 'agent' : 'agents'} running` : ''}{/if}
      </p>
    </div>
    <a class="btn icon" href={globalHref('settings')} aria-label="Settings" title="Settings"><Icon name="settings" /></a>
  </header>

  <a class="attention card" class:waiting={attention.waiting} href={globalHref('control')} data-testid="deck-attention">
    {#if attention.waiting}<span class="num pend" aria-hidden="true">{counts.needsYou}</span>{:else}<span class="signal" aria-hidden="true"></span>{/if}
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

  <a class="activity card" href={globalHref('agents')} data-testid="deck-activity" aria-label="Agents. {headline}">
    <span class="tile" aria-hidden="true"><Icon name="activity" size={20} /></span>
    <span class="who">
      <span class="task">{loaded ? headline : 'Looking…'}</span>
      {#if names}<span class="meta mm">{names}</span>{/if}
    </span>
    <Icon name="right" size={16} />
  </a>

  <nav class="more" aria-label="More">
    <a href={globalHref('help')}>Help and shortcuts</a>
    <button type="button" onclick={() => (app.switcherOpen = true)}>Jump to…</button>
  </nav>
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

  .id {
    display: grid;
    gap: 6px;
    min-width: 0;
  }

  /* The connection and the machines, as one quiet line under the name. */
  .status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0 4px;
    margin: 0;
  }

  .conn,
  .reconnect {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text);
  }

  .reconnect {
    min-height: 44px;
    margin: -12px 0;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    cursor: pointer;
  }

  .status .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--text-2);
  }

  [data-tone='ok'] > .dot {
    background: var(--ok);
  }

  [data-tone='warn'] > .dot {
    background: var(--warn);
  }

  [data-tone='bad'] > .dot {
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

  /* The narrowest phones keep nine keys in three columns by giving the keys the room: less housing, closer keys. */
  @media (max-width: 359px) {
    .housing {
      padding: 12px;
      border-radius: 22px;
    }

    .grid {
      gap: 8px;
    }
  }

  .more {
    display: flex;
    justify-content: center;
    gap: 8px;
  }

  .more a,
  .more button {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 0 12px;
    border: 0;
    background: none;
    color: var(--text-2);
    font: inherit;
    font-size: 13px;
    text-decoration: none;
  }

  .activity {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 64px;
    padding: 10px 14px;
    color: var(--text);
    text-decoration: none;
  }

  .activity > :global(svg:last-child) {
    margin-left: auto;
    flex: none;
    color: var(--text-2);
  }

  .tile {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: 10px;
    background: var(--surface-2);
    box-shadow: var(--tray-sh);
    color: var(--accent);
  }

  .who {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .task {
    font-size: 14px;
    font-weight: 500;
  }

  .meta {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
