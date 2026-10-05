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
      <span class="num pend" aria-hidden="true">{others.length}</span>
      <span class="text">
        <strong>{needsInputText(others.length)}</strong>
        <span class="what">{title}: {oneLine(first.prompt, 110)}</span>
      </span>
      <span class="go">{others.length === 1 ? 'Answer' : 'Review'} →</span>
    </a>
  </div>
{/if}

<style>
  /* Amber is the colour of "needs you": a quiet strip, loud only in its one colour. */
  .banner {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 40px;
    padding: 6px 24px;
    background: color-mix(in srgb, var(--amber) 14%, var(--bg));
    border-bottom: 1px solid color-mix(in srgb, var(--amber) 45%, var(--border));
    color: var(--text);
    text-decoration: none;
    font-size: 13px;
  }

  .banner:hover {
    background: color-mix(in srgb, var(--amber) 20%, var(--bg));
  }

  .text {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-width: 0;
    flex: 1;
  }

  strong {
    flex: none;
    font-weight: 600;
  }

  .what {
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .go {
    flex: none;
    font-weight: 600;
    color: var(--warn-text);
  }

  @media (max-width: 899px) {
    .banner {
      padding: 8px 16px;
    }

    .text {
      display: grid;
      gap: 0;
    }

    .what {
      font-size: 12px;
    }
  }
</style>
