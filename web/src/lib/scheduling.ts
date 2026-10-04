import type { Orchestration, SchedulingDecision } from "./types";

export const schedulingLabels: Record<SchedulingDecision["state"], string> = {
  runnable: "Ready",
  queued: "Queued",
  waiting_dependency: "Waiting on Dependency",
  waiting_schedule: "Scheduled",
  waiting_capacity: "Waiting for Capacity",
  potentially_conflicting: "Potential Conflict",
  blocked: "Blocked",
};
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
  };
}
