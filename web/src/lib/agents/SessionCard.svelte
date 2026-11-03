<script lang="ts">
  import { api } from '../api';
  import { app } from '../state.svelte';
  import { canRetry } from '../agent-overview';
  import type { AgentSession } from '../agent-sessions';
  import { asTerminal } from '../agent-sessions';
  import { terminalApi } from '../assistant/terminal';
  import QuestionCard from '../QuestionCard.svelte';
  import Icon from '../Icon.svelte';
  import AgentMark from '../AgentMark.svelte';
  import type { StepAgent } from '../work-steps';

  // One session in the list, drawn by how much it needs the person: a session waiting for an answer breathes in amber and carries its
  // question, a failure carries Retry, a live one shows it is live. What can be done here is what the controller says the session
  // offers (`capabilities`) and whether the controller is reachable; anything else is done in the open session beside the list.
  let { session, tone, title, meta, doing, agent, now, selected = false, index = 0, live = true, onopen, onchange }: {
    session: AgentSession;
    tone: 'needs' | 'trouble' | 'running' | 'done';
    title: string;
    meta: string;
    /** The state word and what it is doing, already said. */
    doing: { state: string; detail: string; waiting?: boolean; idle?: boolean; outcome?: string };
    /** The session's agent and where it stands, for the ringed mark; only a confirmed live session turns the ring. */
    agent: StepAgent;
    now: number;
    selected?: boolean;
    index?: number;
    /** The controller is reachable now: without it nothing here can be done to a session. */
    live?: boolean;
    onopen: () => void;
    /** Something was done to the session: the list is read again. */
    onchange: () => void;
  } = $props();

  const questions = $derived(session.kind === 'run' && session.live ? app.questions.filter(q => q.runId === session.id && q.projectId === session.projectId) : []);
  const mayAnswer = $derived(live && session.live && (session.capabilities.questions || session.capabilities.message));
  const mayReply = $derived(mayAnswer && session.kind === 'run' && !!session.waiting && !questions.length && session.capabilities.message);
  const mayStop = $derived(live && session.live && session.capabilities.stop);
  const mayRetry = $derived(live && canRetry(session));

  let reply = $state('');
  let busy = $state('');
  let error = $state('');
  let confirmStop = $state(false);
  let confirmTimer: ReturnType<typeof setTimeout> | undefined;

  async function send() {
    const text = reply.trim();
    if (!text || busy) return;
    busy = 'send'; error = '';
    try {
      app.scope(session.projectId)?.upsertRun(await api.sendInput(session.projectId, session.id, text));
      reply = '';
      onchange();
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
  // Stopping ends work in progress, so it takes two presses: the first asks, and the question lapses by itself.
  async function stop() {
    if (busy) return;
    if (!confirmStop) {
      confirmStop = true;
      clearTimeout(confirmTimer);
      confirmTimer = setTimeout(() => (confirmStop = false), 4000);
      return;
    }
    clearTimeout(confirmTimer);
    confirmStop = false;
    busy = 'stop'; error = '';
    try {
      if (session.kind === 'run') app.scope(session.projectId)?.upsertRun(await api.stopRun(session.projectId, session.id));
      else await terminalApi.stop(asTerminal(session));
      onchange();
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
  async function retry() {
    if (!session.taskId || busy) return;
    busy = 'retry'; error = '';
    try {
      app.scope(session.projectId)?.upsertRun(await api.startRun(session.projectId, session.taskId));
      app.notify(`Started again: “${title}”.`, 4000);
      onchange();
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { busy = ''; }
  }
  function key(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); void send(); }
  }
</script>

<li class="card wb-rise" class:selected data-tone={tone} data-session={session.key} data-outcome={session.live ? undefined : doing.outcome} style:--i={index}>
  <button type="button" class="open-card" onclick={onopen} aria-label="Open {title}" aria-current={selected ? 'true' : undefined}>
    <span class="head">
      <AgentMark agentId={agent.agentId} state={agent.state} fresh={agent.fresh} observedAt={agent.observedAt} {now} decorative />
      <span class="title" {title}>{title}</span>
      <span class="state" data-waiting={doing.waiting ? 'true' : undefined} data-idle={doing.idle ? 'true' : undefined} data-outcome={doing.outcome}>{doing.state}</span>
    </span>
    <span class="meta">{meta}</span>
    {#if doing.detail}<span class="doing">{doing.detail}</span>{/if}
  </button>

  {#if questions.length || mayReply || mayStop || mayRetry}
    <!-- Stop on a session that is simply running is kept out of the way until the card is pointed at or focused; what needs you is never hidden. -->
    <div class="inline" class:tucked={tone === 'running' && !doing.idle && !questions.length && !mayReply && !confirmStop && !busy}>
      {#each questions as q (q.id)}
        <div class="q"><QuestionCard question={q} showMeta={false} revealContext={q.kind === 'approval'} disabled={!mayAnswer} /></div>
      {/each}
      {#if mayReply}
        <form class="reply" onsubmit={e => { e.preventDefault(); void send(); }}>
          <label class="sr-only" for="reply-{session.id}">Reply to {title}</label>
          <textarea id="reply-{session.id}" class="input" rows="1" bind:value={reply} onkeydown={key} placeholder="Reply to this agent" disabled={busy === 'send'}></textarea>
          <button class="btn small primary" disabled={!reply.trim() || busy === 'send'}><Icon name="send" size={13} />{busy === 'send' ? 'Sending…' : 'Send'}</button>
        </form>
      {/if}
      {#if mayStop || mayRetry}
        <div class="acts">
          {#if mayRetry}<button class="btn small" type="button" disabled={!!busy} onclick={retry}><Icon name="play" size={12} />{busy === 'retry' ? 'Starting…' : 'Retry'}</button>{/if}
          {#if mayStop}<button class="btn small" class:danger={confirmStop} type="button" disabled={!!busy} onclick={stop}><Icon name="stop" size={12} />{busy === 'stop' ? 'Stopping…' : confirmStop ? 'Press again to stop' : 'Stop'}</button>{/if}
        </div>
      {/if}
      {#if error}<p class="bad" role="alert">{error}</p>{/if}
    </div>
  {/if}
</li>

<style>
  .card { position: relative; list-style: none; display: grid; gap: 0; border: 1px solid var(--edge-strong); border-radius: var(--radius-sm); background: var(--surface); overflow: hidden; transition: background-color 120ms, border-color 120ms; }
  /* The whole card answers the pointer, not just the part that opens it. */
  .card:hover { background: var(--hover); }
  .open-card { position: relative; display: grid; gap: 2px; width: 100%; padding: 9px 12px 9px 14px; text-align: left; border: 0; background: transparent; color: var(--text); font: inherit; cursor: pointer; }
  .open-card:focus-visible, .btn:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
  /* The edge says how much the session needs you before any word is read. */
  .card::before { content: ''; position: absolute; inset: 0 auto 0 0; width: 3px; background: transparent; transition: background 200ms; }
  .card[data-tone='needs']::before { background: var(--amber); }
  .card[data-tone='trouble']::before { background: var(--danger); }
  .card[data-tone='running']::before { background: var(--accent); }
  .card[data-tone='needs'] { border-color: color-mix(in srgb, var(--warn) 50%, var(--border)); background: color-mix(in srgb, var(--amber) 7%, var(--surface)); }
  .card[data-tone='needs']:hover { background: color-mix(in srgb, var(--amber) 13%, var(--surface)); }
  .card[data-tone='trouble'] { border-color: color-mix(in srgb, var(--danger) 38%, var(--border)); }
  .card.selected { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent); }
  .head { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .title { flex: 1; min-width: 0; font-size: 14px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .meta { font-size: 12px; color: var(--text-2); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .doing { font-size: 12px; color: var(--text-2); overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
  .state { flex: none; padding: 1px 8px; border-radius: 999px; background: var(--surface-2); color: var(--text-2); font-size: 12px; }
  .state[data-idle='true'] { background: color-mix(in srgb, var(--amber) 14%, var(--surface)); color: var(--text); }
  .state[data-waiting='true'] { background: color-mix(in srgb, var(--amber) 22%, var(--surface)); color: var(--text); font-weight: 600; }
  .state[data-outcome='failed'], .state[data-outcome='interrupted'] { color: var(--danger-text); }
  .state[data-outcome='completed'] { color: var(--ok-text); }
  .inline { display: grid; gap: 8px; padding: 0 12px 10px 14px; }
  @media (hover: hover) and (prefers-reduced-motion: no-preference) {
    .inline.tucked { grid-template-rows: 0fr; padding-bottom: 0; overflow: hidden; transition: grid-template-rows 200ms var(--ease), padding 200ms var(--ease); }
    .inline.tucked > :global(*) { min-height: 0; overflow: hidden; opacity: 0; transition: opacity 160ms; }
    .card:hover .inline.tucked, .card:focus-within .inline.tucked, .card.selected .inline.tucked { grid-template-rows: 1fr; padding-bottom: 12px; }
    .card:hover .inline.tucked > :global(*), .card:focus-within .inline.tucked > :global(*), .card.selected .inline.tucked > :global(*) { opacity: 1; }
  }
  @media (hover: hover) and (prefers-reduced-motion: reduce) {
    .inline.tucked { display: none; }
    .card:hover .inline.tucked, .card:focus-within .inline.tucked, .card.selected .inline.tucked { display: grid; }
  }
  .q :global(.ask) { margin: 0; }
  .reply { display: flex; gap: 8px; align-items: flex-end; }
  .reply textarea { flex: 1; min-width: 0; resize: none; field-sizing: content; max-height: 120px; }
  .acts { display: flex; gap: 8px; flex-wrap: wrap; }
  .bad { margin: 0; font-size: 12px; color: var(--danger-text); }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  @media (prefers-reduced-motion: no-preference) {
    .card[data-tone='needs'] { animation: wb-breathe 2.6s ease-in-out infinite; }
  }
  @media (max-width: 899px) { .open-card, .btn { min-height: 44px; } }
</style>
