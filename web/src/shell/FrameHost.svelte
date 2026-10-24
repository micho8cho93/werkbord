<script lang="ts">
  // Every workspace the person has opened lives in a frame of its own and stays there, hidden when another is shown, so that
  // going to Individual from a Team ticket and back changes nothing in either. Settings and Help show a workspace's page too, in
  // frames of their own (`hosted`), so that showing a workspace's settings never moves the workspace the person is in. The page of
  // a workspace cannot call the app; it posts messages to this page, which believes only the ones frames.ts accepts.
  import { ask } from './native';
  import { handle, hostedUrl, type FrameRef } from './frames';
  import { generatedWorkspaceID } from './nav';
  import { model } from './model.svelte';
  import { alerts } from './alerts.svelte';
  import { theme } from '../lib/theme.svelte';
  import { salie } from './salie.svelte';

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

  /** Tells a workspace the theme the person chose in the sidebar, which is where it is chosen. Nothing is sent until they choose. */
  function tell(id: string, hosted: boolean): void {
    if (theme.choice) sendTo(id, hosted, { type: 'werkbord.theme', theme: theme.choice });
  }

  /** Asks an open workspace to go to a place inside itself. */
  export function navigate(id: string, href: string, hosted = false): void {
    sendTo(id, hosted, { type: 'werkbord.navigate', href });
  }

  function onMessage(e: MessageEvent): void {
    void handle(refs(), e, {
      onboarding: (required, paused) => { model.setupRequired = required; model.setupPaused = paused; },
      saliePlacement: value => { if (value) salie.set(value); return { ...salie.placements }; },
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
      // "Open in Werkbord" on a Team ticket: show the person's own Werkbord at that task.
      open: (_id, _target, place) => {
        const generated = generatedWorkspaceID(place);
        if (generated) void model.followGenerated(generated);
        else void model.open('personal', place);
      },
      // The assistant has a frame of its own (AssistantFrame); a workspace's page has nothing to say about it.
      assistant: () => {},
      personalAssistant: (workspaceId, revision) => {
        const rect = model.assistantButton?.getBoundingClientRect();
        void model.toggleAssistant(rect ? { x: rect.left, y: rect.top, width: rect.width, height: rect.height } : { x: 24, y: 24, width: 0, height: 0 }, workspaceId, revision);
      },
      // The theme is chosen in one place for the whole window: a page that offers its own tells the sidebar, which tells the rest.
      theme: (_id, choice) => theme.set(choice),
      notify: (_id, alert) => void alerts.tell(alert),
      settings: (id, place) => model.openSettingsAt(id, place),
    });
  }

  // Onboarding and Settings edit the same per-device placement preference.
  $effect(() => {
    const value = { ...salie.placements };
    if (model.frames.personal?.ready) sendTo('personal', false, { type: 'werkbord.salie-placement', value });
  });

  // Changing the theme in the sidebar changes it in every workspace that is open.
  $effect(() => {
    const choice = theme.choice;
    if (!choice) return;
    for (const { id, hosted } of frameList) sendTo(id, hosted, { type: 'werkbord.theme', theme: choice });
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
    <iframe
      bind:this={elements[key]}
      class="frame"
      hidden={model.shownWorkspace !== id || (model.page === 'workspace') === hosted}
      title={(model.view?.items.find((i) => i.id === id)?.name ?? 'Workspace') + (hosted ? ' settings' : '')}
      src={hosted ? hostedUrl(f.target.url) : f.target.url}
      referrerpolicy="no-referrer"
      sandbox="allow-scripts allow-same-origin allow-forms allow-modals allow-downloads"
      data-workspace={hosted ? undefined : id}
      data-hosted={hosted ? id : undefined}
    ></iframe>
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
