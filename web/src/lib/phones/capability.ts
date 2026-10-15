// What this browser's sign-in allows. A phone has a credential of its own (`wbp_…`) that works on a reviewed list of
// routes (internal/api/phone.go); what is not on it is not shown, rather than shown and refused. The controller decides
// either way: this only keeps the screens honest.

import { getToken } from '../api';

/** Whether this browser is signed in as an enrolled phone. */
export function isPhone(): boolean {
  return getToken().startsWith('wbp_');
}
