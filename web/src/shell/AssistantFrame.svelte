<script lang="ts">
  // The assistant's own frame. It is a page of the person's own Werkbord that is nothing but the assistant (the controller's web app
  // with ?surface=assistant), as large as the window and above everything in it, the sidebar included. That is the whole reason for
  // it: the assistant opens out of a button in the sidebar and covers the window, and a frame that sat beside the sidebar could not.
  // The workspaces underneath are not touched: whatever was showing is still there, covered, when the assistant closes.
  //
  // It is made the first time the assistant is opened and then kept, so that closing it stops nothing (a reply goes on, a change
  // waits). While it is closed it is invisible and cannot be clicked. It reports only whether it is open, how many changes wait and
  // whether a reply is running; no word of the conversation reaches this page (frames.ts).
  import { tick, untrack } from 'svelte';
  import { ask } from './native';
  import { handle, type FrameRef } from './frames';
  import { model } from './model.svelte';
  import { theme } from '../lib/theme.svelte';

  let el: HTMLIFrameElement | undefined = $state();
  let loaded = $state(false);

  const refs = (): FrameRef[] => (model.assistantFrame ? [{ id: 'personal', surface: true, origin: model.assistantFrame.origin, window: el?.contentWindow ?? null }] : []);

  function send(message: Record<string, unknown>): void {
    el?.contentWindow?.postMessage(message, model.assistantFrame?.origin ?? '');
  }

  function onMessage(e: MessageEvent): void {
    void handle(refs(), e, {
      // What the assistant may ask of the app is what Individual's page may (the app decides by the kind of workspace, never by the page).
      relay: (id, method, args) => {
        if (method === 'AssistantWorkspaces' && args.length === 0) return ask(a => a.AssistantWorkspaces());
        if (method === 'AssistantWorkspaceCall' && args.length === 4 && args.slice(0, 3).every(v => typeof v === 'string')) {
          return ask(a => a.AssistantWorkspaceCall(args[0] as string, args[1] as string, args[2] as string, args[3]));
        }
        return ask((a) => a.Relay(id, method, args));
      },
      reply: (_frame, message) => send(message),
      remember: async () => {},
      ready: () => {},
      open: () => {},
      assistant: (_id, state) => model.setAssistant(state),
    });
  }

  // The page is told which theme the person chose in the sidebar, as every workspace is, as soon as it can listen.
  function tell(): void {
    if (theme.choice) send({ type: 'werkbord.theme', theme: theme.choice });
  }
  $effect(() => {
    void theme.choice;
    if (loaded) untrack(tell);
  });

  // A request waits until the page has loaded and the frame is on screen (a frame nobody can see has no place to measure from),
  // and the button's place, which is the window's, is the place in the frame too, because the frame is the window.
  $effect(() => {
    const request = model.assistantAsk;
    if (!request || !loaded || !el) return;
    untrack(async () => {
      model.assistantAsk = null;
      await tick();
      if (request.action !== 'close') el?.contentWindow?.focus();
      send({
        type: 'werkbord.assistant',
        action: request.action,
        origin: { x: request.rect.x + request.rect.width / 2, y: request.rect.y + request.rect.height / 2 },
      });
    });
  });
</script>

<svelte:window onmessage={onMessage} />

{#if model.assistantFrame}
  <iframe
    bind:this={el}
    class="assistant-frame"
    class:shown={model.assistantShown}
    title="Assistant"
    src={model.assistantFrame.url}
    referrerpolicy="no-referrer"
    sandbox="allow-scripts allow-same-origin allow-forms allow-modals allow-downloads"
    allow="microphone"
    aria-hidden={!model.assistantShown}
    data-testid="assistant-frame"
    onload={() => {
      loaded = true;
      tell();
    }}
  ></iframe>
{/if}

<style>
  .assistant-frame {
    position: fixed;
    inset: 0;
    width: 100%;
    height: 100%;
    border: 0;
    background: transparent;
    z-index: 1000;
    /* Away: not drawn and not clickable, but still alive, so that a reply that is on its way goes on arriving. */
    visibility: hidden;
    pointer-events: none;
  }
  .assistant-frame.shown {
    visibility: visible;
    pointer-events: auto;
  }
</style>
