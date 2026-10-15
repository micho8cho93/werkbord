import { describe, expect, it } from 'vitest';
import type { Run, Task } from '../types';
import { exampleStage, stageText } from './hello';

const task = (state: Task['state']) => ({ id: 't', state }) as Task;
const run = (state: Run['state'], extra: Partial<Run> = {}) => ({ id: 'r', state, ...extra }) as Run;

describe('exampleStage', () => {
  it('walks the ticket from nothing to done', () => {
    expect(exampleStage(undefined, undefined)).toBe('no-ticket');
    expect(exampleStage(task('backlog'), undefined)).toBe('not-started');
    expect(exampleStage(task('doing'), run('starting'))).toBe('working');
    expect(exampleStage(task('doing'), run('running'))).toBe('working');
    expect(exampleStage(task('doing'), run('waiting_for_user'))).toBe('needs-you');
    expect(exampleStage(task('doing'), run('blocked'))).toBe('needs-you');
    expect(exampleStage(task('review'), run('completed', { branch: 'werkbord/x' }))).toBe('review');
    expect(exampleStage(task('done'), run('completed', { branch: 'werkbord/x' }))).toBe('done');
  });
  it('does not call a run that changed nothing a success', () => {
    expect(exampleStage(task('doing'), run('completed'))).toBe('failed');
    expect(exampleStage(task('doing'), run('failed'))).toBe('failed');
    expect(exampleStage(task('doing'), run('stopped'))).toBe('failed');
  });
});

describe('stageText', () => {
  it('shows what the assistant is doing right now', () => {
    expect(stageText('working', run('running', { activity: 'Writing index.html' }))).toBe('Working: Writing index.html');
    expect(stageText('failed', run('completed'))).toContain('without making changes');
  });
});
