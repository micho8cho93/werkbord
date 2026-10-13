import type { Plan, Task } from './types';

export interface PlanIssue {
  code: string;
  itemId: string;
  otherId?: string;
  field: string;
  message: string;
}

interface Item {
  id: string;
  title: string;
  plan: Plan;
  dependencies: string[];
  done: boolean;
  archived: boolean;
}

interface Finding extends PlanIssue {
  severity: 'error' | 'warning' | 'info';
  impact: number;
}

function day(s: string): number | undefined {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return;
  const time = Date.parse(`${s}T00:00:00Z`);
  if (!Number.isFinite(time) || new Date(time).toISOString().slice(0, 10) !== s) return;
  const year = Number(s.slice(0, 4));
  return year >= 1970 && year <= 2200 ? time / 86_400_000 : undefined;
}

function item(t: Task): Item {
  return {
    id: t.id,
    title: t.title,
    plan: { ...(t.plan ?? {}) },
    dependencies: [...(t.orchestration?.dependencies ?? [])],
    done: t.state === 'done',
    archived: !!t.archivedAt,
  };
}

function cycles(items: Item[]): string[][] {
  const ids = items.map((x) => x.id).sort();
  const byId = new Map(items.map((x) => [x.id, x]));
  const edges = new Map(ids.map((id) => [id, (byId.get(id)?.dependencies ?? []).filter((d) => byId.has(d))]));
  const reverse = new Map(ids.map((id) => [id, [] as string[]]));
  for (const [id, deps] of edges) for (const dep of deps) reverse.get(dep)!.push(id);
  const seen = new Set<string>();
  const order: string[] = [];
  for (const root of ids) {
    if (seen.has(root)) continue;
    const work: { id: string; next: number }[] = [{ id: root, next: 0 }];
    seen.add(root);
    while (work.length) {
      const top = work[work.length - 1];
      const children = edges.get(top.id)!;
      if (top.next < children.length) {
        const child = children[top.next++];
        if (!seen.has(child)) { seen.add(child); work.push({ id: child, next: 0 }); }
      } else { order.push(top.id); work.pop(); }
    }
  }
  const assigned = new Set<string>();
  const result: string[][] = [];
  for (const root of order.reverse()) {
    if (assigned.has(root)) continue;
    const component: string[] = [];
    const work = [root];
    assigned.add(root);
    while (work.length) {
      const id = work.pop()!;
      component.push(id);
      for (const child of reverse.get(id)!) if (!assigned.has(child)) { assigned.add(child); work.push(child); }
    }
    if (component.length > 1) result.push(component.sort());
  }
  return result.sort((a, b) => a[0].localeCompare(b[0]));
}

function analyze(items: Item[]): Finding[] {
  const byId = new Map(items.map((x) => [x.id, x]));
  const out: Finding[] = [];
  const add = (code: string, severity: Finding['severity'], current: Item, field: string, message: string, otherId?: string, impact = 1) => {
    if (!current.archived) out.push({ code, severity, itemId: current.id, otherId, field, message, impact });
  };
  for (const current of items) {
    if (current.archived) continue;
    const { start = '', end = '', milestone = false } = current.plan;
    const firstRaw = start || (milestone ? end : '');
    const lastRaw = milestone ? firstRaw : end || firstRaw;
    const first = firstRaw ? day(firstRaw) : undefined;
    const last = lastRaw ? day(lastRaw) : undefined;
    if ((start && first === undefined) || (end && day(end) === undefined)) {
      add('invalid_date', 'error', current, start && first === undefined ? 'plan.start' : 'plan.end', `“${current.title}” has an invalid planned date.`);
    } else if (first !== undefined && last !== undefined && last < first) {
      add('invalid_range', 'error', current, 'plan.end', `“${current.title}” ends before it starts.`);
    } else if (milestone && firstRaw === '') {
      add('milestone_without_date', 'warning', current, 'plan.start', `“${current.title}” needs a date to appear on the timeline.`);
    }
    const seen = new Set<string>();
    for (const id of current.dependencies) {
      if (seen.has(id)) {
        add('duplicate_dependency', 'warning', current, 'dependencies', `“${current.title}” lists a dependency more than once.`, id);
        continue;
      }
      seen.add(id);
      if (id === current.id) {
        add('self_dependency', 'error', current, 'dependencies', `“${current.title}” cannot depend on itself.`, id);
        continue;
      }
      const prerequisite = byId.get(id);
      if (!prerequisite) {
        add('missing_dependency', 'error', current, 'dependencies', `“${current.title}” depends on a task that is no longer here.`, id);
        continue;
      }
      if (prerequisite.archived && !prerequisite.done) {
        add('archived_dependency', 'warning', current, 'dependencies', `“${current.title}” depends on an unfinished archived task.`, id);
      }
      if (current.done && !prerequisite.done && !prerequisite.archived) {
        add('done_before_dependency_done', 'warning', current, 'dependencies', `“${current.title}” is done before its prerequisite.`, id);
      }
      const depFirstRaw = prerequisite.plan.start || prerequisite.plan.end || '';
      const depLastRaw = prerequisite.plan.milestone ? depFirstRaw : prerequisite.plan.end || depFirstRaw;
      const depLast = depLastRaw ? day(depLastRaw) : undefined;
      if (first !== undefined && depLast === undefined && !prerequisite.done) {
        add('unscheduled_dependency', 'info', current, 'dependencies', `The dates for “${current.title}” cannot be compared with its undated prerequisite.`, id);
      } else if (first !== undefined && depLast !== undefined && !prerequisite.done && first < depLast) {
        add('starts_before_dependency_ends', 'warning', current, 'plan.start', `“${current.title}” starts before its prerequisite ends.`, id, depLast - first);
      }
    }
  }
  for (const cycle of cycles(items)) {
    const current = byId.get(cycle[0])!;
    add('dependency_cycle', 'error', current, 'dependencies', 'These tasks depend on each other in a circle.', undefined, 1);
  }
  return out;
}

function key(f: Finding): string {
  return `${f.code}\0${f.itemId}\0${f.otherId ?? ''}`;
}

/** Finds blocking planning issues introduced or made worse by this draft, including its direct dependants. */
export function validatePlanDraft(tasks: readonly Task[], updated: Task): PlanIssue[] {
  const before = tasks.map(item);
  const after = before.filter((x) => x.id !== updated.id);
  after.push(item(updated));
  const old = new Map<string, number>();
  for (const finding of analyze(before)) old.set(key(finding), Math.max(old.get(key(finding)) ?? 0, finding.impact));
  const issues = new Map<string, PlanIssue>();
  for (const finding of analyze(after)) {
    if (finding.severity === 'info') continue;
    if ((old.get(key(finding)) ?? 0) >= finding.impact) continue;
    const field = finding.code === 'starts_before_dependency_ends'
      ? finding.itemId === updated.id ? 'plan.start' : finding.otherId === updated.id ? updated.plan?.milestone === true ? 'plan.start' : 'plan.end' : finding.field
      : finding.field;
    issues.set(`${key(finding)}\0${field}`, { code: finding.code, itemId: finding.itemId, otherId: finding.otherId, field, message: finding.message });
  }
  return [...issues.values()];
}

/** Date-only checks for a new task, before any request is sent. */
export function validatePlanRange(plan: Plan, title: string): PlanIssue[] {
  const current: Item = { id: '', title, plan, dependencies: [], done: false, archived: false };
  return analyze([current]).filter((f) => f.severity !== 'info').map(({ code, itemId, otherId, field, message }) => ({ code, itemId, otherId, field, message }));
}
