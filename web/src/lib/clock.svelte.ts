// 12-hour or 24-hour clock, one choice for every time the app shows. Unless the person picks one on this device, the
// device's own locale decides (a US machine reads "3:20 PM", a German one "15:20"); the choice is remembered per device.
// Times go through these functions (the formatters are in clock.ts) so no screen can disagree with another.

import { deviceHour12, formatDateTime as dateTimeOn, formatTime as timeOn, formatWall as wallOn, formatWeekdayTime as weekdayTimeOn, hourLabel as hourOn } from './clock';

const KEY = 'werkbord.clock';
export type ClockChoice = '' | '12' | '24';

function stored(): ClockChoice {
  try {
    const c = localStorage.getItem(KEY);
    return c === '12' || c === '24' ? c : '';
  } catch {
    return '';
  }
}

class Clock {
  choice = $state<ClockChoice>(stored());

  /** What is showing now. */
  get hour12(): boolean {
    return this.choice ? this.choice === '12' : deviceHour12();
  }

  set(choice: ClockChoice): void {
    this.choice = choice;
    try {
      if (choice) localStorage.setItem(KEY, choice);
      else localStorage.removeItem(KEY);
    } catch {
      // Not remembered in private mode; it still applies to this page.
    }
  }
}

export const clock = new Clock();

type At = Date | number | string;

// The same functions on the clock showing now; reading `clock.hour12` here makes a template that calls them follow a change.
export const formatTime = (at: At, hour12 = clock.hour12): string => timeOn(at, hour12);
export const formatWeekdayTime = (at: At, hour12 = clock.hour12): string => weekdayTimeOn(at, hour12);
export const formatDateTime = (at: At, hour12 = clock.hour12): string => dateTimeOn(at, hour12);
export const formatWall = (hhmm: string, hour12 = clock.hour12): string => wallOn(hhmm, hour12);
export const hourLabel = (hour: number, hour12 = clock.hour12): string => hourOn(hour, hour12);
