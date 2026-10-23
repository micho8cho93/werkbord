// Only the shell calls this fixed local-controller bridge. No credential or HTTP
// address is handed to generated components or to Team's frontend.
import { ask } from './native';
import { ApiError } from '../lib/api';
import { createPersonalAPI } from '../lib/personal/api';
export const shellPersonalAPI = createPersonalAPI(async (method, path, body) => {
  const response = await ask(a => a.PersonalWorkspaceCall(method, path, body ?? null));
  if (response.status >= 300) {
    const error = response.body as { error?: { code: string; message: string } };
    throw new ApiError(response.status, error?.error?.code ?? 'unavailable', error?.error?.message ?? 'Cannot reach your saved workspaces.');
  }
  return response.body;
});
