import { beforeEach, describe, expect, it, vi } from 'vitest';
import { notifyEvent } from './notifications';
import type { ControllerEvent } from './types';

const shown: { title: string; options: NotificationOptions }[] = [];

beforeEach(() => {
  shown.length = 0;
  const memory = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => memory.get(key) ?? null,
    setItem: (key: string, value: string) => memory.set(key, value),
    removeItem: (key: string) => memory.delete(key),
  });
  vi.stubGlobal('document', { hidden: true });
  vi.stubGlobal('window', { focus: vi.fn() });
  vi.stubGlobal('Notification', class {
    static permission = 'granted';
    constructor(title: string, options: NotificationOptions) { shown.push({ title, options }); }
  });
  localStorage.setItem('werkbord.notifications.enabled', 'true');
});

const event = (type: string, seq: number, payload?: unknown): ControllerEvent => ({ seq, type, payload, createdAt: new Date(0).toISOString() });

describe('browser notifications', () => {
  it('notifies high-value events once and stays quiet for ordinary output', () => {
    notifyEvent(event('agent.output', 1), 'App', 'Task');
    notifyEvent(event('agent.question', 2), 'App', 'Fix login');
    notifyEvent(event('agent.question', 2), 'App', 'Fix login');
    expect(shown).toHaveLength(1);
    expect(shown[0].title).toBe('Needs you');
    expect(shown[0].options.body).toBe('App · Fix login');
  });

  it('only alerts on newly opened repository risks', () => {
    notifyEvent(event('git.health_changed', 3, { opened: 1, summary: { headline: '1 risk', counts: { risk: 1 } } }), 'App', 'Task');
    notifyEvent(event('git.health_changed', 4, { opened: 2, summary: { headline: '2 items need attention', counts: { attention: 2 } } }), 'App', 'Task');
    expect(shown.map((item) => item.title)).toEqual(['Repository needs attention']);
  });
});

describe('alerts inside the desktop window', () => {
  it('tells the window what happened, whatever the page’s own permission or visibility, and shows nothing itself', () => {
    const posted: unknown[] = [];
    const parent = { postMessage: (m: unknown) => posted.push(m) };
    vi.stubGlobal('document', { hidden: false });
    vi.stubGlobal('window', { focus: vi.fn(), parent, postMessage: vi.fn() });
    localStorage.removeItem('werkbord.notifications.enabled');
    notifyEvent(event('agent.output', 1), 'App', 'Task');
    notifyEvent(event('agent.question', 2), 'App', 'Fix login');
    notifyEvent(event('agent.failed', 3), 'App', 'Fix login');
    expect(shown).toHaveLength(0);
    expect(posted).toEqual([
      { type: 'werkbord.frame', event: 'notify', key: 'seq-2', title: 'Needs you', body: 'App · Fix login', urgent: true },
      { type: 'werkbord.frame', event: 'notify', key: 'seq-3', title: 'Agent failed', body: 'App · Fix login', urgent: false },
    ]);
  });
});
