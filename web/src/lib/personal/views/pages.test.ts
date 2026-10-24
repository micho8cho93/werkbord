import { describe, expect, it } from 'vitest';
import { buildTree, descendantIds, pagesLinkingTo } from './pages';
import type { PersonalRecord } from '../schema';

let n = 0;
const page = (id: string, title: string, parent?: string, body = ''): PersonalRecord => ({
  id: `prec_${id.padEnd(16, 'a')}`, collectionId: 'pcl_aaaaaaaaaaaaaaaa', revision: 1, createdAt: `2026-01-01T00:00:${String(n++ % 60).padStart(2, '0')}Z`, updatedAt: '2026-01-01T00:00:00Z',
  values: { title, ...(parent ? { parent: `prec_${parent.padEnd(16, 'a')}` } : {}), body },
});
const title = (r: PersonalRecord) => String(r.values.title);
const names = (nodes: { record: PersonalRecord }[]) => nodes.map((x) => title(x.record));

describe('page tree', () => {
  const a = page('a', 'Alpha'), b = page('b', 'beta', 'a'), c = page('c', 'Gamma', 'a'), d = page('d', 'Delta', 'b');
  it('nests pages under their parents, in title order', () => {
    const tree = buildTree([d, c, b, a, page('e', 'Epsilon')], 'parent', title);
    expect(names(tree.roots)).toEqual(['Alpha', 'Epsilon']);
    expect(names(tree.nodes.get(a.id)!.children)).toEqual(['beta', 'Gamma']);
    expect(tree.trail(d.id).map(title)).toEqual(['Alpha', 'beta', 'Delta']);
  });
  it('puts a page whose parent is gone at the top level', () => {
    const orphan = page('o', 'Orphan', 'z');
    expect(names(buildTree([orphan, a], 'parent', title).roots)).toEqual(['Alpha', 'Orphan']);
  });
  it('never loses a page to a cycle or to a page that is its own parent', () => {
    const x = page('x', 'X', 'y'), y = page('y', 'Y', 'x'), self = page('s', 'Self', 's');
    const tree = buildTree([x, y, self], 'parent', title);
    const seen: string[] = [];
    const walk = (nodes: { record: PersonalRecord; children: never[] | typeof tree.roots }[]) => nodes.forEach((node) => { seen.push(title(node.record)); walk(node.children); });
    walk(tree.roots);
    expect(seen.sort()).toEqual(['Self', 'X', 'Y']);
  });
  it('knows what lies beneath a page', () => {
    const tree = buildTree([a, b, c, d], 'parent', title);
    expect([...descendantIds(tree, a.id)].sort()).toEqual([b.id, c.id, d.id].sort());
    expect(descendantIds(tree, d.id).size).toBe(0);
  });
  it('treats a collection with no parent field as a flat list', () => {
    expect(names(buildTree([a, b], '', title).roots)).toEqual(['Alpha', 'beta']);
  });
  it('finds the pages that link to a page by title, ignoring case', () => {
    const linker = page('l', 'Linker', undefined, 'See [[alpha]] and more');
    const other = page('m', 'Other', undefined, 'Nothing here');
    const self = page('a2', 'Alpha', undefined, 'I mention [[Alpha]] myself');
    expect(pagesLinkingTo([linker, other, self], 'body', 'Alpha', self.id).map(title)).toEqual(['Linker']);
    expect(pagesLinkingTo([linker], 'body', 'Untitled', '')).toEqual([]);
  });
});
