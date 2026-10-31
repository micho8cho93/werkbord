<script lang="ts">
  // Every workspace the person has opened lives in a frame of its own and stays there, hidden when another is shown, so that
  // going to Individual from a Team ticket and back changes nothing in either. Settings and Help show a workspace's page too, in
  // frames of their own (`hosted`), so that showing a workspace's settings never moves the workspace the person is in. The page of
  // a workspace cannot call the app; it posts messages to this page, which believes only the ones frames.ts accepts.
  import { ask } from './native';
  import { handle, hostedUrl, type FrameRef } from './frames';
  import { model } from './model.svelte';
  import { alerts } from './alerts.svelte';
  import { parseChain } from '../lib/origin';
  import { look, theme } from '../lib/theme.svelte';

  let { shown }: { shown: boolean } = $props();

  // A frame is told apart by where it sits: the workspace's own, or the one that shows Settings or Help for it.
  const elements: Record<string, HTMLIFrameElement | undefined> = {};
  const keyOf = (id: string, hosted: boolean): string => (hosted ? 'hosted:' : '') + id;
  const frameList = $derived([
    ...Object.entries(model.frames).map(([id, f]) => ({ key: keyOf(id, false), id, hosted: false, f })),
    ...Object.entries(model.hosted).map(([id, f]) => ({ key: keyOf(id, true), id, hosted: true, f })),
  ]);

  const refs = (): FrameRef[] =>
    frameList.map(({ key, id, hosted, f }) => ({ id, hosted, origin: f.target.origin, window: elements[key]?.contentWindow ?? null }));

  function send(key: string, origin: string | undefined, message: Record<string, unknown>): void {
    elements[key]?.contentWindow?.postMessage(message, origin ?? '');
  }
  const sendTo = (id: string, hosted: boolean, message: Record<string, unknown>): void => {
    const f = hosted ? model.hosted[id] : model.frames[id];
    send(keyOf(id, hosted), f?.target.origin, message);
  };

  /**
   * Tells a workspace the theme and the style the person chose in Settings, which is where they are chosen. The style is told
   * even when it is Paper: a workspace on another origin keeps its own copy, and one that was closed when the person went back to
   * Paper would otherwise open still showing the style it had before.
   */
  function tell(id: string, hosted: boolean): void {
    sendTo(id, hosted, { type: 'werkbord.theme', theme: theme.choice });
    sendTo(id, hosted, { type: 'werkbord.style', style: look.choice });
  }

  /** Asks an open workspace to go to a place inside itself. */
  export function navigate(id: string, href: string, hosted = false): void {
    sendTo(id, hosted, { type: 'werkbord.navigate', href });
  }

  function onMessage(e: MessageEvent): void {
    void handle(refs(), e, {
      onboarding: (required, paused) => { model.setupRequired = required; model.setupPaused = paused; },
      relay: (id, method, args) => ask((a) => a.Relay(id, method, args)),
      remember: (id, place) => {
        model.setPlace(id, place);
        return ask((a) => a.RememberPlace(id, place));
      },
      ready: (id) => {
        // The frame that sent this is the one whose window it came from; both frames of a workspace say the same words.
        const hosted = refs().find((r) => r.id === id && r.window === e.source)?.hosted ?? false;
        tell(id, hosted);
        const place = model.frameReady(id, hosted);
        if (place) navigate(id, place, hosted);
      },
      reply: (frame, message) => sendTo(frame.id, !!frame.hosted, message),
      reauth: (id, manual) => model.reauth(id, manual),
      // "Open in Werkbord" on a Team ticket: show the person's own Werkbord at that task.
      open: (_id, _target, place) => {
        void model.open('personal', place);
      },
      // The assistant has a frame of its own (AssistantFrame); a workspace's page has nothing to say about it.
      assistant: () => {},
      workSalie: (workspace, project, plan) => {
        const pid = workspace === 'personal' ? project : `workspace:${workspace}/${project}`;
        void model.explainCatchUp(plan ? `Help me edit Work plan ${plan} in project ${pid}. Read list_work_plans and the associated tasks first.` : `Help me organise work in project ${pid}. Read its tasks and plans first.`);
      },
      agentSession: (key, from) => { void model.openAgents(key, from ? parseChain(from) : []); },
      page: page => model.returnTo(page),
      openSalie: conversation => { void model.openSalie(conversation); },
      // The theme is chosen in one place for the whole window: a page that offers its own tells the sidebar, which tells the rest.
      theme: (_id, choice) => theme.set(choice),
      style: (_id, choice) => look.set(choice),
      notify: (_id, alert) => void alerts.tell(alert),
      settings: (id, place) => model.openSettingsAt(id, place),
      plan: () => model.openSettings('general/plan'),
    });
  }

  // Changing the theme in Settings changes it in every workspace that is open, and so does going back to Device: a workspace that
  // was told Dark follows the device again. The window also owns the initial Device choice.
  $effect(() => {
    const choice = theme.choice;
    for (const { id, hosted } of frameList) sendTo(id, hosted, { type: 'werkbord.theme', theme: choice });
  });

  // The style is chosen in the sidebar the same way, and told to every workspace that is open. Paper is told too, so a workspace
  // that was showing another style goes back to it.
  $effect(() => {
    const choice = look.choice;
    for (const { id, hosted } of frameList) sendTo(id, hosted, { type: 'werkbord.style', style: choice });
  });

  // A place asked for while the page was still loading is followed once it says it is ready; one asked for after is followed now.
  $effect(() => {
    for (const { id, hosted, f } of frameList) {
      if (f.ready && f.pending) {
        const p = f.pending;
        f.pending = undefined;
        navigate(id, p, hosted);
      }
    }
  });
</script>

<svelte:window onmessage={onMessage} />

<div class="frames" hidden={!shown}>
  {#each frameList as { key, id, hosted, f } (key)}
    {#key f.epoch ?? 0}
    <iframe
      bind:this={elements[key]}
      class="frame"
      hidden={model.shownWorkspace !== id || (model.page === 'workspace' || model.agentsInFrame) === hosted}
      title={(model.view?.items.find((i) => i.id === id)?.name ?? 'Workspace') + (hosted ? ' settings' : '')}
      src={hosted ? hostedUrl(f.target.url) : f.target.url}
      referrerpolicy="no-referrer"
      sandbox="allow-scripts allow-same-origin allow-forms allow-modals allow-downloads"
      data-workspace={hosted ? undefined : id}
      data-hosted={hosted ? id : undefined}
    ></iframe>
    {/key}
  {/each}
</div>


<style>
  .frames {
    flex: 1;
    min-height: 0;
    min-width: 0;
    position: relative;
  }
  .frames[hidden] {
    display: none;
  }
  .frame {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border: 0;
    background: var(--bg);
  }
  .frame[hidden] {
    display: none;
  }
</style>
