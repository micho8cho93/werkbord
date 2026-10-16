import { describe, expect, it } from 'vitest';
import type { ControllerEvent, Run } from '../types';
import { mergeEvents, workflowSteps, workflowRecovery, workflowStatus } from './workflow-progress';
const run = (state: Run['state']) => ({id:'run',state} as Run);
const event = (seq: number, type: string, payload?: unknown): ControllerEvent => ({seq,type,payload,createdAt:'2026-01-01',runId:'run'});
const output = (seq: number, text: string, stream = 'assistant') => event(seq,'agent.output',{stream,text});
describe('durable workflow progress', () => {
  it('advances only on real lifecycle and explicit stage reports, including chunked lines', () => {
    const events = [event(1,'agent.started'),output(2,'WERKBORD_STA'),output(3,'GE 3 completed\nWERKBORD_STAGE 4 running\n'),output(4,'Verified a failing test\n','tool')];
    const steps = workflowSteps(run('running'),events);
    expect(steps.map(s => s.status)).toEqual(['Completed','Completed','Completed','Running','Pending','Pending']);
    expect(steps[3].output.join('')).toContain('Verified a failing test');
    expect(workflowSteps(run('running'),[event(1,'agent.started'),output(2,'WERKBORD_STAGE 6 completed\n','tool')])[5].status).toBe('Pending');
  });
  it('replays unordered overlapping events without duplicate evidence', () => {
    const events = [event(1,'agent.started'),output(2,'WERKBORD_STAGE 3 completed\nWERKBORD_STAGE 4 running\n')];
    expect(workflowSteps(run('running'),mergeEvents(events,[events[1],events[0]]))).toEqual(workflowSteps(run('running'),events));
    expect(mergeEvents(events,events)).toHaveLength(2);
  });
  it('preserves completed results on cancellation and identifies the failing stage', () => {
    const events = [output(1,'WERKBORD_STAGE 3 completed\nWERKBORD_STAGE 4 running\n'),event(2,'agent.failed',{reason:'scan failed'})];
    const stopped = workflowSteps(run('stopped'),events);
    expect(stopped.map(s=>s.status)).toEqual(['Completed','Completed','Completed','Cancelled','Cancelled','Cancelled']);
    const failed = workflowSteps(run('failed'),events);
    expect(failed[3].status).toBe('Failed');
    expect(failed[3].output.join('')).toContain('scan failed');
  });
  it('distinguishes approval from other pauses and confirms only reported success', () => {
    const approval = event(2,'agent.question',{question:{kind:'approval',prompt:'Run scanner?'}});
    expect(workflowSteps(run('waiting_for_user'),[event(1,'agent.started'),approval])[2].status).toBe('Waiting for approval');
    expect(workflowSteps(run('completed'),[])[5].status).toBe('Pending');
    const events = [3,4,5,6].map((s,i)=>output(i+1,`WERKBORD_STAGE ${s} completed\n`));
    expect(workflowSteps(run('completed'),events).every(s=>s.status==='Completed')).toBe(true);
    expect(workflowStatus(run('completed'))).toBe('Completed');
  });
  it('provides actionable recovery for each execution error category', () => {
    for (const [error, action] of [['runner offline','Reconnect'],['agent unavailable','Install'],['unsupported model','Choose'],['repository access failed','checkout'],['authentication failure','sign-in'],['command failed','command output']]) expect(workflowRecovery(error)).toContain(action);
  });
});

it('handles provider message boundaries and keeps workflow-specific labels', () => {
  const events = [output(1,'Investigation begins.'),output(2,'WERKBORD_STAGE 3 completed'),output(3,'WERKBORD_STAGE 4 running')];
  const steps = workflowSteps(run('running'),events,{id:'summary',button:'sprint-summary',output:'report'});
  expect(steps[2].status).toBe('Completed');
  expect(steps[3].status).toBe('Running');
  expect(steps[2].name).toContain('Git activity');
  expect(steps[4].name).not.toContain('create draft tasks');
});
