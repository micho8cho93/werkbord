import { SvelteSet } from 'svelte/reactivity';
import { getContext } from 'svelte';
import { ApiError } from '../api';
import { personalAPI, type PersonalAPI } from './api';
import { validateWorkspace, viewWritableFields, type Collection, type PersonalRecord, type ShortcutTarget, type Values, type Workspace } from './schema';

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
  /** The record the last create or update saved: how a view learns the ID of the record it has just made. */
  lastSaved: PersonalRecord | null = null;
  private mutationGeneration = 0;
  /**
   * Opens a place in the app for a shortcut. The window the workspace is shown in provides it; where there is none the
   * shortcut says so instead of doing nothing. A shortcut names a place, never an address.
   */
  assist?: (prompt: string) => void;
  navigate?: (target: ShortcutTarget) => void;
  /** The places `navigate` can open here: a window offers the ones it has. */
  navigable: ShortcutTarget[] = [];

  constructor(readonly api: PersonalAPI = personalAPI) {}

  /** Resolves once no save is running (or after a few seconds): a drag that ends just after another change is not lost. */
  async idle(): Promise<void> {
    for (let waited = 0; this.busy && waited < 4000; waited += 25) await new Promise((resolve) => setTimeout(resolve, 25));
  }

  async load(id: string) {
    const workspace = await this.api.workspace(id);
    const collections = await Promise.all((workspace.collectionIds ?? []).map(id => this.api.collection(id)));
    const validated = validateWorkspace(workspace, collections);
    const entries = await Promise.all(collections.map(async c => [c.id, await this.api.records(c.id)] as const));
    this.collections = collections;
    this.records = Object.fromEntries(entries);
    this.workspace = validated;
  }

  /** Poll only while mounted; do not replace records if an intervening save changed them. */
  async refreshRecords(): Promise<void> {
    if (this.busy || this.readOnly || !this.workspace) return;
    const before = this.mutationGeneration;
    try {
      const entries = await Promise.all(this.collections.map(async c => [c.id, await this.api.records(c.id)] as const));
      if (!this.busy && this.mutationGeneration === before) this.records = Object.fromEntries(entries);
    } catch { /* Keep saved data and open drafts; explicit Reload surfaces access errors. */ }
  }

  async action(viewID: string, actionID: string, selected: string[], confirmed = false): Promise<boolean> {
    if (this.busy || this.readOnly || !this.workspace) return false;
    const view = this.workspace.definition.views.find(v => v.id === viewID);
    const action = view?.actions?.find(a => a.id === actionID);
    const collection = this.collections.find(c => c.id === view?.collectionId);
    if (!view || !action || !collection || action.prompt || !confirmed) return false;
    this.problem = ''; this.notice = ''; this.busy = true; this.mutationGeneration++;
    try {
      const records = selected.map(id => {
        const r = this.records[collection.id]?.find(r => r.id === id);
        if (!r) throw new Error('A selected record is unavailable. Reload the workspace.');
        return {id: r.id, revision: r.revision};
      });
      const result = await this.api.executeAction(this.workspace.id, viewID, actionID, {workspaceRevision:this.workspace.revision,collectionRevision:collection.revision,records,confirmed:true});
      const changed = new SvelteSet([...result.saved.map(r => r.id), ...result.deleted]);
      this.records[collection.id] = [...(this.records[collection.id] ?? []).filter(r => !changed.has(r.id)), ...result.saved];
      this.notice = `${action.name} completed.`;
      return true;
    } catch (err) {
      this.problem = `${err instanceof Error ? err.message : 'Action failed.'} Your selection is still here. Reload to check the saved state before retrying.`;
      return false;
    } finally {this.busy = false;}
  }

  /** Fixed operations, scoped to a validated view and its explicitly permitted personal fields. */
  async mutate(viewID: string, operation: 'create' | 'update' | 'delete', id?: string, patch: Values = {}, clear: string[] = []): Promise<boolean> {
    if (this.busy || this.readOnly) return false;
    this.problem = ''; this.notice = '';
    const view = this.workspace?.definition.views.find(v => v.id === viewID);
    if (!view?.operations?.includes(operation)) { this.problem = 'This view does not allow that record operation.'; return false; }
    const editable = viewWritableFields(view);
    if ([...Object.keys(patch), ...clear].some(key => !editable.includes(key))) { this.problem = 'This field is not permitted in this view.'; return false; }
    const records = this.records[view.collectionId] ?? [];
    const record = records.find(r => r.id === id);
    if (operation !== 'create' && !record) { this.problem = 'This record is no longer available. Reload the workspace.'; return false; }
    this.busy = true; this.mutationGeneration++;
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
        this.lastSaved = saved;
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
