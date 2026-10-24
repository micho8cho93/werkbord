// Generated workspace glyphs use a separate, closed grammar from app/action icons.
export const legacyWorkspaceIcons = ['overview', 'board', 'calendar', 'timeline', 'activity', 'analytics', 'projects', 'folder', 'check', 'people'] as const;
const maskLimit = 0x1ffffff;

export function generatedWorkspaceIcon(icon: string): boolean {
  if (!/^workspace-[01][0-9a-f]{6}$/.test(icon)) return false;
  const count = parseInt(icon.slice(10), 16).toString(2).replaceAll('0', '').length;
  return count >= 8 && count <= 17;
}
export function validWorkspaceIcon(icon: string): boolean {
  return icon === 'auto' || generatedWorkspaceIcon(icon) || legacyWorkspaceIcons.some(value => value === icon);
}

/** Must match domain.PersonalIconCandidate: FNV-1a over UTF-8, including the separator. */
export function workspaceIconCandidate(name: string, previous: string, attempt = 0): string {
  let hash = 2166136261;
  for (const byte of new TextEncoder().encode(`${name}\0${previous}`)) hash = Math.imul(hash ^ byte, 16777619) >>> 0;
  return `workspace-${((hash + Math.imul(attempt, 0x9e3779b9)) & maskLimit).toString(16).padStart(7, '0')}`;
}
export function displayWorkspaceIcon(icon: string, name: string): string {
  if (generatedWorkspaceIcon(icon)) return icon;
  for (let attempt = 0; ; attempt++) {
    const candidate = workspaceIconCandidate(name, icon, attempt);
    if (generatedWorkspaceIcon(candidate)) return candidate;
  }
}
export function newWorkspaceIcon(): string {
  const random = crypto.getRandomValues(new Uint32Array(1))[0];
  return displayWorkspaceIcon('auto', String(random));
}
export function workspaceIconCells(icon: string, name: string): number[] {
  const mask = parseInt(displayWorkspaceIcon(icon, name).slice(10), 16);
  return Array.from({ length: 25 }, (_, i) => i).filter(i => (mask & (1 << i)) !== 0);
}
