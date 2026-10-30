<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { assistantProjectContext } from './project-context.svelte';
  import Icon from '../Icon.svelte';
  import { desktop } from '../desktop.svelte';
  import { chooseFiles } from './nativefiles';
  import { detect, Dictation, disclosure, joinSpoken, recognisesLocally, type DictationPhase } from './dictation';
  import { currentSession } from './session-context';
  const assistant = currentSession();
  import { retentionNote, size } from './words';
  import AttachmentChip from './AttachmentChip.svelte';

  // The box at the bottom: words, files, dictation, and the one button that sends or stops.

  let box: HTMLTextAreaElement | undefined = $state();
  let picker: HTMLInputElement | undefined = $state();
  let dragging = $state(false);

  const touch = typeof matchMedia === 'function' && matchMedia('(pointer: coarse)').matches;
  const draft = $derived(assistant.draft);
  const canSend = $derived((draft.trim() !== '' || assistant.readyFiles.length > 0) && !assistant.running && assistant.asking.length === 0 && !assistant.staged.some((s) => s.state === 'uploading'));
  const policy = $derived(assistant.policy);
  const providerName = $derived(assistant.provider?.name ?? 'the assistant');

  // ---- the box grows with what is typed, up to a few lines ----
  function fit() {
    if (!box) return;
    box.style.height = 'auto';
    box.style.height = Math.min(box.scrollHeight, 180) + 'px';
  }
  $effect(() => {
    void draft;
    void tick().then(fit);
  });
  onMount(() => {
    // Opening the assistant puts the cursor where the person will type.
    box?.focus({ preventScroll: true });
  });

  function submit(e?: Event) {
    e?.preventDefault();
    // While dictating, Send means "that is all, send it": the dictation is finished, and the message goes as soon as the last
    // words have been turned into text.
    if (listening) {
      if (canFinishAndSend) finishDictation(true);
      return;
    }
    if (!canSend) return;
    void assistant.send();
  }
  function onKey(e: KeyboardEvent) {
    // Enter sends; Shift+Enter makes a new line. On a phone's keyboard Enter is a new line and the button sends.
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && !touch) submit(e);
  }

  // ---- files ----
  // In the Mac app the app's own file dialog is used: the web view's file chooser opens as a sheet that can close again by itself
  // inside a framed page. Anywhere else (a browser, a phone) it is the page's own file input.
  let nativePicker = true;
  async function attach() {
    if (desktop.available && nativePicker) {
      try {
        const files = await chooseFiles();
        if (files.length) void assistant.stageFiles(files);
        return;
      } catch (e) {
        // An app that does not know the request is as good as no app: the page's own chooser is used from now on.
        nativePicker = false;
        if (!(e instanceof Error && /not something this workspace may ask|unknown request/.test(e.message))) {
          assistant.problem = 'The file dialog could not be opened. ' + (e instanceof Error ? e.message : '');
          return;
        }
      }
    }
    picker?.click();
  }
  function picked(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const files = [...(input.files ?? [])];
    input.value = '';
    if (files.length) void assistant.stageFiles(files);
  }
  function dropped(e: DragEvent) {
    e.preventDefault();
    dragging = false;
    if (!assistant.canAttach) return;
    const files = [...(e.dataTransfer?.files ?? [])];
    if (files.length) void assistant.stageFiles(files);
  }
  const TEXT_TYPES = '.txt,.md,.markdown,.csv,.tsv,.json,.jsonl,.log,.yaml,.yml,.toml,.xml,.html,.css,.js,.ts,.py,.go,.rb,.rs,.java,.sh,.sql,.ini,.cfg,.env.example,text/*';

  // ---- dictation ----
  // Pressing the microphone starts it. While it listens the words appear in the box as they are said, and there are three ways out:
  // the cross drops it all, the (now ticked) microphone button finishes it and leaves the text in the box to be edited, and Send
  // finishes it and sends the message as soon as the last words are text.
  const support = detect();
  const lang = typeof navigator !== 'undefined' ? navigator.language || 'en-US' : 'en-US';
  const ACK_KEY = 'werkbord.assistant.dictation';
  let local = $state(false);
  let phase = $state<DictationPhase | 'idle'>('idle');
  const listening = $derived(phase !== 'idle');
  let spoken = $state('');
  let interim = $state('');
  let quiet = $state(false);
  let baseDraft = $state('');
  let askFirst = $state(false);
  let dictationNote = $state('');
  let dictation: Dictation | null = null;
  let sendAfter = false;
  /** What the box shows while listening: what was typed, what has been settled, and what is being said right now. */
  const preview = $derived(joinSpoken(joinSpoken(baseDraft, spoken), interim));
  const canFinishAndSend = $derived(!assistant.running && assistant.asking.length === 0 && !assistant.staged.some((s) => s.state === 'uploading') && phase !== 'finishing');
  const statusLine = $derived(
    phase === 'starting'
      ? 'Starting the microphone… if your Mac asks, choose Allow.'
      : phase === 'finishing'
        ? 'Finishing what you said…'
        : phase === 'hearing'
          ? 'Hearing you…'
          : quiet
            ? 'I can’t hear anything yet. Check that the right microphone is chosen in System Settings → Sound.'
            : `Listening${local ? ' (on this device)' : ''}… go ahead and speak.`,
  );

  function acknowledged(): boolean {
    try {
      return localStorage.getItem(ACK_KEY) === support.vendor;
    } catch {
      return false;
    }
  }
  function acknowledge() {
    try {
      localStorage.setItem(ACK_KEY, support.vendor);
    } catch {
      // Asked again next time; nothing breaks.
    }
  }

  // Whether this browser can recognise speech on the device is asked when the person reaches for the microphone, not before:
  // opening the assistant should not wake a speech service.
  let localKnown = false;
  async function findOutIfLocal() {
    if (localKnown || !support.available) return;
    localKnown = true;
    local = await recognisesLocally(lang);
  }

  function startDictation() {
    dictationNote = '';
    askFirst = false;
    quiet = false;
    baseDraft = assistant.draft;
    spoken = '';
    interim = '';
    sendAfter = false;
    const d = new Dictation(
      {
        onPhase: (p) => (phase = p),
        onInterim: (t) => (interim = t),
        onFinal: (t) => (spoken = joinSpoken(spoken, t)),
        onQuiet: () => (quiet = true),
        onError: (m) => (dictationNote = m),
        onEnd: (how) => ended(how),
      },
      { lang, local, inApp: desktop.available },
    );
    dictation = d;
    d.start();
  }

  /** The dictation is over. What it heard goes into the box (after what was typed), and is sent if the person asked for that. */
  function ended(how: 'finished' | 'cancelled' | 'failed') {
    const text = how === 'cancelled' ? '' : spoken;
    const send = sendAfter && how === 'finished';
    phase = 'idle';
    interim = '';
    quiet = false;
    dictation = null;
    sendAfter = false;
    if (how === 'cancelled') {
      void tick().then(() => box?.focus({ preventScroll: true }));
      return;
    }
    if (text.trim() !== '') assistant.draft = joinSpoken(baseDraft, text);
    else if (how === 'finished') dictationNote = 'I didn’t catch anything. Try again, a little closer to the microphone.';
    void tick().then(() => {
      if (send && canSend) {
        void assistant.send();
        return;
      }
      box?.focus({ preventScroll: true });
      box?.setSelectionRange(box.value.length, box.value.length);
    });
  }

  function finishDictation(thenSend = false) {
    if (!dictation) return;
    sendAfter = sendAfter || thenSend;
    dictation.finish();
  }
  function cancelDictation() {
    dictation?.cancel();
  }
  async function mic() {
    if (listening) return finishDictation();
    if (!support.available) {
      dictationNote = support.reason ?? 'Dictation is not available here.';
      return;
    }
    await findOutIfLocal();
    if (!local && !acknowledged()) {
      askFirst = true;
      return;
    }
    startDictation();
  }
  function agree() {
    acknowledge();
    startDictation();
  }
  /** Escape drops the dictation (and nothing else) while one is going. */
  function escape(e: KeyboardEvent) {
    if (!listening || e.key !== 'Escape') return;
    e.preventDefault();
    e.stopPropagation();
    cancelDictation();
  }
  // Keep the newest words in view while the box fills.
  $effect(() => {
    void preview;
    if (phase !== 'idle') void tick().then(() => { fit(); if (box) box.scrollTop = box.scrollHeight; });
  });
  onDestroy(() => dictation?.cancel());
</script>

<svelte:window onkeydowncapture={escape} />

{#if assistantProjectContext.current !== undefined}<p class="project-context" aria-label="Salie project context">{assistantProjectContext.current?.label ?? 'All Projects'}</p>{/if}
<form class="composer" onsubmit={submit} ondragover={(e) => { if (assistant.canAttach) { e.preventDefault(); dragging = true; } }} ondragleave={() => (dragging = false)} ondrop={dropped} class:dragging>
  {#if assistant.problem}
    <p class="problem" role="alert">{assistant.problem}</p>
  {/if}

  {#if assistant.asking.length > 0}
    <section class="ask card" aria-label="Share files with Salie" data-testid="assistant-authorize">
      <p><strong>Share {assistant.asking.length === 1 ? 'this file' : `these ${assistant.asking.length} files`} with {providerName}?</strong></p>
      <ul>{#each assistant.asking as f (f.key)}<li><span class="fn">{f.name}</span> <span class="sz">{size(f.bytes)}</span></li>{/each}</ul>
      <p class="fine">
        It is shown to {providerName} with your next message, and only then. {policy ? retentionNote(policy.retentionSeconds) : ''}
        {providerName} keeps its own copy as part of your conversation, and Werkbord cannot delete that.
      </p>
      <div class="acts">
        <button class="btn primary" type="button" onclick={() => assistant.allowFiles()}>Share</button>
        <button class="btn" type="button" onclick={() => assistant.declineFiles()}>Cancel</button>
      </div>
    </section>
  {/if}

  {#if askFirst}
    <section class="ask card" aria-label="Before you dictate" data-testid="assistant-dictation-notice">
      <p>{disclosure(support, local)}</p>
      <p class="fine">It is not the voice mode of Claude or ChatGPT. You can always type instead, or use the microphone key on your keyboard.</p>
      <div class="acts">
        <button class="btn primary" type="button" onclick={agree}>Use dictation</button>
        <button class="btn" type="button" onclick={() => (askFirst = false)}>Cancel</button>
      </div>
    </section>
  {/if}

  {#if assistant.staged.some((s) => s.state !== 'asking')}
    <ul class="files" aria-label="Attached files">
      {#each assistant.staged.filter((s) => s.state !== 'asking') as f (f.key)}
        <AttachmentChip file={f} onremove={() => assistant.removeFile(f.key)} />
      {/each}
    </ul>
    <p class="caption" role="status">
      {#if assistant.staged.some(file => file.state === 'uploading')}
        Adding files…
      {:else if assistant.staged.some(file => file.state === 'error')}
        The draft is kept. Remove the failed file or attach it again.
      {:else if assistant.readyFiles.length > 0}
        Files ready for your next message.
      {/if}
    </p>
  {/if}

  {#if listening}
    <div class="listen" role="status" aria-live="polite" data-phase={phase} data-testid="assistant-listening">
      <span class="bars" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i></span>
      <span class="msg">{statusLine}</span>
      <kbd class="key">Esc</kbd><span class="esc">cancels</span>
    </div>
  {:else if dictationNote}
    <p class="caption" role="status" data-testid="assistant-dictation-note">{dictationNote}</p>
  {/if}

  <div class="box">
    {#if assistant.canAttach}
      <button class="tool" type="button" aria-label="Attach a text file" title="Attach a text file" onclick={attach} disabled={assistant.running || listening} data-testid="assistant-attach"><Icon name="attach" size={18} /></button>
      <input bind:this={picker} class="visually-hidden" type="file" multiple accept={TEXT_TYPES} onchange={picked} tabindex="-1" aria-hidden="true" data-testid="assistant-file-input" />
    {/if}
    <textarea
      bind:this={box}
      value={listening ? preview : assistant.draft}
      class="field"
      class:live={listening}
      rows="1"
      readonly={listening}
      placeholder={listening ? 'Speak now…' : assistant.hasConversation ? 'Reply…' : 'Ask about your board, or tell me what to do'}
      aria-label="Message to Salie"
      enterkeyhint={touch ? 'enter' : 'send'}
      autocomplete="off"
      onkeydown={onKey}
      oninput={(e) => {
        assistant.draft = e.currentTarget.value;
        fit();
      }}
    ></textarea>
    {#if listening}
      <button class="tool cancel" type="button" aria-label="Cancel dictation and throw away what was heard" title="Cancel (Esc)" onclick={cancelDictation} data-testid="assistant-dictation-cancel"><Icon name="close" size={18} /></button>
    {/if}
    <button
      class="tool"
      class:on={listening}
      type="button"
      aria-label={listening ? 'Finish dictation and keep the text to edit' : 'Dictate a message'}
      title={listening ? 'Done: keep the text to edit' : 'Dictate'}
      onclick={mic}
      disabled={phase === 'finishing'}
      data-testid="assistant-mic"
    >
      <Icon name={listening ? 'check' : 'mic'} size={18} />
    </button>
    {#if assistant.running}
      <button class="go stop" type="button" aria-label={assistant.stopping ? 'Stop requested; waiting for confirmation' : 'Stop the reply'} title={assistant.stopping ? 'Stopping…' : 'Stop'} disabled={assistant.stopping} onclick={() => assistant.cancel()}><Icon name="stop" size={16} /></button>
    {:else if listening}
      <button class="go" type="submit" aria-label="Finish dictating and send" title="Finish and send" disabled={!canFinishAndSend} data-testid="assistant-send"><Icon name="send" size={18} /></button>
    {:else}
      <button class="go" type="submit" aria-label="Send" title="Send" disabled={!canSend} data-testid="assistant-send"><Icon name="send" size={18} /></button>
    {/if}
  </div>
</form>

<style>
  .project-context { margin: 0; padding: 8px 16px; color: var(--text-2); font-size: 13px; }
  .composer {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
    max-width: var(--asst-w);
    margin: 0 auto;
    padding: 8px 16px calc(14px + env(safe-area-inset-bottom));
  }
  .composer.dragging .box {
    box-shadow:
      var(--tray-sh),
      0 0 0 2px var(--accent);
  }
  .box {
    display: flex;
    align-items: flex-end;
    gap: 4px;
    padding: 6px;
    border: 1px solid var(--border);
    border-radius: 14px;
    background: var(--surface);
    box-shadow: var(--card-sh);
  }
  .box:focus-within {
    border-color: var(--accent);
    box-shadow: var(--live-ring);
  }
  .field {
    flex: 1;
    min-width: 0;
    max-height: 180px;
    padding: 9px 6px;
    border: 0;
    outline: none;
    resize: none;
    background: none;
    line-height: 1.45;
    font-size: 16px;
  }
  .tool,
  .go {
    flex: none;
    display: inline-grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border: 0;
    border-radius: 10px;
    background: none;
    color: var(--text-2);
  }
  .tool:hover:not(:disabled) {
    background: var(--surface-2);
    color: var(--text);
  }
  .tool.on {
    position: relative;
    background: var(--tint);
    color: var(--accent);
  }
  /* A quiet ring that breathes while the microphone is open, so it is plain that it is. */
  .tool.on::after {
    content: '';
    position: absolute;
    inset: -3px;
    border-radius: 13px;
    border: 2px solid var(--accent);
    opacity: 0;
    animation: breathe 1.8s ease-in-out infinite;
    pointer-events: none;
  }
  .tool.cancel:hover:not(:disabled) {
    background: var(--surface-2);
    color: var(--danger-text);
  }
  .field.live {
    color: var(--text);
    caret-color: transparent;
    cursor: default;
  }
  .tool:disabled {
    opacity: 0.4;
  }
  .go {
    background: var(--accent);
    color: var(--accent-text);
  }
  .go:disabled {
    background: var(--surface-2);
    color: var(--text-2);
    opacity: 0.7;
    cursor: not-allowed;
  }
  .go.stop {
    background: var(--text);
    color: var(--bg);
  }
  .problem {
    padding: 8px 12px;
    border-radius: var(--radius-sm);
    background: var(--surface);
    border: 1px solid var(--border);
    border-color: var(--danger);
    color: var(--danger-text);
    font-size: 13px;
  }
  .ask {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px 14px;
    font-size: 14px;
  }
  .ask ul {
    margin: 0;
    padding-left: 1.2em;
  }
  .acts {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .fine {
    font-size: 12px;
    color: var(--text-2);
  }
  .files {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .sz { color: var(--text-2); font-size: 12px; }
  .fn { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .caption {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-2);
    font-size: 13px;
    min-height: 20px;
  }
  .listen {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 28px;
    padding: 4px 12px;
    border-radius: 999px;
    background: var(--tint);
    color: var(--text);
    font-size: 13px;
  }
  .listen .msg {
    min-width: 0;
    flex: 1;
  }
  .listen .esc {
    flex: none;
    color: var(--text-2);
    font-size: 12px;
  }
  .listen .key {
    flex: none;
  }
  /* Five bars: still while the microphone is starting, a gentle sway while it listens, lively while it hears someone. */
  .bars {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 3px;
    height: 18px;
  }
  .bars i {
    display: block;
    width: 3px;
    height: 4px;
    border-radius: 2px;
    background: var(--accent);
    opacity: 0.55;
    transform-origin: center;
  }
  .listen[data-phase='listening'] .bars i {
    animation: sway 1.6s ease-in-out infinite;
  }
  .listen[data-phase='hearing'] .bars i {
    opacity: 1;
    animation: talk 0.55s ease-in-out infinite;
  }
  .bars i:nth-child(2) { animation-delay: 0.12s; }
  .bars i:nth-child(3) { animation-delay: 0.24s; }
  .bars i:nth-child(4) { animation-delay: 0.08s; }
  .bars i:nth-child(5) { animation-delay: 0.18s; }
  @keyframes sway {
    50% { height: 8px; }
  }
  @keyframes talk {
    0%, 100% { height: 5px; }
    50% { height: 18px; }
  }
  @keyframes breathe {
    0%, 100% { opacity: 0; transform: scale(0.96); }
    50% { opacity: 0.55; transform: scale(1.04); }
  }
  @media (prefers-reduced-motion: reduce) {
    .bars i,
    .tool.on::after {
      animation: none !important;
    }
    .tool.on::after {
      opacity: 0.4;
    }
    .listen[data-phase='hearing'] .bars i {
      height: 14px;
    }
  }
  @media (pointer: coarse) {
    .field {
      font-size: 16px;
    }
    .tool,
    .go {
      width: 44px;
      height: 44px;
    }
  }
</style>
