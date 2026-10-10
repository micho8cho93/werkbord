import { describe, expect, it, vi } from 'vitest';
import { detect, Dictation, disclosure, errorMessage, joinSpoken, recognisesLocally, vendorOf, type Env, type RecognitionLike } from './dictation';

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
  const handlers = () => ({ onInterim: vi.fn(), onFinal: vi.fn(), onEnd: vi.fn(), onError: vi.fn() });

  it('listens continuously, shows what it hears as it goes and settles what is final', () => {
    const h = handlers();
    const d = new Dictation(h, { lang: 'en-GB', local: false, env: env() });
    expect(d.start()).toBe(true);
    const r = FakeRecognition.last!;
    expect(r).toMatchObject({ lang: 'en-GB', continuous: true, interimResults: true, started: 1 });
    expect(r.processLocally).toBeUndefined(); // not asked to: the person was told it may use a server
    expect(d.listening).toBe(true);
    r.say([['what is bl', false]]);
    expect(h.onInterim).toHaveBeenLastCalledWith('what is bl');
    r.say([['what is blocked', true]]);
    expect(h.onFinal).toHaveBeenCalledWith('what is blocked');
    d.stop();
    expect(h.onEnd).toHaveBeenCalledTimes(1);
    expect(h.onInterim).toHaveBeenLastCalledWith('');
    expect(d.listening).toBe(false);
  });

  it('asks for on-device recognition only when the browser has said it can', () => {
    const d = new Dictation(handlers(), { lang: 'en-GB', local: true, env: env() });
    d.start();
    // 'processLocally' is a property of this browser's recogniser; the fake has none until it is set
    expect('processLocally' in FakeRecognition.last!).toBe(false);
    class WithLocal extends FakeRecognition {
      processLocally = false;
    }
    new Dictation(handlers(), { lang: 'en-GB', local: true, env: env({ SpeechRecognition: WithLocal as unknown as Env['SpeechRecognition'] }) }).start();
    expect(FakeRecognition.last!.processLocally).toBe(true);
  });

  it('reports a blocked microphone but not a pause in speaking', () => {
    const h = handlers();
    new Dictation(h, { lang: 'en', local: false, env: env() }).start();
    FakeRecognition.last!.onerror?.({ error: 'no-speech' });
    expect(h.onError).not.toHaveBeenCalled();
    FakeRecognition.last!.onerror?.({ error: 'not-allowed' });
    expect(h.onError).toHaveBeenCalledWith(expect.stringMatching(/blocked/));
  });

  it('does not start twice, and says so if it cannot start at all', () => {
    const h = handlers();
    const d = new Dictation(h, { lang: 'en', local: false, env: env() });
    expect(d.start()).toBe(true);
    expect(d.start()).toBe(false);
    class Refuses extends FakeRecognition {
      start() {
        throw new Error('InvalidStateError');
      }
    }
    const h2 = handlers();
    const d2 = new Dictation(h2, { lang: 'en', local: false, env: env({ SpeechRecognition: Refuses as unknown as Env['SpeechRecognition'] }) });
    expect(d2.start()).toBe(false);
    expect(h2.onError).toHaveBeenCalled();
    expect(d2.listening).toBe(false);
  });

  it('can be dropped at once', () => {
    const d = new Dictation(handlers(), { lang: 'en', local: false, env: env() });
    d.start();
    d.abort();
    expect(FakeRecognition.last!.aborted).toBe(1);
  });
});
