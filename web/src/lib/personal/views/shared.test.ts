import { describe, expect, it } from 'vitest';
import { excerpt, fromLocalInput, newRecordValues, propertyFields, relativeTime, titleOf, toLocalInput, wordCount } from './shared';
import type { Collection, PersonalRecord, View } from '../schema';

const collection = {
  id: 'pcl_aaaaaaaaaaaaaaaa', name: 'Notes', schemaVersion: 1, revision: 1, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
  fields: [
    { key: 'title', name: 'Title', type: 'text', required: true }, { key: 'body', name: 'Body', type: 'longtext', required: false },
    { key: 'parent', name: 'Parent', type: 'text', required: false }, { key: 'tags', name: 'Tags', type: 'multiselect', required: false, options: ['a'] },
    { key: 'status', name: 'Status', type: 'select', required: true, options: ['Open', 'Done'] }, { key: 'when', name: 'When', type: 'date', required: true },
    { key: 'n', name: 'N', type: 'number', required: true }, { key: 'ok', name: 'OK', type: 'boolean', required: true },
  ],
} as Collection;
const view = { id: 'v', name: 'V', kind: 'pages', collectionId: collection.id, fields: ['title', 'tags', 'status', 'body', 'parent'], bodyField: 'body', parentField: 'parent' } as View;

describe('workspace view helpers', () => {
  it('names a record by its first field', () => {
    const record = (title: unknown) => ({ values: { title } }) as unknown as PersonalRecord;
    expect(titleOf(view, record('Plan'))).toBe('Plan');
    expect(titleOf(view, record('   '))).toBe('Untitled');
    expect(titleOf(view, record(undefined))).toBe('Untitled');
  });
  it('completes every required field of a new record', () => {
    const values = newRecordValues(collection, view, { title: 'New' });
    expect(values).toMatchObject({ title: 'New', status: 'Open', n: 0, ok: false });
    expect(values.when).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(values).not.toHaveProperty('body');
    expect(newRecordValues(collection, view, { title: 'x', status: 'Done' }).status).toBe('Done');
  });
  it('shows beside a document everything except its title, body and parent', () => {
    expect(propertyFields(collection, view).map((f) => f.key)).toEqual(['tags', 'status']);
  });
  it('makes a plain taste of Markdown', () => {
    expect(excerpt('# Title\n\nSome **bold** text with [a link](https://x.y) and [[Page]].')).toBe('Title Some bold text with a link and Page.');
    expect(excerpt('```\ncode\n```\nafter')).toBe('after');
    expect(excerpt('- [ ] task one\n- [x] task two')).toBe('task one task two');
    expect(excerpt('x'.repeat(300), 20)).toHaveLength(21);
    expect(excerpt(undefined)).toBe('');
  });
  it('converts between a stored time and the person’s local input', () => {
    const stored = '2026-10-24T09:30:00Z';
    const local = toLocalInput(stored);
    expect(local).toMatch(/^2026-10-24T\d\d:\d\d$/);
    expect(fromLocalInput(local)).toBe(stored);
    expect(fromLocalInput('')).toBeUndefined();
    expect(toLocalInput('not a date')).toBe('');
  });
  it('describes how long ago a record changed', () => {
    const now = Date.parse('2026-10-24T12:00:00Z');
    expect(relativeTime('2026-10-24T11:59:40Z', now)).toBe('just now');
    expect(relativeTime('2026-10-24T11:15:00Z', now)).toBe('45 min ago');
    expect(relativeTime('2026-10-24T08:00:00Z', now)).toBe('4 h ago');
    expect(relativeTime('2026-10-20T12:00:00Z', now)).toBe('4 d ago');
  });
  it('counts words', () => {
    expect(wordCount('one two-three  don’t')).toBe(3);
    expect(wordCount(7)).toBe(0);
  });
});
