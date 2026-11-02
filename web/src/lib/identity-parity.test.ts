import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import AgentMark from './AgentMark.svelte';
import Avatar from './Avatar.svelte';
import AssigneeChip from './AssigneeChip.svelte';

// The standalone Team console draws the agent mark, avatar and assignment group with its own small builder
// (internal/team/console/static/agent-identity.js). It must give the same markup as the Svelte components, so identity.css styles
// both alike and both say the same thing to a screen reader. Same inputs, same HTML.

class Node_ {
  attrs: [string, string][] = [];
  kids: (Node_ | string)[] = [];
  constructor(public tag: string) {}
  setAttribute(k: string, v: string) { this.attrs.push([k, v]); }
  append(...c: (Node_ | string)[]) { this.kids.push(...c); }
  html(): string {
    const a = [...this.attrs].sort(([x], [y]) => x.localeCompare(y)).map(([k, v]) => ` ${k}="${v.replace(/"/g, '&quot;')}"`).join('');
    return `<${this.tag}${a}>${this.kids.map(k => (typeof k === 'string' ? k : k.html())).join('')}</${this.tag}>`;
  }
}

async function consoleBuilders() {
  // @ts-expect-error node's own types are not installed in this package; the test runs under node.
  const { readFileSync } = await import('node:fs');
  const root: Record<string, unknown> = {};
  const document = { createElement: (t: string) => new Node_(t), createElementNS: (_: string, t: string) => new Node_(t), createTextNode: (s: string) => s };
  for (const file of ['identity.js', 'agent-marks.js']) new Function('globalThis', readFileSync(new URL('../../../internal/uipresentation/static/' + file, import.meta.url), 'utf8'))(root);
  new Function('globalThis', 'document', readFileSync(new URL('../../../internal/team/console/static/agent-identity.js', import.meta.url), 'utf8'))(root, document);
  return root.WerkbordTeamIdentity as { agentMark(ref: object | null, o?: object): Node_; avatar(id: string, name: string, self: boolean, size?: string): Node_; assignment(ref: object | null, holder: object | null, role?: string, o?: object): Node_ };
}

// Svelte's server output carries hydration comments, template whitespace between flex children and its own attribute order; none of
// that is drawn, so compare the structure.
function normal(html: string): string {
  return html.replace(/<!--[\s\S]*?-->/g, '').replace(/<(\w+)((?:\s+[\w-]+(?:="[^"]*")?)*)\s*\/?>/g, (_m, tag: string, attrs: string) => {
    const list = [...attrs.matchAll(/\s+([\w-]+)(?:="([^"]*)")?/g)].map(([, k, v]) => [k, v ?? ''] as [string, string]);
    return `<${tag}${list.sort(([x], [y]) => x.localeCompare(y)).map(([k, v]) => ` ${k}="${v}"`).join('')}>`;
  }).replace(/>\s+</g, '><').trim();
}

const now = Date.parse('2026-11-02T12:00:00Z');
const at = '2026-11-02T11:54:00Z';
const ui = await consoleBuilders();

describe('the Team console draws what the app draws', () => {
  const cases = [
    { name: 'running', ref: { id: 'claude-code', state: 'running', observedAt: at, fresh: true } },
    { name: 'stale', ref: { id: 'claude-code', state: 'running', observedAt: at, fresh: false } },
    { name: 'letter tile, waiting', ref: { id: 'codex', state: 'needs_input', observedAt: at, fresh: true } },
    { name: 'neutral mark', ref: { id: '', state: 'running', observedAt: at, fresh: true } },
    { name: 'completed', ref: { id: 'gemini-cli', state: 'completed', observedAt: at, fresh: false } },
  ];
  for (const c of cases) {
    it('agent mark: ' + c.name, () => {
      const out = render(AgentMark, { props: { agentId: c.ref.id || undefined, state: c.ref.state, fresh: c.ref.fresh, observedAt: c.ref.observedAt, now } }).body;
      expect(normal(ui.agentMark(c.ref, { now }).html())).toBe(normal(out));
    });
  }
  it('avatar', () => {
    for (const self of [false, true]) {
      const out = render(Avatar, { props: { id: 'mem_1', name: 'Maya Maker', self } }).body;
      expect(normal(ui.avatar('mem_1', 'Maya Maker', self, 'md').html())).toBe(normal(out));
    }
  });
  it('assignment group', () => {
    const ref = cases[0].ref, holder = { id: 'mem_1', name: 'Maya Maker', self: false };
    const out = render(AssigneeChip, { props: { agentId: ref.id, state: ref.state, fresh: ref.fresh, observedAt: ref.observedAt, now, holder } }).body;
    expect(normal(ui.assignment(ref, holder, 'Held by', { now })!.html())).toBe(normal(out));
  });
});
