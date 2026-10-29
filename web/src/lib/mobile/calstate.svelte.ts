// What the Calendar was showing when the person left it: the view, the day, and the project stay as they were when
// they come back, as a page of the desktop app keeps its place.

import { localToday } from '../planning-dates';

export type CalMode = 'month' | 'week';

export const cal = $state({
  mode: 'month' as CalMode,
  /** The day the month and week are around. */
  anchor: localToday(Date.now()),
  /** The day whose items are listed under the month. */
  selected: localToday(Date.now()),
  /** One project, or all of them. */
  project: '',
});
