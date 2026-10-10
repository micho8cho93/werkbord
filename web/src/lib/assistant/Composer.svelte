<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import Icon from '../Icon.svelte';
  import { detect, Dictation, disclosure, joinSpoken, recognisesLocally } from './dictation';
  import { assistant } from './store.svelte';
  import { clock, retentionNote, size } from './words';

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
    if (!canSend) return;
    stopDictation();
    void assistant.send();
  }
  function onKey(e: KeyboardEvent) {
    // Enter sends; Shift+Enter makes a new line. On a phone's keyboard Enter is a new line and the button sends.
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && !touch) submit(e);
  }

  // ---- files ----
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
  const support = detect();
  const lang = typeof navigator !== 'undefined' ? navigator.language || 'en-US' : 'en-US';
  const ACK_KEY = 'werkbord.assistant.dictation';
  let local = $state(false);
  let listening = $state(false);
  let caption = $state('');
  let askFirst = $state(false);
  let dictationNote = $state('');
  let dictation: Dictation | null = null;

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
    dictation = new Dictation(
      {
        onInterim: (t) => (caption = t),
        onFinal: (t) => (assistant.draft = joinSpoken(assistant.draft, t)),
        onEnd: () => {
          listening = false;
          caption = '';
          dictation = null;
        },
        onError: (m) => (dictationNote = m),
      },
      { lang, local },
    );
    listening = dictation.start();
  }
  function stopDictation() {
    dictation?.stop();
  }
  async function mic() {
    if (listening) return stopDictation();
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
  onDestroy(() => dictation?.abort());
</script>

<form class="composer" onsubmit={submit} ondragover={(e) => { if (assistant.canAttach) { e.preventDefault(); dragging = true; } }} ondragleave={() => (dragging = false)} ondrop={dropped} class:dragging>
  {#if assistant.problem}
    <p class="problem" role="alert">{assistant.problem}</p>
  {/if}

  {#if assistant.asking.length > 0}
    <section class="ask card" aria-label="Share files with the assistant" data-testid="assistant-authorize">
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
        <li class="file" data-state={f.state}>
          <Icon name="attach" size={13} />
          <span class="fn">{f.name}</span>
          <span class="sz">
            {#if f.state === 'uploading'}adding…{:else if f.state === 'error'}{f.error}{:else if f.state === 'expired'}kept too long, removed{:else}{size(f.bytes)}{f.expiresAt ? ` · until ${clock(f.expiresAt)}` : ''}{/if}
          </span>
          <button class="x" type="button" onclick={() => assistant.removeFile(f.key)} aria-label="Remove {f.name}"><Icon name="close" size={12} /></button>
        </li>
      {/each}
    </ul>
  {/if}

  {#if listening || dictationNote}
    <p class="caption" role="status">
      {#if listening}<span class="rec" aria-hidden="true"></span>Listening{local ? ' (on this device)' : ''}… {caption}{:else}{dictationNote}{/if}
    </p>
  {/if}

  <div class="box">
    {#if assistant.canAttach}
      <button class="tool" type="button" aria-label="Attach a text file" title="Attach a text file" onclick={() => picker?.click()} disabled={assistant.running}><Icon name="attach" size={18} /></button>
      <input bind:this={picker} class="visually-hidden" type="file" multiple accept={TEXT_TYPES} onchange={picked} tabindex="-1" aria-hidden="true" data-testid="assistant-file-input" />
    {/if}
    <textarea
      bind:this={box}
      bind:value={assistant.draft}
      class="field"
      rows="1"
      placeholder={assistant.hasConversation ? 'Reply…' : 'Ask about your board, or tell me what to do'}
      aria-label="Message to the assistant"
      enterkeyhint={touch ? 'enter' : 'send'}
      autocomplete="off"
      onkeydown={onKey}
      oninput={fit}
    ></textarea>
    <button class="tool" class:on={listening} type="button" aria-label={listening ? 'Stop dictation' : 'Dictate'} aria-pressed={listening} title={listening ? 'Stop dictation' : 'Dictate'} onclick={mic} data-testid="assistant-mic">
      <Icon name="mic" size={18} />
    </button>
    {#if assistant.running}
      <button class="go stop" type="button" aria-label="Stop the reply" title="Stop" onclick={() => assistant.cancel()}><Icon name="stop" size={16} /></button>
    {:else}
      <button class="go" type="submit" aria-label="Send" title="Send" disabled={!canSend}><Icon name="send" size={18} /></button>
    {/if}
  </div>
</form>

<style>
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
    font-size: 15px;
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
    background: var(--tint);
    color: var(--accent);
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
    border-left: 3px solid var(--danger);
    color: var(--danger-text);
    font-size: 13px;
  }
  .ask {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px 14px;
    font-size: 13.5px;
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
  .file {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    min-height: 30px;
    padding: 0 4px 0 10px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--surface);
    font-size: 12.5px;
  }
  .file[data-state='error'] .sz,
  .file[data-state='expired'] .sz {
    color: var(--danger-text);
  }
  .fn {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sz {
    color: var(--text-2);
    font-size: 12px;
    white-space: nowrap;
  }
  .x {
    display: inline-grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 0;
    border-radius: 50%;
    background: none;
    color: var(--text-2);
  }
  .x:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  .caption {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-2);
    font-size: 13px;
    min-height: 20px;
  }
  .rec {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--danger);
    animation: pulse 1.2s ease-in-out infinite;
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
