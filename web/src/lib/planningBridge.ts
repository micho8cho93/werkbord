import { api } from './api';
import { parse } from './location';
import { spanOf } from './planning-dates';
import { editableOrchestration, emptyOrchestration, instantFromWall, wallTime } from './scheduling';

/** The parent shell asks this authenticated page for planning edits; the controller remains the authority. */
export function followPlanning(): () => void {
  if (!window.parent || window.parent === window) return () => {};
  const listener = async (event: MessageEvent) => {
    const m = event.data;
    if (event.source !== window.parent || !m || m.type !== 'werkbord.planning.request' || !/^[a-z0-9_-]{1,64}$/.test(m.id ?? '') ||
      !['read', 'save', 'move', 'reschedule'].includes(m.operation) || typeof m.href !== 'string' || !/^#\/p\/[A-Za-z0-9_-]+\/task\/[A-Za-z0-9_-]+$/.test(m.href) || JSON.stringify(m).length > 8192) return;
    try {
      const loc = parse(m.href);
      const task = (await api.listTasks(loc.projectId)).find(t => t.id === loc.taskId);
      if (!task) throw new Error('This task is no longer available. Refresh the calendar.');
      const payload = m.payload ?? {};
      let current = task;
      if (m.operation !== 'read') {
        if (payload.version !== task.version) throw new Error('This task changed elsewhere. Reopen it before saving.');
        if (m.operation === 'reschedule') {
          const o = editableOrchestration(task.orchestration ?? emptyOrchestration());
          if (task.orchestration?.runId) throw new Error('This scheduled run has already started. Open the task to plan another run.');
          const zone = o.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone;
          const local = Intl.DateTimeFormat().resolvedOptions().timeZone;
          const at = payload.at || instantFromWall(payload.start + 'T' + wallTime(o.scheduledAt || new Date().toISOString(), local).slice(11, 16), local);
          current = await api.editTask(task, { orchestration: { ...o, enabled: true, scheduledAt: at, timezone: zone } });
        } else {
          const plan = { ...task.plan, start: payload.start, end: payload.end, milestone: payload.milestone ?? task.plan?.milestone ?? false };
          current = await api.editTask(task, m.operation === 'save' ? { title: payload.title, description: payload.description, plan } : { plan });
        }
      }
      const span = spanOf(current);
      event.source.postMessage({ type: 'werkbord.planning.result', id: m.id, ok: true, item: { title: current.title, description: current.description, version: current.version,
        start: span?.start || '', end: span?.end || '', milestone: !!span?.milestone, scheduledAt: current.orchestration?.scheduledAt || '', editable: !current.archivedAt,
        canReschedule: !current.archivedAt && !current.orchestration?.runId } }, { targetOrigin: event.origin });
    } catch (error) {
      event.source.postMessage({ type: 'werkbord.planning.result', id: m.id, ok: false, error: error instanceof Error ? error.message : String(error) }, { targetOrigin: event.origin });
    }
  };
  window.addEventListener('message', listener);
  return () => window.removeEventListener('message', listener);
}
