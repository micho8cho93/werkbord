import { render } from 'svelte/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
vi.hoisted(() => {
  vi.stubGlobal('location', {hash:'',search:''});
  vi.stubGlobal('window', {addEventListener() {}, removeEventListener() {}});
});
import RunSessionView from './RunSession.svelte';
import { RunSession } from './run-session.svelte';
import { app } from '../state.svelte';
import type { Run } from '../types';
afterEach(() => { app.questions = []; });
describe('session Svelte surfaces', () => {
  it('shows ended activity and context without a process input field', () => {
    const session = new RunSession('project-a','run-a');
    session.run = {id:'run-a',projectId:'project-a',taskId:'task-a',agentId:'codex',state:'completed',branch:'isolated-branch'} as Run;
    session.worktree = {id:'w',projectId:'project-a',path:'/work/isolated',branch:'isolated-branch',baseRef:'main',state:'active'};
    const html = render(RunSessionView,{props:{session}}).body;
    expect(html).toContain('/work/isolated'); expect(html).toContain('codex'); expect(html).toContain('Return to task');
    expect(html).toContain('This session has ended'); expect(html).not.toContain('<textarea');
  });
});
