import { afterEach, describe, expect, it, vi } from 'vitest';
import { WorkspaceSession } from './session.svelte';
import { personalAPI } from './api';
import { todoCollection, todoWorkspace } from './example';
import { collectionSchema, validateWorkspace } from './schema';
import { ApiError } from '../api';
const cid = 'pcl_abcdefghijklmnop', rid = 'prec_abcdefghijklmnop';
const meta = { revision: 4, createdAt: '2026-10-23T12:00:00Z', updatedAt: '2026-10-23T12:00:00Z' };
function fixture() {
  const session = new WorkspaceSession();
  const c = collectionSchema.parse({ ...todoCollection, ...meta, id: cid });
  session.workspace = validateWorkspace({ ...todoWorkspace(cid), ...meta, id: 'pws_abcdefghijklmnop' }, [c]);
  session.collections = [c];
  session.records = { [cid]: [{ ...meta, id: rid, collectionId: cid, values: { title: 'Keep', done: false, priority: 'Low', notes: 'Hidden durable notes', due: '2026-11-01' } }] };
  return session;
}
afterEach(() => vi.restoreAllMocks());
describe('personal record capabilities', () => {
  it('refuses all record writes in a proposal preview even if a view lists operations', async () => {
    const s = fixture(); s.readOnly = true;
    const update = vi.spyOn(personalAPI, 'update');
    const create = vi.spyOn(personalAPI, 'create');
    const remove = vi.spyOn(personalAPI, 'remove');
    expect(await s.mutate('todos', 'update', rid, { done: true })).toBe(false);
    expect(await s.mutate('todos', 'create', undefined, { title: 'Sample' })).toBe(false);
    expect(await s.mutate('todos', 'delete', rid)).toBe(false);
    expect(update).not.toHaveBeenCalled(); expect(create).not.toHaveBeenCalled(); expect(remove).not.toHaveBeenCalled();
  });
  it('merges visible edits with hidden saved values and uses the current revision', async () => {
    const s = fixture();
    s.workspace!.definition.views[0].fields = ['title', 'done', 'priority', 'due'];
    const update = vi.spyOn(personalAPI, 'update').mockImplementation(async (_cid, _id, revision, values) => ({ ...meta, revision: revision + 1, id: rid, collectionId: cid, values }));
    expect(await s.mutate('todos', 'update', rid, { title: 'Changed' }, ['due'])).toBe(true);
    expect(update).toHaveBeenCalledWith(cid, rid, 4, { title: 'Changed', done: false, priority: 'Low', notes: 'Hidden durable notes' });
    expect(s.records[cid][0].revision).toBe(5);
    expect(s.records[cid][0].values.notes).toBe('Hidden durable notes');
  });
  it('rejects operations outside a view capability and fields outside its binding', async () => {
    const s = fixture();
    const update = vi.spyOn(personalAPI, 'update');
    expect(await s.mutate('progress', 'update', rid, { done: true })).toBe(false);
    expect(await s.mutate('missing', 'update', rid, { done: true })).toBe(false);
    expect(await s.mutate('todos', 'update', rid, { url: '/api/tasks' })).toBe(false);
    expect(await s.mutate('todos', 'update', rid, {}, ['unknown'])).toBe(false);
    s.workspace!.definition.views[0].operations = undefined;
    expect(await s.mutate('todos', 'update', rid, { done: true })).toBe(false);
    expect(update).not.toHaveBeenCalled();
  });
  it('does not replace data or lose the current revision on stale or uncertain writes', async () => {
    const s = fixture();
    vi.spyOn(personalAPI, 'update').mockRejectedValue(new ApiError(409, 'conflict', 'Stale revision'));
    expect(await s.mutate('todos', 'update', rid, { done: true })).toBe(false);
    expect(s.records[cid][0].values.done).toBe(false);
    expect(s.records[cid][0].revision).toBe(4);
    expect(s.problem).toContain('changed elsewhere');
    expect(s.busy).toBe(false);
  });
});

it('loads definitions, schemas and records exclusively through the supplied owner transport', async () => {
  const c = collectionSchema.parse({...todoCollection, ...meta, id: cid});
  const w = validateWorkspace({...todoWorkspace(cid), ...meta, id: 'pws_abcdefghijklmnop'}, [c]);
  const local = { ...personalAPI, workspace: vi.fn().mockResolvedValue(w), collection: vi.fn().mockResolvedValue(c), records: vi.fn().mockResolvedValue([]) };
  const direct = vi.spyOn(personalAPI, 'collection');
  const session = new WorkspaceSession(local);
  await session.load(w.id);
  expect(local.workspace).toHaveBeenCalledWith(w.id);
  expect(local.collection).toHaveBeenCalledWith(cid);
  expect(local.records).toHaveBeenCalledWith(cid);
  expect(direct).not.toHaveBeenCalled();
});
