import { describe, expect, it } from 'vitest';
import {
  actionPlan,
  basisHint,
  basisLabel,
  byProject,
  categoryLabel,
  detectedLabel,
  headlineTone,
  housekeepingLine,
  isDestructive,
  needsAttention,
  severityLabel,
  severityRank,
  severityTone,
  splitFindings,
  taskDescription,
} from './health';
import { timeAgo } from './format';
import type { HealthAction, HealthFinding, RepositoryHealth } from './types';

function finding(over: Partial<HealthFinding> = {}, action: Partial<HealthAction> = {}): HealthFinding {
  return {
    id: 'hf_1',
    projectId: 'prj_1',
    type: 'uncommitted_work',
    category: 'uncommitted',
    severity: 'attention',
    basis: 'deterministic',
    title: 'Uncommitted work in devboard/x',
    explanation: 'The agent left files uncommitted.',
    subject: { branch: 'devboard/x' },
    evidence: [{ label: 'Files', value: '2 modified' }],
    action: { kind: 'review_changes', label: 'Review changes', canPerform: true, ...action },
    state: 'open',
    detectedAt: '2026-10-04T10:00:00Z',
    updatedAt: '2026-10-04T11:00:00Z',
    ...over,
  };
}

function report(findings: HealthFinding[], state: RepositoryHealth['summary']['state'] = 'attention'): RepositoryHealth {
  return {
    projectId: 'prj_1',
    summary: { state, headline: '', counts: { info: 0, attention: 0, risk: 0, critical: 0 }, needsAttention: 0, dismissed: 0, score: 100 },
    findings,
    dismissed: [],
    resolved: [],
  };
}

describe('severity', () => {
  it('orders from housekeeping to critical', () => {
    expect(['critical', 'info', 'risk', 'attention'].map((s) => severityRank(s as never)).sort()).toEqual([1, 2, 3, 4]);
    expect(severityRank('critical')).toBeGreaterThan(severityRank('risk'));
    expect(severityRank('risk')).toBeGreaterThan(severityRank('attention'));
    expect(severityRank('attention')).toBeGreaterThan(severityRank('info'));
  });

  it('calls housekeeping housekeeping, not a problem', () => {
    expect(severityLabel('info')).toBe('Housekeeping');
    expect(severityLabel('attention')).toBe('Needs attention');
    expect(severityLabel('risk')).toBe('Risk');
    expect(severityLabel('critical')).toBe('Critical');
    expect(severityTone('info')).toBe('neutral');
    expect(severityTone('critical')).toBe('bad');
  });

  it('counts only attention and above as needing a person', () => {
    expect(needsAttention(finding({ severity: 'info' }))).toBe(false);
    expect(needsAttention(finding({ severity: 'attention' }))).toBe(true);
    expect(needsAttention(finding({ severity: 'risk' }))).toBe(true);
  });
});

describe('what is wrong, apart from what is only tidy', () => {
  it('splits the findings', () => {
    const r = report([finding({ id: 'a', severity: 'risk' }), finding({ id: 'b', severity: 'info' }), finding({ id: 'c', severity: 'attention' })]);
    const { needs, housekeeping } = splitFindings(r);
    expect(needs.map((f) => f.id)).toEqual(['a', 'c']);
    expect(housekeeping.map((f) => f.id)).toEqual(['b']);
    expect(splitFindings(null)).toEqual({ needs: [], housekeeping: [] });
  });

  it('words the housekeeping and tones the headline by the worst thing open', () => {
    expect(housekeepingLine(0)).toBe('');
    expect(housekeepingLine(1)).toBe('1 housekeeping item');
    expect(housekeepingLine(3)).toBe('3 housekeeping items');
    expect(headlineTone(report([], 'healthy'))).toBe('ok');
    expect(headlineTone(report([], 'attention'))).toBe('ask');
    expect(headlineTone(report([], 'risk'))).toBe('block');
    expect(headlineTone(report([], 'critical'))).toBe('bad');
    expect(headlineTone(null)).toBe('neutral');
  });

  it('is honest about how sure a signal is', () => {
    expect(basisLabel('deterministic')).toBe('Git says so');
    expect(basisLabel('heuristic')).toBe('A guess');
    expect(basisHint('heuristic')).toMatch(/may be wrong/);
    expect(basisHint('deterministic')).toMatch(/fact/);
  });

  it('labels every category', () => {
    for (const c of ['uncommitted', 'unsynced', 'branch', 'worktree', 'orchestration', 'operation'] as const) {
      expect(categoryLabel(c)).toBeTruthy();
    }
  });
});

describe('what tapping an action does', () => {
  it('opens the existing confirmation for push, merge and delete', () => {
    expect(actionPlan(finding({}, { kind: 'push_branch', branch: 'devboard/x' }), 'prj_1')).toEqual({ type: 'sheet', sheet: 'push', branch: 'devboard/x' });
    expect(actionPlan(finding({}, { kind: 'merge_branch', branch: 'devboard/x' }), 'prj_1')).toEqual({ type: 'sheet', sheet: 'merge', branch: 'devboard/x' });
    expect(actionPlan(finding({}, { kind: 'delete_branch', branch: 'devboard/x', destructive: true }), 'prj_1')).toEqual({ type: 'sheet', sheet: 'delete', branch: 'devboard/x' });
  });

  it('opens the clean confirmation for a worktree, never removes it itself', () => {
    expect(actionPlan(finding({}, { kind: 'clean_worktree', worktreeId: 'wt_1', branch: 'devboard/x', destructive: true }), 'prj_1')).toEqual({
      type: 'clean',
      worktreeId: 'wt_1',
      branch: 'devboard/x',
    });
  });

  it('reviews by navigating: to the worktree, the branch, or the checkout', () => {
    expect(actionPlan(finding({}, { kind: 'review_changes', worktreeId: 'wt_1' }), 'prj_1')).toEqual({ type: 'link', href: expect.stringContaining('wt_1') });
    expect(actionPlan(finding({}, { kind: 'review_changes', branch: 'devboard/x' }), 'prj_1')).toEqual({ type: 'link', href: expect.stringContaining('devboard') });
    const checkout = actionPlan(finding({}, { kind: 'review_changes' }), 'prj_1');
    expect(checkout.type).toBe('link');
  });

  it('makes a task of a coordination step, and starts nothing', () => {
    const f = finding({ basis: 'heuristic', evidence: [{ label: 'Shared files', value: 'server.go' }] }, { kind: 'create_task', taskTitle: 'Coordinate a and b', taskDescription: 'a and b both change server.go.' });
    const p = actionPlan(f, 'prj_1');
    expect(p.type).toBe('task');
    if (p.type === 'task') {
      expect(p.title).toBe('Coordinate a and b');
      expect(p.askAgent).toBe(false);
      expect(p.description).toContain('a and b both change server.go.');
      expect(p.description).toContain('Shared files: server.go');
      expect(p.description).toContain('heuristic; verify before acting');
    }
  });

  it('keeps an investigation distinct so the UI can ask for an agent and policy', () => {
    const p = actionPlan(finding({}, { kind: 'ask_agent', taskTitle: 'Investigate the branch', taskDescription: 'Check the branch.' }), 'prj_1');
    expect(p).toMatchObject({ type: 'task', askAgent: true, title: 'Investigate the branch' });
  });

  it('never offers a button for what Werkbord cannot do, and says why', () => {
    const p = actionPlan(
      finding({}, { kind: 'finish_operation', canPerform: false, reason: 'Werkbord never resolves conflicts', detail: 'Run git merge --abort' }),
      'prj_1',
    );
    expect(p).toEqual({ type: 'manual', reason: 'Werkbord never resolves conflicts', detail: 'Run git merge --abort' });
    // Even a kind that has a sheet is manual when it cannot be done.
    expect(actionPlan(finding({}, { kind: 'push_branch', branch: 'x', canPerform: false, reason: 'diverged' }), 'prj_1').type).toBe('manual');
  });

  it('falls back to manual when an action lacks what it needs, rather than opening a sheet on nothing', () => {
    expect(actionPlan(finding({}, { kind: 'push_branch' }), 'prj_1').type).toBe('manual');
    expect(actionPlan(finding({}, { kind: 'clean_worktree' }), 'prj_1').type).toBe('manual');
    expect(actionPlan(finding({}, { kind: 'inspect' }), 'prj_1').type).toBe('manual');
  });

  it('fetches only when asked', () => {
    expect(actionPlan(finding({}, { kind: 'fetch' }), 'prj_1')).toEqual({ type: 'fetch' });
  });

  it('marks as dangerous only an action that removes something and that can be done', () => {
    expect(isDestructive(finding({}, { kind: 'delete_branch', destructive: true }))).toBe(true);
    expect(isDestructive(finding({}, { kind: 'delete_branch', destructive: true, canPerform: false }))).toBe(false);
    expect(isDestructive(finding({}, { kind: 'push_branch' }))).toBe(false);
  });
});

describe('describing a finding', () => {
  it('says when it was first seen', () => {
    const now = Date.parse('2026-10-04T13:00:00Z');
    expect(detectedLabel(finding(), now, timeAgo)).toBe('First seen 3h ago');
  });

  it('writes a task description that carries the evidence', () => {
    const d = taskDescription(finding());
    expect(d).toContain('The agent left files uncommitted.');
    expect(d).toContain('- Files: 2 modified');
    expect(d).toContain('deterministic Git or Werkbord evidence');
  });

  it('groups by project', () => {
    const g = byProject([finding({ id: 'a', projectId: 'p1' }), finding({ id: 'b', projectId: 'p2' }), finding({ id: 'c', projectId: 'p1' })]);
    expect([...g.keys()]).toEqual(['p1', 'p2']);
    expect(g.get('p1')?.map((f) => f.id)).toEqual(['a', 'c']);
  });
});
