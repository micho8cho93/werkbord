import { describe, expect, it } from 'vitest';
import { matchesScheduledExecution } from './control';
import type { Task } from './types';

describe('Control Center scheduled execution filters', () => {
  const task = (execution = {}): Task => ({ execution } as Task);
  it('uses inherited assignment and agent while respecting task overrides', () => {
    expect(matchesScheduledExecution(task(), {runner:'rnr_desktop',agent:'codex'}, {}, 'rnr_laptop', 'codex')).toBe(false);
    expect(matchesScheduledExecution(task({runner:'automatic',agent:'claude-code'}), {runner:'rnr_desktop',agent:'codex'}, {}, 'rnr_laptop', 'claude-code')).toBe(true);
    expect(matchesScheduledExecution(task(), {}, {agent:'claude-code'}, '', 'codex')).toBe(false);
  });
  it('keeps unknown automatic work visible without pretending it has an owner', () => {
    expect(matchesScheduledExecution(task(), {}, {}, 'rnr_desktop', 'codex')).toBe(true);
  });
});
