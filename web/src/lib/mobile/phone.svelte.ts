// Whether this page is being used as a phone: a narrow window in a browser or an installed app. The same width the stylesheets
// change layout at (899px), so the Control Deck and the CSS never disagree. Inside the desktop app a narrow window is still the
// desktop shell's page, with its own sidebar, so it never gets the phone's navigation.

import { embedded } from '../embed';

export const PHONE_QUERY = '(max-width: 899px)';
const query = typeof matchMedia === 'function' ? matchMedia(PHONE_QUERY) : null;

class Phone {
  desktopClient = $state(false);
  private narrow = $state(query?.matches ?? false);

  constructor() {
    query?.addEventListener('change', (e) => (this.narrow = e.matches));
  }

  get on(): boolean {
    return this.narrow && !embedded() && !this.desktopClient;
  }
}

export const phone = new Phone();
