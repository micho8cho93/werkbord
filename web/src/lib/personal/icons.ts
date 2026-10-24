import catalogue from '../../../../internal/domain/personal_icon_catalog.json';

// Names only: SVG and components come from the bundled, approved Lucide registry.
export const legacyWorkspaceIcons = ['overview', 'board', 'calendar', 'timeline', 'activity', 'analytics', 'projects', 'folder', 'check', 'people'] as const;
export const workspaceIcons = catalogue.map(icon => `lucide-${icon.name}`);
export function libraryWorkspaceIcon(icon: string): boolean { return workspaceIcons.includes(icon); }
export function legacyWorkspaceGlyph(icon: string): boolean {
  if (!/^workspace-[01][0-9a-f]{6}$/.test(icon)) return false;
  const count = parseInt(icon.slice(10), 16).toString(2).replaceAll('0', '').length;
  return count >= 8 && count <= 17;
}
export function validWorkspaceIcon(icon: string): boolean {
  return icon === 'auto' || libraryWorkspaceIcon(icon) || legacyWorkspaceGlyph(icon) || legacyWorkspaceIcons.some(value => value === icon);
}
function words(text: string): Set<string> {
  return new Set((text.toLowerCase().match(/[\p{L}\p{N}]+/gu) ?? []).map(word => word.replace(/s$/, '')));
}
/** Must match domain.PersonalIconCandidates. Equal scores retain catalogue order. */
export function workspaceIconCandidates(name: string, context = ''): string[] {
  const title = words(name), detail = words(context);
  return catalogue.map(icon => {
    let score = 0;
    for (const word of words(`${icon.keywords.join(' ')} ${icon.name.replaceAll('-', ' ')}`)) {
      if (title.has(word)) score += 10;
      if (detail.has(word)) score++;
    }
    return {icon: `lucide-${icon.name}`, score};
  }).sort((a, b) => b.score - a.score).map(choice => choice.icon);
}
export function displayWorkspaceIcon(icon: string, name: string): string {
  return libraryWorkspaceIcon(icon) ? icon : workspaceIconCandidates(name)[0];
}
export function newWorkspaceIcon(name: string, used: Iterable<string>, context = ''): string | undefined {
  const occupied = new Set(used);
  return workspaceIconCandidates(name, context).find(icon => !occupied.has(icon));
}
