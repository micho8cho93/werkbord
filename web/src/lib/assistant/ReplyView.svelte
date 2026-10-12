<script lang="ts">
  import { assistant } from './store.svelte';
  import Markdown from './Markdown.svelte';
  import { lookedAt, progressLine, type ReplyItem } from './transcript';
  import { failure, operationFailureTitle } from './words';

  let { reply }: { reply: ReplyItem } = $props();
  const line = $derived(reply.status === 'working' ? progressLine(reply) : '');
  const looked = $derived(reply.status === 'done' ? lookedAt(reply) : []);
  const fail = $derived(reply.error ? failure(reply.error) : null);
  const failedSteps = $derived(reply.steps.filter((s) => s.status === 'error'));
</script>

<article class="reply" aria-busy={reply.status === 'working'} data-status={reply.status}>
  {#if reply.text.trim() !== ''}<Markdown text={reply.text} />{/if}

  {#if line}
    <p class="progress"><span class="dots" aria-hidden="true"><i></i><i></i><i></i></span><span>{line}</span></p>
  {/if}

  {#each reply.notices as n (n)}<p class="note">{n}</p>{/each}

  {#if looked.length > 0}
    <details class="looked">
      <summary>What I looked at</summary>
      <ul>{#each looked as l (l)}<li>{l}</li>{/each}</ul>
    </details>
  {/if}

  {#if reply.status === 'cancelled'}<p class="note">Stopped.</p>{/if}
  {#if reply.status === 'done'}<p class="note" role="status">Reply finished.</p>{/if}

  {#each failedSteps as step (step)}
    <div class="fail" role="alert">
      <strong><span class="status"><span class="status-dot" aria-hidden="true"></span>Failed</span>{operationFailureTitle(step.name)}</strong>
      <p>{step.detail || 'Check the latest state before repeating this step.'}</p>
    </div>
  {/each}

  {#if fail}
    <div class="fail" role="alert">
      <strong><span class="status"><span class="status-dot" aria-hidden="true"></span>Failed</span>{fail.title}</strong>
      <p>{fail.detail}</p>
      <div class="acts">
        {#if fail.action === 'retry'}<button class="btn small" type="button" onclick={() => assistant.retry()}>Try again</button>{/if}
        {#if fail.action === 'model'}
          <button class="btn small" type="button" onclick={() => (assistant.pickerOpen = true)}>Choose another model</button>
          <button class="btn small quiet" type="button" onclick={() => assistant.retry()}>Try again</button>
        {/if}
        {#if fail.action === 'recheck'}
          <button class="btn small" type="button" onclick={() => assistant.prepare(true)}>Check again</button>
          <button class="btn small quiet" type="button" onclick={() => assistant.retry()}>Try again</button>
        {/if}
      </div>
    </div>
  {/if}
</article>

<style>
  .fail strong { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 16px; }
  .status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 500; }
  .status-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--danger); }
  .reply {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: 100%;
  }
  .progress {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-2);
    font-size: 13px;
  }
  .dots {
    display: inline-flex;
    gap: 3px;
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
  .note {
    color: var(--text-2);
    font-size: 12.5px;
  }
  .looked {
    font-size: 12.5px;
    color: var(--text-2);
  }
  .looked summary {
    cursor: pointer;
    width: fit-content;
  }
  .looked ul {
    margin: 6px 0 0;
    padding-left: 1.2em;
  }
  .fail {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 12px 14px;
    border: 1px solid var(--border);

    border-radius: var(--radius);
    background: var(--surface);
    font-size: 13px;
  }
  .fail strong {
    color: var(--danger-text);
    font-weight: 600;
  }
  .fail p {
    color: var(--text-2);
    overflow-wrap: anywhere;
  }
  .acts {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
</style>
