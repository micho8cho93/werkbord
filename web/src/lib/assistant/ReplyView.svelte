<script lang="ts">
  import { Renderer, type ActionEvent, type ParseResult } from '@openuidev/svelte-lang';
  import { assistant } from './store.svelte';
  import Markdown from './Markdown.svelte';
  import { openUILibrary } from './openui/library';
  import { replyParts } from './openui/reply';
  import { lookedAt, progressLine, type ReplyItem } from './transcript';
  import { failure, operationFailureTitle } from './words';
  import { conversationMessage } from './openui/state';

  let { reply }: { reply: ReplyItem } = $props();
  // The OpenUI renderer takes its form-state snapshot once; later edits are retained internally and saved separately.
  // svelte-ignore state_referenced_locally
  const initialUIState = reply.uiState ?? {};
  let openUIResults = $state<Record<number, { ready: boolean; hasRoot: boolean; invalid: boolean }>>({});
  let actionProblem = $state('');
  const line = $derived(reply.status === 'working' ? progressLine(reply) : '');
  const looked = $derived(reply.status === 'done' ? lookedAt(reply) : []);
  const parts = $derived(replyParts(reply.text, reply.status === 'working'));
  const fail = $derived(reply.error ? failure(reply.error) : null);
  const failedSteps = $derived(reply.steps.filter((s) => s.status === 'error'));

  function openUIParsed(index: number, result: ParseResult | null): void {
    openUIResults[index] = {
      ready: true,
      hasRoot: result?.root !== null && result?.root !== undefined,
      invalid: !!result && (result.meta.incomplete || result.meta.errors.length > 0 || result.meta.unresolved.length > 0),
    };
  }

  function openUIAction(event: ActionEvent): void {
    const result = conversationMessage(event);
    if (!result.ok) {
      actionProblem = result.error;
      return;
    }
    if (assistant.running) {
      actionProblem = 'The assistant is still replying. Wait for it to finish before sending this follow-up.';
      return;
    }
    actionProblem = '';
    void assistant.send(result.message);
  }
</script>

<article class="reply" aria-busy={reply.status === 'working'} data-status={reply.status}>
  {#each parts as part, i (i)}
    {#if part.kind === 'text'}
      {#if part.text.trim() !== ''}<Markdown text={part.text} />{/if}
    {:else}
      <section class="openui" aria-label="Generated interface">
        <Renderer
          response={part.text}
          library={openUILibrary}
          isStreaming={reply.status === 'working'}
          initialState={initialUIState}
          onStateUpdate={(state) => assistant.updateOpenUIState(reply.id, state)}
          onAction={openUIAction}
          onParseResult={(result) => openUIParsed(i, result)}
        />
        {#if actionProblem}<p class="openui-action-error" role="alert" data-testid="openui-action-error">{actionProblem}</p>{/if}
        {#if (reply.status !== 'working' && !part.closed) || (part.closed && openUIResults[i]?.ready && (!openUIResults[i].hasRoot || openUIResults[i].invalid))}
          <div class="openui-fallback" role="status">
            <p>{part.closed ? "I couldn't render all of this interface." : 'This interface did not finish, so some content may be missing.'}</p>
            <details>
              <summary>Show the generated interface text</summary>
              <pre>{part.text.slice(0, 4000)}{part.text.length > 4000 ? '\n… preview limited to 4,000 characters' : ''}</pre>
            </details>
          </div>
        {/if}
      </section>
    {/if}
  {/each}

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
  .openui {
    display: grid;
    gap: 8px;
    min-width: 0;
    max-width: 100%;
  }
  .openui-fallback {
    min-width: 0;
    padding: 9px 11px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface-2);
    color: var(--text-2);
    font-size: 12.5px;
    overflow-wrap: anywhere;
  }
  .openui-action-error {
    margin: 0;
    padding: 9px 11px;
    border: 1px solid var(--danger);
    border-radius: var(--radius-sm);
    color: var(--danger-text);
    background: var(--surface);
    font-size: 12.5px;
  }
  .openui-fallback p { margin: 0 0 6px; }
  .openui-fallback summary { width: fit-content; cursor: pointer; }
  .openui-fallback pre {
    max-width: 100%;
    max-height: 220px;
    margin: 8px 0 0;
    overflow: auto;
    color: var(--text);
    font: 11.5px/1.5 var(--mono);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
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
