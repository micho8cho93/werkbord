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
  import { generatedWorkspaceID } from './nav';
  import { model } from './model.svelte';
  import { look, theme } from '../lib/theme.svelte';

  let el: HTMLIFrameElement | undefined = $state();
  let preparing = false;
  $effect(() => {
    if (!model.assistantFrame && model.view?.items.some(i => i.id === 'personal' && i.state === 'ready') && !preparing) {
      preparing = true;
      void model.prepareAssistant().catch(() => {}).finally(() => { preparing = false; });
    }
  });
  const compact = $derived(!model.assistantShown && model.assistant.compact === true && model.assistant.waiting > 0);
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
      open: (_id, target, place) => {
        // A card in the assistant leads to a place in the person's own Werkbord: show it, over the assistant, which closes.
        if (target === 'personal') {
          const generated = generatedWorkspaceID(place);
          if (generated) { void model.followGenerated(generated); return; }

          void (model.current === 'personal' ? model.visit(place) : model.open('personal', place));
          return;
        }
        const connectedTeam = model.view?.items.some((item) => item.id === target && item.kind === 'team' && item.state === 'ready');
        if (connectedTeam) void model.open(target, place);
      },
      assistant: (_id, state) => model.setAssistant(state),
    });
  }

  // The page is told which theme the person chose in the sidebar, as every workspace is, as soon as it can listen.
  function tell(): void {
    if (theme.choice) send({ type: 'werkbord.theme', theme: theme.choice });
    send({ type: 'werkbord.style', style: look.choice });
  }
  $effect(() => {
    void theme.choice;
    void look.choice;
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
      if (request.action === 'open') el?.contentWindow?.focus();
      send({
        type: 'werkbord.assistant',
        action: request.action,
        ...(request.run ? { run: request.run } : {}),
        ...(request.personalPrompt === undefined ? {} : {personalPrompt:request.personalPrompt}),
        ...(request.personalWorkspace === undefined ? {} : { personalWorkspace: request.personalWorkspace, ...(request.personalRevision === undefined ? {} : { personalRevision: request.personalRevision }) }),
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
    class:shown={model.assistantShown || compact}
    class:compact
    style:left={compact ? `${Math.max(12, Math.min(window.innerWidth - 384, (model.assistantFloatingButton?.getBoundingClientRect().left ?? window.innerWidth - 400) - 150))}px` : undefined}
    style:top={compact ? `${Math.max(12, Math.min(window.innerHeight - 520, (model.assistantFloatingButton?.getBoundingClientRect().top ?? window.innerHeight) - 510))}px` : undefined}
    title="Assistant"
    src={model.assistantFrame.url}
    referrerpolicy="no-referrer"
    sandbox="allow-scripts allow-same-origin allow-forms allow-modals allow-downloads"
    allow="microphone"
    aria-hidden={!model.assistantShown && !compact}
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
  .assistant-frame.compact { inset: auto; width: min(380px, calc(100vw - 24px)); height: min(500px, calc(100dvh - 90px)); border: 1px solid var(--border); border-radius: var(--radius-sm); }
  .assistant-frame.shown {
    visibility: visible;
    pointer-events: auto;
  }
</style>
