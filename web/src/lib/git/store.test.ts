import { describe, expect, it, vi } from 'vitest';

const calls = vi.hoisted(() => ({ health: 0 }));

vi.mock('../api', () => ({
  ApiError: class extends Error {},
  api: {
    git: {
      overview: () => new Promise(() => {}),
      pullRequests: () => new Promise(() => {}),
      health: () => {
        calls.health++;
        return Promise.reject(new Error('repository health is not enabled'));
      },
    },
  },
}));

import { gitStore } from './store.svelte';

describe('repository health', () => {
  it('is not asked for again and again after it failed while a screen keeps looking', async () => {
    const store = gitStore('prj_health');
    store.watch();
    await vi.waitFor(() => expect(store.healthError).not.toBe(''));
    for (let i = 0; i < 5; i++) store.watch();
    await new Promise((r) => setTimeout(r, 10));
    expect(calls.health).toBe(1);
  });
});
