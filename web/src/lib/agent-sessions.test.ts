import { describe, expect, it } from 'vitest';
import { asTerminal, isSessionKey, ordered, parseSessionKey, runKey, stateLabel, terminalKey, unique, type AgentSession } from './agent-sessions';
import { hrefOf, parse, sessionsHref } from './location';
import { parseAssistantRequest } from './embed';
import { agentsPlace, isAgentsPlace } from '../shell/nav';

const session = (over: Partial<AgentSession>): AgentSession => ({
  key: 'run:run_a', kind: 'run', id: 'run_a', projectId: 'prj_a', agentId: 'codex', title: 'Fix', state: 'running', live: true,
  capabilities: { feed: true, message: true, terminal: false, questions: true, stop: true }, createdAt: '2026-10-30T09:00:00Z', updatedAt: '2026-10-30T09:00:00Z', ...over,
});

describe('a session key is exactly a kind and the owning record’s ID', () => {
  it('round-trips, and refuses anything else', () => {
    expect(runKey('run_k3j9')).toBe('run:run_k3j9');
    expect(terminalKey('00ff')).toBe('terminal:00ff');
    expect(parseSessionKey('run:run_k3j9')).toEqual({ kind: 'run', id: 'run_k3j9' });
    for (const bad of ['', 'run', 'run:', 'task:tsk_1', 'run:a/b', 'run:../x', 'run:a b', 'RUN:a', 'run:' + 'a'.repeat(97), null, 5, {}]) {
      expect(isSessionKey(bad), String(bad)).toBe(false);
    }
  });
});

describe('Agents is an address that names the session open in it', () => {
  it('parses and rebuilds #/sessions and #/sessions/<key>', () => {
    expect(parse('#/sessions')).toEqual({ view: 'sessions', projectId: '', taskId: '' });
    expect(parse(sessionsHref('run:run_a'))).toMatchObject({ view: 'sessions', sub: 'run:run_a' });
    expect(parse('#/sessions/terminal%3Aabc-1')).toMatchObject({ view: 'sessions', sub: 'terminal:abc-1' });
    expect(hrefOf({ view: 'sessions', projectId: '', taskId: '', sub: 'run:run_a' })).toBe('#/sessions/run%3Arun_a');
    expect(hrefOf({ view: 'sessions', projectId: '', taskId: '' })).toBe('#/sessions');
  });
  it('is the place the shell shows Agents at, and only that', () => {
    expect(agentsPlace()).toBe('#/sessions');
    expect(agentsPlace('run:run_a')).toBe('#/sessions/run%3Arun_a');
    for (const yes of ['#/sessions', '#/sessions/run%3Arun_a', '#/sessions?x=1']) expect(isAgentsPlace(yes), yes).toBe(true);
    for (const no of ['#/control', '#/p/prj_a/task/tsk_1', '#/sessionsx', '?tab=workspace', '']) expect(isAgentsPlace(no), no).toBe(false);
  });
});

describe('the list has one entry per execution, live and waiting first', () => {
  it('drops a repeated key however the list was assembled', () => {
    const a = session({}), b = session({ key: 'terminal:t1', kind: 'terminal', id: 't1' });
    expect(unique([a, b, { ...a, title: 'again' }, b]).map(s => s.key)).toEqual(['run:run_a', 'terminal:t1']);
  });
  it('orders needs-you, then running, then the most recent of the rest', () => {
    const done = session({ key: 'run:old', id: 'old', live: false, state: 'completed', updatedAt: '2026-10-30T08:00:00Z' });
    const newer = session({ key: 'run:new', id: 'new', live: false, state: 'failed', updatedAt: '2026-10-30T09:30:00Z' });
    const busy = session({ key: 'run:busy', id: 'busy' });
    const asks = session({ key: 'run:asks', id: 'asks', waiting: true, state: 'waiting_for_user' });
    expect(ordered([done, newer, busy, asks]).map(s => s.id)).toEqual(['asks', 'busy', 'new', 'old']);
  });
  it('says what a session is doing in the owner’s words', () => {
    expect(stateLabel(session({ waiting: true, state: 'waiting_for_user' }))).toBe('Needs you');
    expect(stateLabel(session({ state: 'waiting_for_user', waiting: false }))).toBe('Waiting for user');
    expect(stateLabel(session({ kind: 'terminal', state: 'interrupted' }))).toBe('Interrupted');
  });
  it('gives the one terminal renderer the record it already knows', () => {
    const t = asTerminal(session({ key: 'terminal:t1', kind: 'terminal', id: 't1', device: 'rnr_1', repository: 'App', state: 'running' }));
    expect(t).toMatchObject({ id: 't1', projectId: 'prj_a', agentId: 'codex', device: 'rnr_1', repository: 'App', state: 'running' });
  });
});

describe('Salie can be asked to open a conversation by its ID and nothing else', () => {
  it('accepts an ID with an open request and refuses everything else', () => {
    expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', conversation: 'ast_k3j9x2m4q7p1a8z5' })).toMatchObject({ action: 'open', conversation: 'ast_k3j9x2m4q7p1a8z5' });
    for (const bad of [{ action: 'close', conversation: 'ast_a' }, { action: 'open', conversation: 'a b' }, { action: 'open', conversation: '' }, { action: 'open', conversation: 5 }, { action: 'open', conversation: 'a'.repeat(81) }, { action: 'open', conversation: 'ast_a', catchUpPrompt: 'x' }]) {
      expect(parseAssistantRequest({ type: 'werkbord.assistant', ...bad }), JSON.stringify(bad)).toBeNull();
    }
  });
});
