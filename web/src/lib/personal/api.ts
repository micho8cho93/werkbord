import { request } from '../api';
import { collectionID, collectionSchema, recordID, recordSchema, workspaceID, workspaceSchema, type Values, type WorkspaceSpec } from './schema';
import { z } from 'zod/v4';
const base = '/api/personal/v1';
const reference = z.strictObject({id: workspaceID, name: z.string()});
export const usageSchema = z.strictObject({collectionId: collectionID, name: z.string(), revision: z.number().int().positive(), ownerWorkspaceId: z.union([workspaceID, z.literal('')]), recordCount: z.number().int().nonnegative(), generation: z.number().int().nonnegative(), referenceCount: z.number().int().nonnegative(), workspaces: z.array(reference).max(20)});
export const deletionSchema = z.strictObject({workspaceId: workspaceID, revision: z.number().int().positive(), name: z.string(), mode: z.enum(['keep_data', 'delete_data']), collections: z.array(usageSchema.extend({delete: z.boolean()})).max(32), fingerprint: z.string().regex(/^[a-f0-9]{64}$/)});
export type Deletion = z.infer<typeof deletionSchema>;
export type Usage = z.infer<typeof usageSchema>;

const recordsPath = (id: string) => `${base}/collections/${collectionID.parse(id)}/records`;
export type PersonalRequest = (method: string, path: string, body?: unknown) => Promise<unknown>;
export function createPersonalAPI(call: PersonalRequest) {
  const request = call;
  return {
    async workspaces() {
      const items = []; let after = '';
      do {
        const page = z.strictObject({ items: z.array(workspaceSchema.extend({ icon: workspaceSchema.shape.icon.catch('overview') })).max(100).nullable(), nextAfter: workspaceID.optional() }).parse(await request('GET', `${base}/workspaces?limit=100${after ? `&after=${after}` : ''}`));
        items.push(...(page.items ?? []));
        if (page.nextAfter && page.nextAfter <= after) throw new Error('Invalid workspace page.');
        after = page.nextAfter ?? '';
        if (items.length > 10000) throw new Error('Too many saved workspaces.');
      } while (after);
      return items;
    },
    updateWorkspace: async (id: string, revision: number, workspace: WorkspaceSpec) => workspaceSchema.parse(await request('PUT', `${base}/workspaces/${workspaceID.parse(id)}`, { revision, workspace })),
    previewDeletion: async (id: string, revision: number, mode: 'keep_data' | 'delete_data') => deletionSchema.parse(await request('POST', `${base}/workspaces/${workspaceID.parse(id)}/deletion-preview`, {revision, mode})),
    deleteWorkspace: async (preview: Deletion) => deletionSchema.parse(await request('DELETE', `${base}/workspaces/${workspaceID.parse(preview.workspaceId)}`, {revision: preview.revision, mode: preview.mode, fingerprint: preview.fingerprint, confirmed: true})),
    history: async (id: string) => z.array(z.strictObject({id: workspaceID, revision: z.number().int().positive(), createdAt: z.iso.datetime({offset:true}), updatedAt: z.iso.datetime({offset:true})})).max(100).parse(await request('GET', `${base}/workspaces/${workspaceID.parse(id)}/history`)),
    snapshot: async (id: string, revision: number) => z.strictObject({workspace: workspaceSchema, collections: z.array(collectionSchema)}).parse(await request('GET', `${base}/workspaces/${workspaceID.parse(id)}/revisions/${z.number().int().positive().parse(revision)}`)),
    rollback: async (id: string, revision: number, targetRevision: number) => workspaceSchema.parse(await request('POST', `${base}/workspaces/${workspaceID.parse(id)}/rollback`, {revision, targetRevision})),
    usage: async (id: string) => usageSchema.parse(await request('GET', `${base}/collections/${collectionID.parse(id)}/usage`)),
    async collections() {
      const items = []; let after = '';
      do {
        const page = z.strictObject({items: z.array(collectionSchema).max(100).nullable(), nextAfter: collectionID.optional()}).parse(await request('GET', `${base}/collections?limit=100${after ? `&after=${after}` : ''}`));
        items.push(...(page.items ?? []));
        if (page.nextAfter && page.nextAfter <= after) throw new Error('Invalid collection page.');
        after = page.nextAfter ?? '';
        if (items.length > 10000) throw new Error('Too many collections for this library.');
      } while (after);
      return items;
    },
    workspace: async (id: string) => workspaceSchema.parse(await request('GET', `${base}/workspaces/${workspaceID.parse(id)}`)),
    collection: async (id: string) => collectionSchema.parse(await request('GET', `${base}/collections/${collectionID.parse(id)}`)),
    async records(id: string) {
      const records = [];
      let after = '';
      const seen = new Set<string>();
      do {
        const page = z.strictObject({ items: z.array(recordSchema).max(100).nullable(), nextAfter: recordID.optional() }).parse(
          await request('GET', `${recordsPath(id)}?limit=100${after ? `&after=${after}` : ''}`),
        );
        for (const record of page.items ?? []) {
          if (record.collectionId !== id || seen.has(record.id)) throw new Error('Collection changed while loading. Reload the workspace.');
          seen.add(record.id); records.push(record);
        }
        if (page.nextAfter && (page.nextAfter <= after || !seen.has(page.nextAfter))) throw new Error('Invalid collection page. Reload the workspace.');
        after = page.nextAfter ?? '';
        if (records.length > 10000) throw new Error('This collection is too large for this workspace screen. Its records are safe.');
      } while (after);
      return records;
    },
    create: async (id: string, values: Values) => recordSchema.parse(await request('POST', recordsPath(id), { values })),
    update: async (cid: string, id: string, revision: number, values: Values) => recordSchema.parse(await request('PUT', `${recordsPath(cid)}/${recordID.parse(id)}`, { revision, values })),
    remove: (cid: string, id: string, revision: number) => request('DELETE', `${recordsPath(cid)}/${recordID.parse(id)}`, { revision }),
    createCollection: async (spec: unknown) => collectionSchema.parse(await request('POST', `${base}/collections`, spec)),
    createWorkspace: async (spec: WorkspaceSpec) => workspaceSchema.parse(await request('POST', `${base}/workspaces`, spec)),
  };
}
export const personalAPI = createPersonalAPI(request);
export type PersonalAPI = ReturnType<typeof createPersonalAPI>;
