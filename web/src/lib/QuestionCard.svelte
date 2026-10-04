<script lang="ts">
  import { api } from './api';
  import { app } from './state.svelte';
  import type { Question } from './types';

  let { question }: { question: Question } = $props();

  let text = $state('');
  let busy = $state(false);
  let error = $state('');

  const approval = $derived(question.kind === 'approval');
  /** An approval is one tap; anything else can also take a typed answer. */
  const options = $derived(question.options?.length ? question.options : approval ? ['Allow', 'Deny'] : []);

  async function answer(value: string) {
    value = value.trim();
    if (!value || busy) return;
    busy = true;
    error = '';
    try {
      await api.answerQuestion(question.id, value);
      text = '';
      await app.loadQuestions();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  const isDeny = (o: string) => /^(deny|no|decline|reject)/i.test(o);
  const isAllow = (o: string) => /^(allow|yes|approve|accept)$/i.test(o);
</script>

<div class="ask" data-kind={question.kind}>
  <p class="prompt">{question.prompt}</p>

  {#if options.length}
    <div class="options">
      {#each options as o (o)}
        <button
          class="btn small"
          class:primary={isAllow(o)}
          class:danger={isDeny(o)}
          disabled={busy}
          onclick={() => answer(o)}
        >
          {o}
        </button>
      {/each}
    </div>
  {/if}

  {#if !approval}
    <form
      class="free"
      onsubmit={(e) => {
        e.preventDefault();
        void answer(text);
      }}
    >
      <label class="visually-hidden" for="answer-{question.id}">{options.length ? 'Or type your own answer' : 'Your answer'}</label>
      <input
        id="answer-{question.id}"
        class="input"
        placeholder={options.length ? 'Or type your own answer' : 'Your answer'}
        autocomplete="off"
        bind:value={text}
      />
      <button class="btn small primary" type="submit" disabled={busy || !text.trim()}>Answer</button>
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
  }

  .prompt {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-weight: 550;
  }

  .ask[data-kind='approval'] .prompt {
    font-family: var(--mono);
    font-size: 0.88rem;
    font-weight: 500;
  }

  .options,
  .free {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .free .input {
    flex: 1 1 180px;
    min-height: 34px;
  }
</style>
