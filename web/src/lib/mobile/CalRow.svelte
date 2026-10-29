<script lang="ts">
  import { formatTime } from '../clock.svelte';
  import { TASK_STATE_LABELS } from '../types';
  import { spanTitle } from '../planning-dates';
  import type { CalItem } from './calendar';

  // One calendar item as a row: what it is, where it sits in time and which kind of time that is. A plan (work dates) is cobalt;
  // a schedule (when an agent starts) is amber and says the hour, so the two are never mistaken for each other.
  let { item, onopen }: { item: CalItem; onopen: (item: CalItem) => void } = $props();
  const when = $derived(
    item.kind === 'scheduled' && item.at
      ? `Agent starts ${formatTime(item.at)}`
      : item.milestone
        ? `Milestone · ${spanTitle({ start: item.start, end: item.end, milestone: true }, new Date().getFullYear())}`
        : `Planned ${spanTitle({ start: item.start, end: item.end, milestone: false }, new Date().getFullYear())}`,
  );
</script>

<button class="row card" type="button" data-kind={item.kind} data-testid="cal-item" onclick={() => onopen(item)}>
  <span class="mark" aria-hidden="true"></span>
  <span class="what">
    <span class="title">{item.title}</span>
    <span class="meta mm">{item.projectName} · {when}</span>
  </span>
  <span class="state mm">{TASK_STATE_LABELS[item.state]}</span>
</button>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 56px;
    padding: 10px 14px;
    text-align: left;
    font: inherit;
    color: var(--text);
    -webkit-tap-highlight-color: transparent;
  }

  .row:active {
    box-shadow: var(--press-sh);
    background: var(--surface-2);
  }

  .mark {
    flex: none;
    width: 4px;
    align-self: stretch;
    border-radius: 2px;
    background: var(--accent);
  }

  [data-kind='scheduled'] .mark {
    background: var(--amber);
  }

  .what {
    display: grid;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }

  .title {
    font-size: 14px;
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .meta {
    overflow-wrap: anywhere;
  }

  .state {
    flex: none;
  }
</style>
