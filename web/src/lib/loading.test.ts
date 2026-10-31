import { describe, expect, it, vi } from 'vitest';
import { awaitAll } from './loading';
import { singleFlight } from './setup/check';

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(r => { resolve = r; });
  return { promise, resolve };
}

describe('loading follows outstanding work', () => {
  it('keeps waiting after an early failure until its sibling finishes', async () => {
    const slow = deferred<string>();
    const finished = vi.fn();
    const result = awaitAll([Promise.reject(new Error('offline')), slow.promise]).catch(finished);
    await Promise.resolve(); await Promise.resolve();
    expect(finished).not.toHaveBeenCalled();
    slow.resolve('ready');
    await result;
    expect(finished).toHaveBeenCalledWith(expect.objectContaining({ message: 'offline' }));
  });
  it('shares checks during polling and allows a fresh check afterwards', async () => {
    const slow = deferred<string>();
    const check = vi.fn(() => slow.promise);
    const load = singleFlight(check);
    const first = load();
    expect(load()).toBe(first);
    expect(check).toHaveBeenCalledTimes(1);
    slow.resolve('ready');
    await first;
    await load();
    expect(check).toHaveBeenCalledTimes(2);
  });
  it('returns results in request order', async () => {
    expect(await awaitAll([Promise.resolve('one'), Promise.resolve(2)])).toEqual(['one', 2]);
  });
});
