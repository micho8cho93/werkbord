<script lang="ts">
  import { api } from "../lib/api";
  import { resolveFor, priorityLabel } from "../lib/execution";
  import { interactionLabel } from "../lib/policy";
  import { taskHref } from "../lib/router.svelte";
  import {
    editableOrchestration,
    emptyOrchestration,
    instantFromWall,
    schedulingLabels,
    wallTime,
  } from "../lib/scheduling";
  import type { ProjectScope } from "../lib/scope.svelte";
  import { app } from "../lib/state.svelte";
  import type { Project, Task } from "../lib/types";
  let { scope, project }: { scope: ProjectScope; project: Project } = $props();
  const initialZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  let zone = $state(initialZone);
  const zoneValid = $derived.by(() => {
    try {
      new Intl.DateTimeFormat(undefined, { timeZone: zone });
      return true;
    } catch {
      return false;
    }
  });
  let view = $state<"day" | "week">(window.innerWidth < 700 ? "day" : "week");
  let date = $state(
    wallTime(new Date().toISOString(), initialZone).slice(0, 10),
  );
  let time = $state("01:00");
  let selected = $state("");
  let busy = $state(false);
  let error = $state("");
  let notice = $state("");
  const days = $derived.by(() => {
    const first = new Date(`${date}T12:00:00Z`);
    if (!Number.isFinite(first.getTime())) return [];
    return Array.from({ length: view === "day" ? 1 : 7 }, (_, i) =>
      new Date(first.getTime() + i * 86400000).toISOString().slice(0, 10),
    );
  });
  const candidates = $derived(
    scope.tasks.filter(
      (t) =>
        t.state !== "done" &&
        !scope.latestRun[t.id]?.state.match(
          /^(starting|running|waiting_for_user|blocked)$/,
        ),
    ),
  );
  const upcoming = $derived(
    scope.tasks
      .filter((t) => t.orchestration?.enabled && !t.orchestration.runId)
      .sort(
        (a, b) =>
          (a.orchestration?.scheduledAt ?? "").localeCompare(
            b.orchestration?.scheduledAt ?? "",
          ) ||
          (a.orchestration?.executionOrder ?? 1000001) -
            (b.orchestration?.executionOrder ?? 1000001),
      ),
  );
  const hours = Array.from({ length: 24 }, (_, i) =>
    String(i).padStart(2, "0"),
  );
  const label = (day: string) =>
    new Intl.DateTimeFormat(undefined, {
      weekday: "short",
      day: "numeric",
      month: "short",
      timeZone: "UTC",
    }).format(new Date(`${day}T12:00Z`));
  function cellTasks(day: string, hour: string) {
    return scope.tasks
      .filter((t) => {
        try {
          return (
            !!t.orchestration?.scheduledAt &&
            wallTime(t.orchestration.scheduledAt, zone).startsWith(
              `${day}T${hour}:`,
            )
          );
        } catch {
          return false;
        }
      })
      .sort((a, b) =>
        (a.orchestration?.scheduledAt ?? "").localeCompare(
          b.orchestration?.scheduledAt ?? "",
        ),
      );
  }
  function changeDate(amount: number) {
    date = new Date(Date.parse(`${date}T12:00Z`) + amount * 86400000)
      .toISOString()
      .slice(0, 10);
  }
  async function schedule(task: Task, day: string, at: string) {
    busy = true;
    error = "";
    notice = "";
    try {
      const scheduledAt = instantFromWall(`${day}T${at}`, zone);
      const o = editableOrchestration(
        task.orchestration ?? emptyOrchestration(),
      );
      scope.upsertTask(
        await api.editTask(task, {
          orchestration: { ...o, enabled: true, scheduledAt, timezone: zone },
        }),
      );
      notice = `${task.title} scheduled for ${day} at ${at} (${zone}).`;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      await scope.load().catch(() => {});
    } finally {
      busy = false;
    }
  }
  async function unschedule(task: Task) {
    busy = true;
    try {
      scope.upsertTask(
        await api.editTask(task, {
          orchestration: {
            ...editableOrchestration(
              task.orchestration ?? emptyOrchestration(),
            ),
            enabled: false,
            scheduledAt: undefined,
          },
        }),
      );
      notice = `${task.title} unscheduled.`;
      error = "";
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
  function drop(e: DragEvent, day: string, hour: string) {
    e.preventDefault();
    if (busy) return;
    const task = scope.tasks.find(
      (t) => t.id === e.dataTransfer?.getData("text/plain"),
    );
    if (task && candidates.some((t) => t.id === task.id))
      void schedule(task, day, `${hour}:00`);
  }
</script>

<div class="calendar-page">
  <header class="intro">
    <h2>Plan the next run</h2>
    <p class="muted">
      The same tasks as your Board. Schedules run on the controller, even when
      you disconnect.
    </p>
  </header>
  <div class="toolbar">
    <div class="views">
      <button
        class="btn"
        aria-pressed={view === "day"}
        onclick={() => (view = "day")}>Day</button
      ><button
        class="btn"
        aria-pressed={view === "week"}
        onclick={() => (view = "week")}>Week</button
      >
    </div>
    <button
      class="btn"
      aria-label="Previous period"
      onclick={() => changeDate(view === "day" ? -1 : -7)}>Previous</button
    >
    <label
      ><span class="visually-hidden">First calendar day</span><input
        class="input"
        type="date"
        bind:value={date}
        required
      /></label
    >
    <button
      class="btn"
      aria-label="Next period"
      onclick={() => changeDate(view === "day" ? 1 : 7)}>Next</button
    >
    <button
      class="btn quiet"
      disabled={!zoneValid}
      onclick={() =>
        (date = wallTime(new Date().toISOString(), zone).slice(0, 10))}
      >Today</button
    >
  </div>
  <form
    class="quick"
    onsubmit={(e) => {
      e.preventDefault();
      const t = candidates.find((t) => t.id === selected);
      if (t) void schedule(t, date, time);
    }}
  >
    <label
      >Task<select class="select" bind:value={selected}
        ><option value="">Choose a task…</option
        >{#each candidates as t (t.id)}<option value={t.id}>{t.title}</option
          >{/each}</select
      ></label
    >
    <label
      >Time<input class="input" type="time" bind:value={time} required /></label
    >
    <label
      >Timezone<input
        class="input"
        bind:value={zone}
        placeholder="Europe/Madrid"
        required
      /></label
    >
    <button
      class="btn primary"
      disabled={busy || !selected || !date || !zoneValid}
      >Schedule on {date}</button
    >
  </form>
  <p class="muted small">
    Drag a task onto an hour, or use the form above. Open a task for
    dependencies, order, deadline, and missed-time policy. Repeated clock-change
    hours use the first occurrence here; the task editor lets you choose.
  </p>
  {#if !zoneValid}<p class="error" role="alert">
      Enter a valid IANA timezone, such as Europe/Madrid.
    </p>{/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}{#if notice}<p
      role="status"
    >
      {notice}
    </p>{/if}
  <div class="layout">
    <section class="agenda" aria-label="Upcoming execution queue">
      <h2>Upcoming queue</h2>
      {#if !upcoming.length}<p class="muted">
          No automatic runs queued. Choose a task to schedule it.
        </p>{/if}
      <ul>
        {#each upcoming as task (task.id)}
          {@const decision = scope.decisions.find((d) => d.taskId === task.id)}
          {@const effective = resolveFor(
            task.execution,
            project.execution,
            app.globalExecution,
          )}
          <li
            draggable="true"
            ondragstart={(e) => e.dataTransfer?.setData("text/plain", task.id)}
          >
            <a href={taskHref(project.id, task.id)}
              >{task.orchestration?.executionOrder != null
                ? `${task.orchestration.executionOrder}. `
                : ""}{task.title}</a
            >
            <p class="small">
              {task.orchestration?.scheduledAt
                ? wallTime(
                    task.orchestration.scheduledAt,
                    task.orchestration.timezone ?? "UTC",
                  ).replace("T", " · ")
                : "No exact time"} · {task.orchestration?.timezone}
            </p>
            <p class="small muted">
              {priorityLabel(effective.priority)} · {interactionLabel({
                interaction: effective.interaction,
              })}
            </p>
            {#if decision}<p class="small" title={decision.reason}>
                <strong>{schedulingLabels[decision.state]}</strong> · {decision.reason}
              </p>{/if}
            <button
              class="btn small quiet"
              disabled={busy}
              onclick={() => unschedule(task)}>Unschedule</button
            >
          </li>
        {/each}
      </ul>
      <details>
        <summary>Tasks to schedule ({candidates.length})</summary>
        <ul>
          {#each candidates as t (t.id)}<li
              draggable="true"
              ondragstart={(e) => e.dataTransfer?.setData("text/plain", t.id)}
            >
              <a href={taskHref(project.id, t.id)}>{t.title}</a><button
                class="btn small"
                onclick={() => (selected = t.id)}>Select</button
              >
            </li>{/each}
        </ul>
      </details>
    </section>
    <section class="timeline" aria-label="Calendar in {zone}">
      <div
        class="grid"
        style:grid-template-columns={`3.5rem repeat(${days.length}, minmax(140px,1fr))`}
      >
        <div class="corner">Time</div>
        {#each days as day (day)}<div class="day-head">{label(day)}</div>{/each}
        {#each hours as hour (hour)}
          <div class="hour">{hour}:00</div>
          {#each days as day (day)}
            <div
              class="slot"
              role="group"
              aria-label="{label(day)} {hour}:00"
              ondragover={(e) => e.preventDefault()}
              ondrop={(e) => drop(e, day, hour)}
            >
              {#each cellTasks(day, hour) as t (t.id)}<a
                  class="event"
                  class:dispatched={!!t.orchestration?.runId ||
                    !t.orchestration?.enabled}
                  href={taskHref(project.id, t.id)}
                  draggable="true"
                  ondragstart={(e) =>
                    e.dataTransfer?.setData("text/plain", t.id)}
                  ><time
                    >{wallTime(t.orchestration?.scheduledAt, zone).slice(
                      11,
                    )}</time
                  ><span>{t.title}</span></a
                >{/each}
            </div>
          {/each}
        {/each}
      </div>
    </section>
  </div>
</div>

<style>
  .calendar-page,
  .intro {
    display: grid;
    gap: 14px;
  }
  .intro h2 {
    color: var(--text);
    font-size: 1.2rem;
    text-transform: none;
    letter-spacing: normal;
  }
  .toolbar,
  .views {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    align-items: center;
  }
  .views .btn[aria-pressed="true"] {
    background: var(--accent);
    color: var(--bg);
  }
  .quick {
    display: flex;
    gap: 12px;
    align-items: end;
    flex-wrap: wrap;
  }
  .quick label {
    display: grid;
    gap: 5px;
    flex: 1;
    min-width: 140px;
  }
  .quick label:first-child {
    flex: 2;
  }
  .quick .btn {
    min-height: 44px;
  }
  .small {
    font-size: 0.82rem;
  }
  .layout {
    display: grid;
    grid-template-columns: minmax(240px, 300px) minmax(0, 1fr);
    gap: 24px;
  }
  .agenda {
    display: grid;
    gap: 12px;
    align-content: start;
  }
  .agenda ul {
    list-style: none;
    padding: 0;
    margin: 0;
  }
  .agenda li {
    padding: 14px 0;
    border-bottom: 1px solid var(--border);
    display: grid;
    gap: 6px;
  }
  .agenda a {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .agenda details {
    margin-top: 12px;
  }
  .agenda summary {
    cursor: pointer;
    min-height: 44px;
  }
  .timeline {
    max-height: 70vh;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .grid {
    display: grid;
  }
  .day-head,
  .corner {
    padding: 12px 8px;
    position: sticky;
    top: 0;
    background: var(--surface);
    z-index: 1;
    border-bottom: 1px solid var(--border);
    font-size: 0.85rem;
    font-weight: 600;
  }
  .hour {
    font-size: 0.8rem;
    color: var(--text-2);
    padding: 10px 6px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .slot {
    min-height: 65px;
    border-left: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
    padding: 4px;
    display: grid;
    gap: 4px;
    align-content: start;
  }
  .slot:hover {
    background: var(--surface);
  }
  .event {
    background: color-mix(in srgb, var(--accent) 12%, var(--surface));
    border-radius: var(--radius-sm);
    padding: 8px;
    display: grid;
    gap: 4px;
    text-decoration: none;
    overflow-wrap: anywhere;
    font-size: 0.82rem;
  }
  .event time {
    font-variant-numeric: tabular-nums;
  }
  .event.dispatched {
    background: var(--surface);
    color: var(--text-2);
  }
  @media (max-width: 1000px) {
    .layout {
      grid-template-columns: 1fr;
    }
    .timeline {
      order: -1;
    }
    .agenda {
      max-width: 52rem;
    }
  }
  @media (max-width: 600px) {
    .toolbar {
      gap: 6px;
    }
    .toolbar .btn {
      font-size: 0.8rem;
    }
    .quick {
      display: grid;
      grid-template-columns: 1fr 1fr;
    }
    .quick label:first-child,
    .quick label:nth-child(3),
    .quick .btn {
      grid-column: 1/-1;
    }
    .timeline {
      max-height: 58vh;
    }
  }
</style>
