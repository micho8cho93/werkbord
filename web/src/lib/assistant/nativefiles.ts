// Choosing files in the Mac app. The web view's own file chooser (a sheet that belongs to the window) can close again by itself
// when it is opened from a page inside a frame, which is where the assistant is; the app's own dialog does not. The app shows it,
// reads what the person chose and hands back names and contents, never paths. The page then treats them exactly as it would files
// from its own chooser: it asks before anything is shown to the assistant.

import { callDesktop } from '../desktop';

interface Chosen {
  name: string;
  /** Base64, as Go writes bytes. */
  data: string;
}

const bytesOf = (b64: string): Uint8Array<ArrayBuffer> => {
  const bin = atob(b64);
  const out = new Uint8Array(new ArrayBuffer(bin.length));
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
};

/** Asks the app for files. Resolves to none if the person cancels. Waits as long as the person takes. */
export async function chooseFiles(): Promise<File[]> {
  const chosen = await callDesktop<Chosen[] | null>('ChooseFiles', [], 0);
  return (chosen ?? []).map((c) => new File([bytesOf(c.data)], c.name, { type: 'text/plain' }));
}
