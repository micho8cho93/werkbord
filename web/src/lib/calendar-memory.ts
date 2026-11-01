// The week a calendar was left on, by the name its page gives it, so that opening a task from it and coming Back finds the same week
// rather than today. In memory only, like the rest of what a window remembers between visits.
const left = new Map<string, string>();

export const weekLeftOn = (key: string): string | undefined => (key ? left.get(key) : undefined);
export const leaveWeek = (key: string, anchor: string): void => { if (key) left.set(key, anchor); };
