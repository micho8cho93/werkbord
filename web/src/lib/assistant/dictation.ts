// Dictation: speaking into the message box. It uses what the browser (or the desktop app's web view) itself offers, the Web
// Speech API, and nothing else. It is not Claude's or ChatGPT's voice: neither provider offers voice input to a program that
// runs them this way (docs/ASSISTANT.md), and this module never claims otherwise.
//
// Where the audio goes is the browser's business, and mostly not on this computer: Chrome sends it to Google, Safari to Apple,
// unless the browser can recognise speech on the device (Chrome 139+ says so through SpeechRecognition.available with
// processLocally). So the person is told, before the first use, where it would go, and a service that is not known to be local
// is treated as sending.

export interface RecognitionResultLike {
  isFinal: boolean;
  0: { transcript: string };
}
export interface RecognitionEventLike {
  resultIndex: number;
  results: ArrayLike<RecognitionResultLike>;
}
export interface RecognitionLike {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  processLocally?: boolean;
  onresult: ((e: RecognitionEventLike) => void) | null;
  onerror: ((e: { error: string }) => void) | null;
  onend: (() => void) | null;
  onstart: (() => void) | null;
  start(): void;
  stop(): void;
  abort(): void;
}
interface RecognitionCtor {
  new (): RecognitionLike;
  available?: (o: { langs: string[]; processLocally?: boolean }) => Promise<string>;
}

export interface Env {
  SpeechRecognition?: RecognitionCtor;
  webkitSpeechRecognition?: RecognitionCtor;
  isSecureContext?: boolean;
  navigator?: { userAgent?: string; language?: string };
}

export interface Support {
  available: boolean;
  /** Why not, in words for the person. */
  reason?: string;
  /** Whose service would hear the audio unless it is recognised on the device. */
  vendor: string;
}

/** Who runs the speech service of this browser, as far as its user agent says. */
export function vendorOf(userAgent: string): string {
  if (/Edg\//.test(userAgent)) return 'Microsoft';
  if (/Chrome\/|Chromium\/|CriOS\//.test(userAgent)) return 'Google';
  if (/Safari\//.test(userAgent) || /AppleWebKit/.test(userAgent)) return 'Apple';
  return "your browser's speech service";
}

export function detect(env: Env = globalThis as unknown as Env): Support {
  const ua = env.navigator?.userAgent ?? '';
  const vendor = vendorOf(ua);
  const Ctor = env.SpeechRecognition ?? env.webkitSpeechRecognition;
  if (!Ctor) {
    return { available: false, vendor, reason: 'Dictation is not available in this browser. On a phone, the microphone key on your keyboard types what you say.' };
  }
  if (env.isSecureContext === false) {
    return { available: false, vendor, reason: 'Dictation needs a secure connection (https, or this computer’s own address). The microphone key on your keyboard works anywhere.' };
  }
  return { available: true, vendor };
}

/** Whether this browser can recognise speech on the device itself, so that nothing is sent. Unknown counts as no. */
export async function recognisesLocally(lang: string, env: Env = globalThis as unknown as Env): Promise<boolean> {
  const Ctor = env.SpeechRecognition ?? env.webkitSpeechRecognition;
  if (!Ctor || typeof Ctor.available !== 'function') return false;
  try {
    return (await Ctor.available({ langs: [lang], processLocally: true })) === 'available';
  } catch {
    return false;
  }
}

/** What the person is told before dictating for the first time. */
export function disclosure(s: Support, local: boolean): string {
  if (local) return 'Speech is recognised on this device. Nothing you say leaves it.';
  return `Dictation uses your browser’s speech service, not Claude or ChatGPT. What you say may be sent to ${s.vendor} to be turned into text, and Werkbord cannot see or control that.`;
}

/** What an error from the recogniser means for the person. Silence and stopping are not errors. */
export function errorMessage(code: string): string | null {
  switch (code) {
    case 'no-speech':
    case 'aborted':
      return null;
    case 'not-allowed':
    case 'service-not-allowed':
      return 'The microphone is blocked. Allow it for this page in your browser’s settings, then try again.';
    case 'audio-capture':
      return 'No microphone was found.';
    case 'network':
      return 'The speech service could not be reached. Check your connection, or type instead.';
    case 'language-not-supported':
      return 'That language is not supported for dictation here.';
    default:
      return 'Dictation stopped unexpectedly.';
  }
}

/** Puts what was spoken after what is already typed, with exactly one space between, and no leading space on an empty box. */
export function joinSpoken(typed: string, spoken: string): string {
  const s = spoken.trim();
  if (s === '') return typed;
  if (typed === '' || /\s$/.test(typed)) return typed + s;
  return typed + ' ' + s;
}

export interface DictationHandlers {
  /** What is being said now, not yet settled. */
  onInterim(text: string): void;
  /** A piece that is settled. */
  onFinal(text: string): void;
  onEnd(): void;
  onError(message: string): void;
}

/** One dictation session: start it, it speaks through the handlers, stop it or let it end. */
export class Dictation {
  private rec: RecognitionLike | null = null;

  constructor(
    private h: DictationHandlers,
    private opts: { lang: string; local: boolean; env?: Env },
  ) {}

  get listening(): boolean {
    return this.rec !== null;
  }

  start(): boolean {
    const env = this.opts.env ?? (globalThis as unknown as Env);
    const Ctor = env.SpeechRecognition ?? env.webkitSpeechRecognition;
    if (!Ctor || this.rec) return false;
    const rec = new Ctor();
    rec.lang = this.opts.lang;
    rec.continuous = true;
    rec.interimResults = true;
    // Only when the person's browser can do it on the device is it asked to; otherwise the default (which may use a server) applies,
    // and the person was told so.
    if (this.opts.local && 'processLocally' in rec) rec.processLocally = true;
    rec.onresult = (e) => {
      let interim = '';
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const r = e.results[i];
        if (r.isFinal) this.h.onFinal(r[0].transcript);
        else interim += r[0].transcript;
      }
      this.h.onInterim(interim);
    };
    rec.onerror = (e) => {
      const m = errorMessage(e.error);
      if (m) this.h.onError(m);
    };
    rec.onend = () => {
      this.rec = null;
      this.h.onInterim('');
      this.h.onEnd();
    };
    this.rec = rec;
    try {
      rec.start();
    } catch {
      this.rec = null;
      this.h.onError('Dictation could not start.');
      return false;
    }
    return true;
  }

  /** Stops listening and lets what was heard so far settle. */
  stop(): void {
    this.rec?.stop();
  }

  /** Stops at once and drops what was not settled. */
  abort(): void {
    this.rec?.abort();
  }
}
