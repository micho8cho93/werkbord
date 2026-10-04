/** Copies text; false if the browser would not (it needs a secure page). The text is always selectable as a fallback. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
