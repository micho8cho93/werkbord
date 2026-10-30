export {};
declare global { var WerkbordWork: { group(item: {done?: boolean; active?: boolean; attention?: boolean; blocked?: boolean; priority?: string; scheduledAt?: string; start?: string}, now?: number): 'now' | 'next' | 'later' | 'done' }; }
