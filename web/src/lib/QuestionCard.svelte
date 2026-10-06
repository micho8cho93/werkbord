<script lang="ts">
  import { api } from './api';
  import { timeAgo } from './format';
  import {
    choicesOf,
    contextIsLong,
    contextSummary,
    describeAnswerFailure,
    freeTextHint,
    isAllow,
    isDeny,
    kindLabel,
    takesText,
  } from './questions';
  import { app } from './state.svelte';
  import type { Question } from './types';

  let { question, showMeta = true }: { question: Question; showMeta?: boolean } = $props();

  let text = $state('');
  /** The choice being sent, so its button can say so. */
  let sending = $state('');
  let busy = $state(false);
  let error = $state('');

  const choices = $derived(choicesOf(question));
  const typed = $derived(takesText(question));
  /** An approval with two choices is one tap each, side by side, like a dialog. */
  const dialog = $derived(question.kind === 'approval' && choices.length <= 3);
  const long = $derived(question.context ? contextIsLong(question.context) : false);
  /** Several lines of text suit a clarification or an instruction; a short reply suits the rest. */
  const multiline = $derived(question.kind === 'clarification' || question.kind === 'instruction');

  async function answer(value: string) {
    value = value.trim();
    if (!value || busy) return;
    busy = true;
    sending = value;
    error = '';
    try {
      const settled = await api.answerQuestion(question, value);
      app.resolveQuestion(settled);
      if(settled.state === "answered" && !settled.deliveredAt) app.notify("Answer saved. Waiting for the runner to acknowledge delivery.");
      text = '';
    } catch (err) {
      const failure = describeAnswerFailure(err);
      if (failure.settled) {
        // Someone else answered, or the agent moved on: say so, and stop offering it.
        app.resolveQuestion(question);
        app.notify(failure.message);
      } else {
        error = failure.message; // the typed answer stays, so trying again is one tap
      }
    } finally {
      busy = false;
      sending = '';
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && !busy) {
      e.preventDefault();
      void answer(text);
    }
  }
</script>

<div class="ask" data-kind={question.kind} role="group" aria-label="{kindLabel(question.kind)}: {question.prompt}">
  {#if showMeta}
    <div class="meta">
      <span class="kind">{kindLabel(question.kind)}</span>
      <time datetime={question.askedAt}>asked {timeAgo(question.askedAt, app.now)}</time>
    </div>
  {/if}

  <p class="prompt">{question.prompt}</p>

  {#if question.context}
    <details class="context" open={!long}>
      <summary>{contextSummary(question.context)}</summary>
      <pre>{question.context}</pre>
    </details>
  {/if}

  {#if choices.length}
    <div class="choices" class:dialog>
      {#each choices as o (o)}
        <button
          class="btn choice"
          class:primary={isAllow(o)}
          class:danger={isDeny(o)}
          disabled={busy}
          onclick={() => answer(o)}
        >
          {sending === o ? 'Sending…' : o}
        </button>
      {/each}
    </div>
  {/if}

  {#if typed}
    <form
      class="free"
      onsubmit={(e) => {
        e.preventDefault();
        void answer(text);
      }}
    >
      <label class="visually-hidden" for="answer-{question.id}">{freeTextHint(question)}</label>
      {#if multiline}
        <textarea
          id="answer-{question.id}"
          class="input"
          rows="2"
          placeholder={freeTextHint(question)}
          bind:value={text}
          onkeydown={onKey}
          title="Enter to send · Shift+Enter for a new line"
        ></textarea>
      {:else}
        <input
          id="answer-{question.id}"
          class="input"
          placeholder={freeTextHint(question)}
          autocomplete="off"
          bind:value={text}
          onkeydown={onKey}
        />
      {/if}
      <button class="btn primary" type="submit" disabled={busy || !text.trim()}>
        {busy && sending === text.trim() ? 'Sending…' : 'Send'}
      </button>
    </form>
  {/if}

  {#if error}
    <p class="error" role="alert">{error}</p>
  {/if}
</div>

<style>
  .ask {
    display: grid;
    gap: 10px;
    min-width: 0;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: 2px 12px;
  }

  .kind {
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--warn-text);
  }

  .meta time {
    font-size: 0.78rem;
    color: var(--text-2);
  }

  .prompt {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: 15px;
    font-weight: 600;
    line-height: 1.35;
  }

  .context {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface-2);
    min-width: 0;
  }

  .context summary {
    cursor: pointer;
    padding: 7px 10px;
    min-height: 32px;
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--text-2);
  }

  .context pre {
    margin: 0;
    padding: 0 10px 10px;
    max-height: 14rem;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font: 0.82rem/1.45 var(--mono);
    color: var(--text);
  }

  .choices {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .choice {
    min-height: 32px;
    max-width: 100%;
    justify-content: flex-start;
    text-align: left;
    white-space: normal;
    overflow-wrap: anywhere;
    padding-block: 6px;
  }

  /* On a touch screen a wrong tap is as costly as a right one: big targets, side by side for a dialog. */
  @media (pointer: coarse) {
    .choices {
      display: grid;
    }

    .choices.dialog {
      grid-template-columns: repeat(auto-fit, minmax(7.5rem, 1fr));
    }

    .choice {
      min-height: 46px;
    }

    .choices.dialog .choice {
      justify-content: center;
      text-align: center;
    }
  }

  .free {
    display: flex;
    gap: 8px;
    align-items: flex-end;
  }

  .free .input {
    flex: 1 1 auto;
    min-width: 0;
  }

  .free textarea.input {
    padding-block: 9px;
    resize: vertical;
    min-height: 56px;
  }

  @media (pointer: coarse) {
    .free .btn {
      min-height: 46px;
    }
  }
</style>
