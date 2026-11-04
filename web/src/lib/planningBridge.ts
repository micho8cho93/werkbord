import { api } from './api';
import { parse } from './location';
import { spanOf } from './planning-dates';
import { editableOrchestration, emptyOrchestration, instantFromWall, wallTime } from './scheduling';

/** What the calendar reads of a task, and may change: its words, its planned days and time, and when an agent is scheduled to start. */
export interface PlanningItem { title: string; description: string; version: number; start: string; end: string; milestone: boolean; time?: string; scheduledAt: string; editable: boolean; canReschedule?: boolean; scheduleVersion?: number }
export type PlanningOperation = 'read' | 'save' | 'move' | 'reschedule';
/** What a change names: the version it was made against, and the new words, dates, time or start. */
interface PlanningPayload { version?: number; at?: string; start?: string; end?: string; milestone?: boolean; time?: string; title?: string; description?: string }

/** Where a planning edit may be aimed: a task of a project, by its address. */
export const PLANNING_HREF = /^#\/p\/[A-Za-z0-9_-]+\/task\/[A-Za-z0-9_-]+$/;

/**
 * Reads a task for the calendar, or changes its dates or its words, with the controller as the authority: a stale `version` is refused.
 * The web app's own Calendar calls this directly; the window's Calendar reaches it through followPlanning, in the frame of the workspace.
 */
export async function planningOperation(href: string, operation: PlanningOperation, body: Record<string, unknown> = {}): Promise<PlanningItem> {
  const payload = body as PlanningPayload;
  if (!PLANNING_HREF.test(href)) throw new Error('This task address is invalid.');
  const loc = parse(href);
  const task = (await api.listTasks(loc.projectId)).find(t => t.id === loc.taskId);
  if (!task) throw new Error('This task is no longer available. Refresh the calendar.');
  let current = task;
  if (operation !== 'read') {
    if (payload.version !== task.version) throw new Error('This task changed elsewhere. Reopen it before saving.');
    if (operation === 'reschedule') {
      const o = editableOrchestration(task.orchestration ?? emptyOrchestration());
      if (task.orchestration?.runId) throw new Error('This scheduled run has already started. Open the task to plan another run.');
      const zone = o.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone;
      const local = Intl.DateTimeFormat().resolvedOptions().timeZone;
      const at = payload.at || instantFromWall(payload.start + 'T' + wallTime(o.scheduledAt || new Date().toISOString(), local).slice(11, 16), local);
      current = await api.editTask(task, { orchestration: { ...o, enabled: true, scheduledAt: at, timezone: zone } });
    } else {
      const milestone = payload.milestone ?? task.plan?.milestone ?? false;
      // A planned time of day belongs to a single day: it is kept (or set, when a save names one), and a range of days clears it.
      const single = milestone || !payload.end || payload.end === payload.start;
      const named = typeof payload.time === 'string' ? payload.time : task.plan?.time;
      const plan = { ...task.plan, start: payload.start, end: payload.end, milestone, time: single && named ? named : undefined };
      current = await api.editTask(task, operation === 'save' ? { title: payload.title, description: payload.description, plan } : { plan });
    }
  }
  const span = spanOf(current);
  return { title: current.title, description: current.description, version: current.version,
    start: span?.start || '', end: span?.end || '', time: span?.time || '', milestone: !!span?.milestone, scheduledAt: current.orchestration?.scheduledAt || '', editable: !current.archivedAt,
    canReschedule: !current.archivedAt && !current.orchestration?.runId };
}

/** The parent shell asks this authenticated page for planning edits; the controller remains the authority. */
export function followPlanning(): () => void {
  if (!window.parent || window.parent === window) return () => {};
  const listener = async (event: MessageEvent) => {
    const m = event.data;
    if (event.source !== window.parent || !m || m.type !== 'werkbord.planning.request' || !/^[a-z0-9_-]{1,64}$/.test(m.id ?? '') ||
      !['read', 'save', 'move', 'reschedule'].includes(m.operation) || typeof m.href !== 'string' || !PLANNING_HREF.test(m.href) || JSON.stringify(m).length > 8192) return;
    try {
      const item = await planningOperation(m.href, m.operation, m.payload ?? {});
      event.source.postMessage({ type: 'werkbord.planning.result', id: m.id, ok: true, item }, { targetOrigin: event.origin });
    } catch (error) {
      event.source.postMessage({ type: 'werkbord.planning.result', id: m.id, ok: false, error: error instanceof Error ? error.message : String(error) }, { targetOrigin: event.origin });
    }
  };
  window.addEventListener('message', listener);
  return () => window.removeEventListener('message', listener);
}
