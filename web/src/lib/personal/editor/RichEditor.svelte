<script lang="ts">
  // A word processor for a workspace document: a formatting bar over the text. What it saves is Markdown (see markdown.ts).
  import { onMount, untrack } from 'svelte';
  import Bold from '@lucide/svelte/icons/bold';
  import Code from '@lucide/svelte/icons/code';
  import Italic from '@lucide/svelte/icons/italic';
  import Link2 from '@lucide/svelte/icons/link';
  import List from '@lucide/svelte/icons/list';
  import ListOrdered from '@lucide/svelte/icons/list-ordered';
  import ListTodo from '@lucide/svelte/icons/list-todo';
  import Minus from '@lucide/svelte/icons/minus';
  import Quote from '@lucide/svelte/icons/quote';
  import Redo2 from '@lucide/svelte/icons/redo-2';
  import Strikethrough from '@lucide/svelte/icons/strikethrough';
  import SquareCode from '@lucide/svelte/icons/square-code';
  import Undo2 from '@lucide/svelte/icons/undo-2';
  import Unlink from '@lucide/svelte/icons/unlink';
  // The editor itself is loaded the first time a document is opened, not with the app.
  import type { EditorCommand, EditorHandle, ToolbarState } from './editor';
  import { byteLength } from './size';

  let { value, label = 'Document text', placeholder = 'Start writing…', readOnly = false, maxBytes = 48000, onchange, onwiki, wikiExists }: {
    value: string; label?: string; placeholder?: string; readOnly?: boolean; maxBytes?: number;
    onchange?: (markdown: string) => void; onwiki?: (title: string) => void; wikiExists?: (title: string) => boolean;
  } = $props();

  const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  const mod = mac ? '⌘' : 'Ctrl+';
  const shift = mac ? '⇧' : 'Shift+';
  const alt = mac ? '⌥' : 'Alt+';

  let host: HTMLDivElement | undefined = $state();
  let editor: EditorHandle | undefined;
  // A request to focus that arrives while the editor is still loading is kept and carried out when it is ready.
  let wantsFocus = false;
  let tb = $state<ToolbarState>({ bold: false, italic: false, strike: false, code: false, link: false, block: 'paragraph', canUndo: false, canRedo: false, empty: true });
  let size = $state(0);
  let words = $state(0);
  let full = $state(false);
  let linkOpen = $state(false);
  let linkText = $state('');
  let linkProblem = $state('');
  const uid = $props.id();

  // The text the editor was built from is the one it starts with; later changes come from typing, not from this prop.
  // svelte-ignore state_referenced_locally
  const initial = value;
  function measure(markdown: string) {
    size = byteLength(markdown);
    words = (markdown.match(/[\p{L}\p{N}][\p{L}\p{N}'’-]*/gu) ?? []).length;
    full = size >= maxBytes;
  }
  onMount(() => {
    let gone = false;
    void import('./editor').then(({ mountEditor }) => {
      if (gone || !host) return;
      editor = mountEditor(host, {
        markdown: initial, readOnly, placeholder, maxBytes,
        onChange: (markdown) => { measure(markdown); onchange?.(markdown); },
        onState: (next) => { tb = next; },
        onWiki: (title) => onwiki?.(title),
        wikiExists,
        onLimit: () => { full = true; },
        onLinkRequest: openLink,
      });
      measure(initial);
      if (wantsFocus) { wantsFocus = false; editor.focus(); }
    });
    return () => { gone = true; editor?.destroy(); editor = undefined; };
  });
  $effect(() => { const ro = readOnly; untrack(() => editor?.setReadOnly(ro)); });
  /** Pages were added or renamed: redraw the [[links]] that point nowhere. */
  export function refreshLinks() { editor?.refreshWiki(); }
  export function focus() { if (editor) editor.focus(); else wantsFocus = true; }
  export function markdown(): string { return editor?.markdown() ?? initial; }

  function run(command: EditorCommand) { editor?.run(command); }
  function openLink() {
    if (readOnly) return;
    linkText = editor?.selectedLink() ?? '';
    linkProblem = '';
    linkOpen = true;
    queueMicrotask(() => document.getElementById(`${uid}-link`)?.focus());
  }
  function applyLink(event: SubmitEvent) {
    event.preventDefault();
    if (!linkText.trim()) { linkProblem = 'Enter a web address.'; return; }
    if (!editor?.setLink(linkText)) { linkProblem = 'Use a web address such as example.com, or an email address.'; return; }
    linkOpen = false;
  }
  function removeLink() { run('unlink'); linkOpen = false; }
  function style(event: Event & { currentTarget: HTMLSelectElement }) {
    const choice = event.currentTarget.value as EditorCommand;
    run(choice);
  }
  /** A toolbar button keeps the caret where it was: a press must not take focus away from the text. */
  const keepFocus = (event: MouseEvent) => event.preventDefault();
  const styleValue = $derived(['h1', 'h2', 'h3'].includes(tb.block) ? tb.block : 'paragraph');
</script>

<div class="rich" class:readonly={readOnly}>
  {#if !readOnly}
    <div class="bar" role="toolbar" aria-label={`${label}: formatting`} aria-controls={`${uid}-text`}>
      <div class="group">
        <button type="button" class="tool" title={`Undo (${mod}Z)`} aria-label="Undo" disabled={!tb.canUndo} onmousedown={keepFocus} onclick={() => run('undo')}><Undo2 size={16} /></button>
        <button type="button" class="tool" title={`Redo (${shift}${mod}Z)`} aria-label="Redo" disabled={!tb.canRedo} onmousedown={keepFocus} onclick={() => run('redo')}><Redo2 size={16} /></button>
      </div>
      <div class="group">
        <select class="style" aria-label="Text style" value={styleValue} onchange={style}>
          <option value="paragraph">Normal text</option>
          <option value="h1">Heading 1</option>
          <option value="h2">Heading 2</option>
          <option value="h3">Heading 3</option>
        </select>
      </div>
      <div class="group">
        <button type="button" class="tool" class:on={tb.bold} aria-pressed={tb.bold} title={`Bold (${mod}B)`} aria-label="Bold" onmousedown={keepFocus} onclick={() => run('bold')}><Bold size={16} /></button>
        <button type="button" class="tool" class:on={tb.italic} aria-pressed={tb.italic} title={`Italic (${mod}I)`} aria-label="Italic" onmousedown={keepFocus} onclick={() => run('italic')}><Italic size={16} /></button>
        <button type="button" class="tool" class:on={tb.strike} aria-pressed={tb.strike} title={`Strikethrough (${shift}${mod}X)`} aria-label="Strikethrough" onmousedown={keepFocus} onclick={() => run('strike')}><Strikethrough size={16} /></button>
        <button type="button" class="tool" class:on={tb.code} aria-pressed={tb.code} title={`Inline code (${mod}E)`} aria-label="Inline code" onmousedown={keepFocus} onclick={() => run('code')}><Code size={16} /></button>
        <button type="button" class="tool" class:on={tb.link || linkOpen} aria-pressed={tb.link} title={`Link (${mod}K)`} aria-label="Link" onmousedown={keepFocus} onclick={openLink}><Link2 size={16} /></button>
      </div>
      <div class="group">
        <button type="button" class="tool" class:on={tb.block === 'bullet'} aria-pressed={tb.block === 'bullet'} title={`Bulleted list (${shift}${mod}8)`} aria-label="Bulleted list" onmousedown={keepFocus} onclick={() => run('bullet')}><List size={16} /></button>
        <button type="button" class="tool" class:on={tb.block === 'ordered'} aria-pressed={tb.block === 'ordered'} title={`Numbered list (${shift}${mod}7)`} aria-label="Numbered list" onmousedown={keepFocus} onclick={() => run('ordered')}><ListOrdered size={16} /></button>
        <button type="button" class="tool" class:on={tb.block === 'task'} aria-pressed={tb.block === 'task'} title={`Task list (${shift}${mod}9)`} aria-label="Task list" onmousedown={keepFocus} onclick={() => run('task')}><ListTodo size={16} /></button>
      </div>
      <div class="group">
        <button type="button" class="tool" class:on={tb.block === 'quote'} aria-pressed={tb.block === 'quote'} title={`Quote (${shift}${mod}B)`} aria-label="Quote" onmousedown={keepFocus} onclick={() => run('quote')}><Quote size={16} /></button>
        <button type="button" class="tool" class:on={tb.block === 'code'} aria-pressed={tb.block === 'code'} title={`Code block (${alt}${mod}C)`} aria-label="Code block" onmousedown={keepFocus} onclick={() => run('codeblock')}><SquareCode size={16} /></button>
        <button type="button" class="tool" title="Divider" aria-label="Divider" onmousedown={keepFocus} onclick={() => run('rule')}><Minus size={16} /></button>
      </div>
    </div>
    {#if linkOpen}
      <form class="link" onsubmit={applyLink} aria-label="Link">
        <label for={`${uid}-link`}>Web address</label>
        <input id={`${uid}-link`} type="text" inputmode="url" autocomplete="off" spellcheck="false" placeholder="example.com/page" bind:value={linkText} onkeydown={(e) => { if (e.key === 'Escape') { linkOpen = false; editor?.focus(); } }} />
        <button class="btn primary" type="submit">Apply</button>
        {#if tb.link}<button class="btn" type="button" onclick={removeLink}><Unlink size={14} />Remove link</button>{/if}
        <button class="btn" type="button" onclick={() => { linkOpen = false; editor?.focus(); }}>Cancel</button>
        {#if linkProblem}<span class="problem" role="alert">{linkProblem}</span>{/if}
      </form>
    {/if}
  {/if}
  <!-- The whole sheet is the writing area: a click below the last line puts the caret there, as in a word processor. -->
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="page" id={`${uid}-text`} bind:this={host} data-rich-editor onclick={(e) => { if (e.target === e.currentTarget) editor?.focus(); }}></div>
  <p class="foot" aria-live="polite">
    <span>{words.toLocaleString()} {words === 1 ? 'word' : 'words'}</span>
    <span class:full>{size.toLocaleString()} of {maxBytes.toLocaleString()} bytes{full ? ' · full' : ''}</span>
  </p>
</div>

<style>
  .rich { display: grid; min-width: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); overflow: clip; }
  .bar { display: flex; flex-wrap: wrap; gap: 4px 14px; padding: 6px 8px; border-bottom: 1px solid var(--border); background: var(--surface-2); }
  .group { display: flex; align-items: center; gap: 2px; }
  .group + .group { padding-left: 14px; border-left: 1px solid var(--border); }
  .tool { display: inline-grid; place-items: center; width: 32px; height: 32px; border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: var(--text-2); cursor: pointer; }
  .tool:hover:not(:disabled) { background: var(--surface); border-color: var(--border); color: var(--text); }
  .tool:disabled { opacity: 0.4; cursor: default; }
  .tool.on { background: var(--tint); border-color: var(--accent); color: var(--accent); }
  .tool:focus-visible, .style:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .style { min-height: 32px; padding: 0 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; font-size: 13px; }
  .link { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 8px 10px; border-bottom: 1px solid var(--border); background: var(--tint); }
  .link label { font-size: 12px; color: var(--text-2); }
  .link input { flex: 1 1 220px; min-width: 0; min-height: 34px; padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; }
  .problem { flex-basis: 100%; color: var(--danger-text); font-size: 12px; }
  .page { min-width: 0; min-height: 320px; max-height: 70vh; overflow: auto; padding: 22px clamp(16px, 4vw, 48px) 36px; cursor: text; }
  .readonly .page { min-height: 0; max-height: none; cursor: default; }
  .foot { display: flex; justify-content: space-between; gap: 12px; margin: 0; padding: 6px 12px; border-top: 1px solid var(--border); color: var(--text-2); font-size: 12px; }
  .full { color: var(--warn-text); font-weight: 600; }

  /* The text itself is made by ProseMirror, outside this component's scope. */
  .page :global(.doc-content) { outline: none; max-width: 76ch; min-height: 280px; margin: 0 auto; font-size: 16px; line-height: 1.7; color: var(--text); overflow-wrap: anywhere; white-space: pre-wrap; }
  .page :global(.doc-content > *:first-child) { margin-top: 0; }
  .page :global(p) { margin: 0 0 0.85em; }
  /* The app's own h2 is a small mono label: a document's headings are headings. */
  .page :global(h1), .page :global(h2), .page :global(h3), .page :global(h4) { margin: 1.3em 0 0.45em; font-family: inherit; line-height: 1.25; font-weight: 650; letter-spacing: -0.01em; text-transform: none; color: var(--text); }
  .page :global(h1) { font-size: 1.85em; }
  .page :global(h2) { font-size: 1.4em; }
  .page :global(h3) { font-size: 1.15em; }
  .page :global(ul), .page :global(ol) { margin: 0 0 0.85em; padding-left: 1.6em; }
  .page :global(li > p) { margin-bottom: 0.3em; }
  .page :global(li[data-task]) { list-style: none; position: relative; margin-left: -1.6em; display: flex; align-items: baseline; gap: 8px; }
  .page :global(li[data-task] .task-box) { flex: none; display: inline-grid; place-items: center; width: 22px; transform: translateY(2px); }
  .page :global(li[data-task] .task-box input) { width: 16px; height: 16px; margin: 0; accent-color: var(--accent); cursor: pointer; }
  .page :global(li[data-task] .task-content) { flex: 1; min-width: 0; }
  .page :global(li[data-task='done'] .task-content) { color: var(--text-2); text-decoration: line-through; }
  .page :global(blockquote) { margin: 0 0 0.85em; padding: 2px 0 2px 16px; border-left: 3px solid var(--accent); color: var(--text-2); }
  .page :global(pre) { margin: 0 0 1em; padding: 12px 14px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface-2); font-family: var(--mono); font-size: 13px; line-height: 1.55; overflow-x: auto; white-space: pre; }
  .page :global(code) { padding: 0.1em 0.35em; border-radius: 4px; background: var(--surface-2); font-family: var(--mono); font-size: 0.9em; }
  .page :global(pre code) { padding: 0; background: none; font-size: inherit; }
  .page :global(hr) { margin: 1.5em 0; border: 0; border-top: 1px solid var(--border); }
  .page :global(a) { color: var(--accent); text-decoration: underline; text-underline-offset: 2px; }
  .page :global(.wiki-link) { padding: 0 5px; border-radius: 4px; background: var(--tint); color: var(--accent); cursor: pointer; white-space: nowrap; }
  .page :global(.wiki-link.wiki-missing) { background: transparent; border: 1px dashed var(--border); color: var(--text-2); }
  .page :global(.wiki-link::before) { content: '↗ '; font-size: 0.8em; }
  .page :global(p.is-empty::before) { content: attr(data-placeholder); float: left; height: 0; color: var(--text-2); pointer-events: none; }
  .page :global(.ProseMirror-selectednode) { outline: 2px solid var(--accent); }
  @media (max-width: 600px) { .tool { width: 40px; height: 40px; } .style { min-height: 40px; } .group + .group { padding-left: 8px; } .bar { gap: 4px 8px; } .page { padding-inline: 14px; max-height: none; } }
</style>
