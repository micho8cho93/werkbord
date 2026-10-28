import { describe, expect, it } from 'vitest';
import { parseSignInFailure } from './signin.svelte';

describe('what the shell may tell a page about signing in again', () => {
  it('believes only its own failure message, as plain short text', () => {
    expect(parseSignInFailure({ type: 'werkbord.reauth', ok: false, message: 'It could\n not read   the token.' })).toBe('It could not read the token.');
    expect(parseSignInFailure({ type: 'werkbord.reauth', ok: false, message: '' })).toBe('The app could not sign this window in.');
    expect(parseSignInFailure({ type: 'werkbord.reauth', ok: false, message: 'x'.repeat(401) })).toBe('The app could not sign this window in.');
    for (const bad of [null, {}, 'text', { type: 'werkbord.reauth', ok: true, message: 'fine' }, { type: 'other', ok: false, message: 'm' }, { type: 'werkbord.reauth', ok: false, message: 7 }]) {
      expect(parseSignInFailure(bad)).toBeNull();
    }
  });
});
