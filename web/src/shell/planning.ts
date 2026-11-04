import { workspaceRequest } from './workspace-request';
import { isPlace } from './frames';
import type { PlanningItem } from '../lib/planningBridge';

export type { PlanningItem } from '../lib/planningBridge';
export async function planningCall(workspace: string, href: string, operation: 'read' | 'save' | 'move' | 'reschedule', payload: Record<string, unknown> = {}): Promise<PlanningItem> {
  if (!isPlace(href)) throw new Error('This task address is invalid.');
  return workspaceRequest<PlanningItem>(workspace, 'planning', { href, operation, payload }, operation === 'read');
}
