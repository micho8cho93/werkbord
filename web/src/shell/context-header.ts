// What the window's header says about the project context in view, worked out from the scoped overview and nothing else: no request, no
// clock of its own that decides anything. It states only what a workspace reported. A person is shown by role and by what is assigned to
// them, never by whether they are around; an agent is shown as running only when its source says the report is fresh.
import { timeAgo } from '../lib/format';
import { projectInHref } from './project-context';
import type { AgentRef, Execution, Item, Overview } from './types';

/** An agent on a piece of work, from either the work a person holds or a queue row (which is where a teammate's agent appears). */
export interface AgentUse {
  key: string;
  workspace: string;
  kind: Item['kind'];
  agentId?: string;
  state: Execution;
  fresh: boolean;
  observedAt: string;
  title: string;
  project: string;
  href: string;
}

const LIVE: readonly Execution[] = ['queued', 'starting', 'running', 'needs_input', 'blocked'];

/** Agents that are on work right now or were last seen on it, one per task, newest observation first. A workspace that cannot answer reports none. */
export function agentUses(o: Overview | null): AgentUse[] {
  const out = new Map<string, AgentUse>();
  const add = (workspace: string, kind: Item['kind'], row: { title: string; project: string; href: string }, agent: AgentRef | undefined) => {
    if (!agent || !LIVE.includes(agent.state)) return;
    const key = `${workspace}|${row.href}`;
    if (out.has(key)) return;
    out.set(key, { key, workspace, kind, agentId: agent.id, state: agent.state, fresh: agent.fresh, observedAt: agent.observedAt, title: row.title, project: row.project, href: row.href });
  };
  for (const e of o?.entries ?? []) {
    if (e.error || e.workspace.state !== 'ready' || !e.summary) continue;
    for (const w of e.summary.work) add(e.workspace.id, e.workspace.kind, w, w.report?.stale ? undefined : w.agent);
    for (const q of e.summary.queue ?? []) if (q.group !== 'plan') add(e.workspace.id, e.workspace.kind, q, q.agent);
  }
  return [...out.values()].sort((a, b) => Date.parse(b.observedAt) - Date.parse(a.observedAt));
}

/** How many agents are confirmed running now: fresh reports only, so a stale or unknown one is never counted. */
export const runningCount = (uses: readonly AgentUse[]): number => uses.filter(u => u.state === 'running' && u.fresh).length;

/** A person on a project in view, and how much is assigned to them. `assigned` is left out when two people share a name, because a queue row names its holder only by name. */
export interface Participant { key: string; id: string; name: string; role?: string; workspace: string; assigned?: number }

export function participants(o: Overview | null): Participant[] {
  const out: Participant[] = [];
  const seen = new Set<string>();
  for (const e of o?.entries ?? []) {
    if (e.workspace.kind !== 'team' || e.error || e.workspace.state !== 'ready' || !e.summary) continue;
    const people = e.summary.projects.flatMap(p => p.people ?? []);
    const names = new Map<string, number>();
    for (const p of people) names.set(p.name, (names.get(p.name) ?? 0) + 1);
    const held = new Map<string, number>();
    for (const q of e.summary.queue ?? []) if (q.owner && q.group !== 'done' && q.group !== 'plan') held.set(q.owner, (held.get(q.owner) ?? 0) + 1);
    for (const p of people) {
      const key = `${e.workspace.id}|${p.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      out.push({ key, id: p.id, name: p.name, role: p.role, workspace: e.workspace.id, assigned: names.get(p.name) === 1 ? (held.get(p.name) ?? 0) : undefined });
    }
  }
  return out;
}

/** "owner · 2 assigned": a person's role on the project and what is assigned to them, and nothing about whether they are around. */
export function describeParticipant(p: Participant): string {
  return [p.role ?? 'member', p.assigned === undefined ? '' : p.assigned === 0 ? 'nothing assigned' : `${p.assigned} assigned`].filter(Boolean).join(' · ');
}

export interface GitSummary {
  /** "main · a1b2c3d" */
  text: string;
  /** Where it comes from, so it is never read as more than it is: "reported 3m ago · unverified" for a Team's report. */
  provenance?: string;
}

/** The branch the person's own project has checked out. Nothing about the working tree: that is on the Git page. */
export function personalGit(head: { branch: string; commit: string; detached?: boolean; unborn?: boolean } | null | undefined): GitSummary | null {
  if (!head || head.unborn) return null;
  if (head.detached) return { text: head.commit ? `detached · ${head.commit}` : 'detached' };
  return head.branch ? { text: head.commit ? `${head.branch} · ${head.commit}` : head.branch } : null;
}

/** A Team project's newest reported push, as a member's connector reported it: unverified until the person's own GitHub sign-in confirms it. */
export function teamGit(o: Overview | null, project: string, now: number): GitSummary | null {
  let best: { branch: string; sha: string; at: string } | null = null;
  for (const e of o?.entries ?? []) {
    if (e.workspace.kind !== 'team' || e.error || !e.summary) continue;
    for (const p of e.summary.pushes ?? []) {
      if (projectInHref('team', p.href) !== project) continue;
      if (!best || Date.parse(p.at) > Date.parse(best.at)) best = p;
    }
  }
  if (!best) return null;
  return { text: `${best.branch} · ${best.sha.slice(0, 7)}`, provenance: `reported ${timeAgo(best.at, now)} · unverified` };
}

export interface Crumb { label: string; title?: boolean }

const PAGE_LABEL: Record<string, string> = { inbox: 'Work', calendar: 'Calendar', git: 'Git', agents: 'Agents', analytics: 'Analytics', reviews: 'Reviews', workspaces: 'Workspaces' };

/**
 * Where the person is, in depth: the project context, the page, and for Work what is open. `title` is what the open page in the frame
 * said it is (a plan's or a task's title); without it the depth stops at the kind. A title belongs to the frame that sent it, so the
 * caller passes it only while that frame is the page on show.
 */
export function crumbs(input: { context: string; page: string; place: string; team: boolean; onGit: boolean; title?: string }): Crumb[] {
  const out: Crumb[] = [{ label: input.context }];
  const { page, place, title } = input;
  if (page !== 'workspace') {
    if (PAGE_LABEL[page]) out.push({ label: PAGE_LABEL[page] });
    return out;
  }
  if (input.onGit) { out.push({ label: 'Git' }); return out; }
  const query = new URLSearchParams(place.includes('?') ? place.slice(place.indexOf('?') + 1) : '');
  const tab = input.team ? query.get('tab') : 'work';
  if (input.team && tab && tab !== 'work') return out;
  out.push({ label: 'Work' });
  const plan = query.get('plan'), section = query.get('section');
  const task = input.team ? query.get('ticket') : /\/task\/[^/?#]+/.test(place) ? 'task' : null;
  if (task) out.push({ label: 'Task' });
  else if (plan) out.push({ label: 'Plans' });
  else if (section === 'plans') out.push({ label: 'Plans' });
  if ((task || plan) && title) out.push({ label: title, title: true });
  return out;
}
