// Light or dark. Unless the person picks one on this device, the operating system decides;
// the choice is remembered per device (public/theme.js applies it before the first paint).

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

  set(choice: Choice): void {
    this.choice = choice;
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
