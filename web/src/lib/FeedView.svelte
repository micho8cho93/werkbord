<script lang="ts">
  import type { FeedItem } from './feed';
  import { messageParts } from './handoff';
  import HandoffSummary from './HandoffSummary.svelte';
  import { answerLine, closedText, contextSummary, kindNoun } from './questions';

  let {
    items,
    hasMore = false,
    loadingOlder = false,
    onOlder,
  }: { items: FeedItem[]; hasMore?: boolean; loadingOlder?: boolean; onOlder?: () => void } = $props();

  const when = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
</script>

<ol class="feed" aria-label="Agent activity">
  {#if hasMore}
    <li class="older">
      <button class="btn small quiet" onclick={onOlder} disabled={loadingOlder}>
        {loadingOlder ? 'Loading…' : 'Load earlier activity'}
      </button>
    </li>
  {/if}

  {#each items as item (item.seq)}
    {#if item.kind === 'message'}
      <li class="msg" data-role={item.role}>
        {#each item.role === 'assistant' ? messageParts(item.text) : [{ text: item.text }] as part, i (i)}
          {#if part.handoff}<HandoffSummary handoff={part.handoff} />{:else if part.text?.trim()}<p class="text">{part.text}</p>{/if}
        {/each}
        <time datetime={item.at}>{when(item.at)}</time>
      </li>
    {:else if item.kind === 'tools'}
      {#if item.lines.length === 1}
        <li class="tool single"><span class="mono">{item.lines[0].text}</span></li>
      {:else}
        <li class="tool">
          <details>
            <summary>
              <span class="count">{item.lines.length} actions</span>
              <span class="latest mono">{item.lines[item.lines.length - 1].text}</span>
            </summary>
            <ul class="lines mono">
              {#each item.lines as line (line.seq)}
                <li>{line.text}</li>
              {/each}
            </ul>
          </details>
        </li>
      {/if}
    {:else if item.kind === 'diagnostics'}
      <li class="tool diag">
        <details>
          <summary><span class="count">Agent diagnostics</span> <span class="latest">{item.lines.length} {item.lines.length === 1 ? 'line' : 'lines'}</span></summary>
          <ul class="lines mono">
            {#each item.lines as line (line.seq)}
              <li>{line.text}</li>
            {/each}
          </ul>
        </details>
      </li>
    {:else if item.kind === 'notice'}
      <li class="notice">{item.text}</li>
    {:else if item.kind === 'marker'}
      <li class="marker" data-tone={item.tone}>
        <span class="label">{item.text}</span>
        <time datetime={item.at}>{when(item.at)}</time>
        {#if item.detail}<span class="detail">{item.detail}</span>{/if}
      </li>
    {:else if item.kind === 'question'}
      {@const q = item.question}
      <li class="asked" data-state={q.state}>
        <span class="who">{q.kind === 'approval' ? 'Asked permission' : `${kindNoun(q.kind)} for you`}</span>
        <p class="text">{q.prompt}</p>
        {#if q.context}
          <details class="ctx">
            <summary>{contextSummary(q.context)}</summary>
            <pre>{q.context}</pre>
          </details>
        {/if}
        {#if q.state === 'answered' && q.answeredBy === 'policy'}
          <p class="reply policy">{answerLine(q)}</p>
        {:else if q.state === 'answered'}
          <p class="reply">You answered: <strong>{q.answer}</strong></p>
        {:else if q.state === 'cancelled'}
          <p class="reply muted">
            {#if q.answer}You answered “{q.answer}”. {/if}{closedText(q)}
          </p>
        {:else}
          <p class="reply pending">Waiting for your answer.</p>
        {/if}
      </li>
    {/if}
  {/each}
</ol>

<style>
  .feed {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }

  .older {
    display: flex;
    justify-content: center;
  }

  .text {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  time {
    font-size: 0.72rem;
    color: var(--text-2);
  }

  .msg {
    display: grid;
    gap: 4px;
    max-width: min(100%, 46rem);
  }

  .msg[data-role='assistant'] {
    padding: 2px 0;
  }

  .msg[data-role='user'] {
    justify-self: end;
    padding: 9px 12px;
    background: color-mix(in srgb, var(--accent) 13%, var(--surface));
    border: 1px solid color-mix(in srgb, var(--accent) 30%, var(--border));
    border-radius: var(--radius);
  }

  .msg[data-role='user'] time {
    justify-self: end;
  }

  .tool {
    font-size: 0.82rem;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .tool.single {
    padding-left: 12px;
    border-left: 2px solid var(--border);
  }

  .tool details {
    padding-left: 12px;
    border-left: 2px solid var(--border);
  }

  .tool summary {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    min-height: 24px;
    cursor: pointer;
    align-items: baseline;
  }

  .count {
    font-weight: 600;
  }

  .latest {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  .lines {
    list-style: none;
    margin: 6px 0 2px;
    padding: 0;
    display: grid;
    gap: 3px;
  }

  .diag {
    opacity: 0.85;
  }

  .notice {
    font-size: 0.82rem;
    font-style: italic;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .marker {
    --tone: var(--text-2);
    display: flex;
    flex-wrap: wrap;
    gap: 2px 10px;
    align-items: baseline;
    justify-content: center;
    padding: 4px 0;
    font-size: 0.82rem;
    color: var(--tone);
  }

  .marker[data-tone='ok'] {
    --tone: var(--ok);
  }

  .marker[data-tone='bad'] {
    --tone: var(--danger);
  }

  .marker[data-tone='info'] {
    --tone: var(--accent);
  }

  .marker[data-tone='block'] {
    --tone: var(--block);
  }

  .marker .label {
    font-weight: 600;
  }

  .marker .detail {
    flex-basis: 100%;
    text-align: center;
    overflow-wrap: anywhere;
  }

  .asked {
    display: grid;
    gap: 6px;
    padding: 10px 12px;
    background: var(--surface);
    border: 1px solid color-mix(in srgb, var(--warn) 40%, var(--border));
    border-radius: var(--radius);
  }

  .asked[data-state='answered'],
  .asked[data-state='cancelled'] {
    border-color: var(--border);
  }

  .who {
    font-size: 0.72rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--warn);
  }

  .asked[data-state='answered'] .who,
  .asked[data-state='cancelled'] .who {
    color: var(--text-2);
  }

  .ctx summary {
    cursor: pointer;
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--text-2);
  }

  .ctx pre {
    margin: 6px 0 0;
    max-height: 12rem;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font: 0.8rem/1.45 var(--mono);
    color: var(--text-2);
  }

  .asked .text {
    font-size: 0.92rem;
  }

  .reply {
    font-size: 0.85rem;
  }

  /* The controller answered for you, as the run's policy says: said plainly, and not in your voice. */
  .reply.policy {
    color: var(--text-2);
    font-style: italic;
  }

  .reply.pending {
    color: var(--warn);
    font-weight: 600;
  }
</style>
