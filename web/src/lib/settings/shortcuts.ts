// The keys Werkbord answers to wherever it is opened.

/** `mac` says whether to write ⌘ or Ctrl. */
export function sharedShortcuts(mac: boolean): { keys: string; what: string }[] {
  const mod = mac ? '⌘' : 'Ctrl+';
  return [
    { keys: `${mod}K`, what: 'Go to a project, a page or a place' },
    { keys: `${mod}B`, what: 'Fold or show the sidebar' },
  ];
}

export const isMac = (): boolean => typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
