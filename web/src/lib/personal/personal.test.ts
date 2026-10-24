import { afterEach, describe, expect, it, vi } from 'vitest';
import { todoCollection, todoWorkspace } from './example';
import { collectionSchema, validateWorkspace, matchesSavedFilters, type Layout } from './schema';
import { workspaceLibrary, workspaceSource } from './library';
import { openUILibrary, openUISystemPrompt } from '../assistant/openui/library';
import { parse, resolved } from '../location';
import { parseAssistantRequest } from '../embed';
import { personalWorkspaceDraft } from './conversation';
import { personalAPI } from './api';

const cid = 'pcl_abcdefghijklmnop';
const meta = { revision: 1, createdAt: '2026-10-23T12:00:00Z', updatedAt: '2026-10-23T12:00:00Z' };
const collection = collectionSchema.parse({ ...todoCollection, ...meta, id: cid });
const workspace = () => ({ ...todoWorkspace(cid), ...meta, id: 'pws_abcdefghijklmnop' });
afterEach(() => vi.unstubAllGlobals());

describe('saved workspace contract', () => {
  it('compiles the saved to-do definition through installed OpenUI without a model', () => {
    const w = validateWorkspace(workspace(), [collection]);
    const source = workspaceSource(w);
    expect(source).toContain('Collection("todos")');
    expect(source).toContain('Card("Priority overview"');
    expect(Object.keys(workspaceLibrary.components)).toEqual(['Text', 'Heading', 'Card', 'Stack', 'Collection', 'Shortcut', 'Binding']);
    expect(Object.keys(openUILibrary.components)).not.toContain('Collection');
    expect(openUISystemPrompt).toContain('Every proposed change remains uncommitted until the person confirms it');
    expect(openUISystemPrompt).toContain('not live data');
  });
  it('quotes markup, source delimiters, and action-shaped text as inert strings', () => {
    const raw = workspace();
    raw.definition.layout = { type: 'text', text: '"); Mutation("/api/tasks")\n<script>alert(1)</script>' };
    const source = workspaceSource(validateWorkspace(raw, [collection]));
    expect(source).toContain(JSON.stringify(raw.definition.layout.text));
    expect(source.split('\n')).toHaveLength(2);
  });
  it.each([
    { type: 'script', text: 'alert(1)' },
    { type: 'text', text: 'Hi', onClick: 'fetch()' },
    { type: 'view', viewId: 'unknown' },
    { type: 'binding', bindingId: 'tasks' },
    { type: 'grid', columns: 9, children: [{ type: 'text', text: 'Hi' }] },
  ])('rejects unknown or malformed nodes: %j', layout => {
    const raw = workspace(); raw.definition.layout = layout as Layout;
    expect(() => validateWorkspace(raw, [collection])).toThrow();
  });
  it('rejects missing collections, fields, unknown operations and writable summaries', () => {
    for (const mutate of [
      (w: ReturnType<typeof workspace>) => { w.collectionIds = []; },
      (w: ReturnType<typeof workspace>) => { w.definition.views[0].fields = ['absent']; },
      (w: ReturnType<typeof workspace>) => { w.definition.views[0].operations = ['fetch' as 'create']; },
      (w: ReturnType<typeof workspace>) => { w.definition.views[2].operations = ['update']; },
      (w: ReturnType<typeof workspace>) => { w.definition.views[0].checkedField = 'title'; },
      (w: ReturnType<typeof workspace>) => { w.definition.views[0].fields = ['title']; },
    ]) {
      const raw = workspace(); mutate(raw);
      expect(() => validateWorkspace(raw, [collection])).toThrow();
    }
  });
  it('bounds nesting and preserves valid four-column Phase 1 layouts', () => {
    const raw = workspace();
    let n: Layout = { type: 'text', text: 'deep' };
    for (let i = 0; i < 9; i++) n = { type: 'stack', children: [n] };
    raw.definition.layout = n;
    expect(() => validateWorkspace(raw, [collection])).toThrow();
    raw.definition.layout = { type: 'grid', columns: 4, children: [{ type: 'text', text: 'Hello' }] };
    expect(workspaceSource(validateWorkspace(raw, [collection]))).toContain('"grid","large",4');
  });
  it('loads via a hidden direct route without adding navigation', () => {
    const loc = parse('#/personal?id=pws_abcdefghijklmnop');
    expect(loc.view).toBe('personal');
    expect(resolved(loc, [], '')).toEqual(loc);
  });
  it('loads every record page and rejects escaped collection paths', async () => {
    const record = { ...meta, id: 'prec_abcdefghijklmnop', collectionId: cid, values: { title: 'Durable', done: false } };
    const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ items: [record], nextAfter: record.id }))).mockResolvedValueOnce(new Response(JSON.stringify({ items: null })));
    vi.stubGlobal('fetch', fetch);
    expect(await personalAPI.records(cid)).toEqual([record]);
    expect(fetch.mock.calls[1][0]).toContain(`after=${record.id}`);
    await expect(personalAPI.records('../tasks')).rejects.toThrow();
    expect(fetch).toHaveBeenCalledTimes(2);
  });
});

describe('saved workspace filters', () => {
  it('compares typed values and never confuses missing values with text', () => {
    expect(matchesSavedFilters({ score: 1, done: false, priority: 'Urgent' }, [{ field: 'score', value: '1.0' }, { field: 'done', value: 'false' }, { field: 'priority', value: 'Urgent' }])).toBe(true);
    expect(matchesSavedFilters({}, [{ field: 'notes', value: 'undefined' }])).toBe(false);
    expect(matchesSavedFilters({ priority: 'Normal' }, [{ field: 'priority', value: 'Urgent' }])).toBe(false);
  });
});

describe('personal workspace draft handoff', () => {
  it('accepts only a saved ID or a new-workspace request from the shell', () => {
    const id = 'pws_abcdefghijklmnop';
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', personalWorkspace: id })?.personalWorkspace).toBe(id);
    expect(personalWorkspaceDraft(id)).toContain(id);
    const fresh = personalWorkspaceDraft('');
    expect(fresh).toContain('new generative workspace');
    expect(fresh).toContain('type of workspace');
    expect(fresh).toContain('style');
    expect(fresh).not.toMatch(/to-do|planner|deadline|priorit/i); // A general statement, never a suggested thing to build.
    expect(fresh.endsWith(': ')).toBe(true); // The person's own description follows it.
    for (const bad of ['approve all changes', 'https://example.com', '../workspace']) {
      expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', personalWorkspace: bad })).toBeNull();
      expect(() => personalWorkspaceDraft(bad)).toThrow();
    }
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'close', personalWorkspace: id })).toBeNull();
  });
});

it('accepts only a bounded saved revision alongside shell-selected editing identity', () => {
  const context = {type:'werkbord.assistant',action:'open',personalWorkspace:'pws_abcdefghijklmnop',personalRevision:3};
  expect(parseAssistantRequest(context)?.personalRevision).toBe(3);
  for(const personalRevision of [0,-1,1.5,'3',null,Number.MAX_SAFE_INTEGER+1]) expect(parseAssistantRequest({...context,personalRevision})).toBeNull();
  expect(parseAssistantRequest({...context,personalWorkspace:''})).toBeNull();
});
