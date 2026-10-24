import { z } from 'zod/v4';
import { validWorkspaceIcon } from './icons';

const key = z.string().regex(/^[a-z][a-z0-9_]{0,63}$/);
/** Must match domain.PersonalViewKinds, PersonalShortcutTargets and the size bounds. */
export const viewKinds = ['table', 'list', 'checklist', 'progress', 'status', 'chart', 'board', 'calendar', 'document', 'pages', 'snippets', 'files', 'form', 'metric', 'countdown', 'timeline', 'hierarchy'] as const;
export const shortcutTargets = ['home', 'projects', 'analytics', 'calendar', 'data-library', 'settings', 'assistant'] as const;
export type ShortcutTarget = (typeof shortcutTargets)[number];
/** Kinds written in a long text body. */
export const documentKinds = ['document', 'pages', 'snippets', 'files'] as const;
export const maxLongTextBytes = 48000;
export const maxURLBytes = 2000;
export const collectionID = z.string().regex(/^pcl_[a-z2-7]{16}$/);
export const workspaceID = z.string().regex(/^pws_[a-z2-7]{16}$/);
export const recordID = z.string().regex(/^prec_[a-z2-7]{16}$/);
const text = (max: number) => z.string().trim().min(1).max(max).refine(v => !v.includes('\0'));
const meta = { id: z.string(), revision: z.number().int().positive(), createdAt: z.iso.datetime({ offset: true }), updatedAt: z.iso.datetime({ offset: true }) };
export interface Formula { op: 'field' | 'literal' | 'add' | 'subtract' | 'multiply' | 'divide' | 'min' | 'max'; field?: string; value?: number; args?: Formula[] }
export const formulaSchema: z.ZodType<Formula> = z.lazy(() => z.strictObject({ op: z.enum(['field', 'literal', 'add', 'subtract', 'multiply', 'divide', 'min', 'max']), field: key.optional(), value: z.number().optional(), args: z.array(formulaSchema).max(8).optional() }));
const valuesSchema = z.record(key, z.union([z.string(), z.number(), z.boolean(), z.array(z.string())]));
const conditionsSchema = z.array(z.strictObject({field: key, value: z.string().max(16000)})).max(16);
export const actionSchema = z.strictObject({id: key, name: text(120), when: conditionsSchema.optional(), steps: z.array(z.strictObject({operation: z.enum(['create','update','delete']), values: valuesSchema.optional(), clear: z.array(key).max(64).optional()})).max(8), prompt: text(2000).optional()});
export const ruleSchema = z.strictObject({id: key, when: conditionsSchema.optional(), set: valuesSchema, schedule: z.strictObject({dateField:key, lastRunField:key, everyDays:z.number().int().min(0).max(3650).optional()}).optional()});
export const fieldSchema = z.strictObject({
  key, name: text(120), type: z.enum(['text', 'longtext', 'url', 'number', 'boolean', 'date', 'datetime', 'select', 'multiselect']),
  formula: formulaSchema.optional(), min: z.number().optional(), max: z.number().optional(), input: z.enum(['slider','rating']).optional(),
  required: z.boolean().default(false), options: z.array(text(120)).max(100).optional(),
});
export const collectionSpecSchema = z.strictObject({ name: text(120), schemaVersion: z.literal(1), fields: z.array(fieldSchema).min(1).max(64), rules: z.array(ruleSchema).max(16).optional() });
export const collectionSchema = collectionSpecSchema.extend({ ...meta, id: collectionID });
export const viewSchema = z.strictObject({
  id: key, name: text(120), kind: z.enum(viewKinds),
  collectionId: collectionID, fields: z.array(key).min(1).max(64),
  checkedField: key.optional(), groupBy: key.optional(), dateField: key.optional(), bodyField: key.optional(), parentField: key.optional(),
  operations: z.array(z.enum(['create', 'update', 'delete'])).max(3).optional(),
  filters: conditionsSchema.optional(), visibleWhen: conditionsSchema.optional(), actions: z.array(actionSchema).max(16).optional(),
  metric: z.strictObject({operation:z.enum(['count','sum','average','min','max']),field:key.optional()}).optional(), endDateField:key.optional(),
});
export type View = z.infer<typeof viewSchema>;
export interface Layout { type: 'stack' | 'grid' | 'card' | 'text' | 'heading' | 'view' | 'binding' | 'shortcut'; text?: string; viewId?: string; bindingId?: string; target?: ShortcutTarget; columns?: number; children?: Layout[] }
const nodeSchema: z.ZodType<Layout> = z.lazy(() => z.union([
  z.strictObject({ type: z.literal('stack'), children: z.array(nodeSchema).min(1).max(128) }),
  z.strictObject({ type: z.literal('grid'), columns: z.number().int().min(2).max(4), children: z.array(nodeSchema).min(1).max(128) }),
  z.strictObject({ type: z.literal('card'), text: text(160), children: z.array(nodeSchema).min(1).max(128) }),
  z.strictObject({ type: z.enum(['text', 'heading']), text: text(2000) }),
  z.strictObject({ type: z.literal('view'), viewId: key }),
  z.strictObject({ type: z.literal('binding'), bindingId: key }),
  z.strictObject({ type: z.literal('shortcut'), target: z.enum(shortcutTargets), text: text(80).optional() }),
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

/** The fields a view's kind binds, besides the ones it lists: what a card moves between, what a document is written in. */
export function viewBoundFields(view: View): string[] {
  return [view.checkedField, view.groupBy, view.dateField, view.bodyField, view.parentField].filter((key): key is string => !!key);
}
/** Every field a view may write: the ones it shows and the ones its kind binds. */
export function viewWritableFields(view: View): string[] { return [...view.fields, ...viewBoundFields(view)]; }

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
      if (!field || field.type === 'multiselect' || field.type === 'longtext') invalid();
    }
    if (!ids.includes(v.collectionId) || !c || !unique(v.fields) || !unique(v.operations ?? [])) invalid();
    if (!v.fields.every(key => c.fields.some(f => f.key === key))) invalid();
    const field = (key?: string) => c.fields.find(f => f.key === key);
    const document = (documentKinds as readonly string[]).includes(v.kind) || v.kind === 'hierarchy';
    if (!document && (v.bodyField || v.parentField) || !['pages', 'hierarchy'].includes(v.kind) && v.parentField) invalid();
    if (['checklist', 'progress', 'status'].includes(v.kind)) {
      if (field(v.checkedField)?.type !== 'boolean' || v.groupBy || v.dateField) invalid();
    } else if (v.kind === 'chart') {
      if (field(v.groupBy)?.type !== 'select' || (field(v.groupBy)?.options?.length ?? 0) > 20 || v.checkedField || v.dateField) invalid();
    } else if (['table','list','form','metric'].includes(v.kind)) {
      if (v.checkedField || v.groupBy || v.dateField) invalid();
    } else if (v.kind === 'board') {
      if (field(v.groupBy)?.type !== 'select' || v.dateField || v.checkedField) invalid();
    } else if (['calendar','countdown','timeline'].includes(v.kind)) {
      if (!['date', 'datetime'].includes(field(v.dateField)?.type ?? '') || v.groupBy || v.checkedField) invalid();
    } else if (v.kind === 'document' || v.kind === 'pages' || v.kind === 'snippets') {
      // The first field is the title; the body is the long text it is written in.
      if (field(v.fields[0])?.type !== 'text' || field(v.bodyField)?.type !== 'longtext' || v.bodyField === v.fields[0] || v.dateField || v.checkedField) invalid();
      if (v.kind === 'snippets' ? v.groupBy && field(v.groupBy)?.type !== 'select' : v.groupBy) invalid();
      if (v.parentField && (field(v.parentField)?.type !== 'text' || v.parentField === v.fields[0] || v.parentField === v.bodyField)) invalid();
    } else if (v.kind === 'hierarchy') {
      if (field(v.parentField)?.type !== 'text' || v.groupBy || v.dateField || v.checkedField) invalid();
    } else if (v.kind === 'files') {
      if (field(v.fields[0])?.type !== 'text' || v.bodyField && field(v.bodyField)?.type !== 'longtext' || v.groupBy && field(v.groupBy)?.type !== 'select' || v.dateField || v.checkedField) invalid();
    } else invalid();
    if (['progress', 'status', 'chart','metric','countdown','timeline','hierarchy'].includes(v.kind) && v.operations?.length) invalid();
    if (v.operations?.includes('create') && c.fields.some(f => f.required && !v.fields.includes(f.key) && !viewBoundFields(v).includes(f.key))) invalid();
  }
  validateCapabilities(w, collections);
  count = 0;
  function walk(n: Layout, depth: number) {
    if (depth > 8 || ++count > 128) invalid();
    if (n.type === 'view' && !w.definition.views.some(v => v.id === n.viewId)) invalid();
    if (n.type === 'binding' && !w.readBindings?.some(b => b.id === n.bindingId)) invalid();
    if (n.type === 'shortcut' && (n.children?.length ?? 0) > 0) invalid();
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

export function validateCapabilities(w: Workspace, collections: Collection[]): void {
  for (const c of collections) {
    for (const f of c.fields) {
      if ((f.min !== undefined || f.max !== undefined || f.input || f.formula) && f.type !== 'number' || f.min !== undefined && f.max !== undefined && f.min > f.max) invalid();
      if (f.input && (f.min === undefined || f.max === undefined || f.min >= f.max)) invalid();
      if (!f.formula) continue;
      if (f.required || f.min !== undefined || f.max !== undefined || f.input) invalid();
      let count = 0;
      const visiting = new Set([f.key]);
      function walk(x: Formula, depth: number) {
        if (++count > 64 || depth > 8) invalid();
        if (x.op === 'field') {
          const ref = c.fields.find(f => f.key === x.field);
          if (!ref || ref.type !== 'number' || visiting.has(ref.key) || x.value !== undefined || x.args?.length) invalid();
          if (ref.formula) {visiting.add(ref.key); walk(ref.formula, depth + 1); visiting.delete(ref.key);}
        } else if (x.op === 'literal') {
          if (x.value === undefined || x.field || x.args?.length) invalid();
        } else {
          if (x.field || x.value !== undefined || !x.args || x.args.length < 2 || ['subtract','divide'].includes(x.op) && x.args.length !== 2) invalid();
          x.args.forEach(a => walk(a, depth + 1));
        }
      }
      walk(f.formula, 1);
    }
    if (!unique((c.rules ?? []).map(r => r.id))) invalid();
    for (const rule of c.rules ?? []) {
      if (!Object.keys(rule.set).length || Object.keys(rule.set).some(k => !c.fields.some(f => f.key === k && !f.formula))) invalid();
      if (rule.schedule && (!['date','datetime'].includes(c.fields.find(f => f.key === rule.schedule!.dateField)?.type ?? '') || c.fields.find(f => f.key === rule.schedule!.lastRunField)?.type !== 'datetime')) invalid();
    }
  }
  for (const v of w.definition.views) {
    const c = collections.find(c => c.id === v.collectionId)!;
    if (v.kind === 'metric' ? !v.metric || v.metric.operation !== 'count' && c.fields.find(f => f.key === v.metric!.field)?.type !== 'number' || v.metric.operation === 'count' && v.metric.field : v.metric) invalid();
    if (v.endDateField && (v.kind !== 'timeline' || !['date','datetime'].includes(c.fields.find(f => f.key === v.endDateField)?.type ?? ''))) invalid();
    if (!unique((v.actions ?? []).map(a => a.id))) invalid();
    for (const a of v.actions ?? []) {
      if (a.prompt ? a.steps.length : !a.steps.length) invalid();
      for (const step of a.steps) {
        if (!v.operations?.includes(step.operation)) invalid();
        if ([...Object.keys(step.values ?? {}), ...(step.clear ?? [])].some(k => !viewWritableFields(v).includes(k) || c.fields.find(f => f.key === k)?.formula)) invalid();
      }
    }
    for (const condition of [...(v.visibleWhen ?? []), ...(v.actions ?? []).flatMap(a => a.when ?? [])]) {
      if (!c.fields.some(f => f.key === condition.field && !['longtext','multiselect'].includes(f.type))) invalid();
    }
  }
}
