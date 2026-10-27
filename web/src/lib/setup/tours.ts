import type { Onboarding } from '../types';

/**
 * The places in Individual that show a short first-visit tour, once each. Team's own tours live in Team's pages
 * (internal/team), which keep their own record.
 */
export type TourSurface = 'desktop' | 'phone' | 'settings' | 'analytics';

export interface TourStep {
  title: string;
  body: string;
  /** CSS selector of the element the tip points at. Without one, or if it is not on screen, the tip sits at the bottom. */
  target?: string;
}

/**
 * Tours are written against the real screens and never show sample data. Each is a few tips, because the rest of the app is
 * the same wherever you meet it.
 */
export const TOURS: Record<TourSurface, readonly TourStep[]> = {
  desktop: [
    {
      title: 'Your Workspace comes first',
      body: 'Agent questions, work to review and anything blocked appear here. Start here when you come back.',
      target: '.rail nav[aria-label="Everywhere"] a',
    },
    {
      title: 'Each project has its own board',
      body: 'Open a project to see its work. Repository projects also have Git and agent runs.',
      target: '.rail nav[aria-label="Projects"]',
    },
    {
      title: 'Press N for a new task',
      body: 'Inside a project, N adds a task. Start an agent and follow its work from the task.',
    },
    {
      title: 'Help has short guides',
      body: 'Each one uses the words you see in the app. You can replay these tips from Help.',
      target: '.rail a[href*="help"]',
    },
  ],
  // The phone uses DeckOnboarding's real interactions; this entry retains tourDue/Help replay eligibility.
  phone: [{
    title: 'Make the Control Deck yours',
    body: 'Choose a workflow, hold to configure it, tap to run and open its progress. The pencil icon at the top edits all nine positions; the checkmark saves. The Agents corner badge counts running agents.',
  }],
  settings: [
    {
      title: 'Three sections are worth knowing',
      body: 'Runners are your machines. Agents are the coding agents that work on tasks. Task defaults control how new runs start.',
      target: 'nav[aria-label="Settings sections"]',
    },
  ],
  analytics: [
    {
      title: 'This fills in as you work',
      body: 'Charts appear as activity is recorded. Change the filters to explore another period.',
    },
  ],
};

export const TOUR_SURFACES = Object.keys(TOURS) as TourSurface[];

/**
 * A phone's tour is remembered on the phone, not on the computer: every phone you pair reaches the same computer, and each
 * should get its own first look.
 */
export const LOCAL_SURFACES: readonly TourSurface[] = ['phone'];

const localKey = (surface: TourSurface) => `werkbord-tour-${surface}${surface === 'phone' ? '-deck-v2' : ''}`;

export function seenHere(surface: TourSurface): boolean {
  try { return localStorage.getItem(localKey(surface)) !== null; } catch { return false; }
}

export function markHere(surface: TourSurface): void {
  try { localStorage.setItem(localKey(surface), new Date().toISOString()); } catch { /* shown again next time; harmless */ }
}

export function forgetHere(): void {
  for (const s of LOCAL_SURFACES) { try { localStorage.removeItem(localKey(s)); } catch { /* nothing to forget */ } }
}

/**
 * A tour is offered when it has tips and this device has not shown it yet. The ones kept on the computer wait for the onboarding
 * record to load; the ones kept on a phone do not need it.
 */
export function tourDue(
  onboarding: Onboarding | null,
  surface: TourSurface,
  tours: Record<TourSurface, readonly TourStep[]> = TOURS,
  seenLocally: (s: TourSurface) => boolean = seenHere,
): boolean {
  if (tours[surface].length === 0) return false;
  if (LOCAL_SURFACES.includes(surface)) return !seenLocally(surface);
  if (!onboarding) return false;
  return !onboarding.tours?.[surface];
}
