<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import { ask } from './native';
  import { handle, type FrameRef } from './frames';
  import { model } from './model.svelte';
  import { look, theme } from '../lib/theme.svelte';

  // A short write-up from Salie, drawn here so it is read without leaving Work. The buttons only ask; the write-up is made and shown by
  // a frame of the person's own Werkbord (BriefSurface), so no word of it reaches this page: it is told whether the frame is writing,
  // done or failed, and how tall it is. Salie is asked only when one of these is pressed, never when Work opens.
  const ranges = [
    { label: 'Last day', ask: 'Summarize the last day' },
    { label: 'Last 3 days', ask: 'Summarize the last 3 days' },
    { label: 'Last week', ask: 'Summarize the last week' },
  ];
  // `children`: what changed since the last visit, which needs no one to write it, shown above the write-up Salie can add to it.
  let { children }: { children?: Snippet } = $props();
  const scope = $derived(model.selectedProject ? model.contextLabel : 'all projects');

  let el: HTMLIFrameElement | undefined = $state();
  let loaded = $state(false);
  let phase = $state<'idle' | 'working' | 'done' | 'failed'>('idle');
  let height = $state(0);
  let wanted = $state('');
  let queued: string | null = null;

  onMount(() => { void model.prepareBrief().catch(() => {}); });
  // The window's own Werkbord may become ready after Work opens.
  $effect(() => { if (model.personalReady) void model.prepareBrief().catch(() => {}); });

  const refs = (): FrameRef[] => (model.briefFrame ? [{ id: 'personal', surface: true, origin: model.briefFrame.origin, window: el?.contentWindow ?? null }] : []);
  function send(message: Record<string, unknown>): void {
    el?.contentWindow?.postMessage(message, model.briefFrame?.origin ?? '');
  }
  function tell(): void {
    send({ type: 'werkbord.theme', theme: theme.choice });
    send({ type: 'werkbord.style', style: look.choice });
  }
  $effect(() => { void theme.choice; void look.choice; if (loaded) tell(); });

  function post(prompt: string): void {
    send({ type: 'werkbord.brief', action: 'ask', prompt, projectContext: model.selectedProject ? { workspace: model.selectedProject.workspace, project: model.selectedProject.project, label: model.contextLabel } : null });
  }
  function write(text: string, label: string): void {
    wanted = label;
    phase = 'working';
    if (loaded) post(text); else queued = text;
  }

  function onMessage(e: MessageEvent): void {
    void handle(refs(), e, {
      // The same things the assistant's frame may ask of the app (so Salie can read the person's other workspaces), and nothing else.
      relay: (id, method, args) => {
        if (method === 'AssistantWorkspaces' && args.length === 0) return ask(a => a.AssistantWorkspaces());
        if (method === 'AssistantWorkspaceCall' && args.length === 6 && args.slice(0, 5).every(v => typeof v === 'string')) {
          return ask(a => a.AssistantWorkspaceCall(args[0] as string, args[1] as string, args[2] as string, args[3] as string, args[4] as string, args[5]));
        }
        return ask((a) => a.Relay(id, method, args));
      },
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

{#if !model.remotePersonal}
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
      {#if model.briefFrame}
        <iframe
          bind:this={el}
          class="out"
          class:open={height > 0}
          style:height="{height}px"
          title="Salie’s write-up"
          src={model.briefFrame.url}
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
