/** Which tour is open now, so that two never stack: a page that opens while one is showing waits for its next visit. */
export const tourHost = $state({ open: '' });
