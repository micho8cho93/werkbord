import type { PersonalRecord } from '../schema';
import { untitled } from './shared';

export interface TreeNode { record: PersonalRecord; children: TreeNode[]; parentId: string }
export interface PageTree {
  roots: TreeNode[];
  nodes: Map<string, TreeNode>;
  /** The page and the pages above it, top first. */
  trail(id: string): PersonalRecord[];
}

export function pageTitle(record: PersonalRecord, titleKey: string): string {
  const value = record.values[titleKey];
  return typeof value === 'string' && value.trim() ? value : untitled;
}

/**
 * Pages nest by the parent each keeps. A parent that is gone, or one that would make a page its own ancestor, leaves the page
 * at the top level: a tree is always drawn, and no page is ever hidden by a bad link.
 */
export function buildTree(records: PersonalRecord[], parentKey: string, title: (record: PersonalRecord) => string): PageTree {
  const nodes = new Map<string, TreeNode>();
  for (const record of records) {
    const wanted = parentKey ? record.values[parentKey] : undefined;
    nodes.set(record.id, { record, children: [], parentId: typeof wanted === 'string' && wanted !== record.id ? wanted : '' });
  }
  for (const node of nodes.values()) if (node.parentId && !nodes.has(node.parentId)) node.parentId = '';
  for (const start of nodes.values()) {
    const path: TreeNode[] = [];
    const seen = new Set<string>();
    let at: TreeNode | undefined = start;
    while (at && at.parentId && !seen.has(at.record.id)) { seen.add(at.record.id); path.push(at); at = nodes.get(at.parentId); }
    if (at && seen.has(at.record.id)) {
      const cycle = path.slice(path.indexOf(at));
      cycle.reduce((a, b) => (a.record.id < b.record.id ? a : b)).parentId = '';
    }
  }
  const order = (a: TreeNode, b: TreeNode) => title(a.record).localeCompare(title(b.record), undefined, { numeric: true, sensitivity: 'base' }) || a.record.createdAt.localeCompare(b.record.createdAt) || a.record.id.localeCompare(b.record.id);
  const roots: TreeNode[] = [];
  for (const node of nodes.values()) (node.parentId ? nodes.get(node.parentId)!.children : roots).push(node);
  roots.sort(order);
  for (const node of nodes.values()) node.children.sort(order);
  return {
    roots, nodes,
    trail(id) {
      const found: PersonalRecord[] = [];
      for (let at = nodes.get(id); at && found.length < 64; at = at.parentId ? nodes.get(at.parentId) : undefined) found.unshift(at.record);
      return found;
    },
  };
}

/** Every page beneath a page: what it cannot be moved under. */
export function descendantIds(tree: PageTree, id: string): Set<string> {
  const found = new Set<string>();
  const visit = (node: TreeNode | undefined) => { for (const child of node?.children ?? []) if (!found.has(child.record.id)) { found.add(child.record.id); visit(child); } };
  visit(tree.nodes.get(id));
  return found;
}

/** The pages that mention a page by its title in a [[link]], other than the page itself. */
export function pagesLinkingTo(records: PersonalRecord[], bodyKey: string, title: string, selfId: string): PersonalRecord[] {
  const needle = `[[${title.toLocaleLowerCase()}]]`;
  if (!bodyKey || title === untitled) return [];
  return records
    .filter((r) => r.id !== selfId && typeof r.values[bodyKey] === 'string' && (r.values[bodyKey] as string).toLocaleLowerCase().includes(needle))
    .sort((a, b) => a.id.localeCompare(b.id));
}
