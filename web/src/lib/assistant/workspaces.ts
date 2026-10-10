import { callDesktop } from '../desktop';
import { surface } from '../embed';
import { assistantApi } from './api';
import type { AssistantEvent } from './types';

// Only the assistant's dedicated desktop surface can ask its parent for connected board operations. Browser/PWA
// sessions continue to use their own board. No workspace credential or arbitrary request reaches this page.
export async function connectWorkspaces(sessionId: string): Promise<void> {
  if (!surface) return;
  const workspaces = await callDesktop<{ id: string; name: string }[]>('AssistantWorkspaces');
  await assistantApi.workspaces(sessionId, workspaces);
}

const answering = new Map<string, Promise<void>>();
export function answerWorkspace(e: AssistantEvent): void {
  const w = e.workspace;
  if (!surface || !w || answering.has(w.id)) return;
  const answer = async () => {
    // An SSE replay may include a request that has already been answered. Never carry it out again.
    if (!(await assistantApi.workspacePending(e.sessionId, w.id)).pending) return;
    let data: unknown = null;
    let problem = '';
    try {
      data = await callDesktop('AssistantWorkspaceCall', [w.id, w.workspaceId, w.method, w.args], 0);
    } catch (err) {
      problem = err instanceof Error ? err.message : String(err);
    }
    await assistantApi.workspaceAnswer(e.sessionId, w.id, data, problem);
  };
  answering.set(w.id, answer().catch(() => {}).finally(() => answering.delete(w.id)));
}
