// How the shell talks to the pages it shows in frames. A workspace's page cannot call the app; it posts a message to the page
// that framed it, and this decides whether to believe it. The rules are short on purpose: the message must come from the
// window of a frame the shell itself made, from the origin the app said that frame would be at; and it can only ask the app to
// do something (the app then decides, by the kind of workspace the frame is) or say where in itself the person is.

export interface FrameRef {
  /** The workspace the frame shows: the app's identity for it, never anything the page says. */
  id: string;
  /** The origin the app said the frame is loaded from. */
  origin: string;
  window: Window | null;
}

export interface MessageLike {
  source: unknown;
  origin: string;
  data: unknown;
}

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface Deps {
  relay(id: string, method: string, args: unknown[]): Promise<unknown>;
  remember(id: string, place: string): Promise<void>;
  ready(id: string): void;
  reply(frame: FrameRef, message: Record<string, unknown>): void;
  /** The person clicked the product name in a frame's own header: open the switcher there (rect is in the frame's viewport). */
  switcher(id: string, rect: Rect): void;
  /** A frame asks to show the person's own Werkbord at a place inside it. */
  open(id: string, target: 'personal', place: string): void;
}

/** A rectangle a frame says its switcher button occupies: four small finite numbers, nothing else. */
export function isRect(r: unknown): r is Rect {
  if (!r || typeof r !== 'object') return false;
  const o = r as Record<string, unknown>;
  return Object.keys(o).length === 4 && ['x', 'y', 'width', 'height'].every((k) => typeof o[k] === 'number' && Number.isFinite(o[k]) && Math.abs(o[k] as number) < 100000);
}

const METHOD = /^[A-Za-z]{1,40}$/;
const REQUEST_ID = /^[A-Za-z0-9_-]{1,64}$/;
/** A place inside a workspace's own page: the same rule the app applies (internal/workspace ValidHref). */
export const PLACE = /^[#?][A-Za-z0-9/_.:=&%?#-]{0,299}$/;

export function isPlace(s: unknown): s is string {
  if (typeof s !== 'string' || !PLACE.test(s) || s.includes('//') || s.includes('..')) return false;
  try {
    return !/[?&](token|join|invite)=/i.test(decodeURIComponent(s));
  } catch { return false; }
}

/** The frame a message came from, if it is one of ours and said so from the right origin. */
export function frameOf(frames: readonly FrameRef[], e: MessageLike): FrameRef | undefined {
  if (!e.source) return undefined;
  const f = frames.find((x) => x.window !== null && x.window === e.source);
  return f && f.origin === e.origin ? f : undefined;
}

/** Handles one message. Anything that is not exactly what a frame may send is dropped without an answer. */
export async function handle(frames: readonly FrameRef[], e: MessageLike, d: Deps): Promise<void> {
  const frame = frameOf(frames, e);
  const m = e.data as Record<string, unknown> | null;
  if (!frame || !m || typeof m !== 'object') return;
  if (m.type === 'werkbord.native.request') {
    const { id, method, args } = m as { id?: unknown; method?: unknown; args?: unknown };
    if (typeof id !== 'string' || !REQUEST_ID.test(id) || typeof method !== 'string' || !METHOD.test(method) || !Array.isArray(args) || args.length > 4) return;
    let size: number;
    try {
      size = JSON.stringify(args).length;
    } catch {
      return;
    }
    if (size > 8192) return;
    try {
      const result = await d.relay(frame.id, method, args);
      d.reply(frame, { type: 'werkbord.native.result', id, ok: true, result: result ?? null });
    } catch (err) {
      d.reply(frame, { type: 'werkbord.native.result', id, ok: false, error: err instanceof Error ? err.message : String(err) });
    }
    return;
  }
  if (m.type === 'werkbord.frame') {
    if (m.event === 'ready') d.ready(frame.id);
    else if (m.event === 'place' && isPlace(m.place)) await d.remember(frame.id, m.place).catch(() => {});
    else if (m.event === 'switcher' && isRect(m.rect)) d.switcher(frame.id, m.rect);
    else if (m.event === 'open' && m.target === 'personal' && isPlace(m.place) && String(m.place).startsWith('#')) d.open(frame.id, 'personal', m.place as string);
  }
}
