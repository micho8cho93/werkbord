import type { Orchestration, SchedulingDecision, Task, Run } from "./types";

export const schedulingLabels: Record<SchedulingDecision["state"], string> = {
  runnable: "Ready",
  waiting_runner: "Waiting for runner",
  waiting_retry: "Waiting to retry",
  queued: "Queued",
  waiting_dependency: "Waiting for a prerequisite",
  waiting_schedule: "Scheduled",
  waiting_capacity: "Waiting for capacity",
  potentially_conflicting: "May change the same files",
  blocked: "Blocked",
};
export function schedulingReason(decision: SchedulingDecision): string {
  if (decision.reason.startsWith('This one-shot schedule already dispatched run ')) return 'The scheduled run was dispatched.';
  if (decision.reason === 'Schedule was marked missed; reschedule to rearm') return 'The scheduled time was missed. Change the schedule and choose “Schedule another run”.';
  return decision.reason;
}
/** What a refused start means for the person, by the scheduler's reason. The controller's own reason names runs and
 *  runners by ID; that stays on the task's scheduling details. */
const refusals: Partial<Record<SchedulingDecision["state"], string>> = {
  waiting_runner: "The scheduled task is preserved. It waits for an eligible runner to reconnect.",
  waiting_retry: "A temporary provider limit delayed this start. Werkbord will retry within the configured limit.",
  queued: "Earlier tasks are ahead of it. It starts when their turn has passed.",
  waiting_dependency: "It waits for the tasks it depends on to finish.",
  waiting_schedule: "It is scheduled for later and starts at its scheduled time.",
  waiting_capacity: "No run slot is free for this project yet. It can start when a current run finishes, or after you stop one.",
  potentially_conflicting: "A running task may change the same files. Start this one when that run finishes.",
};

/** The words for a start the scheduler refused (the controller says "conflict: <state>: <reason>"), or the message as it came. */
export function startRefusal(message: string): string {
  const m = /^conflict: ([a-z_]+): ([\s\S]*)$/.exec(message);
  if (!m || !(m[1] in schedulingLabels)) return message;
  return refusals[m[1] as SchedulingDecision["state"]] ?? m[2];
}
export function emptyOrchestration(): Orchestration {
  return {
    enabled: false,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    dependencies: [],
    expectedPaths: [],
    missedPolicy: "run_late",
    graceSeconds: 300,
  };
}
/** A used or blocked one-shot needs explicit authorisation for another attempt. */
export function canScheduleAnother(original: Orchestration | undefined, enabled: boolean): boolean {
  return !!original?.enabled && enabled && !!(original.runId || original.missed || original.error || original.waitingRunner && original.reconnectPolicy === "manual");
}
export function wallTime(
  instant: string | undefined,
  timezone: string,
): string {
  if (!instant) return "";
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(new Date(instant));
  const p = Object.fromEntries(parts.map((x) => [x.type, x.value]));
  return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`;
}
/** Resolve an IANA wall time explicitly, including missing/repeated DST hours. */
export function instantFromWall(
  wall: string,
  timezone: string,
  occurrence: "earlier" | "later" = "earlier",
): string | undefined {
  if (!wall) return undefined;
  const naive = Date.parse(`${wall}:00Z`);
  if (!Number.isFinite(naive)) throw new Error("Enter a valid date and time.");
  const candidates = new Set<number>();
  // Derive the zone's offsets on either side of any nearby DST transition.
  for (let hours = -36; hours <= 36; hours += 6) {
    const sample = naive + hours * 3600000;
    const represented = Date.parse(
      `${wallTime(new Date(sample).toISOString(), timezone)}:00Z`,
    );
    const candidate = naive - (represented - sample);
    if (wallTime(new Date(candidate).toISOString(), timezone) === wall)
      candidates.add(candidate);
  }
  const sorted = [...candidates].sort((a, b) => a - b);
  if (!sorted.length)
    throw new Error(
      `This time does not exist in ${timezone} because the clocks change. Choose another time.`,
    );
  return new Date(
    occurrence === "earlier" ? sorted[0] : sorted[sorted.length - 1],
  ).toISOString();
}
export function editableOrchestration(o: Orchestration): Orchestration {
  const {
    enabled,
    scheduledAt,
    notBefore,
    deadline,
    timezone,
    executionOrder,
    dependencies,
    expectedPaths,
    targetCommit,
    missedPolicy,
    graceSeconds,
    reconnectPolicy,
    retryLimit,
    usageLimitBehavior,
  } = o;
  return {
    enabled,
    scheduledAt,
    notBefore,
    deadline,
    timezone,
    executionOrder,
    dependencies,
    expectedPaths,
    targetCommit,
    missedPolicy,
    graceSeconds,
    reconnectPolicy,
    retryLimit,
    usageLimitBehavior,
  };
}

/** Match the controller dependency gate, including a newer failed retry. */
export function dependencySatisfied(task: Task | undefined, run?: Run): boolean {
  if (!task) return false;
  if (run && ['starting', 'running', 'waiting_for_user', 'blocked', 'failed', 'stopped'].includes(run.state)) return false;
  return task.state === 'done' || run?.state === 'completed';
}
