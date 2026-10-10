import { describe, expect, it, vi } from 'vitest';
import { detect, Dictation, disclosure, errorMessage, joinSpoken, recognisesLocally, vendorOf, type DictationOptions, type Env, type RecognitionLike } from './dictation';

const CHROME = 'Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36';
const SAFARI = 'Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15';
const EDGE = CHROME + ' Edg/140.0.0.0';

class FakeRecognition implements RecognitionLike {
  static last: FakeRecognition | null = null;
  static available?: (o: { langs: string[]; processLocally?: boolean }) => Promise<string>;
  lang = '';
  continuous = false;
  interimResults = false;
  declare processLocally?: boolean;
  onresult: RecognitionLike['onresult'] = null;
  onerror: RecognitionLike['onerror'] = null;
  onend: RecognitionLike['onend'] = null;
  onstart: RecognitionLike['onstart'] = null;
  onaudiostart: RecognitionLike['onaudiostart'] = null;
  onspeechstart: RecognitionLike['onspeechstart'] = null;
  onspeechend: RecognitionLike['onspeechend'] = null;
  started = 0;
  stopped = 0;
  aborted = 0;
  constructor() {
    FakeRecognition.last = this;
  }
  start() {
    this.started++;
  }
  stop() {
    this.stopped++;
    this.onend?.();
  }
  abort() {
    this.aborted++;
    this.onend?.();
  }
  say(parts: [string, boolean][], resultIndex = 0) {
    this.onresult?.({ resultIndex, results: parts.map(([t, f]) => ({ isFinal: f, 0: { transcript: t } })) });
  }
}

const env = (extra: Partial<Env> = {}): Env => ({ SpeechRecognition: FakeRecognition as unknown as Env['SpeechRecognition'], isSecureContext: true, navigator: { userAgent: CHROME, language: 'en-GB' }, ...extra });

describe('what dictation is available', () => {
  it('is available where the browser offers speech recognition on a secure page', () => {
    expect(detect(env())).toMatchObject({ available: true, vendor: 'Google' });
    expect(detect(env({ SpeechRecognition: undefined, webkitSpeechRecognition: FakeRecognition as unknown as Env['SpeechRecognition'] })).available).toBe(true);
  });
  it('says plainly when it is not, and what to do instead', () => {
    const none = detect(env({ SpeechRecognition: undefined }));
    expect(none.available).toBe(false);
    expect(none.reason).toMatch(/microphone key on your keyboard/);
    const insecure = detect(env({ isSecureContext: false }));
    expect(insecure.available).toBe(false);
    expect(insecure.reason).toMatch(/secure connection/);
  });
  it('knows whose service would hear the audio', () => {
    expect(vendorOf(CHROME)).toBe('Google');
    expect(vendorOf(EDGE)).toBe('Microsoft');
    expect(vendorOf(SAFARI)).toBe('Apple');
    expect(vendorOf('SomethingElse/1.0')).toBe("your browser's speech service");
  });
});

describe('where the audio goes', () => {
  it('is on the device only when the browser says it can recognise speech there', async () => {
    FakeRecognition.available = vi.fn().mockResolvedValue('available');
    expect(await recognisesLocally('en-GB', env())).toBe(true);
    expect(FakeRecognition.available).toHaveBeenCalledWith({ langs: ['en-GB'], processLocally: true });
    FakeRecognition.available = vi.fn().mockResolvedValue('downloadable');
    expect(await recognisesLocally('en-GB', env())).toBe(false);
    FakeRecognition.available = vi.fn().mockRejectedValue(new Error('no'));
    expect(await recognisesLocally('en-GB', env())).toBe(false);
    FakeRecognition.available = undefined;
    expect(await recognisesLocally('en-GB', env())).toBe(false); // not knowing counts as not local
  });

  it('is disclosed before use, names the service, and never claims to be Claude or ChatGPT voice', () => {
    const s = detect(env());
    const sent = disclosure(s, false);
    expect(sent).toMatch(/may be sent to Google/);
    expect(sent).toMatch(/not Claude or ChatGPT/);
    expect(disclosure(s, true)).toMatch(/Nothing you say leaves it/);
    expect(disclosure(detect(env({ navigator: { userAgent: SAFARI } })), false)).toMatch(/Apple/);
  });
});

describe('words for what goes wrong', () => {
  it('is silent about silence and stopping, and clear about the rest', () => {
    expect(errorMessage('no-speech')).toBeNull();
    expect(errorMessage('aborted')).toBeNull();
    expect(errorMessage('not-allowed')).toMatch(/microphone is blocked/);
    expect(errorMessage('service-not-allowed')).toMatch(/blocked/);
    expect(errorMessage('audio-capture')).toMatch(/No microphone/);
    expect(errorMessage('network')).toMatch(/could not be reached/);
    expect(errorMessage('whatever')).toMatch(/stopped unexpectedly/);
  });
  it('sends someone in the Mac app to the right settings, not to a browser’s', () => {
    expect(errorMessage('not-allowed', true)).toMatch(/System Settings → Privacy & Security/);
    expect(errorMessage('service-not-allowed', true)).toMatch(/Speech Recognition/);
    expect(errorMessage('not-allowed', true)).not.toMatch(/browser/);
  });
});

describe('putting speech into the box', () => {
  it.each([
    ['', 'hello', 'hello'],
    ['Hi', 'there', 'Hi there'],
    ['Hi ', ' there ', 'Hi there'],
    ['Hi\n', 'there', 'Hi\nthere'],
    ['Hi', '   ', 'Hi'],
  ])('%j + %j = %j', (typed, spoken, want) => {
    expect(joinSpoken(typed, spoken)).toBe(want);
  });
});

describe('a dictation', () => {
  const handlers = () => ({ onPhase: vi.fn(), onInterim: vi.fn(), onFinal: vi.fn(), onEnd: vi.fn(), onError: vi.fn(), onQuiet: vi.fn() });
  const make = (h = handlers(), more: Partial<DictationOptions> = {}) => ({ h, d: new Dictation(h, { lang: 'en-GB', local: false, env: env(), ...more }) });

  it('listens continuously, shows what it hears as it goes and settles what is final', () => {
    const { h, d } = make();
    expect(d.start()).toBe(true);
    const r = FakeRecognition.last!;
    expect(r).toMatchObject({ lang: 'en-GB', continuous: true, interimResults: true, started: 1 });
    expect(r.processLocally).toBeUndefined(); // not asked to: the person was told it may use a server
    expect(d.listening).toBe(true);
    expect(h.onPhase).toHaveBeenLastCalledWith('starting');
    r.onstart?.();
    expect(h.onPhase).toHaveBeenLastCalledWith('listening');
    r.say([['what is bl', false]]);
    expect(h.onInterim).toHaveBeenLastCalledWith('what is bl');
    expect(h.onPhase).toHaveBeenLastCalledWith('hearing');
    r.say([['what is blocked', true]]);
    expect(h.onFinal).toHaveBeenCalledWith('what is blocked');
    d.finish();
    expect(h.onEnd).toHaveBeenCalledTimes(1);
    expect(h.onEnd).toHaveBeenCalledWith('finished');
    expect(h.onInterim).toHaveBeenLastCalledWith('');
    expect(d.listening).toBe(false);
  });

  it('knows when it is hearing someone and when it is not', () => {
    const { h, d } = make();
    d.start();
    const r = FakeRecognition.last!;
    r.onaudiostart?.();
    expect(h.onPhase).toHaveBeenLastCalledWith('listening');
    r.onspeechstart?.();
    expect(h.onPhase).toHaveBeenLastCalledWith('hearing');
    r.onspeechend?.();
    expect(h.onPhase).toHaveBeenLastCalledWith('listening');
  });

  it('keeps unchanged interim segments when only the later segment changes', () => {
    const { h, d } = make();
    d.start();
    const r = FakeRecognition.last!;
    r.say([['hello', false], ['wor', false]]);
    r.say([['hello', false], ['world', false]], 1);
    expect(h.onInterim).toHaveBeenLastCalledWith('hello world');
    d.finish();
    expect(h.onFinal).toHaveBeenCalledWith('hello world');
  });

  it('ends a failed recognizer even when it never dispatches an end event', () => {
    const { h, d } = make();
    d.start();
    const r = FakeRecognition.last!;
    r.abort = () => {};
    r.onerror?.({ error: 'not-allowed' });
    expect(d.listening).toBe(false);
    expect(h.onEnd).toHaveBeenCalledExactlyOnceWith('failed');
    expect(h.onError).toHaveBeenCalled();
  });

  it('keeps the last words even when the engine never settles them', () => {
    // Safari's engine can stop without turning the last thing it heard into a final result.
    const { h, d } = make();
    d.start();
    const r = FakeRecognition.last!;
    r.say([['please add a ticket', false]]);
    d.finish();
    expect(h.onFinal).toHaveBeenCalledTimes(1);
    expect(h.onFinal).toHaveBeenCalledWith('please add a ticket');
    expect(h.onEnd).toHaveBeenCalledWith('finished');
  });

  it('does not say the same words twice when the engine does settle them', () => {
    const { h, d } = make();
    d.start();
    const r = FakeRecognition.last!;
    r.say([['please add a ticket', false]]);
    r.stop = () => {
      r.say([['please add a ticket', true]]);
      r.onend?.();
    };
    d.finish();
    expect(h.onFinal.mock.calls.map((c) => c[0])).toEqual(['please add a ticket']);
  });

  it('drops everything when the person cancels', () => {
    const { h, d } = make();
    d.start();
    const r = FakeRecognition.last!;
    r.say([['one two', true]]);
    r.say([['three', false]], 1);
    d.cancel();
    expect(r.aborted).toBe(1);
    expect(h.onFinal).toHaveBeenCalledTimes(1); // what had already been settled was delivered when it was; nothing more
    expect(h.onEnd).toHaveBeenCalledWith('cancelled');
    expect(h.onInterim).toHaveBeenLastCalledWith('');
    expect(d.listening).toBe(false);
  });

  it('listens on when the engine stops by itself, and keeps what it had not settled', () => {
    const { h, d } = make();
    d.start();
    const first = FakeRecognition.last!;
    first.say([['so far', false]]);
    first.onend?.(); // a long silence, or the engine's own time limit
    expect(h.onEnd).not.toHaveBeenCalled();
    expect(h.onFinal).toHaveBeenCalledWith('so far');
    const second = FakeRecognition.last!;
    expect(second).not.toBe(first);
    expect(second.started).toBe(1);
    second.say([['and more', true]]);
    expect(h.onFinal).toHaveBeenLastCalledWith('and more');
    d.finish();
    expect(h.onEnd).toHaveBeenCalledWith('finished');
  });

  it('gives up on an engine that ends again the moment it is started, and says so', () => {
    const { h, d } = make();
    d.start();
    for (let i = 0; i < 6 && d.listening; i++) FakeRecognition.last!.onend?.();
    expect(d.listening).toBe(false);
    expect(h.onError).toHaveBeenCalledWith(expect.stringMatching(/stopped unexpectedly/));
    expect(h.onEnd).toHaveBeenCalledWith('failed');
  });

  it('is not left waiting for an engine that never answers a stop', () => {
    vi.useFakeTimers();
    try {
      const { h, d } = make();
      d.start();
      const r = FakeRecognition.last!;
      r.stop = () => {}; // never calls onend
      r.say([['hello', false]]);
      d.finish();
      expect(h.onPhase).toHaveBeenLastCalledWith('finishing');
      expect(h.onEnd).not.toHaveBeenCalled();
      vi.advanceTimersByTime(3000);
      expect(h.onFinal).toHaveBeenCalledWith('hello');
      expect(h.onEnd).toHaveBeenCalledWith('finished');
    } finally {
      vi.useRealTimers();
    }
  });

  it('says once that the microphone seems quiet, and not at all if it hears something', () => {
    vi.useFakeTimers();
    try {
      const a = make(handlers(), { quietAfterMs: 5000 });
      a.d.start();
      vi.advanceTimersByTime(4900);
      expect(a.h.onQuiet).not.toHaveBeenCalled();
      vi.advanceTimersByTime(200);
      expect(a.h.onQuiet).toHaveBeenCalledTimes(1);
      a.d.cancel();
      const b = make(handlers(), { quietAfterMs: 5000 });
      b.d.start();
      FakeRecognition.last!.onspeechstart?.();
      vi.advanceTimersByTime(6000);
      expect(b.h.onQuiet).not.toHaveBeenCalled();
      b.d.cancel();
    } finally {
      vi.useRealTimers();
    }
  });

  it('asks for on-device recognition only when the browser has said it can', () => {
    make(handlers(), { local: true }).d.start();
    // 'processLocally' is a property of this browser's recogniser; the fake has none until it is set
    expect('processLocally' in FakeRecognition.last!).toBe(false);
    class WithLocal extends FakeRecognition {
      processLocally = false;
    }
    make(handlers(), { local: true, env: env({ SpeechRecognition: WithLocal as unknown as Env['SpeechRecognition'] }) }).d.start();
    expect(FakeRecognition.last!.processLocally).toBe(true);
  });

  it('reports a blocked microphone but not a pause in speaking, and ends rather than trying again', () => {
    const { h, d } = make();
    d.start();
    FakeRecognition.last!.onerror?.({ error: 'no-speech' });
    expect(h.onError).not.toHaveBeenCalled();
    const first = FakeRecognition.last!;
    first.onerror?.({ error: 'not-allowed' });
    expect(h.onError).toHaveBeenCalledWith(expect.stringMatching(/blocked/));
    first.onend?.();
    expect(FakeRecognition.last).toBe(first); // no second attempt
    expect(h.onEnd).toHaveBeenCalledWith('failed');
  });

  it('uses the app’s words for a refusal when it runs in the app', () => {
    const { h, d } = make(handlers(), { inApp: true });
    d.start();
    FakeRecognition.last!.onerror?.({ error: 'service-not-allowed' });
    expect(h.onError).toHaveBeenCalledWith(expect.stringMatching(/System Settings/));
  });

  it('does not start twice, and says so if it cannot start at all', () => {
    const { d } = make();
    expect(d.start()).toBe(true);
    expect(d.start()).toBe(false);
    class Refuses extends FakeRecognition {
      start() {
        throw new Error('InvalidStateError');
      }
    }
    const b = make(handlers(), { env: env({ SpeechRecognition: Refuses as unknown as Env['SpeechRecognition'] }) });
    expect(b.d.start()).toBe(false);
    expect(b.h.onError).toHaveBeenCalled();
    expect(b.h.onEnd).toHaveBeenCalledWith('failed');
    expect(b.d.listening).toBe(false);
  });
});
