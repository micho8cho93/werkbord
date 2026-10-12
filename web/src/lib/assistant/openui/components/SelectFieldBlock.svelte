<script lang="ts">
  import { getFormName, getGetFieldValue, getIsStreaming, getSetFieldValue, getFormValidation, parseStructuredRules, useSetDefaultValue, type ComponentRenderProps } from '@openuidev/svelte-lang';
  import { onDestroy } from 'svelte';
  import { assistant } from '../../store.svelte';
  import { validOpenUIFieldName } from '../state';

  type Option = { value: string; label: string };
  let { props }: ComponentRenderProps<{ name: string; label: string; options?: Option[]; value?: string; required?: boolean }> = $props();
  const instanceId = $props.id();
  const formName = getFormName();
  const getValue = getGetFieldValue();
  const setValue = getSetFieldValue();
  const isStreaming = getIsStreaming();
  const validation = getFormValidation();
  // Field identity and validation rules come from this parsed component and stay fixed while the person edits.
  // svelte-ignore state_referenced_locally
  const name = validOpenUIFieldName(props.name) ? props.name : 'invalid-field';
  // svelte-ignore state_referenced_locally
  const rules = parseStructuredRules({ required: props.required });
  const options = $derived((props.options ?? []).filter((option) => typeof option?.value === 'string' && typeof option?.label === 'string'));
  const defaultValue = $derived(props.value && options.some((option) => option.value === props.value) ? props.value : '');
  let value = $state('');
  const disabled = $derived(isStreaming() || assistant.running);
  const error = $derived(validation?.errors[name]);
  const id = `${instanceId}-${formName ?? 'openui'}-${name}`;
  const errorId = `${id}-error`;

  // svelte-ignore state_referenced_locally
  useSetDefaultValue({ formName, componentType: 'SelectField', name, defaultValue: props.value ?? '' });
  validation?.registerField(name, rules, () => getValue(formName, name) ?? defaultValue);
  onDestroy(() => validation?.unregisterField(name));

  $effect(() => {
    const stored = getValue(formName, name);
    if (typeof stored === 'string' && options.some((option) => option.value === stored)) {
      if (stored !== value) value = stored;
    } else if (stored !== undefined && !isStreaming()) {
      value = defaultValue;
      setValue(formName, 'SelectField', name, defaultValue, true);
    } else if (stored === undefined && defaultValue !== value) value = defaultValue;
  });

  function changed(event: Event): void {
    value = (event.currentTarget as HTMLSelectElement).value;
    if (value !== '' && !options.some((option) => option.value === value)) {
      value = '';
      setValue(formName, 'SelectField', name, '', true);
      return;
    }
    setValue(formName, 'SelectField', name, value, true);
    validation?.validateField(name, value, rules);
  }
</script>

<div class="field">
  <label for={id}>{props.label}{#if props.required}<span class="required">Required</span>{/if}</label>
  <select id={id} bind:value={value} required={props.required ?? false} aria-invalid={error ? 'true' : undefined} aria-describedby={error ? errorId : undefined} disabled={disabled} onchange={changed}>
    <option value="">Choose…</option>
    {#each options as option (option.value)}<option value={option.value}>{option.label}</option>{/each}
  </select>
  {#if error}<span class="error" id={errorId}>{error}</span>{/if}
</div>

<style>
  .field { display: grid; min-width: 0; gap: 5px; }
  label { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 3px 10px; color: var(--text); font-size: 12.5px; font-weight: 500; }
  .required { color: var(--text-2); font-size: 11.5px; font-weight: 400; }
  select { width: 100%; min-width: 0; min-height: 40px; padding: 8px 30px 8px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; }
  select:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  select[aria-invalid='true'] { border-color: var(--danger); }
  select:disabled { opacity: .68; }
  .error { color: var(--danger-text); font-size: 12px; }
  @media (max-width: 600px) { select { min-height: 44px; font-size: 16px; } }
</style>
