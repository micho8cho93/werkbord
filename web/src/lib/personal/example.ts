import type { WorkspaceSpec } from './schema';

// A complete, model-free definition. Priority is a native selection field with
// explicit choices; changing this layout never replaces the collection records.
export const todoCollection = {
  name: 'Personal to-dos', schemaVersion: 1,
  fields: [
    { key: 'title', name: 'To-do', type: 'text', required: true },
    { key: 'done', name: 'Completed', type: 'boolean', required: true },
    { key: 'due', name: 'Due date', type: 'date', required: false },
    { key: 'priority', name: 'Priority', type: 'select', required: true, options: ['Low', 'Normal', 'High'] },
    { key: 'notes', name: 'Notes', type: 'text', required: false },
  ],
};
export function todoWorkspace(collectionId: string): WorkspaceSpec {
  const fields = ['title', 'done', 'due', 'priority', 'notes'];
  return {
    name: 'Personal to-dos', icon: 'auto', uiSchemaVersion: 1,
    visibility: { universal: true, individual: true }, collectionIds: [collectionId], readBindings: [],
    definition: {
      views: [
        { id: 'todos', name: 'To-dos', kind: 'checklist', collectionId, fields, checkedField: 'done', operations: ['create', 'update', 'delete'] },
        { id: 'table', name: 'All records', kind: 'table', collectionId, fields, operations: ['create', 'update', 'delete'] },
        { id: 'progress', name: 'Completion', kind: 'progress', collectionId, fields: ['done'], checkedField: 'done' },
        { id: 'status', name: 'Current status', kind: 'status', collectionId, fields: ['done'], checkedField: 'done' },
        { id: 'priorities', name: 'To-dos by priority', kind: 'chart', collectionId, fields: ['priority'], groupBy: 'priority' },
      ],
      layout: { type: 'stack', children: [
        { type: 'text', text: 'Your own list, saved on this computer. Edit it directly and return whenever you need it.' },
        { type: 'grid', columns: 2, children: [{ type: 'view', viewId: 'progress' }, { type: 'view', viewId: 'status' }] },
        { type: 'view', viewId: 'todos' },
        { type: 'card', text: 'Priority overview', children: [{ type: 'view', viewId: 'priorities' }] },
        { type: 'heading', text: 'The same collection, another layout' },
        { type: 'view', viewId: 'table' },
      ] },
    },
  };
}
