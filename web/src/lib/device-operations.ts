import { request } from './api';
import type { GitWorkingTree } from './types';
export interface GitSnapshot {
  runnerId: string; projectId: string; runId?: string; status: GitWorkingTree;
  previewId?: string; action?: 'commit' | 'push'; files?: string[]; message?: string; remote?: string; expiresAt?: string;
}
export interface DeviceRequest {
  requestId: string; runnerId: string; projectId: string; runId?: string; kind: string;
  action?: 'commit' | 'push'; previewId?: string; confirmed?: boolean; files?: string[]; message?: string;
}
export interface DeviceRecord {
  operation: { id: string; request: DeviceRequest; expiresAt: string };
  result: { id: string; state: 'pending' | 'done' | 'failed' | 'uncertain'; error?: string; git?: GitSnapshot; outcome?: string; message?: string };
}
export const deviceOperations = {
  submit: (body: DeviceRequest) => request<DeviceRecord>('POST', '/api/device/operations', body),
  get: (id: string) => request<DeviceRecord>('GET', `/api/device/operations/${encodeURIComponent(id)}`),
};
