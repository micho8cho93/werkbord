<script lang="ts">
  import { needsInputText } from './questions';
  import { oneLine } from './format';
  import { globalHref, router, taskHref } from './router.svelte';
  import { app } from './state.svelte';

  // Questions come from every project: what needs you is never hidden by the project you happen to be in, so the
  // banner names the project, and leads to the Control Center when there are several.
  /** Questions the user is not already looking at: not the ones on the task page they have open. */
  const others = $derived(app.questions.filter((q) => q.taskId !== router.taskId));
  const onControlCenter = $derived(router.view === 'control');
  const first = $derived(others[0]);
  const info = $derived(first ? app.taskInfo(first.taskId, first.projectId) : undefined);
  const title = $derived(info ? (info.projectName ? `${info.projectName} · ${info.title}` : info.title) : 'A task');
  const href = $derived(others.length === 1 && first ? taskHref(first.projectId, first.taskId) : globalHref('control'));
</script>

{#if others.length > 0 && !onControlCenter && first}
  <div role="status">
    <a class="banner" {href}>
      <span class="pulse" aria-hidden="true"></span>
      <span class="text">
        <strong>{needsInputText(others.length)}</strong>
        <span class="what">{title}: {oneLine(first.prompt, 90)}</span>
      </span>
      <span class="go">{others.length === 1 ? 'Answer' : 'Review'} →</span>
    </a>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 46px;
    padding: 8px 16px;
    background: var(--warn);
    color: #1a1204;
    text-decoration: none;
    border-bottom: 1px solid color-mix(in srgb, var(--warn) 70%, #000);
  }

  .pulse {
    flex: none;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: #1a1204;
    animation: ping 1.4s ease-in-out infinite;
  }

  .text {
    display: grid;
    min-width: 0;
    flex: 1;
    line-height: 1.25;
  }

  .what {
    font-size: 0.82rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .go {
    flex: none;
    font-weight: 700;
    font-size: 0.85rem;
  }

  @keyframes ping {
    0%,
    100% {
      transform: scale(1);
      opacity: 1;
    }
    50% {
      transform: scale(0.6);
      opacity: 0.5;
    }
  }

  @media (min-width: 900px) {
    .banner {
      padding-inline: 24px;
    }
  }
</style>
