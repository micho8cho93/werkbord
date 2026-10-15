import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

beforeEach(() => vi.resetModules());
afterEach(() => vi.unstubAllGlobals());

async function as(token: string) {
  vi.stubGlobal('window', { localStorage: { getItem: () => token, setItem: () => {}, removeItem: () => {} } });
  const bodies: Record<string, unknown>[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_u: string, init?: RequestInit) => {
    bodies.push(JSON.parse((init?.body as string) ?? '{}'));
    return new Response('{}', { status: 200 });
  }));
  const { api } = await import('../api');
  const { isPhone } = await import('./capability');
  return { api, isPhone, bodies };
}

const choice = { agentId: 'claude', model: 'm', reasoning: 'high', runnerId: 'r1', interaction: 'autonomous' } as const;

describe('what a phone does not choose', () => {
  it('knows a phone by its credential', async () => {
    expect((await as('wbp_abc')).isPhone()).toBe(true);
    expect((await as('owner-token')).isPhone()).toBe(false);
    expect((await as('')).isPhone()).toBe(false);
  });

  it('sends no execution settings when it creates, edits or starts', async () => {
    const { api, bodies } = await as('wbp_abc');
    await api.createTask('p1', 'T', 'd', { agent: 'claude' } as never);
    await api.editTask({ id: 't1', projectId: 'p1', version: 2 } as never, { execution: { agent: 'x' }, title: 'y' } as never);
    await api.startRun('p1', 't1', choice, 'careful', true);
    expect(bodies[0]).toEqual({ title: 'T', description: 'd' });
    expect(bodies[1]).toEqual({ title: 'y', version: 2 });
    expect(bodies[2]).toEqual({ instructions: 'careful', resume: true });
  });

  it('leaves everything in for the owner', async () => {
    const { api, bodies } = await as('owner-token');
    await api.startRun('p1', 't1', choice, '', false);
    expect(bodies[0]).toMatchObject({ agentId: 'claude', model: 'm', runnerId: 'r1', policy: { interaction: 'autonomous' } });
  });
});
