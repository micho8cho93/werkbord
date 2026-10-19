// Alerts: what happened while the person was in another app, as a notification from the Mac and a bounce of the Dock icon. The
// window does it through the app (a web view has no notifications of its own, which is why a page cannot be asked to), and the
// person's choice to hear about them is the window's, not any one workspace's: it is on until they turn it off.

import { ask } from './native';
import type { Alert } from './frames';

const KEY = 'werkbord.shell.alerts';
const SEEN = 120;

export type AlertStatus = 'granted' | 'denied' | 'unknown' | 'unsupported';

function read(): boolean {
  try {
    return localStorage.getItem(KEY) !== 'off';
  } catch {
    return true;
  }
}

class Alerts {
  /** Whether the person wants to hear about things while they are in another app. On unless they turned it off. */
  enabled = $state(read());
  /** What the system allows this app to do: `granted`, `denied` (in System Settings), `unknown` (not asked yet) or `unsupported`. */
  status = $state<AlertStatus>('unknown');
  private seen: string[] = [];

  async refresh(): Promise<void> {
    try {
      this.status = ((await ask((a) => a.AlertStatus())) as AlertStatus) || 'unsupported';
    } catch {
      this.status = 'unsupported';
    }
  }

  set(on: boolean): void {
    this.enabled = on;
    try {
      if (on) localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, 'off');
    } catch {
      // Remembered for this window only.
    }
  }

  /** One thing Individual says happened. Nothing is shown while the app is in front, or after the person turned alerts off. */
  async tell(alert: Alert): Promise<void> {
    if (!this.enabled || this.seen.includes(alert.key)) return;
    this.seen = [...this.seen.slice(-SEEN), alert.key];
    try {
      await ask((a) => a.Alert(alert.title, alert.body, alert.urgent === true));
    } catch {
      // An alert that cannot be shown is not worth an error on screen: the work itself says the same thing.
    }
  }

  /** Shows one now, even with the app in front, so that the person sees what an alert looks like and the system asks if it must. */
  async test(): Promise<boolean> {
    try {
      const shown = (await ask((a) => a.TestAlert())) as boolean;
      await this.refresh();
      return shown;
    } catch {
      return false;
    }
  }

  async openSystemSettings(): Promise<void> {
    try {
      await ask((a) => a.OpenAlertSettings());
    } catch {
      // Nothing to open on this system.
    }
  }
}

export const alerts = new Alerts();
