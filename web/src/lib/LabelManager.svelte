<script lang="ts">
  import { api } from './api';
  import LabelChip from './LabelChip.svelte';
  import { LABEL_COLORS, sortedLabels } from './labels';
  import { app } from './state.svelte';
  import type { LabelUse } from './types';

  // Where labels are made, renamed, recoloured and deleted. They belong to no project: one list serves
  // every board and timeline. Renaming a label renames it everywhere it is used; deleting one takes it
  // off the tasks that carry it and nothing else.

  let name = $state('');
  let color = $state(LABEL_COLORS[5]);
  let description = $state('');
  let busy = $state(false);
  let error = $state('');

  let editing = $state<string>('');
  let draft = $state({ name: '', color: '', description: '' });
  let armed = $state('');

  async function create(e: SubmitEvent) {
    e.preventDefault();
    if (!name.trim() || busy) return;
    busy = true;
    error = '';
    try {
      const l = await api.createLabel(name.trim(), color, description.trim());
      app.labels = [...app.labels, { ...l, tasks: 0 }];
      name = '';
      description = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  function edit(l: LabelUse) {
    editing = l.id;
    draft = { name: l.name, color: l.color, description: l.description ?? '' };
    error = '';
  }

  async function save(l: LabelUse) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const saved = await api.editLabel(l, { name: draft.name, color: draft.color, description: draft.description });
      app.labels = app.labels.map((x) => (x.id === l.id ? { ...saved, tasks: l.tasks } : x));
      editing = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      void api.listLabels().then((ls) => (app.labels = ls), () => {});
    } finally {
      busy = false;
    }
  }

  async function remove(l: LabelUse) {
    if (armed !== l.id) {
      armed = l.id;
      return;
    }
    busy = true;
    error = '';
    try {
      await api.deleteLabel(l.id);
      app.labels = app.labels.filter((x) => x.id !== l.id);
      armed = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="mgr">
  <form class="new" onsubmit={create}>
    <div class="fields">
      <label>
        <span>Name</span>
        <input class="input" maxlength="40" placeholder="Q4 launch, Waiting on legal, Design…" bind:value={name} required />
      </label>
      <label>
        <span>What it is for <span class="opt">optional</span></span>
        <input class="input" maxlength="200" bind:value={description} />
      </label>
    </div>
    <div class="swatches" role="radiogroup" aria-label="Colour">
      {#each LABEL_COLORS as c (c)}
        <button type="button" class="sw" role="radio" aria-checked={color === c} aria-label="Colour {c}" style:background={c} onclick={() => (color = c)}></button>
      {/each}
      <input class="custom" type="color" aria-label="Pick another colour" bind:value={color} />
    </div>
    <div class="preview">
      {#if name.trim()}<LabelChip label={{ name: name.trim(), color }} />{/if}
      <button class="btn primary" type="submit" disabled={busy || !name.trim()}>Add label</button>
    </div>
  </form>

  {#if error}<p class="error" role="alert">{error}</p>{/if}

  {#if app.labels.length}
    <ul class="list">
      {#each sortedLabels(app.labels) as l (l.id)}
        <li>
          {#if editing === l.id}
            <form class="edit" onsubmit={(e) => { e.preventDefault(); void save(l); }}>
              <input class="input" maxlength="40" bind:value={draft.name} aria-label="Name" required />
              <input class="input" maxlength="200" bind:value={draft.description} aria-label="What it is for" placeholder="What it is for" />
              <div class="swatches" role="radiogroup" aria-label="Colour">
                {#each LABEL_COLORS as c (c)}
                  <button type="button" class="sw" role="radio" aria-checked={draft.color === c} aria-label="Colour {c}" style:background={c} onclick={() => (draft.color = c)}></button>
                {/each}
                <input class="custom" type="color" aria-label="Pick another colour" bind:value={draft.color} />
              </div>
              <div class="acts">
                <button class="btn small primary" type="submit" disabled={busy || !draft.name.trim()}>Save</button>
                <button class="btn small" type="button" onclick={() => (editing = '')}>Cancel</button>
              </div>
            </form>
          {:else}
            <LabelChip label={l} />
            <span class="what">{l.description ?? ''}</span>
            <span class="n" title="Open tasks with this label">{l.tasks} {l.tasks === 1 ? 'task' : 'tasks'}</span>
            <span class="acts">
              <button class="btn small" onclick={() => edit(l)} aria-label="Edit label {l.name}">Edit</button>
              <button class="btn small danger" class:armed={armed === l.id} disabled={busy} onclick={() => remove(l)} onblur={() => (armed = armed === l.id ? '' : armed)} aria-label="Delete label {l.name}">
                {armed === l.id ? (l.tasks ? `Remove from ${l.tasks}` : 'Delete?') : 'Delete'}
              </button>
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  {:else}
    <p class="muted">No labels yet. A label is a name and a colour of your choosing, which you can put on any task in any project.</p>
  {/if}
</div>

<style>
  .mgr {
    display: grid;
    gap: 16px;
  }

  .new,
  .edit {
    display: grid;
    gap: 10px;
  }

  .fields {
    display: grid;
    gap: 10px;
    grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  }

  label {
    display: grid;
    gap: 6px;
    font-size: 13px;
    font-weight: 500;
  }

  .opt {
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 400;
    color: var(--text-2);
  }

  .swatches {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  .sw {
    width: 22px;
    height: 22px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    cursor: pointer;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.18);
  }

  .sw[aria-checked='true'] {
    outline: 2px solid var(--text);
    outline-offset: 2px;
  }

  .custom {
    width: 28px;
    height: 24px;
    padding: 0;
    border: 0;
    background: none;
    cursor: pointer;
  }

  .preview {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .preview .btn {
    margin-left: auto;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }

  .list li {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 46px;
    padding: 6px 0;
    border-top: 1px solid var(--border);
  }

  .what {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
    color: var(--text-2);
  }

  .n {
    font-family: var(--mono);
    font-size: 11px;
    color: var(--text-2);
    white-space: nowrap;
  }

  .acts {
    display: flex;
    gap: 6px;
  }

  .edit {
    width: 100%;
  }

  @media (max-width: 640px) {
    .list li {
      flex-wrap: wrap;
    }

    .what {
      flex-basis: 100%;
      order: 3;
    }
  }
</style>
