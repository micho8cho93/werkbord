import { model } from './model.svelte';
import { ask } from './native';
import { isPlace } from './frames';

export interface PlanningItem { title: string; description: string; version: number; start: string; end: string; milestone: boolean; scheduledAt: string; editable: boolean; canReschedule?: boolean; scheduleVersion?: number }
export async function planningCall(workspace: string, href: string, operation: 'read' | 'save' | 'move' | 'reschedule', payload: Record<string, unknown> = {}): Promise<PlanningItem> {
  if (!isPlace(href)) throw new Error('This task address is invalid.');
  if (!model.view?.items.some(i => i.id === workspace && i.state === 'ready')) throw new Error('Reconnect this workspace before editing.');
  if (!model.frames[workspace]) {
    const target = await ask(a => a.LoadWorkspace(workspace));
    model.frames[workspace] = { target, ready: false };
  }
  const deadline = Date.now() + 10000;
  while (!model.frames[workspace]?.ready && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 50));
  const frame = document.querySelector<HTMLIFrameElement>(`iframe[data-workspace="${CSS.escape(workspace)}"]`);
  const target = model.frames[workspace]?.target;
  if (!frame?.contentWindow || !target || !model.frames[workspace]?.ready) throw new Error('The workspace did not load. Reconnect and try again.');
  const source = frame.contentWindow, id = crypto.randomUUID();
  return new Promise((resolve, reject) => {
    const listener = (event: MessageEvent) => {
      if (event.source !== source || event.origin !== target.origin || event.data?.type !== 'werkbord.planning.result' || event.data.id !== id) return;
      clearTimeout(timer); window.removeEventListener('message', listener);
      if (event.data.ok) resolve(event.data.item); else reject(new Error(event.data.error || 'This change was refused.'));
    };
    const timer = setTimeout(() => { window.removeEventListener('message', listener); reject(new Error(operation === 'read' ? 'The workspace did not answer.' : 'The workspace did not confirm this change. Refresh and check the task before trying again.')); }, 15000);
    window.addEventListener('message', listener);
    source.postMessage({ type: 'werkbord.planning.request', id, href, operation, payload }, target.origin);
  });
}
