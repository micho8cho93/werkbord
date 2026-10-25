<script lang="ts">
  import { getFormName, getFormValidation, getIsStreaming, getTriggerAction, type ComponentRenderProps } from '@openuidev/svelte-lang';
  import { currentSession } from '../../session-context';
  const assistant = currentSession();

  let { props }: ComponentRenderProps<{ label: string }> = $props();
  const formName = getFormName();
  const validation = getFormValidation();
  const isStreaming = getIsStreaming();
  const triggerAction = getTriggerAction();
  let submitted = $state(false);
  let button: HTMLButtonElement | undefined = $state();
  const disabled = $derived(isStreaming() || assistant.running || submitted);

  $effect(() => {
    if (!assistant.running) submitted = false;
  });

  function activate(): void {
    if (disabled) return;
    if (validation && !validation.validateForm()) {
      button?.closest('form')?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
      return;
    }
    submitted = true;
    triggerAction(props.label, formName);
    // The parent handles unsupported or invalid action payloads synchronously. Keep the button locked only
    // when that callback actually started the assistant turn; a person can correct a rejected form and retry.
    if (!assistant.running) submitted = false;
  }
</script>

<button bind:this={button} class="btn primary action" type="button" disabled={disabled} aria-label={props.label} onclick={activate}>
  {submitted ? 'Sending…' : props.label}
</button>

<style>
  .action { min-height: 42px; justify-self: start; white-space: normal; }
  @media (max-width: 600px) { .action { width: 100%; min-height: 44px; justify-self: stretch; } }
</style>
