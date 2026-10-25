<script lang="ts">
  import type { ComponentRenderProps } from '@openuidev/svelte-lang';
  import SnapshotStamp from './SnapshotStamp.svelte';

  type TimelineItem = { id?: string; title?: string; start?: string; end?: string; state?: 'backlog' | 'doing' | 'review' | 'done'; milestone?: boolean; dependsOn?: string[] };
  type TimelineRow = TimelineItem & { startMs: number; endMs: number; left: number; width: number };
  let { props }: ComponentRenderProps<{ title?: string; items?: TimelineItem[]; asOf?: string; note?: string }> = $props();

  const prepared = $derived.by(() => {
    const parsed = (props.items ?? []).slice(0, 50).map((item) => ({
      item,
      startMs: item.start ? Date.parse(item.start) : Number.NaN,
      endMs: item.end ? Date.parse(item.end) : Number.NaN,
    }));
    const dated = parsed.filter((row) => Number.isFinite(row.startMs) && Number.isFinite(row.endMs));
    if (dated.length === 0) return { rows: [] as TimelineRow[], undated: parsed.map((row) => row.item), start: '', end: '' };
    const min = Math.min(...dated.map((row) => Math.min(row.startMs, row.endMs)));
    const max = Math.max(...dated.map((row) => Math.max(row.startMs, row.endMs)));
    const span = Math.max(max - min, 24 * 60 * 60 * 1000);
    const rows = dated.map(({ item, startMs, endMs }) => {
      const left = (Math.min(startMs, endMs) - min) / span * 100;
      const range = Math.max(0, endMs - startMs) / span * 100;
      return { ...item, startMs, endMs, left, width: item.milestone ? 0 : Math.max(range, 1.5) };
    });
    return {
      rows,
      undated: parsed.filter((row) => !Number.isFinite(row.startMs) || !Number.isFinite(row.endMs)).map((row) => row.item),
      start: new Date(min).toISOString().slice(0, 10),
      end: new Date(max).toISOString().slice(0, 10),
    };
  });
</script>

<section class="timeline">
  {#if props.title}<h3>{props.title}</h3>{/if}
  <SnapshotStamp asOf={props.asOf} />
  {#if (props.items ?? []).length === 0}
    <p class="empty">No scheduled tasks are in this view.</p>
  {:else if prepared.rows.length === 0}
    <p class="empty">No complete planned date ranges are recorded. The task list below keeps the available details.</p>
  {/if}
  {#if prepared.rows.length > 0}
    <div class="scale" aria-hidden="true"><span>{prepared.start}</span><span>{prepared.end}</span></div>
    <ol class="rows" aria-label={props.title ?? 'Project timeline'}>
      {#each prepared.rows as item, i (`${item.id ?? item.title}-${i}`)}
        <li class="row" data-state={item.state ?? 'backlog'}>
          <div class="task"><strong>{item.title ?? 'Untitled task'}</strong><span class="state">{item.state ?? 'State not reported'}</span></div>
          <div class="lane" aria-label={`${item.start} to ${item.end}`}>
            <span class:milestone={item.milestone} class="bar" style={`--left:${item.left}%;--width:${item.width}%`} title={`${item.title ?? 'Task'}: ${item.start} to ${item.end}`}></span>
          </div>
          <div class="dates"><span>{item.start}</span><span>{item.end}</span></div>
          {#if item.dependsOn?.length}<p class="dependencies">Depends on {item.dependsOn.join(', ')}</p>{/if}
        </li>
      {/each}
    </ol>
  {/if}
  {#if prepared.undated.length > 0}
    <div class="undated"><h4>Dates incomplete</h4><ul>{#each prepared.undated as item, i (`${item.id ?? item.title}-${i}`)}<li><strong>{item.title ?? 'Untitled task'}</strong><span>{item.start && item.end ? `${item.start} to ${item.end}` : item.start ? `Start recorded: ${item.start}` : item.end ? `End recorded: ${item.end}` : 'No planned range recorded'}</span></li>{/each}</ul></div>
  {/if}
  {#if props.note}<p class="note">{props.note}</p>{/if}
</section>

<style>
  .timeline { display: grid; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; }
  .scale { display: flex; justify-content: space-between; gap: 8px; padding-left: min(30%, 190px); color: var(--text-2); font: 12px var(--mono); font-variant-numeric: tabular-nums; }
  .rows { display: grid; gap: 6px; list-style: none; margin: 0; padding: 0; }
  .row { display: grid; grid-template-columns: minmax(110px, 30%) minmax(100px, 1fr); align-items: center; gap: 5px 12px; padding: 6px 0; border-top: 1px solid var(--border); }
  .task { display: flex; flex-wrap: wrap; align-items: baseline; gap: 3px 8px; min-width: 0; }
  .task strong { min-width: 0; font-size: 12px; font-weight: 500; overflow-wrap: anywhere; }
  .state { color: var(--text-2); font-size: 12px; }
  .lane { position: relative; grid-column: 2; height: 16px; min-width: 0; border-bottom: 1px solid var(--border); background-image: linear-gradient(to right, transparent calc(50% - .5px), var(--border) 50%, transparent calc(50% + .5px)); }
  .bar { position: absolute; top: 3px; left: var(--left); width: var(--width); height: 8px; min-width: 5px; border-radius: 2px; background: var(--accent); }
  .row[data-state='done'] .bar { background: var(--ok); }
  .row[data-state='review'] .bar { background: var(--warn); }
  .bar.milestone { top: 3px; width: 9px; min-width: 9px; height: 9px; transform: translateX(-50%) rotate(45deg); border-radius: 1px; background: var(--warn); }
  .dates { display: flex; justify-content: space-between; grid-column: 2; gap: 8px; color: var(--text-2); font: 12px var(--mono); font-variant-numeric: tabular-nums; }
  .dates span { overflow-wrap: anywhere; }
  .dependencies, .note { grid-column: 1 / -1; margin: 0; color: var(--text-2); font-size: 12px; line-height: 1.4; overflow-wrap: anywhere; }
  .undated { padding-top: 8px; border-top: 1px solid var(--border); }
  h4 { margin: 0 0 5px; font-size: 12px; font-weight: 600; }
  .undated ul { display: grid; gap: 4px; list-style: none; margin: 0; padding: 0; }
  .undated li { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 3px 12px; font-size: 12px; }
  .undated li span { color: var(--text-2); }
  .empty { margin: 0; padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text-2); font-size: 12px; line-height: 1.45; }
  @media (max-width: 520px) {
    .scale { padding-left: 0; }
    .row { grid-template-columns: minmax(0, 1fr); gap: 4px; }
    .lane, .dates { grid-column: 1; }
    .dependencies { grid-column: 1; }
  }
</style>
