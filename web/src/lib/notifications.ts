import type { ControllerEvent } from './types';

const ENABLED_KEY = 'devboard.notifications.enabled';
const SEEN_KEY = 'devboard.notifications.seen';

/** Browser notifications are optional, local to this browser and never sent through a Dev Board service. */
export function notificationsEnabled(): boolean {
  try {
    return localStorage.getItem(ENABLED_KEY) === 'true';
  } catch {
    return false;
  }
}

export function notificationPermission(): NotificationPermission | 'unsupported' {
  return typeof Notification === 'undefined' ? 'unsupported' : Notification.permission;
}

export async function enableNotifications(): Promise<NotificationPermission | 'unsupported'> {
  if (typeof Notification === 'undefined') return 'unsupported';
  const permission = await Notification.requestPermission();
  try {
    localStorage.setItem(ENABLED_KEY, permission === 'granted' ? 'true' : 'false');
  } catch {
    // Private browsing can reject storage; this browser session still has permission.
  }
  return permission;
}

export function disableNotifications(): void {
  try {
    localStorage.removeItem(ENABLED_KEY);
  } catch {
    // The app continues without the preference.
  }
}

/** Dedupe replayed SSE events, and notify only on high-value state changes. */
export function notifyEvent(ev: ControllerEvent, projectName: string, taskTitle: string, readyForReview = false): void {
  if (!notificationsEnabled() || notificationPermission() !== 'granted' || !document.hidden) return;
  let message: { title: string; body: string } | undefined;
  switch (ev.type) {
    case 'agent.question':
      message = { title: 'Needs input', body: `${projectName} · ${taskTitle}` };
      break;
    case 'agent.blocked':
      message = { title: 'Agent blocked', body: `${projectName} · ${taskTitle}` };
      break;
    case 'agent.failed':
      message = { title: 'Agent failed', body: `${projectName} · ${taskTitle}` };
      break;
    case 'agent.completed':
      message = { title: readyForReview ? 'Ready for review' : 'Agent finished', body: `${projectName} · ${taskTitle}` };
      break;
    case 'git.health_changed': {
      let change: { opened?: number; summary?: { counts?: { risk?: number; critical?: number }; headline?: string } };
      try {
        change = typeof ev.payload === 'string' ? JSON.parse(ev.payload) : (ev.payload ?? {}) as typeof change;
      } catch {
        return;
      }
      if (!change.opened || !(change.summary?.counts?.risk || change.summary?.counts?.critical)) return;
      message = { title: 'Repository needs attention', body: `${projectName} · ${change.summary.headline ?? 'Important finding'}` };
      break;
    }
    default:
      return;
  }
  if (!message) return;
  const seq = String(ev.seq);
  try {
    const seen = JSON.parse(localStorage.getItem(SEEN_KEY) ?? '[]') as string[];
    if (seen.includes(seq)) return;
    seen.push(seq);
    localStorage.setItem(SEEN_KEY, JSON.stringify(seen.slice(-200)));
  } catch {
    // Without storage, rely on the controller's SSE replay behavior for this connection.
  }
  try {
    const notification = new Notification(message.title, { body: message.body, tag: `devboard-${seq}`, icon: '/icon-192.png' });
    notification.onclick = () => window.focus();
  } catch {
    // Permissions or platform notification support may change while the app is open.
  }
}
