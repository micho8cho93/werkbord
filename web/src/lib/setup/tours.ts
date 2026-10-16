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
      title: 'Needs you comes first',
      body: 'Questions from the assistant, work to review and anything blocked wait here. Start here when you come back.',
      target: '.rail nav[aria-label="Everywhere"] a',
    },
    {
      title: 'Each project is a repository',
      body: 'Open one to see its board, calendar, Git and runs. Add the next one with the plus.',
      target: '.rail nav[aria-label="Projects"]',
    },
    {
      title: 'Press N for a new ticket',
      body: 'Inside a project, N adds a ticket. Start the assistant on it and watch it work from the ticket.',
    },
    {
      title: 'Help has short guides',
      body: 'Each one uses the words you see in the app. You can replay these tips from Help.',
      target: '.rail a[href*="help"]',
    },
  ],
  phone: [
    {
      title: 'Your Control Deck',
      body: 'Six keys open your work. Needs you keeps questions, blocked work and reviews within reach. Three empty positions are ready for your own buttons.',
      target: '.housing',
    },
    {
      title: 'Make the deck yours',
      body: 'Use Edit Deck to add, replace or remove any button. Drag to swap positions, or choose Move to position in a button’s menu. Done saves your layout to this computer for this phone.',
      target: '[data-testid="edit-deck"]',
    },
    {
      title: 'Workflows run on a repository',
      body: 'Add Find Bugs, Improve Tests or another workflow. Choose an Individual repository and an agent and model, or keep the defaults. Saving configures it; tapping it starts work.',
      target: '.housing',
    },
    {
      title: 'Follow the work',
      body: 'Workflow executions shows progress. Open a task for output, questions and Stop. Work continues when you leave this page. Hold a workflow, press Shift+F10, or use Edit Deck to configure it again.',
    },
    {
      title: 'Search and help are here',
      body: 'Jump to searches your work. Help explains the controls. This phone can use workflows and Git actions; repository registration, runner management and general settings stay on your computer.',
      target: '.header-actions',
    },
  ],
  settings: [
    {
      title: 'Three sections are worth knowing',
      body: 'Runners are your machines, Agents are the assistants that may run, and Task defaults say how new tickets run. The rest can wait.',
      target: 'nav[aria-label="Settings sections"]',
    },
  ],
  analytics: [
    {
      title: 'This fills in as you work',
      body: 'Cost, time and outcomes appear after your first runs. Nothing here is sample data, so an empty chart means nothing has run yet.',
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
