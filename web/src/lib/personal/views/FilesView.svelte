<script lang="ts">
  // Files: a catalogue of PDFs, pictures, brochures and notes, and a viewer for the one chosen. The entry is saved with the
  // workspace; the file stays on this device (see files.ts).
  import { onMount, untrack } from 'svelte';
  import Icon from '../../Icon.svelte';
  import RecordEditor from '../RecordEditor.svelte';
  import RichEditor from '../editor/RichEditor.svelte';
  import { workspaceSession } from '../session.svelte';
  import { matchesSavedFilters, type Collection, type PersonalRecord, type View } from '../schema';
  import { browserFiles, fileKey, maxFileBytes, previewKind, sizeLabel, stem, typeOf, type FileStore, type StoredMeta } from './files';
  import PropertyBar from './PropertyBar.svelte';
  import { excerpt, newRecordValues, titleOf } from './shared';

  let { view, collection, store = browserFiles }: { view: View; collection: Collection; store?: FileStore } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  const workspaceId = $derived(session.workspace?.id ?? '');
  const canCreate = $derived((view.operations?.includes('create') ?? false) && !session.readOnly);
  const canUpdate = $derived((view.operations?.includes('update') ?? false) && !session.readOnly);
  const canDelete = $derived((view.operations?.includes('delete') ?? false) && !session.readOnly);
  const records = $derived((session.records[view.collectionId] ?? []).filter((r) => matchesSavedFilters(r.values, view.filters)));
  const tagKey = $derived(view.fields.find((key) => ['multiselect', 'select'].includes(collection.fields.find((f) => f.key === key)?.type ?? '')));

  let stored = $state<Record<string, StoredMeta>>({});
  let search = $state('');
  let chosen = $state('');
  let editing = $state<PersonalRecord | null>(null);
  let note = $state('');
  let problem = $state('');
  let url = $state('');
  let text = $state('');
  let truncated = $state(false);
  let loading = $state(false);
  let deleting = $state(false);
  const available = $derived(store.available());
  const shown = $derived.by(() => {
    const needle = search.trim().toLocaleLowerCase();
    return records.filter((r) => !needle || titleOf(view, r).toLocaleLowerCase().includes(needle) || view.fields.some((key) => String(r.values[key] ?? '').toLocaleLowerCase().includes(needle)))
      .sort((a, b) => titleOf(view, a).localeCompare(titleOf(view, b), undefined, { numeric: true, sensitivity: 'base' }));
  });
  const current = $derived<PersonalRecord | undefined>(records.find((r) => r.id === chosen) ?? shown[0]);
  const meta = $derived(current ? stored[current.id] : undefined);
  const kind = $derived(meta ? previewKind(meta.type, meta.name) : 'none');

  async function refresh() {
    if (!available || !workspaceId) return;
    try {
      const found = await store.list(`${workspaceId}/`);
      stored = Object.fromEntries(found.map((m) => [m.key.slice(workspaceId.length + 1), m]));
    } catch { problem = 'This device’s file storage could not be read.'; }
  }
  onMount(() => { void refresh(); });

  // The open file is read when the chosen entry, or the file kept for it, changes; what was made for the last one is let go.
  const loadKey = $derived(current && meta ? `${current.id}:${meta.addedAt}` : '');
  $effect(() => {
    if (!loadKey) return;
    const id = untrack(() => current!.id), shape = untrack(() => kind);
    let live = true, made = '';
    loading = true;
    void store.get(fileKey(workspaceId, id)).then(async (file) => {
      if (!live || !file) return;
      if (shape === 'text' || shape === 'markdown') {
        const raw = await file.blob.slice(0, 1_000_001).text();
        if (!live) return;
        truncated = raw.length > 1_000_000;
        text = truncated ? raw.slice(0, 1_000_000) : raw;
      } else if (shape !== 'none') {
        made = URL.createObjectURL(new Blob([file.blob], { type: file.type }));
        url = made;
      }
    }).catch(() => { if (live) problem = 'That file could not be opened from this device’s storage.'; }).finally(() => { if (live) loading = false; });
    return () => { live = false; if (made) URL.revokeObjectURL(made); url = ''; text = ''; truncated = false; loading = false; };
  });

  async function attach(record: PersonalRecord, file: File): Promise<boolean> {
    const type = typeOf(file.name, file.type);
    if (file.size > maxFileBytes) { problem = `${file.name} is larger than ${sizeLabel(maxFileBytes)}.`; return false; }
    if (previewKind(type, file.name) === 'none') { problem = `${file.name} cannot be shown here. Add a PDF, picture, video, audio, Markdown or text file.`; return false; }
    try {
      await store.put({ key: fileKey(workspaceId, record.id), name: file.name, type, size: file.size, addedAt: Date.now(), blob: file });
    } catch { problem = `${file.name} could not be stored on this device.`; return false; }
    return true;
  }
  async function addFiles(files: FileList | null) {
    problem = ''; note = '';
    if (!files?.length || !available) return;
    let added = 0;
    for (const file of Array.from(files)) {
      if (file.size > maxFileBytes || previewKind(typeOf(file.name, file.type), file.name) === 'none') { await attach({ id: '' } as PersonalRecord, file); continue; }
      await session.idle();
      if (!await session.mutate(view.id, 'create', undefined, newRecordValues(collection, view, { [view.fields[0]]: stem(file.name).slice(0, 300) })) || !session.lastSaved) return;
      if (await attach(session.lastSaved, file)) { added++; chosen = session.lastSaved.id; }
    }
    await refresh();
    if (added) note = `Added ${added} ${added === 1 ? 'file' : 'files'} to this device.`;
  }
  async function replace(files: FileList | null) {
    problem = ''; note = '';
    const file = files?.[0];
    if (!file || !current || !available) return;
    if (await attach(current, file)) { await refresh(); note = 'File added to this device.'; }
  }
  async function forget() {
    if (!current) return;
    try { await store.remove(fileKey(workspaceId, current.id)); } catch { problem = 'The file could not be removed.'; return; }
    await refresh(); note = 'File removed from this device. The entry is kept.';
  }
  async function remove() {
    if (!current) return;
    const id = current.id;
    if (await session.mutate(view.id, 'delete', id)) {
      deleting = false;
      try { await store.remove(fileKey(workspaceId, id)); } catch { /* The entry is gone; a file left behind is harmless. */ }
      chosen = ''; await refresh();
    }
  }
  const tagsOf = (record: PersonalRecord): string[] => {
    const value = tagKey ? record.values[tagKey] : undefined;
    return Array.isArray(value) ? value : typeof value === 'string' ? [value] : [];
  };
  const glyph = (id: string) => { const m = stored[id]; const k = m ? previewKind(m.type, m.name) : 'none'; return k === 'none' ? 'attach' : 'folder'; };
</script>

<section class="files" aria-label={view.name} data-view-kind="files">
  <div class="top">
    <h3>{view.name}</h3>
    {#if canCreate && available}
      <label class="btn primary pick"><Icon name="plus" size={14} />Add files
        <input class="visually-hidden" type="file" multiple accept="application/pdf,image/*,video/*,audio/*,text/*,.md,.markdown,.json,.csv,.yaml,.yml,.toml,.log" onchange={(e) => { void addFiles(e.currentTarget.files); e.currentTarget.value = ''; }} data-testid="files-add" />
      </label>
    {/if}
  </div>
  <p class="about">Entries are saved with this workspace. The files themselves stay on this computer, in this app’s own storage, and are never uploaded.</p>
  {#if !available}<p class="problem" role="alert">This device cannot keep files for this view. Entries still work.</p>{/if}
  {#if problem}<p class="problem" role="alert">{problem}</p>{/if}
  {#if note}<p class="note" role="status">{note}</p>{/if}
  {#if editing}{#key editing.id}<RecordEditor {collection} {view} record={editing} onclose={() => editing = null} />{/key}{/if}

  <div class="shell">
    <aside class="catalogue" aria-label="Catalogue">
      <label class="search" for={`${uid}-search`}><span class="visually-hidden">Search the catalogue</span><input id={`${uid}-search`} type="search" placeholder="Search the catalogue" bind:value={search} /></label>
      <ul>
        {#each shown as record (record.id)}
          <li><button type="button" class="item" class:current={record.id === current?.id} aria-current={record.id === current?.id ? 'true' : undefined} onclick={() => { chosen = record.id; deleting = false; note = ''; problem = ''; }}>
            <span class="glyph" aria-hidden="true"><Icon name={glyph(record.id)} size={16} /></span>
            <span class="text"><strong>{titleOf(view, record)}</strong>
              <span class="sub">{stored[record.id] ? `${stored[record.id].type.split('/')[1]?.split('+')[0]?.toUpperCase() ?? 'FILE'} · ${sizeLabel(stored[record.id].size)}` : 'No file on this device'}</span>
              {#if tagsOf(record).length}<span class="tags">{#each tagsOf(record) as tag (tag)}<span class="chip">{tag}</span>{/each}</span>{/if}
            </span></button></li>
        {:else}<li class="none">{records.length ? 'Nothing matches.' : 'Nothing here yet. Add a file to begin.'}</li>{/each}
      </ul>
    </aside>

    <div class="viewer">
      {#if current}
        <header>
          <div class="heading"><h4>{titleOf(view, current)}</h4><p>{meta ? `${meta.name} · ${sizeLabel(meta.size)} · added ${new Date(meta.addedAt).toLocaleDateString()}` : 'No file on this device yet'}</p></div>
          <div class="actions">
            {#if available && canUpdate}
              <label class="btn pick">{meta ? 'Replace file' : 'Attach file'}
                <input class="visually-hidden" type="file" accept="application/pdf,image/*,video/*,audio/*,text/*,.md,.markdown,.json,.csv,.yaml,.yml,.toml,.log" onchange={(e) => { void replace(e.currentTarget.files); e.currentTarget.value = ''; }} />
              </label>
            {/if}
            {#if meta}<button class="btn warn" type="button" onclick={forget}>Remove file</button>{/if}
            {#if canUpdate}<button class="btn" type="button" disabled={session.busy || !!editing} onclick={() => editing = current}>Edit details</button>{/if}
            {#if canDelete}
              {#if deleting}<span class="confirm" role="group" aria-label="Delete entry">Delete this entry?<button class="btn danger" type="button" disabled={session.busy} onclick={remove}>Delete</button><button class="btn ok" type="button" onclick={() => deleting = false}>Keep</button></span>
              {:else}<button class="btn danger" type="button" onclick={() => deleting = true}>Delete</button>{/if}
            {/if}
          </div>
        </header>
        <PropertyBar {view} {collection} record={current} />
        {#if view.bodyField && current.values[view.bodyField]}<p class="notes">{excerpt(current.values[view.bodyField], 600)}</p>{/if}
        <div class="stage" aria-busy={loading}>
          {#if !meta}
            <div class="blank"><p>{available ? 'Attach the file to read it here.' : 'File storage is not available on this device.'}</p></div>
          {:else if loading && !url && !text}<div class="blank"><p>Opening…</p></div>
          {:else if kind === 'pdf' && url}<iframe title={`${titleOf(view, current)} (PDF)`} src={url}></iframe>
          {:else if kind === 'image' && url}<img src={url} alt={titleOf(view, current)} />
          {:else if kind === 'video' && url}<!-- svelte-ignore a11y_media_has_caption --><video src={url} controls preload="metadata"></video>
          {:else if kind === 'audio' && url}<audio src={url} controls preload="metadata"></audio>
          {:else if kind === 'markdown'}{#key `${current.id}:${text.length}`}<RichEditor value={text} readOnly label={titleOf(view, current)} maxBytes={2_000_000} />{/key}{#if truncated}<p class="note">Showing the first megabyte.</p>{/if}
          {:else if kind === 'text'}<!-- svelte-ignore a11y_no_noninteractive_tabindex --><pre tabindex="0" aria-label={`${titleOf(view, current)} text`}>{text}</pre>{#if truncated}<p class="note">Showing the first megabyte.</p>{/if}
          {:else}<div class="blank"><p>This kind of file cannot be shown here.</p></div>{/if}
        </div>
      {:else}
        <div class="blank big"><h4>Nothing to show</h4><p>Add a brochure, PDF, picture or notes file. It opens here.</p></div>
      {/if}
    </div>
  </div>
</section>

<style>
  .files { display: grid; gap: 12px; min-width: 0; padding: 16px 18px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--card-sh); }
  :global(.card) .files { padding: 0; border: 0; border-radius: 0; background: none; box-shadow: none; }
  .top { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  h3 { margin: 0; font-size: 16px; font-weight: 600; }
  .about, .note { margin: 0; color: var(--text-2); font-size: 13px; }
  .problem { margin: 0; color: var(--danger-text); font-size: 13px; }
  .pick { cursor: pointer; }
  .pick:has(input:focus-visible) { outline: 2px solid var(--accent); outline-offset: 2px; }
  .shell { display: grid; grid-template-columns: minmax(220px, 300px) minmax(0, 1fr); gap: 16px; align-items: start; }
  .catalogue { display: grid; gap: 8px; min-width: 0; }
  .search input { width: 100%; min-width: 0; min-height: 34px; padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; font-size: 13px; }
  .search input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  ul { display: grid; gap: 4px; margin: 0; padding: 0; list-style: none; max-height: 70vh; overflow: auto; }
  .item { display: flex; gap: 10px; width: 100%; padding: 9px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; text-align: left; cursor: pointer; }
  .item:hover { background: var(--surface-2); }
  .item.current { border-color: var(--accent); background: var(--tint); }
  .item:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
  .glyph { flex: none; display: grid; place-items: center; width: 30px; height: 30px; border-radius: var(--radius-sm); background: var(--surface-2); color: var(--text-2); }
  .item.current .glyph { background: var(--surface); color: var(--accent); }
  .text { display: grid; gap: 2px; min-width: 0; }
  .text strong { font-size: 13px; font-weight: 600; overflow-wrap: anywhere; }
  .sub { color: var(--text-2); font-size: 12px; }
  .tags { display: flex; flex-wrap: wrap; gap: 4px; }
  .chip { padding: 0 8px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-2); color: var(--text-2); font-size: 12px; }
  .none { padding: 16px 6px; color: var(--text-2); font-size: 13px; }
  .viewer { display: grid; gap: 10px; min-width: 0; align-content: start; }
  header { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 10px; }
  .heading { min-width: 0; }
  h4 { margin: 0; font-size: 20px; font-weight: 650; overflow-wrap: anywhere; }
  .heading p { margin: 3px 0 0; color: var(--text-2); font-size: 13px; overflow-wrap: anywhere; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
  .confirm { display: inline-flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 13px; }
  .notes { margin: 0; padding: 8px 12px; border-left: 3px solid var(--border); color: var(--text-2); font-size: 13px; line-height: 1.55; overflow-wrap: anywhere; }
  .stage { min-width: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface-2); overflow: hidden; }
  .stage iframe { display: block; width: 100%; height: min(78vh, 900px); border: 0; background: #fff; }
  .stage img { display: block; max-width: 100%; max-height: 78vh; margin: 0 auto; object-fit: contain; }
  .stage video { display: block; width: 100%; max-height: 78vh; background: #000; }
  .stage audio { display: block; width: 100%; padding: 16px; box-sizing: border-box; }
  .stage pre { margin: 0; max-height: 78vh; overflow: auto; padding: 14px 16px; font-family: var(--mono); font-size: 13px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
  .stage :global(.rich) { border: 0; border-radius: 0; background: var(--surface); }
  .blank { display: grid; place-items: center; align-content: center; gap: 4px; min-height: 180px; padding: 24px; color: var(--text-2); text-align: center; }
  .blank p { margin: 0; font-size: 13px; }
  .blank.big { min-height: 260px; border: 1px dashed var(--border); border-radius: var(--radius); }
  .visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  @media (max-width: 760px) { .shell { grid-template-columns: minmax(0, 1fr); } ul { max-height: 40vh; } .actions :global(.btn) { min-height: 44px; } .search input { min-height: 44px; font-size: 16px; } .stage iframe { height: 70vh; } }
</style>
