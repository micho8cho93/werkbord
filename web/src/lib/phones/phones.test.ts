import { describe, expect, it } from 'vitest';
import { countdown, deviceName, lastSeen } from './phones';

const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1';
const IPHONE_CHROME = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/130.0.0.0 Mobile/15E148 Safari/604.1';
const PIXEL = 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36';
const ANDROID_PLAIN = 'Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36';
const MAC = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15';

describe('a name for a device', () => {
  it.each([
    [IPHONE, 'iPhone (Safari)'],
    [IPHONE_CHROME, 'iPhone (Chrome)'],
    [PIXEL, 'Pixel 8 (Chrome)'],
    [ANDROID_PLAIN, 'Android phone (Chrome)'],
    [MAC, 'Browser (Safari)'],
    ['', 'Phone'],
    ['unknown agent', 'Phone'],
  ])('%s', (ua, name) => {
    expect(deviceName(ua)).toBe(name);
  });

  it('never exceeds the 60 characters the controller accepts', () => {
    expect(deviceName(`Mozilla/5.0 (Linux; Android 14; ${'x'.repeat(200)}) Chrome/1 Mobile`).length).toBeLessThanOrEqual(60);
  });
});

describe('the countdown', () => {
  const at = new Date('2026-10-15T12:05:00Z').getTime();
  it('counts down from five minutes and stops at zero', () => {
    expect(countdown(at, at - 300_000)).toBe('5:00');
    expect(countdown(at, at - 247_000)).toBe('4:07');
    expect(countdown(at, at - 1)).toBe('0:01');
    expect(countdown(at, at)).toBe('0:00');
    expect(countdown(at, at + 60_000)).toBe('0:00');
  });
});

describe('last seen', () => {
  const now = new Date('2026-10-15T12:00:00Z').getTime();
  it('says it in words', () => {
    expect(lastSeen(undefined, now)).toBe('not yet');
    expect(lastSeen('2026-10-15T11:59:30Z', now)).toBe('just now');
    expect(lastSeen('2026-10-15T11:40:00Z', now)).toBe('20 min ago');
    expect(lastSeen('2026-10-15T07:00:00Z', now)).toBe('5 h ago');
  });
});
