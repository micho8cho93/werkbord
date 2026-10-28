<script lang="ts">
  import { tick, untrack } from "svelte";
  import { SvelteSet } from "svelte/reactivity";
  import type { Orchestration, Task } from "./types";
  import { canScheduleAnother, instantFromWall, wallTime } from "./scheduling";
  import { confirmAction } from "./confirm.svelte";
  let {
    value = $bindable(),
    tasks,
    taskId,
    original,
    onsave,
    busy = false,
  }: {
    value: Orchestration;
    tasks: Task[];
    taskId: string;
    original?: Orchestration;
    onsave: (o: Orchestration) => Promise<void>;
    busy?: boolean;
  } = $props();
  let zone = $state("");
  let scheduled = $state("");
  let notBefore = $state("");
  let deadline = $state("");
  let paths = $state("");
  let order = $state<number | undefined>();
  let grace = $state(5);
  let occurrence = $state<"earlier" | "later">("earlier");
  let error = $state("");
  const draftIssues = $derived.by(() => {
    const out: { field: string; message: string }[] = [];
    let zoneOK = !!zone.trim();
    if (!zoneOK) out.push({ field: 'timezone', message: 'Enter a timezone.' });
    else {
      try { new Intl.DateTimeFormat('en', { timeZone: zone }).format(0); }
      catch { zoneOK = false; out.push({ field: 'timezone', message: 'Choose a valid IANA timezone.' }); }
    }
    const instants: Record<string, string | undefined> = {};
    if (zoneOK) for (const [field, value] of [['scheduled', scheduled], ['notBefore', notBefore], ['deadline', deadline]] as const) {
      try { instants[field] = instantFromWall(value, zone, occurrence); }
      catch (e) { out.push({ field, message: e instanceof Error ? e.message : 'Enter a valid date and time.' }); }
    }
    const deadlineAt = instants.deadline ? Date.parse(instants.deadline) : undefined;
    if (deadlineAt !== undefined && ['scheduled', 'notBefore'].some((key) => instants[key] && Date.parse(instants[key]!) > deadlineAt)) {
      out.push({ field: 'deadline', message: 'Deadline must be at or after the scheduled and not-before times.' });
    }
    const seen = new SvelteSet<string>();
    for (const id of value.dependencies) {
      if (seen.has(id)) out.push({ field: 'dependencies', message: 'Choose each prerequisite once.' });
      seen.add(id);
      const prerequisite = tasks.find((t) => t.id === id);
      if (id === taskId) out.push({ field: 'dependencies', message: 'A task cannot depend on itself.' });
      else if (!prerequisite) out.push({ field: 'dependencies', message: 'A selected prerequisite is no longer available.' });
      else if (reachesTask(id, taskId, new SvelteSet<string>())) out.push({ field: 'dependencies', message: 'These prerequisites create a dependency cycle.' });
    }
    if (value.missedPolicy === 'skip' && (!Number.isFinite(grace) || grace < 0 || grace > 43200)) out.push({ field: 'grace', message: 'Enter a wait from 0 to 43,200 minutes.' });
    return out;
  });
  function reachesTask(id: string, target: string, seen: Set<string>): boolean {
    if (id === target) return true;
    if (!id || seen.has(id)) return false;
    seen.add(id);
    const task = tasks.find((t) => t.id === id);
    return (task?.orchestration?.dependencies ?? []).some((dep) => reachesTask(dep, target, seen));
  }
  const issue = (field: string) => draftIssues.find((i) => i.field === field)?.message ?? '';
  async function focusIssue(field: string) {
    await tick();
    const id = field === 'dependencies' ? 'schedule-dependencies' : field === 'grace' ? 'schedule-grace' : `schedule-${field}`;
    document.getElementById(id)?.focus({ preventScroll: false });
  }
  $effect(() => {
    void taskId;
    const v = untrack(() => value);
    zone = v.timezone || "UTC";
    scheduled = wallTime(v.scheduledAt, zone);
    notBefore = wallTime(v.notBefore, zone);
    deadline = wallTime(v.deadline, zone);
    paths = v.expectedPaths.join("\n");
    order = v.executionOrder;
    grace = (v.graceSeconds ?? 300) / 60;
  });
  function dependency(id: string, checked: boolean) {
    error = '';
    value = {
      ...value,
      dependencies: checked
        ? [...value.dependencies, id]
        : value.dependencies.filter((x) => x !== id),
    };
  }
  async function save(rearm = false) {
    error = '';
    if (draftIssues.length) {
      error = 'Fix the highlighted fields before saving.';
      await focusIssue(draftIssues[0].field);
      return;
    }
    try {
      const o: Orchestration = {
        ...value,
        rearm,
        timezone: zone,
        scheduledAt: instantFromWall(scheduled, zone, occurrence),
        notBefore: instantFromWall(notBefore, zone, occurrence),
        deadline: instantFromWall(deadline, zone, occurrence),
        executionOrder: order,
        expectedPaths: paths
          .split("\n")
          .map((p) => p.trim())
          .filter(Boolean),
        graceSeconds: Math.round(grace * 60),
      };
      await onsave(o);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }
</script>

<form
  class="schedule-fields"
  onsubmit={(e) => {
    e.preventDefault();
    void save();
  }}
>
  <label class="check"
    ><input type="checkbox" bind:checked={value.enabled} /><span
      >Run automatically from Werkbord</span
    ></label
  >
  <p class="muted small">Starts the agent once, when the schedule, dependencies and capacity allow.</p>
  <div class="fields">
    <label
      >Timezone<input
        class="input"
        class:invalid={!!issue('timezone')}
        id="schedule-timezone"
        bind:value={zone}
        oninput={() => (error = '')}
        placeholder="Europe/Madrid"
        required
        aria-invalid={!!issue('timezone')}
        aria-describedby={issue('timezone') ? 'schedule-timezone-error' : undefined}
      />{#if issue('timezone')}<small class="field-error" id="schedule-timezone-error">{issue('timezone')}</small>{/if}</label
    >
    <label
      >Scheduled time<input
        class="input"
        type="datetime-local"
        class:invalid={!!issue('scheduled')}
        id="schedule-scheduled"
        bind:value={scheduled}
        oninput={() => (error = '')}
        aria-invalid={!!issue('scheduled')}
        aria-describedby={issue('scheduled') ? 'schedule-scheduled-error' : undefined}
      />{#if issue('scheduled')}<small class="field-error" id="schedule-scheduled-error">{issue('scheduled')}</small>{/if}</label
    >
    <label
      >Not before<input
        class="input"
        type="datetime-local"
        class:invalid={!!issue('notBefore')}
        id="schedule-notBefore"
        bind:value={notBefore}
        oninput={() => (error = '')}
        aria-invalid={!!issue('notBefore')}
        aria-describedby={issue('notBefore') ? 'schedule-notBefore-error' : undefined}
      />{#if issue('notBefore')}<small class="field-error" id="schedule-notBefore-error">{issue('notBefore')}</small>{/if}</label
    >
    <label
      >Deadline<input
        class="input"
        type="datetime-local"
        class:invalid={!!issue('deadline')}
        id="schedule-deadline"
        bind:value={deadline}
        oninput={() => (error = '')}
        aria-invalid={!!issue('deadline')}
        aria-describedby={issue('deadline') ? 'schedule-deadline-error' : undefined}
      />{#if issue('deadline')}<small class="field-error" id="schedule-deadline-error">{issue('deadline')}</small>{/if}</label
    >
    <label
      >Run order<input
        class="input"
        type="number"
        min="0"
        max="1000000"
        bind:value={order}
        placeholder="Unordered"
      /></label
    >
    <label>When this time occurs twice
      <select class="select" bind:value={occurrence}>
        <option value="earlier">Earlier occurrence</option>
        <option value="later">Later occurrence</option>
      </select>
    </label>
    <label
      >If the scheduled time is missed<select
        class="select"
        bind:value={value.missedPolicy}
        ><option value="run_late"
          >Run when ready, including after restart</option
        ><option value="skip">Mark missed after grace period</option></select
      ></label
    >
    {#if value.missedPolicy === "skip"}
      <label
        >Wait before marking missed (minutes)<input
          class="input"
          type="number"
          min="0"
          max="43200"
          bind:value={grace}
          oninput={() => (error = '')}
          required
          class:invalid={!!issue('grace')}
          id="schedule-grace"
          aria-invalid={!!issue('grace')}
          aria-describedby={issue('grace') ? 'schedule-grace-error' : undefined}
        /></label>
      {#if issue('grace')}<small class="field-error" id="schedule-grace-error">{issue('grace')}</small>{/if}
    {/if}
  </div>
  <p class="muted small">Run order affects priority; dependencies make a task wait.</p>
  <fieldset id="schedule-dependencies" tabindex="-1" class:invalid={!!issue('dependencies')} aria-describedby={issue('dependencies') ? 'schedule-dependencies-error' : undefined}>
    <legend>Wait for tasks</legend>
    {#each tasks.filter((t) => t.id !== taskId) as t (t.id)}<label class="check"
        ><input
          type="checkbox"
          aria-invalid={!!issue('dependencies')}
          checked={value.dependencies.includes(t.id)}
          onchange={(e) => dependency(t.id, e.currentTarget.checked)}
        /><span>{t.title}</span></label
      >{/each}
    {#if issue('dependencies')}<p class="field-error" id="schedule-dependencies-error">{issue('dependencies')}</p>{/if}
    {#if tasks.length <= 1}<p class="muted">Add another task to define a prerequisite.</p>{/if}
  </fieldset>
  <label
    >Expected files or directories <span class="muted"
      >(optional, one per line)</span
    ><textarea
      class="input"
      rows="2"
      bind:value={paths}
      placeholder="web/src/&#10;internal/api/"></textarea></label
  >
  <label
    >Expected target commit <span class="muted">(optional)</span><input
      class="input"
      bind:value={value.targetCommit}
      placeholder="Full commit ID from Git"
    /></label
  >
  <p class="muted small">Unknown or overlapping file scope runs sequentially.</p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <button class="btn primary" disabled={busy}
    >{busy ? "Saving…" : "Save schedule and dependencies"}</button
  >
{#if canScheduleAnother(original, value.enabled)}
  <button type="button" class="btn ok" disabled={busy} onclick={async () => { if (await confirmAction({ title: "Schedule another run?", message: "This authorises another attempt when the requirements are met; it does not start an agent now.", confirmLabel: "Schedule another run" })) void save(true); }}>Schedule another run</button>
{/if}
</form>

<style>
  .schedule-fields {
    display: grid;
    gap: 14px;
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px;
  }
  label {
    display: grid;
    gap: 6px;
    font-size: 0.9rem;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 44px;
  }
  .check input {
    width: 18px;
    height: 18px;
    flex-shrink: 0;
  }
  fieldset {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 12px;
    max-height: 260px;
    overflow: auto;
  }
  fieldset.invalid { border-color: var(--danger); box-shadow: 0 0 0 1px var(--danger); }
  legend {
    font-weight: 600;
  }
  .small {
    font-size: 0.82rem;
  }
  .btn {
    justify-self: start;
  }
  @media (max-width: 600px) {
    .fields {
      grid-template-columns: 1fr;
    }
    input {
      min-width: 0;
    }
  }
</style>
