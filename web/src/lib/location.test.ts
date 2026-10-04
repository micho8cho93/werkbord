import { describe, expect, it } from 'vitest';
import { parse, taskHref } from './location';

describe('parse', () => {
  it('knows the three primary pages and defaults to the board', () => {
    expect(parse('#/board')).toEqual({ route: 'board', taskId: '' });
    expect(parse('#/control')).toEqual({ route: 'control', taskId: '' });
    expect(parse('#/git')).toEqual({ route: 'git', taskId: '' });
    expect(parse('')).toEqual({ route: 'board', taskId: '' });
    expect(parse('#/nonsense')).toEqual({ route: 'board', taskId: '' });
  });

  it('knows a task page, which belongs to the board', () => {
    expect(parse('#/task/tsk_abc123')).toEqual({ route: 'board', taskId: 'tsk_abc123' });
    expect(parse('#/task/tsk_abc?x=1')).toEqual({ route: 'board', taskId: 'tsk_abc' });
    expect(parse('#/task/')).toEqual({ route: 'board', taskId: '' });
  });

  it('survives a mangled task id', () => {
    expect(parse('#/task/%E0%A4%A')).toEqual({ route: 'board', taskId: '' });
  });

  it('round-trips ids through links', () => {
    expect(taskHref('tsk_x')).toBe('#/task/tsk_x');
    expect(parse(taskHref('tsk_a/b c')).taskId).toBe('tsk_a/b c');
  });
});
