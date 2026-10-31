<script lang="ts">
  import { tick } from 'svelte';
  import { app } from '../state.svelte';
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
  let { session, taskLink = true, header = true, controls }: { session: RunSession; taskLink?: boolean; header?: boolean; controls?: Controls } = $props();
  const may = $derived({ message: controls?.message ?? true, stop: controls?.stop ?? true });
  let output: HTMLDivElement | undefined = $state();
  let following = $state(true);
  $effect(() => {
    void session.feed.items;
    if (following) void tick().then(() => { if (output) output.scrollTop = output.scrollHeight; });
  });
  const run = $derived(session.run);
  function ticket(event: MouseEvent) {
    if (!run) return;
    assistant.hide();
    if (embedded()) {
      event.preventDefault();
      window.parent.postMessage({ type: 'werkbord.frame', event: 'open', target: 'personal', place: taskHref(run.projectId, run.taskId) }, '*');
    }
  }
</script>
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
        {#if may.stop}<button class="btn danger" disabled={session.busy} onclick={() => session.act('stop')}>Stop run</button>{/if}
        {#if run.pendingRequests?.length}<span role="status">Waiting for runner acknowledgement: {run.pendingRequests.join(', ')}</span>{/if}
      </div>
    {:else if run}<p>This session has ended. Its activity remains available.</p>{/if}
  </div>
</section>
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
</style>
