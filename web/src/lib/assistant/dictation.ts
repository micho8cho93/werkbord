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
  onaudiostart?: (() => void) | null;
  onspeechstart?: (() => void) | null;
  onspeechend?: (() => void) | null;
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

/**
 * What an error from the recogniser means for the person. Silence and stopping are not errors. `inApp` is the Mac app, where
 * the permission is the app's own (System Settings), not a page's.
 */
export function errorMessage(code: string, inApp = false): string | null {
  switch (code) {
    case 'no-speech':
    case 'aborted':
      return null;
    case 'not-allowed':
    case 'service-not-allowed':
      return inApp
        ? 'Werkbord is not allowed to listen. Turn on Microphone and Speech Recognition for Werkbord in System Settings → Privacy & Security, then try again. Dictation (Edit → Start Dictation, or pressing fn twice) works meanwhile.'
        : 'The microphone is blocked. Allow it for this page in your browser’s settings, then try again.';
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

/** Where a dictation is: asking for the microphone, listening, hearing someone, or finishing what it heard. */
export type DictationPhase = 'starting' | 'listening' | 'hearing' | 'finishing';

export interface DictationHandlers {
  /** The phase changed. */
  onPhase(phase: DictationPhase): void;
  /** What is being said now, not yet settled. Empty when nothing is. */
  onInterim(text: string): void;
  /** A piece that is settled. */
  onFinal(text: string): void;
  /** Nothing has come in from the microphone for a while after starting (said once). */
  onQuiet?(): void;
  /** Over: the person finished it, or cancelled it, or it could not go on. Everything heard has been delivered by now, unless cancelled. */
  onEnd(how: 'finished' | 'cancelled' | 'failed'): void;
  onError(message: string): void;
}

export interface DictationOptions {
  lang: string;
  local: boolean;
  /** Whether this is the Mac app, which words a refused permission differently. */
  inApp?: boolean;
  /** How long to wait for any sound before saying the microphone seems quiet. */
  quietAfterMs?: number;
  env?: Env;
}

/** The engine ends a continuous recognition on its own now and then (a long silence, a time limit); it is carried on. */
const MAX_RESTARTS = 30;
/** An engine that ends again as soon as it is started is not going to work; after this many in a row, stop. */
const MAX_QUICK_ENDS = 3;
const QUICK_END_MS = 700;
/** After the person finishes it, the engine is given this long to hand over the last words before they are taken as they are. */
const FINISH_WAIT_MS = 2500;

/**
 * One dictation: start it, and it speaks through the handlers until the person finishes it (the last words are kept) or cancels
 * it (they are dropped). The engine's own ending in between is not the end of the dictation.
 */
export class Dictation {
  private rec: RecognitionLike | null = null;
  private wanted = false;
  private cancelled = false;
  private over = true;
  private phase: DictationPhase = 'starting';
  /** What has been heard but not settled: kept, so the words just before "done" are not lost if the engine does not settle them. */
  private pending = '';
  private restarts = 0;
  private quickEnds = 0;
  private startedAt = 0;
  private failed = false;
  private heard = false;
  private quietTimer: ReturnType<typeof setTimeout> | undefined;
  private finishTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(
    private h: DictationHandlers,
    private opts: DictationOptions,
  ) {}

  get listening(): boolean {
    return !this.over;
  }

  private setPhase(p: DictationPhase): void {
    if (this.phase === p || this.over) return;
    if (this.phase === 'finishing') return; // it is being wound up: nothing makes it listen again
    this.phase = p;
    this.h.onPhase(p);
  }

  start(): boolean {
    const env = this.opts.env ?? (globalThis as unknown as Env);
    const Ctor = env.SpeechRecognition ?? env.webkitSpeechRecognition;
    if (!Ctor || !this.over) return false;
    this.over = false;
    this.wanted = true;
    this.cancelled = this.failed = this.heard = false;
    this.pending = '';
    this.restarts = this.quickEnds = 0;
    this.phase = 'starting';
    this.h.onPhase('starting');
    this.quietTimer = setTimeout(() => {
      if (!this.heard && !this.over && this.wanted) this.h.onQuiet?.();
    }, this.opts.quietAfterMs ?? 7000);
    if (!this.launch(Ctor)) {
      this.conclude('failed');
      return false;
    }
    return true;
  }

  /** Creates and starts the engine's recogniser (again, if it ended on its own). */
  private launch(Ctor: NonNullable<Env['SpeechRecognition']>): boolean {
    const rec = new Ctor();
    rec.lang = this.opts.lang;
    rec.continuous = true;
    rec.interimResults = true;
    // Only when the person's browser can do it on the device is it asked to; otherwise the default (which may use a server) applies,
    // and the person was told so.
    if (this.opts.local && 'processLocally' in rec) rec.processLocally = true;
    rec.onstart = () => {
      if (this.rec === rec) this.setPhase('listening');
    };
    const hear = (on: boolean) => () => {
      if (this.rec !== rec) return;
      if (on) this.heard = true;
      this.setPhase(on ? 'hearing' : 'listening');
    };
    rec.onaudiostart = () => this.rec === rec && this.setPhase('listening');
    rec.onspeechstart = hear(true);
    rec.onspeechend = hear(false);
    rec.onresult = (e) => {
      if (this.rec !== rec) return;
      let interim = '';
      for (let i = 0; i < e.results.length; i++) {
        const r = e.results[i];
        if (r.isFinal) {
          if (i >= e.resultIndex) this.h.onFinal(r[0].transcript);
        } else interim = joinSpoken(interim, r[0].transcript);
      }
      this.heard = true;
      this.pending = interim;
      if (interim !== '') this.setPhase('hearing');
      this.h.onInterim(interim);
    };
    rec.onerror = (e) => {
      if (this.rec !== rec) return;
      const m = errorMessage(e.error, this.opts.inApp);
      if (m) {
        // Not being allowed, no microphone and no network are not mended by trying again.
        this.failed = true;
        this.wanted = false;
        this.h.onError(m);
        this.rec = null;
        try { rec.abort(); } catch { /* already ended */ }
        this.conclude('failed');
      }
    };
    rec.onend = () => this.ended(rec);
    this.rec = rec;
    this.startedAt = Date.now();
    try {
      rec.start();
    } catch {
      this.rec = null;
      this.h.onError('Dictation could not start.');
      return false;
    }
    return true;
  }

  private ended(rec: RecognitionLike): void {
    if (this.rec !== rec) return;
    this.rec = null;
    if (this.wanted && !this.cancelled) {
      // It stopped by itself. What it had not settled is kept, and it listens again.
      this.settle();
      this.quickEnds = Date.now() - this.startedAt < QUICK_END_MS ? this.quickEnds + 1 : 0;
      const env = this.opts.env ?? (globalThis as unknown as Env);
      const Ctor = env.SpeechRecognition ?? env.webkitSpeechRecognition;
      if (Ctor && this.quickEnds < MAX_QUICK_ENDS && this.restarts < MAX_RESTARTS) {
        this.restarts++;
        if (this.launch(Ctor)) return;
      } else if (this.quickEnds >= MAX_QUICK_ENDS) {
        this.h.onError('Dictation stopped unexpectedly.');
      } else {
        this.h.onError('Dictation stopped after listening for a long time. What it heard is kept.');
      }
      this.failed = true;
      return this.conclude('failed');
    }
    this.conclude(this.cancelled ? 'cancelled' : this.failed ? 'failed' : 'finished');
  }

  /** What was heard and not settled becomes settled text, once. */
  private settle(): void {
    const p = this.pending;
    this.pending = '';
    if (p.trim() !== '' && !this.cancelled) this.h.onFinal(p);
    this.h.onInterim('');
  }

  private conclude(how: 'finished' | 'cancelled' | 'failed'): void {
    if (this.over) return;
    clearTimeout(this.quietTimer);
    clearTimeout(this.finishTimer);
    if (how !== 'cancelled') this.settle();
    else {
      this.pending = '';
      this.h.onInterim('');
    }
    this.over = true;
    this.wanted = false;
    this.rec = null;
    this.h.onEnd(how);
  }

  /** The person is done: the engine hands over what it has, which is then delivered, and the dictation ends. */
  finish(): void {
    if (this.over || !this.wanted) return;
    this.wanted = false;
    this.setPhase('finishing');
    const rec = this.rec;
    if (!rec) return this.conclude('finished');
    // Some engines never end after stop() when nothing was being heard; the person is not left waiting.
    this.finishTimer = setTimeout(() => {
      if (this.over) return;
      try {
        rec.abort();
      } catch {
        // already gone
      }
      this.rec = null;
      this.conclude(this.failed ? 'failed' : 'finished');
    }, FINISH_WAIT_MS);
    try {
      rec.stop();
    } catch {
      this.rec = null;
      this.conclude('finished');
    }
  }

  /** The person does not want it: it stops at once and nothing of it is kept. */
  cancel(): void {
    if (this.over) return;
    this.cancelled = true;
    this.wanted = false;
    const rec = this.rec;
    this.rec = null;
    try {
      rec?.abort();
    } catch {
      // already gone
    }
    this.conclude('cancelled');
  }
}
