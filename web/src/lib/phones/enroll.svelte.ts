// A phone that scanned the QR code arrives with /#enroll=<code>. This spends the code, keeps the token it is given
// where the app keeps its token, and removes the code from the address bar. The person does nothing: they are signed in.

import { api, ApiError, setToken, takeEnrollmentCode } from '../api';
import { deviceName } from './phones';

export const enrollment = $state<{ state: 'idle' | 'enrolling' | 'done' | 'failed'; name: string; error: string }>({
  state: 'idle',
  name: '',
  error: '',
});

/** Call once, before the app starts asking for things. Resolves when the phone is signed in or the code was refused. */
export async function enrollFromURL(ua: string = typeof navigator === 'undefined' ? '' : navigator.userAgent): Promise<void> {
  const code = takeEnrollmentCode();
  if (code === null) return;
  enrollment.state = 'enrolling';
  try {
    if (!code) throw new ApiError(403, 'forbidden', 'That link is not complete.');
    const phone = await api.enrollPhone(code, deviceName(ua));
    setToken(phone.token);
    enrollment.name = phone.name;
    enrollment.state = 'done';
  } catch (err) {
    enrollment.error =
      err instanceof ApiError && err.status === 429
        ? 'Too many tries. Wait a minute, then scan again.'
        : err instanceof ApiError && err.status === 0
          ? 'Cannot reach Werkbord. Check that this phone is on the same Tailscale account, then scan again.'
          : 'That code did not work. It is used once and lasts five minutes: make a new one in Settings → Phones on your computer.';
    enrollment.state = 'failed';
  }
}

/** The person has seen it. */
export function dismissEnrollment(): void {
  if (enrollment.state === 'done') enrollment.state = 'idle';
}
