import { request, getToken } from '../api';

export interface TerminalSession {
  phones?: string[]; id: string; projectId: string; agentId: string; device: string; repository: string;
  state: 'running' | 'exited' | 'stopped' | 'interrupted'; createdAt: string;
}
export interface TerminalOutput { session: TerminalSession; data: string | null; next: number; reset: boolean; replay?: boolean }
export const terminalPath = (session: TerminalSession) => `/api/projects/${encodeURIComponent(session.projectId)}/terminals/${encodeURIComponent(session.id)}`;
export const terminalApi = {
  list: () => request<TerminalSession[]>('GET', '/api/terminals'),
  create: (projectId: string, agentId: string, device: string) => request<TerminalSession>('POST', `/api/projects/${encodeURIComponent(projectId)}/terminals`, { agentId, device, cols: 100, rows: 30 }),
  grant: (session: TerminalSession, phoneId: string, enabled: boolean) => request('PUT', `${terminalPath(session)}/phone`, { phoneId, enabled }),
  input: (session: TerminalSession, data: string, client: string, sequence: number, rendered: number) => request('POST', `${terminalPath(session)}/input`, { data, client, sequence, rendered }),
  resize: (session: TerminalSession, cols: number, rows: number) => request('POST', `${terminalPath(session)}/resize`, { cols, rows }),
  stop: (session: TerminalSession) => request('POST', `${terminalPath(session)}/stop`, {}),
  async output(session: TerminalSession, after: number, signal: AbortSignal, immediate = false): Promise<TerminalOutput> {
    const response = await fetch(`${terminalPath(session)}/output?after=${after}${immediate ? '&wait=0' : ''}`, { headers: { Authorization: `Bearer ${getToken()}` }, signal, cache: 'no-store' });
    if (!response.ok) throw new Error(response.status === 404 ? 'This terminal session no longer exists on the execution device.' : `Terminal connection refused (${response.status}).`);
    return response.json();
  },
};

// Keep ordered input and uncertainty attached to the process when its view is switched/closed.
interface InputClient { id: string; sequence: number; uncertain: boolean; pending: string[]; sending: boolean }
const inputClients = new Map<string, InputClient>();
export function terminalInputClient(session: TerminalSession): InputClient {
  let client = inputClients.get(session.id);
  if (!client) {
    client = { id: crypto.randomUUID(), sequence: 0, uncertain: false, pending: [], sending: false };
    inputClients.set(session.id, client);
  }
  return client;
}
