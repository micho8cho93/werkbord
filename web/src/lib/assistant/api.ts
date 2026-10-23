// The assistant's HTTP API (internal/api/assistant.go). The token goes in the Authorization header and nowhere else: the
// stream is read with fetch, not EventSource, because EventSource cannot send one and the server refuses a token in the URL.

import { ApiError, getToken, request, requestNoContent } from '../api';
import type { AttachmentInfo, ProvidersResponse, SessionView } from './types';

export interface AssistantResolveResponse {
  operation: string;
  data?: { ticket?: { id: string; projectId: string; title: string; state: string }; message?: string };
}

const base = '/api/assistant';
const enc = encodeURIComponent;

export const assistantApi = {
  providers: () => request<ProvidersResponse>('GET', `${base}/providers`),
  workspaces: (id: string, workspaces: { id: string; name: string }[]) => request<unknown>('POST', `${base}/sessions/${enc(id)}/workspaces`, { workspaces }),
  workspacePending: (id: string, requestId: string) => request<{ pending: boolean; request?: { id: string; proposalId?: string; workspaceId: string; method: string; args: unknown } }>('GET', `${base}/sessions/${enc(id)}/workspace-requests/${enc(requestId)}`),
  workspaceAnswer: (id: string, requestId: string, data: unknown, error = '', code = '') => request<unknown>('POST', `${base}/sessions/${enc(id)}/workspace-requests/${enc(requestId)}`, { data, error, code }),
  create: (b: { provider: string; model?: string; reasoning?: string }) => request<SessionView>('POST', `${base}/sessions`, b),
  get: (id: string) => request<SessionView>('GET', `${base}/sessions/${enc(id)}`),
  /** Changes provider, model or reasoning level between messages. Another provider starts a fresh conversation. */
  configure: (id: string, b: { provider?: string; model?: string; reasoning?: string }) => request<SessionView>('PATCH', `${base}/sessions/${enc(id)}`, b),
  remove: (id: string) => requestNoContent('DELETE', `${base}/sessions/${enc(id)}`),
  send: (id: string, text: string, attachments: string[], openuiPrompt?: string, personalEditing?: {workspaceId: string; revision: number}) =>
    request<{ turnId: string }>('POST', `${base}/sessions/${enc(id)}/messages`, { text, attachments, ...(openuiPrompt ? { openuiPrompt } : {}), ...(personalEditing ? { personalEditing } : {}) }),
  cancel: (id: string) => request<{ cancelled: boolean }>('POST', `${base}/sessions/${enc(id)}/cancel`, {}),
  resolve: (id: string, actionId: string, decision: 'approve' | 'decline') =>
    request<AssistantResolveResponse>('POST', `${base}/sessions/${enc(id)}/actions/${enc(actionId)}`, { decision }),
  detach: (id: string, attachmentId: string) => requestNoContent('DELETE', `${base}/sessions/${enc(id)}/attachments/${enc(attachmentId)}`),

  /**
   * Holds a text file for the next message. `authorized` is the person's yes to showing this file to the conversation's
   * provider; the server refuses without it. The file is the body, so nothing is written into a form or a URL.
   */
  async attach(id: string, file: Blob, name: string, authorized: boolean): Promise<AttachmentInfo> {
    const headers: Record<string, string> = { Accept: 'application/json', 'Content-Type': 'application/octet-stream', 'X-Attachment-Name': encodeURIComponent(name) };
    if (authorized) headers['X-Attachment-Authorized'] = 'true';
    const token = getToken();
    if (token) headers.Authorization = `Bearer ${token}`;
    let res: Response;
    try {
      res = await fetch(`${base}/sessions/${enc(id)}/attachments`, { method: 'POST', headers, body: file });
    } catch {
      throw new ApiError(0, 'offline', 'Cannot reach Werkbord.');
    }
    if (!res.ok) {
      let code = 'http_error';
      let message = 'Werkbord could not attach this file. Check the conversation before trying again.';
      try {
        const data = (await res.json()) as { error?: { code: string; message: string } };
        if (data.error) ({ code, message } = data.error);
      } catch {
        // Not JSON.
      }
      throw new ApiError(res.status, code, message);
    }
    return (await res.json()) as AttachmentInfo;
  },
};
