<script lang="ts">
  import { getFormName, getGetFieldValue, getIsStreaming, getSetFieldValue, getFormValidation, parseStructuredRules, useSetDefaultValue, type ComponentRenderProps } from '@openuidev/svelte-lang';
  import { onDestroy } from 'svelte';
  import { assistant } from '../../store.svelte';
  import { validOpenUIFieldName } from '../state';

  let { props }: ComponentRenderProps<{ name: string; label: string; value?: string; placeholder?: string; required?: boolean; maxLength?: number }> = $props();
  const instanceId = $props.id();
  const formName = getFormName();
  const getValue = getGetFieldValue();
  const setValue = getSetFieldValue();
  const isStreaming = getIsStreaming();
  const validation = getFormValidation();
  // Field identity and rules come from this parsed component and stay fixed while the person edits.
  // svelte-ignore state_referenced_locally
  const name = validOpenUIFieldName(props.name) ? props.name : 'invalid-field';
  // svelte-ignore state_referenced_locally
  const rules = parseStructuredRules({ required: props.required, maxLength: Math.min(500, Math.max(1, props.maxLength ?? 500)) });
  // svelte-ignore state_referenced_locally
  let value = $state(props.value ?? '');
  const disabled = $derived(isStreaming() || assistant.running);
  const error = $derived(validation?.errors[name]);
  const id = `${instanceId}-${formName ?? 'openui'}-${name}`;
  const errorId = `${id}-error`;

  // svelte-ignore state_referenced_locally
  useSetDefaultValue({ formName, componentType: 'TextField', name, defaultValue: props.value ?? '' });
  validation?.registerField(name, rules, () => getValue(formName, name) ?? props.value ?? '');
  onDestroy(() => validation?.unregisterField(name));

  $effect(() => {
    const stored = getValue(formName, name);
    if (typeof stored === 'string' && stored !== value) value = stored;
  });

  function changed(event: Event): void {
    value = (event.currentTarget as HTMLInputElement).value;
    setValue(formName, 'TextField', name, value, true);
    if (error) validation?.validateField(name, value, rules);
  }

  function blurred(): void {
    setValue(formName, 'TextField', name, value, true);
    validation?.validateField(name, value, rules);
  }
</script>

<div class="field">
  <label for={id}>{props.label}{#if props.required}<span class="required">Required</span>{/if}</label>
  <input id={id} type="text" value={value} placeholder={props.placeholder ?? ''} maxlength={Math.min(500, Math.max(1, props.maxLength ?? 500))} required={props.required ?? false} aria-invalid={error ? 'true' : undefined} aria-describedby={error ? errorId : undefined} disabled={disabled} oninput={changed} onblur={blurred} />
  {#if error}<span class="error" id={errorId}>{error}</span>{/if}
</div>

<style>
  .field { display: grid; min-width: 0; gap: 5px; }
  label { display: flex; flex-wrap: wrap; align-items: baseline; justify-content: space-between; gap: 3px 10px; color: var(--text); font-size: 12.5px; font-weight: 500; }
  .required { color: var(--text-2); font-size: 11.5px; font-weight: 400; }
  input { width: 100%; min-width: 0; min-height: 40px; padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font: inherit; }
  input:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  input[aria-invalid='true'] { border-color: var(--danger); }
  input:disabled { opacity: .68; }
  .error { color: var(--danger-text); font-size: 12px; }
  @media (max-width: 600px) { input { min-height: 44px; font-size: 16px; } }
</style>
