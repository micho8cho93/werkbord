import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DraftSaver } from './draft.svelte';

describe('draft saving', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it('waits for a pause in typing and then saves the latest text once', async () => {
    const commit = vi.fn(async () => true);
    const saver = new DraftSaver(commit, 500);
    saver.push({ body: 'a' }); saver.push({ body: 'ab' }); saver.push({ title: 'T' });
    expect(saver.status).toBe('unsaved');
    await vi.advanceTimersByTimeAsync(499);
    expect(commit).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2);
    expect(commit).toHaveBeenCalledTimes(1);
    expect(commit).toHaveBeenCalledWith({ body: 'ab', title: 'T' });
    expect(saver.status).toBe('saved');
  });

  it('keeps the text when a save is refused and saves it with the next change', async () => {
    const commit = vi.fn(async (): Promise<boolean> => false);
    const saver = new DraftSaver(commit, 100);
    saver.push({ body: 'precious' });
    await vi.advanceTimersByTimeAsync(150);
    expect(saver.status).toBe('failed');
    expect(saver.dirty).toBe(true);
    commit.mockImplementation(async () => true);
    saver.push({ title: 'T' });
    await vi.advanceTimersByTimeAsync(150);
    expect(commit).toHaveBeenLastCalledWith({ body: 'precious', title: 'T' });
    expect(saver.status).toBe('saved');
  });

  it('saves text typed while a save is in flight, in order, and never twice at once', async () => {
    let release: (stored: boolean) => void = () => {};
    let active = 0, overlap = 0;
    const calls: unknown[] = [];
    const commit = vi.fn(async (patch: unknown) => {
      calls.push(patch);
      active++; overlap = Math.max(overlap, active);
      const stored = await new Promise<boolean>((resolve) => { release = resolve; });
      active--;
      return stored;
    });
    const saver = new DraftSaver(commit, 10);
    saver.push({ body: 'one' });
    const first = saver.flush();
    await Promise.resolve();
    saver.push({ body: 'two' });
    release(true);
    await vi.advanceTimersByTimeAsync(0);
    const done = saver.flush();
    await vi.advanceTimersByTimeAsync(0);
    release(true);
    await first; await done;
    expect(calls).toEqual([{ body: 'one' }, { body: 'two' }]);
    expect(overlap).toBe(1);
    expect(saver.status).toBe('saved');
  });

  it('flush resolves false when the text could not be stored', async () => {
    const saver = new DraftSaver(async () => false, 10);
    saver.push({ body: 'x' });
    expect(await saver.flush()).toBe(false);
    expect(await new DraftSaver(async () => true).flush()).toBe(true);
  });
});
