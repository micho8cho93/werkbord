import type { Onboarding } from '../types';

/** The places that show a short first-visit tour, once each per device. */
export type TourSurface = 'desktop' | 'desktop-team' | 'phone' | 'phone-team' | 'settings' | 'analytics';

export interface TourStep {
  title: string;
  body: string;
  /** CSS selector of the element the tip points at. Without one, or if it is not on screen, the tip sits at the bottom. */
  target?: string;
}

/** Tours are written against the real screens and never show sample data. Surfaces fill in as their tours are built. */
export const TOURS: Record<TourSurface, readonly TourStep[]> = {
  desktop: [],
  'desktop-team': [],
  phone: [],
  'phone-team': [],
  settings: [],
  analytics: [],
};

export const TOUR_SURFACES = Object.keys(TOURS) as TourSurface[];

/** A tour is offered when it has tips, the onboarding record has loaded, and this device has not shown it yet. */
export function tourDue(onboarding: Onboarding | null, surface: TourSurface, tours: Record<TourSurface, readonly TourStep[]> = TOURS): boolean {
  if (!onboarding || tours[surface].length === 0) return false;
  return !onboarding.tours?.[surface];
}
