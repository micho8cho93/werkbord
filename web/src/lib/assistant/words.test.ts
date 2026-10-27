import { describe, expect, it } from 'vitest';
import { ApiError } from '../api';
import { clock, duration, explain, failure, looksLikeText, operationFailureTitle, operationTitle, retentionNote, size } from './words';

describe('what a person is told', () => {
  it('explains a request that failed', () => {
    expect(explain(new ApiError(0, 'offline', 'x'))).toMatch(/Cannot reach Werkbord/);
    expect(explain(new ApiError(401, 'unauthorized', 'x'))).toMatch(/could not authorise this request/);
    expect(explain(new ApiError(503, 'unavailable', 'x'))).toMatch(/Check the conversation/);
    expect(explain(new ApiError(409, 'conflict', 'the assistant is still replying to the last message'))).toBe('the assistant is still replying to the last message');
    expect(explain(new Error('boom'))).toBe('boom');
    expect(explain('odd')).toBe('Werkbord could not complete this request.');
  });

  it('offers the one thing most worth doing about each failure', () => {
    const f = (code: string, retryable = false) => failure({ code, message: 'm', retryable });
    expect(f('model_unavailable').action).toBe('model');
    expect(f('not_signed_in').action).toBe('recheck');
    expect(f('not_installed').action).toBe('recheck');
    expect(f('timeout', true).action).toBe('retry');
    expect(f('transient', true).action).toBe('retry');
    expect(f('transient', false).action).toBe('none');
    expect(f('rate_limited').action).toBe('none');
    expect(f('policy').action).toBe('none');
    expect(f('policy').title).toBe('Salie stopped for safety');
    expect(f('protocol').title).toBe('Werkbord could not read Salie’s response');
    expect(f('rate_limited').title).toBe('The coding agent’s service is limiting requests');
    expect(f('model_unavailable').detail).toBe('m');
  });

  it('writes sizes, times and the retention rule', () => {
    expect([12, 1536, 262144, 3 * 1024 * 1024].map(size)).toEqual(['12 B', '2 KB', '256 KB', '3.0 MB']);
    expect([60, 1800, 3600, 7200].map(duration)).toEqual(['1 minute', '30 minutes', '1 hour', '2 hours']);
    expect(retentionNote(1800)).toBe('Kept on this computer for up to 30 minutes, or until you send or remove it. Never written to disk.');
    expect(clock(new Date(2026, 2, 9, 7, 5).getTime())).toBe('07:05');
    expect(operationTitle('create_ticket')).toBe('New task');
    expect(operationTitle('whatever')).toBe('Change');
    expect(operationFailureTitle('update_ticket')).toBe('Could not update the task');
    expect(operationFailureTitle('get_ticket')).toBe('Could not load this task');
    expect(operationFailureTitle('unknown')).toBe('Could not complete this Salie step');
  });

  it('spots a file that is not text before sending it anywhere', () => {
    const enc = (s: string) => new TextEncoder().encode(s);
    expect(looksLikeText(enc('# notes\nthe\tcsv,line\r\n'))).toBe(true);
    expect(looksLikeText(new Uint8Array([0x25, 0x50, 0x44, 0x46, 0x00]))).toBe(false); // a PDF
    expect(looksLikeText(new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))).toBe(false); // a PNG
  });
});
