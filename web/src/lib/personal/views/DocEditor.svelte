<script lang="ts">
  // One document open for writing: its title, its properties and the rich text editor, saved as it is typed.
  import { onDestroy, type Snippet } from 'svelte';
  import { copyText } from '../../clipboard';
  import RichEditor from '../editor/RichEditor.svelte';
  import { maxLongTextBytes, type Collection, type PersonalRecord, type Values, type View } from '../schema';
  import { workspaceSession } from '../session.svelte';
  import { DraftSaver } from './draft.svelte';
  import PropertyBar from './PropertyBar.svelte';
  import { relativeTime, untitled } from './shared';

  let { view, collection, record, ondelete, onwiki, wikiExists, before, after, deleteNote = '' }: {
    view: View; collection: Collection; record: PersonalRecord;
    ondelete?: () => void | Promise<void>; onwiki?: (title: string) => void; wikiExists?: (title: string) => boolean;
    before?: Snippet; after?: Snippet; deleteNote?: string;
  } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const titleKey = $derived(view.fields[0]);
  const bodyKey = $derived(view.bodyField ?? '');
  const writable = $derived((view.operations?.includes('update') ?? false) && !session.readOnly);
  const required = $derived(new Set(collection.fields.filter((f) => f.required).map((f) => f.key)));

  // A document is opened once and then edited in place: later changes to the record come from this editor.
  // svelte-ignore state_referenced_locally
  let title = $state(typeof record.values[view.fields[0]] === 'string' ? (record.values[view.fields[0]] as string) : '');
  // svelte-ignore state_referenced_locally
  const body = typeof record.values[view.bodyField ?? ''] === 'string' ? (record.values[view.bodyField ?? ''] as string) : '';
  let editor: ReturnType<typeof RichEditor> | undefined = $state();
  let confirming = $state(false);
  let copied = $state(false);

  const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
  const saver = new DraftSaver(async (patch) => {
    const values: Values = {};
    const clear: string[] = [];
    for (const [key, value] of Object.entries(patch)) {
      if (value === '' && !required.has(key)) clear.push(key); else values[key] = value;
    }
    // Another save (a property changed a moment ago) finishes first.
    for (let i = 0; i < 80 && session.busy; i++) await sleep(50);
    return session.mutate(view.id, 'update', record.id, values, clear);
  });
  /** Saves what is waiting; the parent calls it before showing another document. */
  export function flush(): Promise<boolean> { return saver.flush(); }
  export function focusTitle() { document.getElementById(`${uid}-title`)?.focus(); }
  export function refreshLinks() { editor?.refreshLinks(); }
  onDestroy(() => { void saver.flush(); saver.dispose(); });

  function leaving() { if (document.visibilityState === 'hidden') void saver.flush(); }
  async function copy() {
    copied = await copyText(`${title ? `# ${title}\n\n` : ''}${editor?.markdown() ?? body}`);
    setTimeout(() => { copied = false; }, 1800);
  }
  const status = $derived(saver.status === 'saved' ? `Saved ${relativeTime(record.updatedAt)}` : saver.status === 'unsaved' ? 'Unsaved changes' : saver.status === 'saving' ? 'Saving…' : 'Not saved');
</script>

<svelte:document onvisibilitychange={leaving} />

<article class="doc" aria-label={title || untitled}>
  {@render before?.()}
  <input id={`${uid}-title`} class="title" type="text" maxlength="500" aria-label="Title" placeholder={untitled} readonly={!writable} bind:value={title}
    oninput={() => saver.push({ [titleKey]: title })} onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); editor?.focus(); } }} />
  <PropertyBar {view} {collection} {record} />
  <RichEditor bind:this={editor} value={body} label={title || untitled} readOnly={!writable} maxBytes={maxLongTextBytes} {onwiki} {wikiExists}
    onchange={(markdown) => { if (bodyKey) saver.push({ [bodyKey]: markdown }); }} />
  <div class="meta">
    <span class="status" class:bad={saver.status === 'failed'} role="status" aria-live="polite">{status}</span>
    {#if saver.status === 'failed'}<span class="why" role="alert">{session.problem || 'Your text is still here.'}</span><button class="btn quiet" type="button" onclick={() => saver.flush()}>Try again</button>{/if}
    <span class="spacer"></span>
    <button class="btn" type="button" onclick={copy}>{copied ? 'Copied' : 'Copy as Markdown'}</button>
    {#if ondelete && view.operations?.includes('delete') && !session.readOnly}
      {#if confirming}
        <span class="confirm" role="group" aria-label="Delete">Delete “{title || untitled}”?{#if deleteNote} {deleteNote}{/if}
          <button class="btn danger" type="button" disabled={session.busy} onclick={async () => { await saver.flush(); await ondelete?.(); }}>Delete</button>
          <button class="btn ok" type="button" onclick={() => confirming = false}>Keep</button>
        </span>
      {:else}<button class="btn danger" type="button" onclick={() => confirming = true}>Delete</button>{/if}
    {/if}
  </div>
  {@render after?.()}
</article>

<style>
  .doc { display: grid; gap: 10px; min-width: 0; align-content: start; }
  .title { width: 100%; min-width: 0; padding: 4px 2px; border: 0; border-bottom: 1px solid transparent; background: transparent; color: var(--text); font: inherit; font-size: 26px; font-weight: 650; letter-spacing: -0.015em; }
  .title::placeholder { color: var(--text-2); }
  .title:hover:not([readonly]) { border-bottom-color: var(--border); }
  .title:focus-visible { outline: none; border-bottom-color: var(--accent); }
  .meta { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; font-size: 12px; color: var(--text-2); }
  .spacer { flex: 1; }
  .status.bad, .why { color: var(--danger-text); }
  .confirm { display: inline-flex; flex-wrap: wrap; align-items: center; gap: 8px; color: var(--text); }
  @media (max-width: 600px) { .title { font-size: 22px; } .meta :global(.btn) { min-height: 44px; } }
</style>
