<script lang="ts">
  import { tick } from 'svelte';
  import { app } from '../state.svelte';
  import { confirmAction } from '../confirm.svelte';
  import { agentName, runStatus } from '../format';
  import FeedView from '../FeedView.svelte';
  import QuestionCard from '../QuestionCard.svelte';
  import { taskHref } from '../router.svelte';
  import { embedded } from '../embed';
  import { assistant } from './sessions.svelte';
  import { RunSession, liveRun, messageableRun } from './run-session.svelte';
  import type { Controls } from '../agent-overview';
  // `taskLink` is off where the page already has its own link to the task (Agents), which also carries the way back.
  // `header` is off where the page already names the session once (Agents). `controls` limits what is offered to what this controller can
  // do to the session; without it the session is driven as it always was (Salie, the phone).
  // `variant` is "terminal" where the session sits in Agents: the run's feed is drawn like the agent's own command line (dark, monospaced,
  // a prompt to type at), with its questions pinned above it. It is still the controller's structured run, so questions, approvals and
  // Stop are the same ones; only the way it looks and is typed at changes.
  let { session, taskLink = true, header = true, controls, variant = 'chat' }: { session: RunSession; taskLink?: boolean; header?: boolean; controls?: Controls; variant?: 'chat' | 'terminal' } = $props();
  const may = $derived({ message: controls?.message ?? true, stop: controls?.stop ?? true });
  let output: HTMLDivElement | undefined = $state();
  let following = $state(true);
  $effect(() => {
    void session.feed.items;
    if (following) void tick().then(() => { if (output) output.scrollTop = output.scrollHeight; });
  });
  const run = $derived(session.run);
  // Stopping ends work in progress, so it asks first, as Stop does on an Agents card.
  async function stopRun() {
    if (await confirmAction({ title: 'Stop this run?', message: 'The agent stops where it is. Work it has already done stays in its working copy.', confirmLabel: 'Stop run', danger: true })) void session.act('stop');
  }
  function ticket(event: MouseEvent) {
    if (!run) return;
    assistant.hide();
    if (embedded()) {
      event.preventDefault();
      window.parent.postMessage({ type: 'werkbord.frame', event: 'open', target: 'personal', place: taskHref(run.projectId, run.taskId) }, '*');
    }
  }
</script>
{#snippet where()}{session.worktree?.path ?? session.project?.repoPath ?? 'Loading working context…'}{run?.branch || session.worktree?.branch ? ` · ${run?.branch ?? session.worktree?.branch}` : ''} · Run {session.runId}{run?.runnerId ? ` · Runner ${run.runnerId}` : ''}{/snippet}
{#if variant === 'terminal'}
<section class="console term" aria-label="Live task session">
  <div class="tbar">
    <span class="ctx" title={`${session.worktree?.path ?? session.project?.repoPath ?? ""}`}>{@render where()}</span>
    {#if run && liveRun(run)}<span class="pulse" role="status" aria-label="Session is live"></span>{/if}
    {#if run && liveRun(run) && may.message && !session.questions.length && (run.state === 'blocked' || (run.state === 'waiting_for_user' && run.waiting === 'idle'))}<button class="btn small" disabled={session.busy} onclick={() => session.act('finish')}>Finish run</button>{/if}
    {#if run && liveRun(run) && may.stop}<button class="btn small" disabled={session.busy} onclick={stopRun}>Stop run</button>{/if}
  </div>
  {#if may.message && session.questions.length}<div class="pinned">{#each session.questions as question (question.id)}<QuestionCard {question} revealContext />{/each}</div>{/if}
  <div bind:this={output} onscroll={() => { if (output) following = output.scrollTop + output.clientHeight >= output.scrollHeight - 100; }} class="output" tabindex="-1" role="region" aria-label="Session output">
    {#if session.feed.loading}<p>Loading activity…</p>{/if}
    <FeedView items={session.feed.items} hasMore={session.feed.hasMore} loadingOlder={session.feed.loadingOlder} onOlder={() => session.feed.loadOlder()} answeredBelow={new Set(session.questions.map(q => q.id))} />
    {#if run?.activity}<p class="activity">{run.activity}</p>{/if}
    {#if run?.reason}<p>{run.reason}</p>{/if}
    {#if run && !liveRun(run)}<p class="ended">This session has ended. Its activity remains available.</p>{/if}
  </div>
  {#if session.error || session.feed.error}<p class="alert" role="alert">{session.error || session.feed.error} <button class="btn small" onclick={() => session.refresh()}>Refresh session</button></p>{/if}
  {#if run && liveRun(run)}
    {#if !may.message && !may.stop}<p class="viewonly" role="status">{controls?.reason || 'This session is shown, not operated, from here.'}</p>
    {:else if may.message}
      <form class="prompt" onsubmit={e => { e.preventDefault(); void session.act('send'); }}>
        <span class="caret" aria-hidden="true">❯</span>
        <label class="visually-hidden" for="run-input-{session.runId}">Additional instructions</label>
        <textarea id="run-input-{session.runId}" rows="1" bind:value={session.draft} disabled={session.busy || !messageableRun(run)} placeholder={messageableRun(run) ? 'Type to this agent, Enter to send' : 'Answer the pending request to continue'}
          onkeydown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); void session.act('send'); } }}></textarea>
        {#if run.pendingRequests?.length}<span class="wait" role="status">Waiting for runner acknowledgement: {run.pendingRequests.join(', ')}</span>{/if}
      </form>
    {/if}
  {/if}
</section>
{:else}
<section class="console" aria-label="Live task session">
  {#if header}<header>
    <strong>{run ? agentName(app.agents, run.agentId) : 'Loading agent…'}</strong>
    <span>{session.project?.name ?? session.projectId} · {run ? runStatus(run).label : 'Loading'}</span>
    {#if run && taskLink}<a href={taskHref(run.projectId, run.taskId)} onclick={ticket}>Return to task</a>{/if}
  </header>{/if}
  <p class="context">{session.worktree?.path ?? session.project?.repoPath ?? 'Loading working context…'}{run?.branch || session.worktree?.branch ? ` · ${run?.branch ?? session.worktree?.branch}` : ''} · Run {session.runId}{run?.runnerId ? ` · Runner ${run.runnerId}` : ''}</p>
  <div bind:this={output} onscroll={() => { if (output) following = output.scrollTop + output.clientHeight >= output.scrollHeight - 100; }} class="output" tabindex="-1" role="region" aria-label="Session output">
    {#if session.feed.loading}<p>Loading activity…</p>{/if}
    <FeedView items={session.feed.items} hasMore={session.feed.hasMore} loadingOlder={session.feed.loadingOlder} onOlder={() => session.feed.loadOlder()} answeredBelow={new Set(session.questions.map(q => q.id))} />
    {#if run?.activity}<p>{run.activity}</p>{/if}
    {#if run?.reason}<p>{run.reason}</p>{/if}
  </div>
  {#if session.error || session.feed.error}<p role="alert">{session.error || session.feed.error} <button class="btn small" onclick={() => session.refresh()}>Refresh session</button></p>{/if}
  <div class="controls">
    {#if may.message}{#each session.questions as question (question.id)}<QuestionCard {question} />{/each}{/if}
    {#if run && liveRun(run) && !may.message && !may.stop}<p class="viewonly" role="status">{controls?.reason || 'This session is shown, not operated, from here.'}</p>
    {:else if run && liveRun(run)}
      {#if may.message}<form onsubmit={e => { e.preventDefault(); void session.act('send'); }}>
        <label class="visually-hidden" for="run-input-{session.runId}">Additional instructions</label>
        <textarea id="run-input-{session.runId}" class="input" rows="2" bind:value={session.draft} disabled={session.busy || !messageableRun(run)} placeholder={messageableRun(run) ? 'Send instructions to this agent' : 'Answer the pending request to continue'}></textarea>
        <button class="btn primary" disabled={session.busy || !messageableRun(run) || !session.draft.trim()}>Send</button>
      </form>{/if}
      <div class="actions">
        {#if may.message && !session.questions.length && (run.state === 'blocked' || (run.state === 'waiting_for_user' && run.waiting === 'idle'))}<button class="btn" disabled={session.busy} onclick={() => session.act('finish')}>Finish run</button>{/if}
        {#if may.stop}<button class="btn danger" disabled={session.busy} onclick={stopRun}>Stop run</button>{/if}
        {#if run.pendingRequests?.length}<span role="status">Waiting for runner acknowledgement: {run.pendingRequests.join(', ')}</span>{/if}
      </div>
    {:else if run}<p>This session has ended. Its activity remains available.</p>{/if}
  </div>
</section>
{/if}
<style>
  .console { display: flex; flex-direction: column; min-height: 0; flex: 1; padding: 16px; gap: 12px; }
  header { display: flex; flex-wrap: wrap; gap: 8px 16px; align-items: baseline; }
  header span, .context { color: var(--text-2); font-size: 12px; overflow-wrap: anywhere; }
  header a { margin-left: auto; }
  .context { font-family: var(--mono); }
  .output { flex: 1; min-height: 100px; overflow: auto; padding: 16px; background: var(--surface-2); border-radius: var(--radius-sm); font: 13px/1.5 var(--mono); }
  .output :global(.text) { font-family: var(--mono); }
  .controls { display: grid; gap: 12px; max-height: 45%; overflow: auto; }
  form, .actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  textarea { flex: 1; min-width: 150px; }
  .viewonly { margin: 0; font-size: 13px; color: var(--text-2); }
  .actions span { font-size: 12px; color: var(--text-2); }

  /* The terminal look: the same dark surface as the agent's own terminal, drawn with colours of its own so it reads the same in every style. */
  .term { --bg: #0b0b0c; --surface: #141415; --surface-2: #1d1d1f; --border: #2a2a2d; --text: #f0f0ec; --text-2: #a9aaa4; --accent: #7c93ff; --ok-text: #7ad39b; --danger-text: #ff8f88; --warn-text: #f0bd62; --amber: #e0a84a;
    color-scheme: dark; background: #0b0b0c; color: var(--text); padding: 0; gap: 0; }
  .tbar { display: flex; align-items: center; gap: 12px; padding: 6px 12px; background: var(--surface); border-bottom: 1px solid var(--border); font-size: 12px; flex-wrap: wrap; }
  .tbar .ctx { flex: 1; min-width: 0; font-family: var(--mono); color: var(--text-2); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; direction: rtl; text-align: left; }
  .term :global(.btn:not(.primary):not(.danger)) { background: var(--surface-2); color: var(--text); border-color: var(--border); }
  .term :global(.btn:not(.primary):not(.danger):hover:not(:disabled)) { background: #26262a; }
  .pulse { width: 8px; height: 8px; border-radius: 50%; background: var(--ok-text); }
  .pinned { display: grid; gap: 8px; padding: 10px 12px; background: color-mix(in srgb, var(--amber) 9%, #0b0b0c); border-bottom: 1px solid var(--border); max-height: 50%; overflow: auto; }
  .term .output { background: transparent; border-radius: 0; padding: 12px 14px; }
  .term .output .activity { color: var(--text-2); }
  .term .output .ended { color: var(--text-2); font-style: italic; }
  .prompt { display: flex; align-items: flex-start; gap: 8px; padding: 8px 14px; border-top: 1px solid var(--border); background: #0b0b0c; font: 13px/1.5 var(--mono); }
  .prompt .caret { color: var(--accent); padding-top: 1px; }
  .prompt textarea { flex: 1; min-width: 0; min-height: 0; padding: 0; border: 0; outline: 0; background: transparent; color: var(--text); font: inherit; resize: none; field-sizing: content; max-height: 160px; }
  .prompt textarea::placeholder { color: var(--text-2); }
  .prompt:focus-within { box-shadow: inset 2px 0 0 var(--accent); }
  .prompt .wait { flex-basis: 100%; font-size: 12px; color: var(--text-2); }
  .term .viewonly, .term .alert { margin: 0; padding: 8px 14px; border-top: 1px solid var(--border); }
  .term .alert { color: var(--danger-text); }
  @media (prefers-reduced-motion: no-preference) { .pulse { animation: wb-pop 1.6s ease-in-out infinite alternate; } }
</style>
