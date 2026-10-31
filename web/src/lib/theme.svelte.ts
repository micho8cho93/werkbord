// Light or dark. Unless the person picks one on this device, the operating system decides;
// the choice is remembered per device (public/theme.js applies it before the first paint).

import { isEmbedded } from '../../../internal/nativebridge/bridge.js';

const KEY = 'werkbord.theme';
type Choice = 'light' | 'dark' | '';

function stored(): Choice {
  try {
    const t = localStorage.getItem(KEY);
    return t === 'light' || t === 'dark' ? t : '';
  } catch {
    return '';
  }
}

const media = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null;

class Theme {
  choice = $state<Choice>(stored());
  private systemDark = $state(media?.matches ?? false);

  constructor() {
    media?.addEventListener('change', (e) => (this.systemDark = e.matches));
  }

  /** What is showing now. */
  get dark(): boolean {
    return this.choice ? this.choice === 'dark' : this.systemDark;
  }

  toggle(): void {
    this.set(this.dark ? 'light' : 'dark');
  }

  /** fromShell: the window's sidebar chose it and told this page, which has nobody to tell. */
  set(choice: Choice, fromShell = false): void {
    this.choice = choice;
    // In the desktop window the theme is the window's: a choice made on this page is told to the sidebar, which tells the rest.
    if (!fromShell && isEmbedded()) window.parent.postMessage({ type: 'werkbord.frame', event: 'theme', theme: choice }, '*');
    if (choice) document.documentElement.dataset.theme = choice;
    else delete document.documentElement.dataset.theme;
    try {
      if (choice) localStorage.setItem(KEY, choice);
      else localStorage.removeItem(KEY);
    } catch {
      // Not remembered in private mode; it still applies to this page.
    }
  }
}

export const theme = new Theme();

// The style is a second choice beside light or dark: how the interface is drawn, not which of its two colour modes shows.
// Paper is the default (flat, warm ivory, cobalt and amber); Graphite is cool blue-grey with an ink primary and a teal accent;
// All styles share flat controls and unchanged icons. Each has a light and dark palette, remembered per device; in the
// desktop window it is the window's, chosen in one place and told to every workspace, as the theme is.

const STYLE_KEY = 'werkbord.style';
export type StyleChoice = 'paper' | 'graphite' | 'classic' | 'cloud' | 'moss' | 'dune' | 'plum';
export const STYLES: readonly { id: StyleChoice; label: string; about: string }[] = [
  { id: 'paper', label: 'Paper', about: 'Warm white and charcoal with restrained cobalt. The default.' },
  { id: 'graphite', label: 'Graphite', about: 'Cool neutrals, an ink primary and a muted teal accent.' },
  { id: 'classic', label: 'Classic', about: 'Clean white and cobalt, with flat controls.' },
  { id: 'moss', label: 'Moss', about: 'Sage and forest green, calm and grounded.' },
  { id: 'dune', label: 'Dune', about: 'Warm mineral neutrals and a clay accent.' },
  { id: 'plum', label: 'Plum', about: 'Soft mauve and aubergine, with a restrained orchid accent.' },
  { id: 'cloud', label: 'Cloud', about: 'Clean white surfaces on a light cool grey, with a soft lavender accent.' },
];

export function isStyle(value: unknown): value is StyleChoice {
  return value === 'paper' || value === 'graphite' || value === 'classic' || value === 'cloud' || value === 'moss' || value === 'dune' || value === 'plum';
}

function storedStyle(): StyleChoice {
  try {
    const s = localStorage.getItem(STYLE_KEY);
    return s === 'graphite' || s === 'classic' || s === 'cloud' || s === 'moss' || s === 'dune' || s === 'plum' ? s : 'paper';
  } catch {
    return 'paper';
  }
}

class Look {
  choice = $state<StyleChoice>(storedStyle());

  /** fromShell: the window's sidebar chose it and told this page, which has nobody to tell. */
  set(choice: StyleChoice, fromShell = false): void {
    this.choice = choice;
    if (!fromShell && isEmbedded()) window.parent.postMessage({ type: 'werkbord.frame', event: 'style', style: choice }, '*');
    if (choice === 'paper') delete document.documentElement.dataset.style;
    else document.documentElement.dataset.style = choice;
    try {
      if (choice === 'paper') localStorage.removeItem(STYLE_KEY);
      else localStorage.setItem(STYLE_KEY, choice);
    } catch {
      // Not remembered in private mode; it still applies to this page.
    }
  }
}

export const look = new Look();
