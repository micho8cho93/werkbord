<script lang="ts">
  // Every workspace the person has opened lives in a frame of its own and stays there, hidden when another is shown, so that
  // going to Personal from a Team ticket and back changes nothing in either. The page of a workspace cannot call the app;
  // it posts messages to this page, which believes only the ones frames.ts accepts.
  import { ask } from './native';
  import { handle, type FrameRef } from './frames';
  import { model } from './model.svelte';

  let { shown }: { shown: boolean } = $props();

  const elements: Record<string, HTMLIFrameElement | undefined> = {};

  const refs = (): FrameRef[] =>
    Object.entries(model.frames).map(([id, f]) => ({ id, origin: f.target.origin, window: elements[id]?.contentWindow ?? null }));

  function send(id: string, message: Record<string, unknown>): void {
    const f = model.frames[id];
    elements[id]?.contentWindow?.postMessage(message, f?.target.origin ?? '');
  }

  /** Asks an open workspace to go to a place inside itself. */
  export function navigate(id: string, href: string): void {
    send(id, { type: 'werkbord.navigate', href });
  }

  function onMessage(e: MessageEvent): void {
    void handle(refs(), e, {
      relay: (id, method, args) => ask((a) => a.Relay(id, method, args)),
      remember: (id, place) => ask((a) => a.RememberPlace(id, place)),
      ready: (id) => {
        const place = model.frameReady(id);
        if (place) navigate(id, place);
      },
      reply: (frame, message) => send(frame.id, message),
      // The frame says where its product name is in its own page; the switcher opens under it, in the window.
      switcher: (id, r) => {
        if (model.switcher) return model.closeSwitcher();
        const box = elements[id]?.getBoundingClientRect();
        if (!box || model.current !== id) return;
        model.openSwitcher({ x: box.left + r.x, y: box.top + r.y, width: r.width, height: r.height, from: id });
        send(id, { type: 'werkbord.switcher', open: true });
      },
      // "Open in Individual" on a Team ticket: show the person's own Werkbord at that task.
      open: (_id, _target, place) => void model.open('personal', place),
    });
  }

  // The frame whose name opened the switcher is told it closed, and takes the focus back when the person pressed Escape.
  model.onSwitcherClosed = (from, focus) => {
    if (focus) elements[from]?.focus();
    send(from, { type: 'werkbord.switcher', open: false, focus });
  };

  // A place asked for while the page was still loading is followed once it says it is ready; one asked for after is followed now.
  $effect(() => {
    for (const [id, f] of Object.entries(model.frames)) {
      if (f.ready && f.pending) {
        const p = f.pending;
        f.pending = undefined;
        navigate(id, p);
      }
    }
  });
</script>

<svelte:window onmessage={onMessage} />

<div class="frames" hidden={!shown}>
  {#each Object.entries(model.frames) as [id, f] (id)}
    <iframe
      bind:this={elements[id]}
      class="frame"
      hidden={model.current !== id}
      title={model.view?.items.find((i) => i.id === id)?.name ?? 'Workspace'}
      src={f.target.url}
      referrerpolicy="no-referrer"
      sandbox="allow-scripts allow-same-origin allow-forms allow-modals allow-downloads"
      data-workspace={id}
    ></iframe>
  {/each}
</div>

<style>
  .frames {
    flex: 1;
    min-height: 0;
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
