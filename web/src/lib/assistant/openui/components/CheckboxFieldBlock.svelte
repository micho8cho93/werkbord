<script lang="ts">
  import { getFormName, getGetFieldValue, getIsStreaming, getSetFieldValue, type ComponentRenderProps, useSetDefaultValue } from '@openuidev/svelte-lang';
  import { currentSession } from '../../session-context';
  const assistant = currentSession();
  import { validOpenUIFieldName } from '../state';

  let { props }: ComponentRenderProps<{ name: string; label: string; checked?: boolean }> = $props();
  const instanceId = $props.id();
  const formName = getFormName();
  const getValue = getGetFieldValue();
  const setValue = getSetFieldValue();
  const isStreaming = getIsStreaming();
  // Field identity and initial value come from this parsed component and stay fixed while the person edits.
  // svelte-ignore state_referenced_locally
  const name = validOpenUIFieldName(props.name) ? props.name : 'invalid-field';
  // svelte-ignore state_referenced_locally
  let checked = $state(props.checked ?? false);
  const disabled = $derived(isStreaming() || assistant.running);
  const id = `${instanceId}-${formName ?? 'openui'}-${name}`;

  // svelte-ignore state_referenced_locally
  useSetDefaultValue({ formName, componentType: 'CheckboxField', name, defaultValue: props.checked ?? false });
  $effect(() => {
    const stored = getValue(formName, name);
    if (typeof stored === 'boolean' && stored !== checked) checked = stored;
  });

  function changed(event: Event): void {
    checked = (event.currentTarget as HTMLInputElement).checked;
    setValue(formName, 'CheckboxField', name, checked, true);
  }
</script>

<label class="check" for={id}>
  <input id={id} type="checkbox" checked={checked} disabled={disabled} onchange={changed} />
  <span>{props.label}</span>
</label>

<style>
  .check { display: flex; align-items: center; gap: 9px; min-height: 40px; color: var(--text); font-size: 12.5px; cursor: pointer; }
  input { width: 18px; height: 18px; flex: 0 0 18px; accent-color: var(--accent); }
  input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  input:disabled + span { opacity: .68; }
  @media (max-width: 600px) { .check { min-height: 44px; } }
</style>
