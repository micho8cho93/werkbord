// Brand marks for the connection catalog, keyed by the catalog's service id: a 24x24 SVG path and the service's own colour.
// A service with no mark here is drawn as a letter tile instead (BrandMark.svelte), so the catalog works with this empty.
// The marks are trademarks of their owners and are shown only to say which service a tile connects to; they do not mean the service
// endorses or is affiliated with Werkbord.

export interface BrandMark { path: string; colour: string }

export const BRAND_MARKS: Record<string, BrandMark> = {};
