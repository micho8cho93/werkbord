import { describe, expect, it } from 'vitest';
import { agentUses, crumbs, participants, personalGit, runningCount, teamGit } from './context-header';
import type { AgentRef, Entry, Overview, Summary } from './types';

const NOW = Date.parse('2026-11-02T12:00:00Z');
const at = (minutes: number) => new Date(NOW - minutes * 60_000).toISOString();
const personal: Entry = { id: 'personal', kind: 'personal', name: 'Personal', state: 'ready' };
const team: Entry = { id: 'team:main', kind: 'team', name: 'Acme', state: 'ready' };
const agent = (state: AgentRef['state'], fresh: boolean, id = 'codex'): AgentRef => ({ id, state, observedAt: at(1), fresh });
const summary = (workspace: Entry, parts: Partial<Summary>): Summary => ({ schema: 'werkbord.workspace/v1', workspace, projects: [], work: [], attention: [], schedule: [], at: at(0), ...parts });
const overview = (...entries: Overview['entries']): Overview => ({ entries, at: at(0) });

describe('agents in the header', () => {
  it('counts only fresh running reports, never stale or merely starting ones', () => {
    const o = overview({ workspace: personal, summary: summary(personal, { work: [
      { id: 'a', title: 'A', project: 'App', status: 'doing', href: '#/a', execution: 'running', agent: agent('running', true) },
      { id: 'b', title: 'B', project: 'App', status: 'doing', href: '#/b', execution: 'running', agent: agent('running', false) },
      { id: 'c', title: 'C', project: 'App', status: 'doing', href: '#/c', execution: 'starting', agent: agent('starting', true, 'claude-code') },
    ] }) });
    const uses = agentUses(o);
    expect(uses).toHaveLength(3);
    expect(runningCount(uses)).toBe(1);
  });

  it('does not show an agent for work whose report is marked stale, or for a workspace that cannot answer', () => {
    const o = overview(
      { workspace: personal, summary: summary(personal, { work: [{ id: 'a', title: 'A', project: 'App', status: 'doing', href: '#/a', agent: agent('running', true), report: { stale: true } }] }) },
      { workspace: { ...team, state: 'offline' }, summary: summary(team, { queue: [{ id: 'q', title: 'Q', project: 'P', href: '?x=1', group: 'now', agent: agent('running', true) }] }) },
    );
    expect(agentUses(o)).toEqual([]);
  });

  it('shows a teammate\'s agent from the queue and keeps one entry per task', () => {
    const o = overview({ workspace: team, summary: summary(team, {
      work: [{ id: 'w', title: 'Mine', project: 'P', status: 'doing', href: '?tab=work&ticket=1', agent: agent('running', true) }],
      queue: [
        { id: '1', title: 'Mine', project: 'P', href: '?tab=work&ticket=1', group: 'now', agent: agent('running', true) },
        { id: '2', title: 'Maya\'s', project: 'P', href: '?tab=work&ticket=2', group: 'now', owner: 'Maya', agent: { state: 'running', observedAt: at(1), fresh: true } },
        { id: 'p', title: 'A plan', project: 'P', href: '?plan=1', group: 'plan' },
      ] }) });
    const uses = agentUses(o);
    expect(uses.map(u => u.href).sort()).toEqual(['?tab=work&ticket=1', '?tab=work&ticket=2']);
    expect(uses).toHaveLength(2);
    expect(uses.find(u => u.href.endsWith('ticket=2'))?.agentId).toBeUndefined();
    expect(runningCount(uses)).toBe(2);
  });
});

describe('participants', () => {
  const people = [{ id: 'm1', name: 'Maya', role: 'owner' }, { id: 'm2', name: 'Lucas', role: 'member' }, { id: 'm3', name: 'Sam', role: 'member' }, { id: 'm4', name: 'Sam', role: 'reviewer' }];
  const o = overview({ workspace: team, summary: summary(team, {
    projects: [{ id: 'p', name: 'P', href: '?x', people }],
    queue: [
      { id: '1', title: 'T1', project: 'P', href: '?a', group: 'now', owner: 'Maya' },
      { id: '2', title: 'T2', project: 'P', href: '?b', group: 'next', owner: 'Maya' },
      { id: '3', title: 'T3', project: 'P', href: '?c', group: 'done', owner: 'Maya' },
      { id: '4', title: 'T4', project: 'P', href: '?d', group: 'now', owner: 'Sam' },
    ] }) });
  it('lists people by role with what is assigned to them', () => {
    const list = participants(o);
    expect(list.map(p => [p.name, p.role, p.assigned])).toEqual([['Maya', 'owner', 2], ['Lucas', 'member', 0], ['Sam', 'member', undefined], ['Sam', 'reviewer', undefined]]);
  });
  it('has no notion of presence', () => {
    for (const p of participants(o)) expect(Object.keys(p).sort()).toEqual(['assigned', 'id', 'key', 'name', 'role', 'workspace'].filter(k => k in p).sort());
    expect(JSON.stringify(participants(o))).not.toMatch(/online|offline|available|lastSeen|presence/i);
  });
  it('names nobody for Personal', () => {
    expect(participants(overview({ workspace: personal, summary: summary(personal, { projects: [{ id: 'p', name: 'P', href: '#', people }] }) }))).toEqual([]);
  });
});

describe('git in the header', () => {
  it('says the branch and short commit of a Personal project, and nothing when there is none', () => {
    expect(personalGit({ branch: 'main', commit: 'a1b2c3d' })?.text).toBe('main · a1b2c3d');
    expect(personalGit({ branch: '', commit: 'a1b2c3d', detached: true })?.text).toBe('detached · a1b2c3d');
    expect(personalGit({ branch: '', commit: '', unborn: true })).toBeNull();
    expect(personalGit(null)).toBeNull();
  });
  it('keeps the provenance of a Team\'s reported push', () => {
    const o = overview({ workspace: team, summary: summary(team, { pushes: [
      { id: '1', key: 'WB-1', project: 'P', branch: 'old', sha: '1111111aaaa', href: '?tab=work&project=p&ticket=1', at: at(30) },
      { id: '2', key: 'WB-2', project: 'P', branch: 'feature/auth', sha: 'a1b2c3d4e5f', href: '?tab=work&project=p&ticket=2', at: at(3) },
      { id: '3', key: 'WB-3', project: 'Q', branch: 'other', sha: '9999999', href: '?tab=work&project=q&ticket=3', at: at(1) },
    ] }) });
    expect(teamGit(o, 'p', NOW)).toEqual({ text: 'feature/auth · a1b2c3d', provenance: 'reported 3m ago · unverified' });
    expect(teamGit(o, 'none', NOW)).toBeNull();
  });
});

describe('breadcrumb depth', () => {
  const base = { context: 'All Projects', team: false, onGit: false };
  it('goes from the project context to what is open', () => {
    expect(crumbs({ ...base, page: 'inbox', place: '' }).map(c => c.label)).toEqual(['All Projects', 'Work']);
    expect(crumbs({ ...base, page: 'workspace', place: '#/p/p1/work?plan=pl1', title: 'Authentication overhaul' })).toEqual([{ label: 'All Projects' }, { label: 'Work' }, { label: 'Plans' }, { label: 'Authentication overhaul', title: true }]);
    expect(crumbs({ ...base, context: 'App', page: 'workspace', place: '#/p/p1/task/t1', title: 'Fix login' }).map(c => c.label)).toEqual(['App', 'Work', 'Task', 'Fix login']);
    expect(crumbs({ ...base, page: 'workspace', place: '#/p/p1/work?section=plans' }).map(c => c.label)).toEqual(['All Projects', 'Work', 'Plans']);
  });
  it('shows no title the frame did not send, and the kind alone then', () => {
    expect(crumbs({ ...base, page: 'workspace', place: '#/p/p1/work?plan=pl1' }).map(c => c.label)).toEqual(['All Projects', 'Work', 'Plans']);
  });
  it('reads a Team place and leaves its other tabs out of Work', () => {
    const team = { ...base, team: true };
    expect(crumbs({ ...team, page: 'workspace', place: '?tab=work&project=p&plan=pl', title: 'Launch' }).map(c => c.label)).toEqual(['All Projects', 'Work', 'Plans', 'Launch']);
    expect(crumbs({ ...team, page: 'workspace', place: '?tab=people' }).map(c => c.label)).toEqual(['All Projects']);
    expect(crumbs({ ...team, page: 'workspace', place: '?tab=work&project=p&ticket=t1' }).map(c => c.label)).toEqual(['All Projects', 'Work', 'Task']);
  });
  it('names the other pages', () => {
    expect(crumbs({ ...base, page: 'agents', place: '' }).map(c => c.label)).toEqual(['All Projects', 'Agents']);
    expect(crumbs({ ...base, page: 'workspace', place: '#/p/p/git', onGit: true }).map(c => c.label)).toEqual(['All Projects', 'Git']);
  });
});
