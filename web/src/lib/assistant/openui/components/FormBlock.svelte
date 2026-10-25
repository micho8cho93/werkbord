<script lang="ts">
  import { createFormValidation, getIsStreaming, setFormNameContext, setFormValidationContext, type ComponentRenderProps } from '@openuidev/svelte-lang';
  import type { Snippet } from 'svelte';
  import { currentSession } from '../../session-context';
  const assistant = currentSession();
  import { validOpenUIFormName } from '../state';

  let { props, renderNode }: ComponentRenderProps<{ name: string; title: string; description?: string; children?: unknown[] }> & { renderNode: Snippet<[unknown]> } = $props();
  const validation = createFormValidation();
  const instanceId = $props.id();
  // The generated form identity is fixed when this parsed component mounts.
  // svelte-ignore state_referenced_locally
  setFormNameContext(validOpenUIFormName(props.name) ? props.name : undefined);
  setFormValidationContext(validation);
  const isStreaming = getIsStreaming();
  const disabled = $derived(isStreaming() || assistant.running);
  const headingId = $derived(`${instanceId}-${props.name}-heading`);
  const descriptionId = $derived(`${instanceId}-${props.name}-description`);

  function preventSubmit(event: SubmitEvent): void {
    event.preventDefault();
  }
</script>

<form class="generated-form" aria-labelledby={headingId} aria-describedby={props.description ? descriptionId : undefined} aria-busy={disabled} data-testid="openui-form" data-form-name={props.name} onsubmit={preventSubmit}>
  <fieldset disabled={disabled}>
    <legend id={headingId}>{props.title}</legend>
    {#if props.description}<p class="description" id={descriptionId}>{props.description}</p>{/if}
    <div class="fields">
      {@render renderNode(props.children ?? [])}
    </div>
  </fieldset>
</form>

<style>
  .generated-form { display: grid; min-width: 0; gap: 10px; padding: 0; border: 0; background: transparent; }
  fieldset { display: grid; min-width: 0; gap: 8px; margin: 0; padding: 0; border: 0; }
  legend { width: 100%; padding: 0; color: var(--text); font-size: 14px; font-weight: 600; }
  .description { margin: 0; color: var(--text-2); font-size: 13px; line-height: 1.45; }
  .fields { display: grid; min-width: 0; gap: 12px; }
  @media (max-width: 600px) { .generated-form { padding: 12px; } }
</style>
