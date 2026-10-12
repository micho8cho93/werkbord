<script lang="ts">
  import { untrack } from "svelte";
  import type { Orchestration, Task } from "./types";
  import { instantFromWall, wallTime } from "./scheduling";
  let {
    value = $bindable(),
    tasks,
    taskId,
    onsave,
    busy = false,
  }: {
    value: Orchestration;
    tasks: Task[];
    taskId: string;
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
    value = {
      ...value,
      dependencies: checked
        ? [...value.dependencies, id]
        : value.dependencies.filter((x) => x !== id),
    };
  }
  async function save(rearm = false) {
    try {
      error = "";
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
  <p class="muted small">
    One attempt per schedule. Saving edits does not request another attempt. Allow one more run to rearm the schedule; the
    controller starts it only when its schedule, dependencies and capacity allow. Your browser can be closed.
  </p>
  <p class="muted small">This schedules when Werkbord starts agent work. To plan work dates or milestones, use Timeline.</p>
  <div class="fields">
    <label
      >Timezone<input
        class="input"
        bind:value={zone}
        placeholder="Europe/Madrid"
        required
      /></label
    >
    <label
      >Scheduled time<input
        class="input"
        type="datetime-local"
        bind:value={scheduled}
      /></label
    >
    <label
      >Not before<input
        class="input"
        type="datetime-local"
        bind:value={notBefore}
      /></label
    >
    <label
      >Deadline<input
        class="input"
        type="datetime-local"
        bind:value={deadline}
      /></label
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
    <p class="muted small">The selected timezone can repeat a local time when clocks move back. Choose which occurrence to use.</p>
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
          required
        /></label>
      <p class="muted small">The wait starts at the scheduled time, including while dependencies or runner capacity are unavailable. After it ends, this attempt is marked missed and needs to be allowed again.</p>
    {/if}
  </div>
  <p class="muted small">
    The scheduler uses Run order after scheduled time and before priority. Use dependencies when one task must finish before
    another starts; Run order alone never makes a task wait. This order applies within the project.
  </p>
  <fieldset>
    <legend>Wait for tasks</legend>
    {#each tasks.filter((t) => t.id !== taskId) as t (t.id)}<label class="check"
        ><input
          type="checkbox"
          checked={value.dependencies.includes(t.id)}
          onchange={(e) => dependency(t.id, e.currentTarget.checked)}
        /><span>{t.title}</span></label
      >{/each}
    {#if tasks.length <= 1}<p class="muted">
        Add another task to define dependencies.
      </p>{/if}
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
  <p class="muted small">
    Unknown or overlapping file scope runs sequentially. Expected scope is a
    hint, checked against observed Git changes.
  </p>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <button class="btn primary" disabled={busy}
    >{busy ? "Saving…" : "Save schedule and dependencies"}</button
  >
<button type="button" class="btn" disabled={busy} onclick={() => { if (confirm("Allow one more scheduled run? This rearms the schedule; it does not start an agent now.")) void save(true); }}>Allow one more run</button>
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
