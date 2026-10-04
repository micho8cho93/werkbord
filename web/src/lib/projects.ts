// What the interface needs to know about projects to make them easy to tell
// apart and quick to switch between. No framework code, so it can be tested.

import type { Project, ProjectActivity } from './types';

/** A hue, 0–359, that is the same for a project every time and spread well across projects. */
export function projectHue(id: string): number {
  let h = 2166136261;
  for (let i = 0; i < id.length; i++) {
    h ^= id.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return (h >>> 0) % 360;
}

/** The project's colour: its avatar, and the stripe across the top while you are in it. */
export function projectColor(id: string): string {
  return `hsl(${projectHue(id)} 58% 46%)`;
}

/** One letter for the avatar. */
export function projectInitial(name: string): string {
  const ch = Array.from(name.trim())[0];
  return ch ? ch.toUpperCase() : '?';
}

/** Projects matching what was typed, best first: a name that starts with it, then one that contains it, then a path that does. */
export function filterProjects(projects: readonly Project[], query: string): Project[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...projects];
  const rank = (p: Project): number => {
    const name = p.name.toLowerCase();
    if (name.startsWith(q)) return 0;
    if (name.includes(q)) return 1;
    if (p.repoPath.toLowerCase().includes(q)) return 2;
    return 3;
  };
  return projects
    .map((p, i) => ({ p, i, r: rank(p) }))
    .filter((x) => x.r < 3)
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .map((x) => x.p);
}

/** What a project is asking of the user, as a short list of words: "2 need input · 1 blocked". Empty if nothing. */
export function activitySummary(a: ProjectActivity | undefined): string {
  if (!a) return '';
  const parts: string[] = [];
  if (a.needsInput) parts.push(`${a.needsInput} ${a.needsInput === 1 ? 'needs' : 'need'} input`);
  if (a.blocked) parts.push(`${a.blocked} blocked`);
  if (a.idle) parts.push(`${a.idle} waiting`);
  if (a.running) parts.push(`${a.running} running`);
  return parts.join(' · ');
}

/** How much of it needs the user: what the badge on the switcher counts. */
export function attentionCount(a: ProjectActivity | undefined): number {
  return a ? a.needsInput + a.blocked + a.idle : 0;
}

/** Moves a highlighted row through a list of n, wrapping round at both ends. */
export function stepIndex(current: number, delta: 1 | -1, n: number): number {
  if (n <= 0) return -1;
  if (current < 0) return delta > 0 ? 0 : n - 1;
  return (current + delta + n) % n;
}
