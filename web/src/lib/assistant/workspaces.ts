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
    let problemCode = '';
    try {
      const result = await callDesktop<unknown>('AssistantWorkspaceCall', [e.sessionId, w.id, w.proposalId ?? '', w.workspaceId, w.method, w.args], 0);
      if (result && typeof result === 'object' && 'kind' in result && result.kind === 'werkbord_workspace_failure' && 'code' in result && typeof result.code === 'string' && 'message' in result && typeof result.message === 'string') {
        problem = result.message;
        problemCode = result.code;
      } else data = result;
    } catch (err) {
      problem = err instanceof Error ? err.message : String(err);
    }
    await assistantApi.workspaceAnswer(e.sessionId, w.id, data, problem, problemCode);
  };
  answering.set(w.id, answer().catch(() => {}).finally(() => answering.delete(w.id)));
}
