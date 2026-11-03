<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import BriefView from '../assistant/BriefView.svelte';
  import { handle, type FrameRef } from '../../shell/frames';
  import { look, theme } from '../theme.svelte';

  // A short write-up from Salie, drawn here so it is read without leaving Work. The buttons only ask; the write-up is made and shown by
  // a frame of the person's own Werkbord (BriefSurface), so no word of it reaches this page: it is told whether the frame is writing,
  // done or failed, and how tall it is. Salie is asked only when one of these is pressed, never when Work opens.
  const ranges = [
    { label: 'Last day', ask: 'Summarize the last day' },
    { label: 'Last 3 days', ask: 'Summarize the last 3 days' },
    { label: 'Last week', ask: 'Summarize the last week' },
  ];
  // `children`: what changed since the last visit, which needs no one to write it, shown above the write-up Salie can add to it.
  // The window and the web app each say where the frame is, what it may ask of its parent, and which project it is about.
  let { scope, projectContext, frame, inline = false, relay, prepare, hidden = false, children }: {
    scope: string;
    projectContext: { workspace: string; project: string; label: string } | null;
    /** The frame that writes the brief, and the origin it is served from; none until the person's own Werkbord can be reached. */
    frame: { url: string; origin: string } | null;
    /** Write it in this page instead of a frame: where the page that holds the card is the person's own Werkbord. */
    inline?: boolean;
    /** What the frame may ask of the page that holds it; left out where it may ask nothing. */
    relay?: (id: string, method: string, args: unknown[]) => Promise<unknown>;
    /** Gets the frame ready: called when the card opens and whenever it might have become possible. */
    prepare?: () => void;
    /** Salie is not available here. */
    hidden?: boolean;
    children?: Snippet;
  } = $props();

  let el: HTMLIFrameElement | undefined = $state();
  let loaded = $state(false);
  let phase = $state<'idle' | 'working' | 'done' | 'failed'>('idle');
  let height = $state(0);
  let wanted = $state('');
  let queued: string | null = null;
  let view: BriefView | undefined = $state();

  onMount(() => { prepare?.(); });
  // The person's own Werkbord may become ready after Work opens.
  $effect(() => { void frame; prepare?.(); });

  const refs = (): FrameRef[] => (frame ? [{ id: 'personal', surface: true, origin: frame.origin, window: el?.contentWindow ?? null }] : []);
  function send(message: Record<string, unknown>): void {
    el?.contentWindow?.postMessage(message, frame?.origin ?? '');
  }
  function tell(): void {
    send({ type: 'werkbord.theme', theme: theme.choice });
    send({ type: 'werkbord.style', style: look.choice });
  }
  $effect(() => { void theme.choice; void look.choice; if (loaded) tell(); });

  function post(prompt: string): void {
    send({ type: 'werkbord.brief', action: 'ask', prompt, projectContext });
  }
  function write(text: string, label: string): void {
    wanted = label;
    phase = 'working';
    if (inline) { void view?.ask(text, projectContext); return; }
    if (loaded) post(text); else queued = text;
  }

  function onMessage(e: MessageEvent): void {
    void handle(refs(), e, {
      // The same things the assistant's frame may ask of the app (so Salie can read the person's other workspaces), and nothing else.
      relay: (id, method, args) => relay ? relay(id, method, args) : Promise.reject(new Error('Not available here.')),
      reply: (_frame, message) => send(message),
      remember: async () => {},
      ready: () => {},
      theme: () => {},
      notify: () => {},
      settings: () => {},
      open: () => {},
      agentSession: () => {},
      assistant: () => {},
      brief: (state, h) => {
        // The first report is the page saying it is listening.
        if (!loaded) { loaded = true; tell(); if (queued) { post(queued); queued = null; } }
        phase = state;
        height = h;
      },
    });
  }
</script>

<svelte:window onmessage={onMessage} />

{#if !hidden}
  <section class="tray brief" aria-labelledby="brief-h" data-testid="work-brief" data-phase={phase}>
    <header><h2 id="brief-h" class="section-title">Catch up</h2><span class="tray-note">{scope}</span></header>
    <div class="tray-card card">
      {@render children?.()}
      <p class="note">A short write-up from Salie on {scope}: what needs you, what is blocked, what was done, what is ahead. She is asked only when you press one.</p>
      <div class="ranges" role="group" aria-label="Write a brief">
        {#each ranges as r (r.label)}
          <button type="button" class="btn small" data-ask={r.ask} disabled={phase === 'working'} onclick={() => write(r.ask, r.label)}>{r.label}</button>
        {/each}
        <button type="button" class="btn primary small" disabled={phase === 'working'} onclick={() => write('Plan my day', 'Plan my day')}>Plan my day</button>
      </div>
      {#if phase === 'working'}<p class="status" role="status"><span class="spin" aria-hidden="true"></span>Salie is writing{wanted ? ` · ${wanted.toLowerCase()}` : ''}…</p>{/if}
      {#if inline}
    <div class="out inline" class:open={height > 0} style:height="auto" data-testid="brief-output"><BriefView bind:this={view} onreport={(r) => { phase = r.state; height = r.height; }} /></div>
  {:else if frame}
        <iframe
          bind:this={el}
          class="out"
          class:open={height > 0}
          style:height="{height}px"
          title="Salie’s write-up"
          src={frame.url}
          referrerpolicy="no-referrer"
          sandbox="allow-scripts allow-same-origin"
          data-testid="brief-frame"
          onload={tell}
        ></iframe>
      {/if}
    </div>
  </section>
{/if}

<style>
  .card { padding: 14px 16px; display: grid; gap: 12px; }
  .note { margin: 0; font-size: 13px; line-height: 1.5; color: var(--text-2); }
  .ranges { display: flex; flex-wrap: wrap; gap: 8px; }
  .status { margin: 0; display: flex; align-items: center; gap: 10px; font-size: 13px; color: var(--text-2); }
  .out { display: block; width: 100%; border: 0; background: transparent; height: 0; color-scheme: normal; }
  .out.open { border-top: 1px solid var(--border); padding-top: 12px; box-sizing: content-box; }
</style>
