// What the Calendar was showing when the person left it: the view, the day, the project and the zoom stay as they were when
// they come back, as a page of the desktop app keeps its place.

import { localToday } from '../timeline';
import type { Zoom } from '../timeline';

export type CalMode = 'month' | 'week' | 'timeline';

export const cal = $state({
  mode: 'month' as CalMode,
  /** The day the month, week and timeline are around. */
  anchor: localToday(Date.now()),
  /** The day whose items are listed under the month. */
  selected: localToday(Date.now()),
  /** One project, or all of them. */
  project: '',
  zoom: 'week' as Exclude<Zoom, 'day'>,
});
