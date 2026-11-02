import { beforeAll, describe, expect, it } from 'vitest';
import { AGENT_MARKS, agentName } from './agent-marks';

// The standalone Team console draws the same marks from a classic script. The two copies must not drift.
describe('agent-marks.js (the Team console copy)', () => {
  const root: { WerkbordAgentMarks?: { MARKS: typeof AGENT_MARKS; agentName(id?: string, name?: string): string } } = {};
  beforeAll(async () => {
    // @ts-expect-error node's own types are not installed in this package; the test runs under node.
    const { readFileSync } = await import('node:fs');
    new Function('globalThis', readFileSync(new URL('../../../internal/uipresentation/static/agent-marks.js', import.meta.url), 'utf8'))(root);
  });
  const copy = { get MARKS() { return root.WerkbordAgentMarks!.MARKS; }, agentName: (id?: string, name?: string) => root.WerkbordAgentMarks!.agentName(id, name) };

  it('has exactly the marks the app has', () => {
    expect(copy.MARKS).toEqual(AGENT_MARKS);
  });

  it('names agents the same way', () => {
    for (const id of [...Object.keys(AGENT_MARKS), 'codex', 'some-new-agent', undefined]) expect(copy.agentName(id)).toBe(agentName(id));
    expect(copy.agentName('codex', ' Custom ')).toBe(agentName('codex', ' Custom '));
  });
});
