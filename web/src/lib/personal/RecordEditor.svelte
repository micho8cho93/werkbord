<script lang="ts">
  import { maxLongTextBytes, maxURLBytes, viewWritableFields, type Collection, type Field, type PersonalRecord, type Values, type View } from './schema';
  import { workspaceSession } from './session.svelte';
  let { collection, view, record, onclose }: { collection: Collection; view: View; record?: PersonalRecord; onclose: () => void } = $props();
  const session = workspaceSession();
  const uid = $props.id();
  // Draft identity stays fixed until this editor is explicitly closed or saved.
  // svelte-ignore state_referenced_locally
  let draft = $state<Record<string, string | boolean | string[]>>(Object.fromEntries(collection.fields.map(f => [f.key,
    record?.values[f.key] ?? (f.type === 'boolean' ? false : f.type === 'multiselect' ? [] : ''),
  ])) as Record<string, string | boolean | string[]>);
  // What the view shows, then what its kind binds (a card's column, a snippet's body); a page's parent is moved, not typed.
  const fields = $derived([...new Set(viewWritableFields(view))].filter(key => key !== view.parentField).map(key => collection.fields.find(f => f.key === key)).filter((f): f is Field => !!f));
  async function save(event: SubmitEvent) {
    event.preventDefault();
    const values: Values = {};
    const clear: string[] = [];
    for (const f of fields) {
      const value = draft[f.key];
      if (f.type === 'longtext' && typeof value === 'string' && new TextEncoder().encode(value).length > maxLongTextBytes) {
        session.problem = `${f.name} is longer than ${maxLongTextBytes.toLocaleString()} bytes. Shorten it, then save again.`;
        return;
      }
      if (value === '' && !f.required) { clear.push(f.key); continue; }
      values[f.key] = f.type === 'number' ? Number(value) : value;
    }
    if (await session.mutate(view.id, record ? 'update' : 'create', record?.id, values, clear)) onclose();
  }
</script>

<form class="record-editor" onsubmit={save} aria-label={record ? 'Edit record' : 'Add record'}>
  <h4>{record ? 'Edit record' : 'Add record'}</h4>
  <div class="fields">
    {#each fields as field (field.key)}
      <div class="field" class:check={field.type === 'boolean'}>
        <label for={`${uid}-${field.key}`}>{field.name}{field.required ? ' (required)' : ''}</label>
        {#if field.type === 'boolean'}
          <input id={`${uid}-${field.key}`} type="checkbox" checked={draft[field.key] === true} onchange={(e) => draft[field.key] = e.currentTarget.checked} disabled={session.busy} />
        {:else if field.type === 'select' || field.type === 'multiselect'}
          {#if field.type === 'multiselect'}
            <select id={`${uid}-${field.key}`} multiple required={field.required} value={draft[field.key]} onchange={(e) => draft[field.key] = Array.from(e.currentTarget.selectedOptions, o => o.value)} disabled={session.busy}>
              {#each field.options ?? [] as option (option)}<option value={option}>{option}</option>{/each}
            </select>
          {:else}
            <select id={`${uid}-${field.key}`} required={field.required} value={draft[field.key]} onchange={(e) => draft[field.key] = e.currentTarget.value} disabled={session.busy}>
              <option value="">Choose {field.name.toLowerCase()}</option>
              {#each field.options ?? [] as option (option)}<option value={option}>{option}</option>{/each}
            </select>
          {/if}
        {:else if field.type === 'longtext'}
          <textarea id={`${uid}-${field.key}`} class:mono={view.kind === 'snippets'} rows="8" required={field.required} maxlength={maxLongTextBytes} spellcheck={view.kind === 'snippets' ? false : undefined} value={String(draft[field.key])} oninput={(e) => draft[field.key] = e.currentTarget.value} disabled={session.busy}></textarea>
        {:else}
          <input id={`${uid}-${field.key}`} type={field.type === 'number' ? 'number' : field.type === 'date' ? 'date' : field.type === 'url' ? 'url' : 'text'} step={field.type === 'number' ? 'any' : undefined} required={field.required} maxlength={field.type === 'text' ? 16000 : field.type === 'url' ? maxURLBytes : undefined} placeholder={field.type === 'datetime' ? '2026-10-23T12:00:00Z' : field.type === 'url' ? 'https://example.com' : undefined} value={String(draft[field.key])} oninput={(e) => draft[field.key] = e.currentTarget.value} disabled={session.busy} />
        {/if}
      </div>
    {/each}
  </div>
  <div class="actions"><button class="btn primary" type="submit" disabled={session.busy}>{session.busy ? 'Saving…' : 'Save record'}</button><button class="btn" type="button" disabled={session.busy} onclick={onclose}>Cancel</button></div>
</form>

<style>
  .record-editor { display: grid; gap: 14px; padding: 16px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); }
  h4 { margin: 0; font-size: 14px; }
  .fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr)); gap: 12px 16px; }
  .field { display: grid; align-content: start; gap: 6px; min-width: 0; }
  label { color: var(--text-2); font-size: 12px; overflow-wrap: anywhere; }
  textarea { width: 100%; min-width: 0; padding: 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; resize: vertical; }
  textarea.mono { font-family: var(--mono); font-size: 13px; tab-size: 2; }
  textarea:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .field:has(textarea) { grid-column: 1 / -1; }
  input, select { width: 100%; min-width: 0; min-height: 38px; padding: 8px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; }
  input:focus-visible, select:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .check { grid-template-columns: 1fr auto; align-items: center; }
  .check input { width: 20px; accent-color: var(--accent); }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }
  @media (pointer: coarse), (max-width: 600px) { input, select, textarea { font-size: 16px; } }
  @media (max-width: 600px) { input, select, .actions button { min-height: 44px; } }
</style>
