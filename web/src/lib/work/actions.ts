import { api } from '../api';
import { parse } from '../location';
import { PLANNING_HREF } from '../planningBridge';

export interface WorkAction { operation: 'reply' | 'retry' | 'link' | 'clone'; href?: string; runId?: string; answer?: string; sourceRef?: string; path?: string }

/** Each user action is rechecked against current controller records, never trusted from a summary. */
export async function workOperation(action: WorkAction): Promise<void> {
  if (action.operation === 'link' || action.operation === 'clone') {
    const item = (await api.waiting()).items.find(p => p.sourceRef === action.sourceRef);
    if (!item) throw new Error('This incoming item is no longer waiting for a repository. Refresh Work.');
    if (action.operation === 'link') {
      if (!action.path?.trim()) throw new Error('Choose the repository folder first.');
      await api.linkWaiting(action.path, item.repository);
    } else {
      if (!item.github) throw new Error('This repository cannot be cloned from GitHub. Choose its local folder.');
      await api.githubAddRepo(item.github);
    }
    return;
  }
  if (!action.href || !PLANNING_HREF.test(action.href) || !/^[A-Za-z0-9_-]{1,80}$/.test(action.runId ?? '')) throw new Error('This run address is invalid.');
  const { projectId, taskId } = parse(action.href);
  const runs = await api.listTaskRuns(projectId, taskId);
  const run = runs.at(-1);
  if (!run || run.id !== action.runId) throw new Error('This task has a newer run. Refresh Work before acting.');
  if (action.operation === 'retry') {
    if (run.state !== 'failed') throw new Error('This run is no longer failed. Refresh Work.');
    await api.startRun(projectId, taskId);
  } else {
    if (run.state !== 'blocked' || !run.blocker?.options?.includes(action.answer ?? '')) throw new Error('This blocker changed. Open the task to check its current choices.');
    await api.sendInput(projectId, run.id, action.answer!);
  }
}

export function followWorkActions(): () => void {
  if (!window.parent || window.parent === window) return () => {};
  const listener = async (event: MessageEvent) => {
    const m = event.data;
    let size: number;
    try { size = JSON.stringify(m)?.length ?? 0; } catch { return; }
    if (event.source !== window.parent || m?.type !== 'werkbord.work.request' || !/^[a-z0-9_-]{1,64}$/.test(m.id ?? '') || !['reply', 'retry', 'link', 'clone'].includes(m.action?.operation) || size > 8192) return;
    try {
      await workOperation(m.action);
      event.source.postMessage({ type: 'werkbord.work.result', id: m.id, ok: true }, { targetOrigin: event.origin });
    } catch (error) {
      event.source.postMessage({ type: 'werkbord.work.result', id: m.id, ok: false, error: error instanceof Error ? error.message : String(error) }, { targetOrigin: event.origin });
    }
  };
  window.addEventListener('message', listener);
  return () => window.removeEventListener('message', listener);
}
