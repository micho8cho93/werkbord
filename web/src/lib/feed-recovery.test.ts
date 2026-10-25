import { afterEach, expect, it, vi } from 'vitest';
import { RunFeed } from './feed.svelte';
import { api } from './api';
import type { ControllerEvent } from './types';
const event = (seq: number): ControllerEvent => ({seq,runId:'r',projectId:'p',type:'agent.output',createdAt:'2026-01-01T00:00:00Z',payload:{stream:'assistant',text:`line ${seq}`}} as ControllerEvent);
afterEach(() => vi.restoreAllMocks());
it('recovers all missing pages after reconnect and deduplicates concurrent events', async () => {
  const get = vi.spyOn(api,'runEvents').mockResolvedValueOnce({events:[event(1)],hasMore:false})
    .mockResolvedValueOnce({events:[event(5),event(6)],hasMore:true})
    .mockResolvedValueOnce({events:[event(3),event(4)],hasMore:true})
    .mockResolvedValueOnce({events:[event(1),event(2)],hasMore:false});
  const feed = new RunFeed('p','r'); await feed.load();
  const loading = feed.load(); feed.push(event(6)); await loading;
  expect(get.mock.calls.map(c => c[2])).toEqual([undefined,undefined,5,3]);
  const text = feed.items.filter(i => i.kind === 'message').map(i => i.text).join('\n');
  for (let i=1;i<=6;i++) expect(text.match(new RegExp(`line ${i}`, 'g'))).toHaveLength(1);
});
