import { afterEach, describe, expect, it, vi } from 'vitest';
import { RunSession } from './run-session.svelte';
import { api } from '../api';
import { app } from '../state.svelte';
import type { ControllerEvent, Run } from '../types';
const run = (id = 'run-a', state: Run['state'] = 'running'): Run => ({id, projectId:'project-a',taskId:'task-a',state,version:1} as Run);
afterEach(() => vi.restoreAllMocks());
describe('attached run session', () => {
  it('routes instructions to the existing run and rejects input to a completed process', async () => {
    const session = new RunSession('project-a', 'run-a'); session.run = run(); session.draft = 'Do this';
    const send = vi.spyOn(api, 'sendInput').mockResolvedValue(run());
    const start = vi.spyOn(api, 'startRun');
    await session.act('send');
    expect(send).toHaveBeenCalledExactlyOnceWith('project-a','run-a','Do this');
    expect(start).not.toHaveBeenCalled();
    session.run = run('run-a','completed'); session.draft = 'Too late';
    await session.act('send'); await session.act('stop');
    expect(send).toHaveBeenCalledTimes(1); expect(session.draft).toBe('Too late');
  });
  it('recovers after reconnect, filters cross-project events and never stops a run when detached', async () => {
    let event!: (ev: ControllerEvent) => void, reconnect!: () => void;
    const unwatch = vi.fn(), unreconnect = vi.fn();
    vi.spyOn(app,'watchRun').mockImplementation((_id, fn) => { event = fn; return unwatch; });
    vi.spyOn(app,'onReconnect').mockImplementation(fn => { reconnect = fn; return unreconnect; });
    const get = vi.spyOn(api,'getRun').mockResolvedValue(run());
    vi.spyOn(api,'runEvents').mockResolvedValue({events:[],hasMore:false});
    const stop = vi.spyOn(api,'stopRun');
    const session = new RunSession('project-a','run-a'); session.attach(); session.attach();
    await vi.waitFor(() => expect(session.run?.state).toBe('running'));
    event({runId:'run-a',projectId:'other',payload:{run:run('run-a','stopped')}} as ControllerEvent);
    expect(session.run?.state).toBe('running');
    reconnect(); await vi.waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    session.disconnect(); expect(unwatch).toHaveBeenCalledTimes(1); expect(unreconnect).toHaveBeenCalledTimes(1); expect(stop).not.toHaveBeenCalled();
  });
  it('does not bypass questions or finish an active request', async () => {
    const session = new RunSession('project-a','run-a'); session.run = {...run('run-a','waiting_for_user'),waiting:'question'};
    session.draft = 'Approve everything';
    const send = vi.spyOn(api,'sendInput'), finish = vi.spyOn(api,'finishRun'), answer = vi.spyOn(api,'answerQuestion');
    await session.act('send'); await session.act('finish');
    expect(send).not.toHaveBeenCalled(); expect(finish).not.toHaveBeenCalled(); expect(answer).not.toHaveBeenCalled();
  });
});
