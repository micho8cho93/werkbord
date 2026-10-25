import { render } from 'svelte/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
vi.hoisted(() => {
  vi.stubGlobal('location', {hash:'',search:''});
  vi.stubGlobal('window', {addEventListener() {}, removeEventListener() {}});
});
import PendingInteraction from './PendingInteraction.svelte';
import RunSessionView from './RunSession.svelte';
import { RunSession } from './run-session.svelte';
import { assistant } from './sessions.svelte';
import { app } from '../state.svelte';
import type { Question, Run } from '../types';
afterEach(() => { app.questions = []; });
const question = (id: string): Question => ({id,runId:`run-${id}`,projectId:'project-a',taskId:'task-a',kind:'approval',prompt:`Permission ${id}?`,state:'pending',askedAt:'2026-01-01T00:00:00Z',allowFreeText:false,options:['Allow','Deny']});
describe('session and compact Svelte surfaces', () => {
  it('renders exactly one of four requests with its authorised choices and correct pending count', () => {
    app.questions = ['a','b','c','d'].map(question);
    const html = render(PendingInteraction).body;
    expect(html).toContain('4 pending'); expect(html).toContain('Permission a?');
    expect(html).not.toContain('Permission b?'); expect(html).toContain('Allow'); expect(html).toContain('Deny');
    app.questions = app.questions.slice(1);
    expect(assistant.waiting).toBe(3);
    expect(render(PendingInteraction).body).toContain('Permission b?');
  });
  it('shows ended activity and context without a process input field', () => {
    const session = new RunSession('project-a','run-a');
    session.run = {id:'run-a',projectId:'project-a',taskId:'task-a',agentId:'codex',state:'completed',branch:'isolated-branch'} as Run;
    session.worktree = {id:'w',projectId:'project-a',path:'/work/isolated',branch:'isolated-branch',baseRef:'main',state:'active'};
    const html = render(RunSessionView,{props:{session}}).body;
    expect(html).toContain('/work/isolated'); expect(html).toContain('codex'); expect(html).toContain('Return to task');
    expect(html).toContain('This session has ended'); expect(html).not.toContain('<textarea');
  });
});
