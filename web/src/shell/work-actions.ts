import { model } from './model.svelte';
import { workspaceRequest } from './workspace-request';
import type { WorkAction } from '../lib/work/actions';
export async function workAction(workspace: string, action: WorkAction): Promise<void> {
  if (workspace !== 'personal' || model.remotePersonal) throw new Error('Open Personal on its own computer to perform this action.');
  await workspaceRequest<void>(workspace, 'work', { action });
  await model.refreshOverview();
}
