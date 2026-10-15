import { describe, expect, it } from 'vitest';
import { repoDigest, workingLine } from './repositories';
import type { GitHubState, GitOverview, RepositoryHealth } from '../types';

const overview = (over: Record<string, unknown> = {}) =>
  ({
    local: {
      head: { branch: 'main', commit: 'abcdef123456', detached: false, unborn: false },
      workingTree: { staged: [{}], unstaged: [{}, {}], untracked: [], conflicted: [], ahead: 2, behind: 0 },
    },
    summary: { needsYou: 1 },
    ...over,
  }) as unknown as GitOverview;
const health = (counts: Partial<Record<'info' | 'attention' | 'risk' | 'critical', number>>, headline = 'Healthy') =>
  ({ summary: { headline, counts: { info: 0, attention: 0, risk: 0, critical: 0, ...counts } } }) as unknown as RepositoryHealth;
const gh = (states: string[], available = true) => ({ available, pullRequests: states.map((state, n) => ({ number: n, state })) }) as unknown as GitHubState;

describe('repoDigest', () => {
  it('is nothing until Git has been read', () => {
    expect(repoDigest(null, null, null)).toBeNull();
  });

  it('counts what the working tree says', () => {
    const d = repoDigest(overview(), null, null)!;
    expect(d).toMatchObject({ branch: 'main', changed: 3, ahead: 2, behind: 0, branchesNeedingYou: 1, openPRs: null, line: 'Not checked yet', tone: 'neutral' });
  });

  it('counts only open pull requests, and says nothing when GitHub could not be asked', () => {
    expect(repoDigest(overview(), gh(['open', 'merged', 'open', 'closed']), null)!.openPRs).toHaveLength(2);
    expect(repoDigest(overview(), gh(['open'], false), null)!.openPRs).toBeNull();
  });

  it('takes its tone from the worst finding, and a conflict asks for the person', () => {
    expect(repoDigest(overview(), null, health({}))!.tone).toBe('ok');
    expect(repoDigest(overview(), null, health({ info: 4 }))!.tone).toBe('ok');
    expect(repoDigest(overview(), null, health({ attention: 1 }, '1 item needs attention'))).toMatchObject({ tone: 'ask', findings: 1, line: '1 item needs attention' });
    expect(repoDigest(overview(), null, health({ risk: 1, attention: 2 }))).toMatchObject({ tone: 'bad', findings: 3 });
  });

  it('names a detached head by its commit', () => {
    const d = repoDigest(overview({ local: { head: { branch: '', commit: 'abcdef123456', detached: true, unborn: false }, workingTree: { staged: [], unstaged: [], untracked: [], conflicted: [{}], ahead: 0, behind: 0 } } }), null, null)!;
    expect(d.branch).toBe('detached at abcdef1');
    expect(d.conflicted).toBe(1);
  });
});

describe('workingLine', () => {
  it('says only what is true', () => {
    expect(workingLine({ changed: 3, ahead: 2, behind: 1 })).toBe('3 changed · 2 ahead · 1 behind');
    expect(workingLine({ changed: 0, ahead: 0, behind: 4 })).toBe('4 behind');
    expect(workingLine({ changed: 0, ahead: 0, behind: 0 })).toBe('Clean');
  });
});
