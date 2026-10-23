import { getContext } from 'svelte';
import { ApiError } from '../api';
import { personalAPI, type PersonalAPI } from './api';
import { validateWorkspace, type Collection, type PersonalRecord, type Values, type Workspace } from './schema';

export const WORKSPACE_CONTEXT = Symbol('personal-workspace');
export function workspaceSession(): WorkspaceSession { return getContext(WORKSPACE_CONTEXT); }
export class WorkspaceSession {
  workspace = $state<Workspace | null>(null);
  collections = $state<Collection[]>([]);
  records = $state<Record<string, PersonalRecord[]>>({});
  readOnly = false;
  busy = $state(false);
  problem = $state('');
  notice = $state('');

  constructor(readonly api: PersonalAPI = personalAPI) {}

  async load(id: string) {
    const workspace = await this.api.workspace(id);
    const collections = await Promise.all((workspace.collectionIds ?? []).map(id => this.api.collection(id)));
    const validated = validateWorkspace(workspace, collections);
    const entries = await Promise.all(collections.map(async c => [c.id, await this.api.records(c.id)] as const));
    this.collections = collections;
    this.records = Object.fromEntries(entries);
    this.workspace = validated;
  }

  /** Fixed operations, scoped to a validated view and its explicitly permitted personal fields. */
  async mutate(viewID: string, operation: 'create' | 'update' | 'delete', id?: string, patch: Values = {}, clear: string[] = []): Promise<boolean> {
    if (this.busy || this.readOnly) return false;
    this.problem = ''; this.notice = '';
    const view = this.workspace?.definition.views.find(v => v.id === viewID);
    if (!view?.operations?.includes(operation)) { this.problem = 'This view does not allow that record operation.'; return false; }
    const editable = [...view.fields, ...(view.checkedField ? [view.checkedField] : [])];
    if ([...Object.keys(patch), ...clear].some(key => !editable.includes(key))) { this.problem = 'This field is not permitted in this view.'; return false; }
    const records = this.records[view.collectionId] ?? [];
    const record = records.find(r => r.id === id);
    if (operation !== 'create' && !record) { this.problem = 'This record is no longer available. Reload the workspace.'; return false; }
    this.busy = true;
    try {
      if (operation === 'delete') {
        await this.api.remove(view.collectionId, record!.id, record!.revision);
        this.records[view.collectionId] = records.filter(r => r.id !== id);
      } else {
        // Replacements preserve every stored field, including ones hidden by this layout.
        const values = { ...(record?.values ?? {}), ...patch };
        for (const key of clear) delete values[key];
        const saved = operation === 'create'
          ? await this.api.create(view.collectionId, values)
          : await this.api.update(view.collectionId, record!.id, record!.revision, values);
        this.records[view.collectionId] = operation === 'create' ? [...records, saved] : records.map(r => r.id === saved.id ? saved : r);
      }
      this.notice = operation === 'delete' ? 'Record deleted.' : 'Saved.';
      return true;
    } catch (err) {
      this.problem = err instanceof ApiError && err.status === 409
        ? 'This record changed elsewhere. Your edit is still here. Copy it, then reload before trying again.'
        : `${err instanceof Error ? err.message : 'The record could not be saved.'} Your edit is still here. Reload to check the saved state before retrying.`;
      return false;
    } finally { this.busy = false; }
  }
}
