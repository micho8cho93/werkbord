import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ControllerEvent, Run } from '../types';
const mocks = vi.hoisted(() => ({history:vi.fn(), list:vi.fn(), events:vi.fn(), get:vi.fn(), stop:vi.fn(), watch:vi.fn()}));
vi.mock('../api', () => ({api:{listTaskRuns:mocks.list,runEvents:mocks.events,getRun:mocks.get,stopRun:mocks.stop}, request:vi.fn()}));
vi.mock('../state.svelte', () => ({app:{watchRun:mocks.watch}}));
vi.mock('./layout', () => ({deckApi:{history:mocks.history}}));
import { Workflows } from './workflows.svelte';
const run = (id: string, state: Run['state'] = 'running', version = 1) => ({id,taskId:id,projectId:'repo',createdAt:'2026-01-01',state,version} as Run);
const event = (seq: number, id: string, type = 'agent.output', payload: unknown = {stream:'assistant',text:'Evidence\n'}): ControllerEvent => ({seq,runId:id,type,payload,createdAt:'2026-01-01'});
beforeEach(() => {
  vi.resetAllMocks();
  mocks.history.mockResolvedValue(['one','two'].map(id=>({requestId:'request',slotId:'button',taskId:id,projectId:'repo'})));
  mocks.list.mockImplementation(async (_project,id)=>[run(id)]);
  mocks.get.mockImplementation(async (_project,id)=>run(id));
  mocks.events.mockImplementation(async (_project,id)=>({events:[event(1,id)],hasMore:false}));
  mocks.watch.mockReturnValue(vi.fn());
});
describe('workflow monitor recovery and cancellation', () => {
  it('restores simultaneous repositories and keeps stream subscriptions bound after history refresh', async () => {
    const monitor = new Workflows(); await monitor.refresh(); await monitor.refresh();
    expect(monitor.executions).toHaveLength(2);
    expect(mocks.watch).toHaveBeenCalledTimes(2);
    const push = mocks.watch.mock.calls[0][1];
    push(event(2,'one','run.state_changed',{run:run('one','completed',2)}));
    expect(monitor.executions[0].run?.state).toBe('completed');
    expect(monitor.executions[1].run?.state).toBe('running');
    monitor.dispose();
  });
  it('reconnects by reloading state and missing events without starting jobs', async () => {
    const monitor = new Workflows(); await monitor.refresh();
    mocks.events.mockImplementation(async (_project,id)=>({events:[event(2,id),event(1,id)],hasMore:false}));
    mocks.get.mockImplementation(async (_project,id)=>run(id,'completed',3));
    await monitor.refresh();
    expect(monitor.executions[0].events.map(e=>e.seq)).toEqual([1,2]);
    expect(monitor.executions[0].run?.state).toBe('completed');
    expect(mocks.watch).toHaveBeenCalledTimes(2);
  });
  it('propagates Stop and retains results when acknowledgement is lost', async () => {
    const monitor = new Workflows(); await monitor.refresh();
    mocks.stop.mockRejectedValue(new Error('connection interrupted'));
    await monitor.stop(monitor.executions[0]);
    expect(mocks.stop).toHaveBeenCalledWith('repo','one');
    expect(monitor.executions[0].error).toContain('Cancellation cannot be confirmed');
    expect(monitor.executions[0].events).toHaveLength(1);
    expect(monitor.executions[0].run?.state).toBe('running');
    mocks.stop.mockResolvedValue(run('one','stopped',2));
    await monitor.stop(monitor.executions[0]);
    expect(monitor.executions[0].run?.state).toBe('stopped');
  });
  it('pages full event history and deduplicates live events arriving during recovery', async () => {
    mocks.history.mockResolvedValue([{requestId:'r',taskId:'one',projectId:'repo',slotId:'b'}]);
    mocks.events.mockImplementation(async (_project,id,before)=>before ? {events:[event(1,id)],hasMore:false} : {events:[event(2,id)],hasMore:true});
    const monitor = new Workflows(); await monitor.refresh();
    mocks.watch.mock.calls[0][1](event(2,'one'));
    expect(monitor.executions[0].events.map(e=>e.seq)).toEqual([1,2]);
  });
  it('keeps partial failures alongside healthy concurrent executions', async () => {
    mocks.list.mockImplementation(async (_project,id)=>{if(id==='one') throw new Error('repository unavailable'); return [run(id)];});
    const monitor = new Workflows(); await monitor.refresh();
    expect(monitor.executions[0].error).toContain('repository unavailable');
    expect(monitor.executions[1].run?.state).toBe('running');
  });
});

it('stops all repositories of one invocation while retaining concurrent workflows', async () => {
  mocks.history.mockResolvedValue(['one','two','three'].map(id=>({requestId:id==='three'?'other':'group',slotId:'button',taskId:id,projectId:'repo'})));
  mocks.stop.mockImplementation(async (_project,id)=>run(id,'stopped',2));
  const monitor = new Workflows(); await monitor.refresh();
  await monitor.stopWorkflow('group');
  expect(mocks.stop).toHaveBeenCalledTimes(2);
  expect(monitor.executions.map(x=>x.run?.state)).toEqual(['stopped','stopped','running']);
});

it('discovers a task retry and replaces its old event subscription', async () => {
  const monitor = new Workflows(); await monitor.refresh();
  const previous = mocks.watch.mock.results[0].value;
  mocks.list.mockImplementation(async (_project,id)=>[run(id),{...run('retry'),createdAt:'2026-02-01'}]);
  mocks.get.mockImplementation(async ()=>run('retry'));
  await monitor.refresh();
  expect(previous).toHaveBeenCalled();
  expect(monitor.executions[0].run?.id).toBe('retry');
  expect(monitor.executions[0].events.every(e=>e.runId==='retry')).toBe(true);
});

it('recovers output missed before a terminal event when reconnecting', async () => {
  const monitor = new Workflows(); await monitor.refresh();
  mocks.watch.mock.calls[0][1](event(3,'one','run.state_changed',{run:run('one','completed',3)}));
  mocks.list.mockImplementation(async (_project,id)=>[run(id,'completed',3)]);
  mocks.get.mockImplementation(async (_project,id)=>run(id,'completed',3));
  mocks.events.mockImplementation(async (_project,id)=>({events:[event(2,id)],hasMore:false}));
  await monitor.refresh();
  expect(monitor.executions[0].events.map(e=>e.seq)).toEqual([1,2,3]);
});
