<script lang="ts">
  import { tick } from 'svelte';
  import Icon from '../Icon.svelte';
  import Mark from '../Mark.svelte';
  import ConfirmCard from './ConfirmCard.svelte';
  import ReplyView from './ReplyView.svelte';
  import { assistant } from './store.svelte';
  import { size } from './words';

  // What is on the screen between the heading and the box: the conversation, or, where there cannot be one yet, why not.

  let scroller: HTMLElement | undefined = $state();
  let col: HTMLElement | undefined = $state();
  let stuck = $state(true);
  let behind = $state(false);

  const items = $derived(assistant.transcript.items);
  /** Changes whenever there is something new to see at the bottom: a new item, more words, a new step. */
  const tail = $derived.by(() => {
    const last = items[items.length - 1];
    if (!last) return '0';
    // The bar that answers several changes at once is at the bottom too, and comes and goes with how many are waiting.
    const bar = `${assistant.waitingItems.length >= 2 || assistant.batch ? 'all' : ''}`;
    return last.kind === 'reply' ? `${items.length}:${last.text.length}:${last.steps.length}:${last.status}:${bar}` : `${items.length}:${last.kind === 'confirm' ? last.state : ''}:${bar}`;
  });

  function onScroll() {
    if (!scroller) return;
    stuck = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
    if (stuck) behind = false;
  }
  function toEnd() {
    if (!scroller) return;
    scroller.scrollTop = scroller.scrollHeight;
    stuck = true;
    behind = false;
  }
  // Follow the reply as it arrives, unless the person scrolled up to read: then say there is more below.
  $effect(() => {
    void tail;
    void tick().then(() => (stuck ? toEnd() : (behind = true)));
  });
  // Whatever makes the conversation taller (a reply settling, a card, the bar that answers several changes) keeps the end in view for
  // someone who is following it; someone who scrolled up to read is left where they are.
  $effect(() => {
    if (!col || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => {
      if (stuck) toEnd();
    });
    ro.observe(col);
    return () => ro.disconnect();
  });
  // Opening the screen on a conversation that is already there starts at its end.
  $effect(() => {
    if (assistant.phase === 'opening') void tick().then(toEnd);
  });

  const suggestions = ['What needs me today?', 'What is blocked right now?', 'What is planned for this week?'];
  const kinds = $derived(assistant.providers);

  /** What the screen says about the connection, once there is a conversation to be connected to. */
  const linkNote = $derived(
    assistant.session && assistant.link === 'reconnecting'
      ? 'Reconnecting… your reply keeps going meanwhile.'
      : assistant.session && assistant.link === 'connecting' && assistant.running
        ? 'Reconnecting…'
        : '',
  );
</script>

{#if linkNote}<p class="linknote" role="status">{linkNote}</p>{/if}

{#if assistant.status === 'ready' || assistant.hasConversation}
  <div class="scroll" bind:this={scroller} onscroll={onScroll} role="log" aria-label="Conversation" aria-live="off" tabindex="-1" data-testid="assistant-log">
    <div class="col" bind:this={col}>
      {#if items.length === 0}
        <div class="hello" data-testid="assistant-empty">
          <Mark height={34} />
          <h2 class="hi">What can I help with?</h2>
          <p class="sub">Ask where things stand, or tell me what to change. I will ask you before I change anything.</p>
          <div class="suggest">
            {#each suggestions as s (s)}
              <button class="pill" type="button" onclick={() => assistant.send(s)}>{s}</button>
            {/each}
          </div>
        </div>
      {/if}
      {#each items as it (it.id)}
        {#if it.kind === 'user'}
          <div class="user">
            <p class="bubble">{it.text}</p>
            {#if it.files.length}
              <ul class="sent">{#each it.files as f (f.name)}<li><Icon name="attach" size={12} /> {f.name} <span>{size(f.bytes)}</span></li>{/each}</ul>
            {/if}
          </div>
        {:else if it.kind === 'reply'}
          <ReplyView reply={it} />
        {:else if it.kind === 'confirm'}
          <ConfirmCard item={it} />
        {:else}
          <p class="notice">{it.text}</p>
        {/if}
      {/each}
      <!-- Several changes at once (four tasks, say): one press answers them all, each still on its own. -->
      {#if assistant.batch || assistant.waitingItems.length >= 2}
        {@const total = assistant.batch?.total ?? assistant.waitingItems.length}
        <div class="all" role="group" aria-label="All the changes waiting" data-testid="assistant-confirm-all">
          {#if assistant.batch}
            <span class="count" role="status">{assistant.batch.decision === 'approve' ? 'Confirming' : 'Declining'} {Math.min(assistant.batch.done + 1, total)} of {total}…</span>
          {:else}
            <span class="count">{total} changes are waiting for you.</span>
            <button class="btn primary" type="button" onclick={() => assistant.resolveAll('approve')} data-testid="assistant-confirm-all-yes">Confirm all {total}</button>
            <button class="btn" type="button" onclick={() => assistant.resolveAll('decline')}>Not now</button>
          {/if}
        </div>
      {/if}
    </div>
  </div>
  {#if behind}
    <button class="latest btn small" type="button" onclick={toEnd}><Icon name="down" size={12} /> Latest</button>
  {/if}
{:else}
  <div class="state" role="status" data-testid="assistant-state" data-status={assistant.status}>
    {#if assistant.status === 'loading' || assistant.status === 'idle'}
      <p class="muted"><span class="dots" aria-hidden="true"><i></i><i></i><i></i></span> Getting ready…</p>
    {:else if assistant.status === 'offline'}
      <h2 class="t">Cannot reach Werkbord</h2>
      <p>Werkbord on this computer is not answering. If it was restarted, this is usually over in a few seconds.</p>
      {#if assistant.problem}<p class="muted">{assistant.problem}</p>{/if}
      <button class="btn" type="button" onclick={() => assistant.prepare(true)}>Try again</button>
    {:else if assistant.status === 'unauthorized'}
      <h2 class="t">This device is not signed in</h2>
      <p>Open Werkbord again from its link, then come back.</p>
    {:else if assistant.status === 'disabled'}
      <h2 class="t">The assistant is turned off</h2>
      <p>It is switched off in this Werkbord’s settings file (<code>assistant.disabled</code>).</p>
    {:else if assistant.status === 'no-provider'}
      <h2 class="t">Sign in to Claude Code or Codex first</h2>
      <p>The assistant uses the one you already have on this computer, signed in the way you signed it in. Werkbord has no account or key of its own and never asks for one.</p>
      <ul class="provs">
        {#each kinds as p (p.id)}
          <li>
            <strong>{p.name}</strong>
            <span>{p.installed ? (p.signedIn === false ? 'installed, not signed in' : (p.detail ?? 'not ready')) : 'not installed'}</span>
            {#if p.guidance}<span class="how">{p.guidance}</span>{/if}
          </li>
        {/each}
      </ul>
      <button class="btn" type="button" onclick={() => assistant.prepare(true)}>Check again</button>
    {/if}
  </div>
{/if}

<style>
  .all {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px 10px;
    padding: 10px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    font-size: 13.5px;
  }
  .all .count {
    margin-right: auto;
    color: var(--text-2);
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-gutter: stable both-edges;
    outline: none;
  }
  .col {
    display: flex;
    flex-direction: column;
    gap: 20px;
    width: 100%;
    max-width: var(--asst-w);
    min-height: 100%;
    margin: 0 auto;
    padding: 24px 16px 12px;
    font-size: 15px;
    line-height: 1.55;
  }
  .hello {
    margin: auto 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 24px 0 8px;
    text-align: center;
  }
  .hi {
    font-family: var(--font);
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.01em;
    text-transform: none;
    color: var(--text);
  }
  .sub {
    max-width: 30rem;
    color: var(--text-2);
    font-size: 14px;
  }
  .suggest {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 8px;
    padding-top: 6px;
  }
  .pill {
    min-height: 40px;
    padding: 0 14px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--surface);
    box-shadow: var(--card-sh);
    font-size: 14px;
    color: var(--text);
  }
  .pill:hover {
    background: var(--surface-2);
  }
  .user {
    align-self: flex-end;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 6px;
    max-width: min(85%, 34rem);
  }
  .bubble {
    padding: 9px 14px;
    border-radius: 16px 16px 4px 16px;
    background: var(--surface-2);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .sent {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
    color: var(--text-2);
    font-size: 12.5px;
  }
  .sent li {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .notice {
    align-self: center;
    max-width: 90%;
    text-align: center;
    color: var(--text-2);
    font-size: 12.5px;
  }
  .latest {
    position: absolute;
    left: 50%;
    bottom: 96px;
    transform: translateX(-50%);
    box-shadow: var(--win-sh);
  }
  .linknote {
    flex: none;
    padding: 6px 16px;
    text-align: center;
    font-size: 12.5px;
    color: var(--warn-text);
    background: color-mix(in srgb, var(--amber) 14%, transparent);
  }
  .state {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    width: 100%;
    max-width: 32rem;
    margin: 0 auto;
    padding: 24px 20px;
    text-align: center;
    font-size: 14px;
  }
  .t {
    font-family: var(--font);
    font-size: 18px;
    font-weight: 600;
    letter-spacing: -0.01em;
    text-transform: none;
    color: var(--text);
  }
  .provs {
    width: 100%;
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 8px;
    text-align: left;
  }
  .provs li {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
  }
  .provs span {
    color: var(--text-2);
    font-size: 13px;
  }
  .provs .how {
    font-size: 12.5px;
    overflow-wrap: anywhere;
  }
  .dots {
    display: inline-flex;
    gap: 3px;
    margin-right: 6px;
  }
  .dots i {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--text-2);
    animation: pulse 1.2s ease-in-out infinite;
  }
  .dots i:nth-child(2) {
    animation-delay: 0.15s;
  }
  .dots i:nth-child(3) {
    animation-delay: 0.3s;
  }
</style>
