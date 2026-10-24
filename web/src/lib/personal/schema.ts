import { z } from 'zod/v4';
import { validWorkspaceIcon } from './icons';

const key = z.string().regex(/^[a-z][a-z0-9_]{0,63}$/);
export const collectionID = z.string().regex(/^pcl_[a-z2-7]{16}$/);
export const workspaceID = z.string().regex(/^pws_[a-z2-7]{16}$/);
export const recordID = z.string().regex(/^prec_[a-z2-7]{16}$/);
const text = (max: number) => z.string().trim().min(1).max(max).refine(v => !v.includes('\0'));
const meta = { id: z.string(), revision: z.number().int().positive(), createdAt: z.iso.datetime({ offset: true }), updatedAt: z.iso.datetime({ offset: true }) };
export const fieldSchema = z.strictObject({
  key, name: text(120), type: z.enum(['text', 'number', 'boolean', 'date', 'datetime', 'select', 'multiselect']),
  required: z.boolean().default(false), options: z.array(text(120)).max(100).optional(),
});
export const collectionSpecSchema = z.strictObject({ name: text(120), schemaVersion: z.literal(1), fields: z.array(fieldSchema).min(1).max(64) });
export const collectionSchema = collectionSpecSchema.extend({ ...meta, id: collectionID });
export const viewSchema = z.strictObject({
  id: key, name: text(120), kind: z.enum(['table', 'list', 'checklist', 'progress', 'status', 'chart', 'board', 'calendar']),
  collectionId: collectionID, fields: z.array(key).min(1).max(64),
  checkedField: key.optional(), groupBy: key.optional(), dateField: key.optional(),
  operations: z.array(z.enum(['create', 'update', 'delete'])).max(3).optional(),
  filters: z.array(z.strictObject({ field: key, value: z.string().max(16000) })).max(16).optional(),
});
export type View = z.infer<typeof viewSchema>;
export interface Layout { type: 'stack' | 'grid' | 'card' | 'text' | 'heading' | 'view' | 'binding'; text?: string; viewId?: string; bindingId?: string; columns?: number; children?: Layout[] }
const nodeSchema: z.ZodType<Layout> = z.lazy(() => z.union([
  z.strictObject({ type: z.literal('stack'), children: z.array(nodeSchema).min(1).max(128) }),
  z.strictObject({ type: z.literal('grid'), columns: z.number().int().min(2).max(4), children: z.array(nodeSchema).min(1).max(128) }),
  z.strictObject({ type: z.literal('card'), text: text(160), children: z.array(nodeSchema).min(1).max(128) }),
  z.strictObject({ type: z.enum(['text', 'heading']), text: text(2000) }),
  z.strictObject({ type: z.literal('view'), viewId: key }),
  z.strictObject({ type: z.literal('binding'), bindingId: key }),
]));
export const workspaceSpecSchema = z.strictObject({
  name: text(120), icon: z.string().max(32).refine(validWorkspaceIcon, 'Invalid workspace icon'),
  uiSchemaVersion: z.literal(1), definition: z.strictObject({ views: z.array(viewSchema).max(32), layout: nodeSchema }),
  visibility: z.strictObject({ universal: z.boolean(), individual: z.boolean(), teamWorkspaceIds: z.array(z.string()).max(64).nullish() }),
  collectionIds: z.array(collectionID).max(32).nullable(),
  readBindings: z.array(z.strictObject({ id: key, kind: z.enum(['individual.project', 'individual.tasks']), projectId: z.string().regex(/^prj_[a-z2-7]{16}$/) })).max(16).nullish(),
});
export const workspaceSchema = workspaceSpecSchema.extend({ ...meta, id: workspaceID });
export type Workspace = z.infer<typeof workspaceSchema>;
export type WorkspaceSpec = z.infer<typeof workspaceSpecSchema>;
export type Collection = z.infer<typeof collectionSchema>;
export type Field = z.infer<typeof fieldSchema>;
export type Values = Record<string, string | number | boolean | string[]>;
export const recordSchema = z.strictObject({ ...meta, id: recordID, collectionId: collectionID, values: z.record(key, z.union([z.string(), z.number(), z.boolean(), z.array(z.string())])) });
export type PersonalRecord = z.infer<typeof recordSchema>;

function unique(values: string[]): boolean { return new Set(values).size === values.length; }
function invalid(): never { throw new Error('This saved workspace uses an unsupported component or data binding. Its records are safe. Reload after correcting its definition.'); }

/** Validate before any saved node reaches OpenUI. No saved source or expressions are evaluated. */
export function validateWorkspace(raw: unknown, collections: Collection[]): Workspace {
  // Bound nesting before the recursive schema parser sees an untrusted tree.
  if (JSON.stringify(raw).length > 40000) invalid();
  let count = 0;
  function bound(value: unknown, depth: number) {
    if (depth > 24 || ++count > 4000) invalid();
    if (value && typeof value === 'object') for (const child of Object.values(value)) bound(child, depth + 1);
  }
  bound(raw, 0);
  const parsed = workspaceSchema.safeParse(raw);
  if (!parsed.success) invalid();
  const w = parsed.data;
  const ids = w.collectionIds ?? [];
  if (!unique(ids) || !unique(w.definition.views.map(v => v.id))) invalid();
  for (const c of collections) {
    if (!unique(c.fields.map(f => f.key))) invalid();
    for (const f of c.fields) {
      const selection = f.type === 'select' || f.type === 'multiselect';
      if (selection ? !f.options?.length || !unique(f.options) : !!f.options?.length) invalid();
    }
  }
  for (const v of w.definition.views) {
    if (!unique((v.filters ?? []).map(f => f.field))) invalid();
    const c = collections.find(c => c.id === v.collectionId);
    for (const f of v.filters ?? []) {
      const field = c?.fields.find(field => field.key === f.field);
      if (!field || field.type === 'multiselect') invalid();
    }
    if (!ids.includes(v.collectionId) || !c || !unique(v.fields) || !unique(v.operations ?? [])) invalid();
    if (!v.fields.every(key => c.fields.some(f => f.key === key))) invalid();
    const field = (key?: string) => c.fields.find(f => f.key === key);
    if (['checklist', 'progress', 'status'].includes(v.kind)) {
      if (field(v.checkedField)?.type !== 'boolean' || v.groupBy || v.dateField) invalid();
    } else if (v.kind === 'chart') {
      if (field(v.groupBy)?.type !== 'select' || (field(v.groupBy)?.options?.length ?? 0) > 20 || v.checkedField || v.dateField) invalid();
    } else if (v.kind === 'table' || v.kind === 'list') {
      if (v.checkedField || v.groupBy || v.dateField) invalid();
    } else invalid(); // Board/calendar are stored in Phase 1, but not supported by this renderer yet.
    if (['progress', 'status', 'chart'].includes(v.kind) && v.operations?.length) invalid();
    if (v.operations?.includes('create') && c.fields.some(f => f.required && !v.fields.includes(f.key) && f.key !== v.checkedField)) invalid();
  }
  count = 0;
  function walk(n: Layout, depth: number) {
    if (depth > 8 || ++count > 128) invalid();
    if (n.type === 'view' && !w.definition.views.some(v => v.id === n.viewId)) invalid();
    if (n.type === 'binding') invalid(); // No external resolver or additional authority in Phase 2.
    for (const child of n.children ?? []) walk(child, depth + 1);
  }
  walk(w.definition.layout, 1);
  return w;
}

/** Typed equality for saved filters; missing values never match literal text. */
export function matchesSavedFilters(values: Values, filters: View['filters']): boolean {
  return (filters ?? []).every(filter => {
    const value = values[filter.field];
    if (value === undefined || Array.isArray(value)) return false;
    if (typeof value === 'number') return value === Number(filter.value);
    if (typeof value === 'boolean') return String(value) === filter.value.trim();
    return value === filter.value;
  });
}
