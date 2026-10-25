import { request, getToken } from '../api';

export interface TerminalSession {
  id: string; projectId: string; agentId: string; device: string; repository: string;
  state: 'running' | 'exited' | 'stopped' | 'interrupted'; createdAt: string;
}
export interface TerminalOutput { session: TerminalSession; data: string | null; next: number; reset: boolean }
export const terminalPath = (session: TerminalSession) => `/api/projects/${encodeURIComponent(session.projectId)}/terminals/${encodeURIComponent(session.id)}`;
export const terminalApi = {
  list: () => request<TerminalSession[]>('GET', '/api/terminals'),
  create: (projectId: string, agentId: string, device: string) => request<TerminalSession>('POST', `/api/projects/${encodeURIComponent(projectId)}/terminals`, { agentId, device, cols: 100, rows: 30 }),
  input: (session: TerminalSession, data: string) => request('POST', `${terminalPath(session)}/input`, { data }),
  resize: (session: TerminalSession, cols: number, rows: number) => request('POST', `${terminalPath(session)}/resize`, { cols, rows }),
  stop: (session: TerminalSession) => request('POST', `${terminalPath(session)}/stop`, {}),
  async output(session: TerminalSession, after: number, signal: AbortSignal): Promise<TerminalOutput> {
    const response = await fetch(`${terminalPath(session)}/output?after=${after}`, { headers: { Authorization: `Bearer ${getToken()}` }, signal, cache: 'no-store' });
    if (!response.ok) throw new Error(response.status === 404 ? 'This terminal session no longer exists on the execution device.' : `Terminal connection refused (${response.status}).`);
    return response.json();
  },
};
