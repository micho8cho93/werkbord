// What the phone screens say, apart from the screens: a name for a device, and how long a code has left.

/** A name for this device from its user agent, which the person can change: "iPhone (Safari)", "Pixel 8 (Chrome)". */
export function deviceName(ua: string): string {
  const browser = /EdgiOS|EdgA|Edg\//.test(ua)
    ? 'Edge'
    : /FxiOS|Firefox/.test(ua)
      ? 'Firefox'
      : /CriOS|Chrome/.test(ua)
        ? 'Chrome'
        : /Safari/.test(ua)
          ? 'Safari'
          : '';
  let device = 'Phone';
  if (/iPhone/.test(ua)) device = 'iPhone';
  else if (/iPad/.test(ua)) device = 'iPad';
  else if (/Android/.test(ua)) {
    // "Linux; Android 14; Pixel 8" names the model after the version.
    const model = /Android[^;)]*;\s*([^;)]+)/.exec(ua)?.[1]?.trim();
    device = model && !/^(K|Mobile|wv)$/.test(model) ? model : /Mobile/.test(ua) ? 'Android phone' : 'Android tablet';
  } else if (/Macintosh|Windows|Linux|X11/.test(ua)) device = 'Browser';
  return (browser ? `${device} (${browser})` : device).slice(0, 60);
}

/** "4:07": what is left of a code's five minutes. "0:00" once it has run out. */
export function countdown(expiresAt: string | number | Date, now: number): string {
  const left = Math.max(0, Math.ceil((new Date(expiresAt).getTime() - now) / 1000));
  return `${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}`;
}

/** "not yet", "just now", "5 min ago", "3 h ago", or the date. */
export function lastSeen(at: string | undefined, now: number): string {
  if (!at) return 'not yet';
  const minutes = Math.floor((now - new Date(at).getTime()) / 60000);
  if (minutes < 2) return 'just now';
  if (minutes < 60) return `${minutes} min ago`;
  if (minutes < 60 * 24) return `${Math.floor(minutes / 60)} h ago`;
  return new Date(at).toLocaleDateString();
}
