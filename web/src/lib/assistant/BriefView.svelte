<script lang="ts">
  import { onMount } from 'svelte';
  import { AssistantStore } from './store.svelte';
  import { assistantProjectContext } from './project-context.svelte';
  import { provideSession } from './session-context';
  import ReplyView from './ReplyView.svelte';

  // One write-up from Salie, drawn inside a card of Work so it can be read without leaving it. It is a conversation of its own, kept
  // apart from Salie's (its own storage key), and asked only when the person presses a button in the card. The page that holds it is
  // told a state and a height, never a word of it: in the desktop window that page is a frame (BriefSurface); in the web app it is the
  // Work page itself.
  let { onreport }: { onreport?: (r: { state: 'idle' | 'working' | 'done' | 'failed'; height: number }) => void } = $props();

  const store = new AssistantStore('werkbord.brief.v1');
  provideSession(store);

  let root: HTMLDivElement | undefined = $state();
  const reply = $derived([...store.transcript.items].reverse().find((i) => i.kind === 'reply'));
  const phase = $derived<'idle' | 'working' | 'done' | 'failed'>(
    store.problem || (!reply && store.status !== 'idle' && store.status !== 'loading' && store.status !== 'ready') ? 'failed'
      : !reply ? (store.sending || store.status === 'loading' ? 'working' : 'idle')
      : reply.status === 'working' ? 'working'
      : reply.status === 'done' ? 'done'
      : 'failed',
  );
  const trouble = $derived(
    store.problem ||
      (store.status === 'no-provider' ? 'Salie has no assistant signed in yet. Open Salie to set one up.'
        : store.status === 'disabled' ? 'The assistant is turned off in Settings.'
        : store.status === 'offline' ? 'Your own Werkbord is not answering.'
        : store.status === 'unauthorized' ? 'Sign in to your own Werkbord to ask Salie.'
        : ''),
  );

  export async function ask(prompt: string, context: typeof assistantProjectContext.current): Promise<void> {
    // A new brief replaces the last: it is one write-up at a time, not a conversation that grows.
    await store.startOver();
    assistantProjectContext.current = context;
    await store.prepare();
    if (store.status !== 'ready') return;
    store.draft = prompt;
    await store.send();
  }

  /** Clears what is written. */
  export async function clear(): Promise<void> { await store.startOver(); }

  onMount(() => {
    store.init(true);
    return () => store.disconnect();
  });

  // The card is as tall as what is written, and says whether Salie is writing, done, or could not.
  $effect(() => {
    const el = root;
    if (!el) return;
    const tell = () => onreport?.({ state: phase, height: phase === 'idle' && !reply ? 0 : el.scrollHeight });
    tell();
    const watch = new ResizeObserver(tell);
    watch.observe(el);
    return () => watch.disconnect();
  });
</script>

<div class="brief" bind:this={root}>
  {#if phase === 'working' && !reply}
    <p class="working" role="status"><span class="spin" aria-hidden="true"></span>Salie is reading the work…</p>
  {:else if reply}
    <ReplyView {reply} />
  {:else if trouble}
    <p class="trouble" role="alert">{trouble}</p>
  {/if}
  {#if phase === 'failed' && !trouble && reply}
    <p class="trouble" role="alert">That brief could not be written. Press the button again to retry.</p>
  {/if}
</div>

<style>
  .brief { padding: 2px 0 4px; font-size: 14px; line-height: 1.55; color: var(--text); }
  .working, .trouble { margin: 0; display: flex; align-items: center; gap: 10px; font-size: 13px; color: var(--text-2); }
  .trouble { color: var(--warn-text); }
</style>
