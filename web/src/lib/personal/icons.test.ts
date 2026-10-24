import { describe, expect, it } from 'vitest';
import { displayWorkspaceIcon, libraryWorkspaceIcon, newWorkspaceIcon, validWorkspaceIcon, workspaceIconCandidates, workspaceIcons } from './icons';
import { workspaceIconComponents } from './icon-components';

describe('workspace Lucide icon contract', () => {
  it.each([
    ['Notes', 'auto', 'lucide-notebook-pen'],
    ['Demo · Focus planner', 'workspace-1297741', 'lucide-target'],
    ['Demo · Reading journal', 'folder', 'lucide-book-open'],
    ['Demo · Launch room', 'projects', 'lucide-rocket'],
    ['Demo · Research hub', 'people', 'lucide-microscope'],
    ['Weekly planner', 'check', 'lucide-calendar-days'],
  ])('matches the server choice for %s', (name, old, expected) => {
    expect(displayWorkspaceIcon(old, name)).toBe(expected);
    expect(displayWorkspaceIcon(expected, 'Renamed')).toBe(expected);
    expect(libraryWorkspaceIcon(expected)).toBe(true);
  });
  it('has a bundled renderer for every approved icon and no extra renderers', () => {
    expect(Object.keys(workspaceIconComponents)).toEqual(workspaceIcons);
    expect(new Set(workspaceIcons).size).toBe(workspaceIcons.length);
  });
  it('picks the closest available match, including hidden and Team workspace choices', () => {
    const first = newWorkspaceIcon('Reading journal', []);
    const next = newWorkspaceIcon('Reading journal', [first!]);
    expect(first).toBe('lucide-book-open');
    expect(next).toBe('lucide-notebook-pen');
    expect(newWorkspaceIcon('Reading journal', workspaceIcons)).toBeUndefined();
    expect(workspaceIconCandidates('Untitled', 'Research interviews')[0]).toBe('lucide-microscope');
  });
  it.each(['lucide-not-real', 'lucide-rocket<script>', '<svg/>', 'https://example.com/icon.svg', 'workspace-0000000', 'workspace-1ffffff', 'workspace-fffffff'])('rejects unknown or unsafe icon %s', icon => {
    expect(validWorkspaceIcon(icon)).toBe(false);
  });
  it.each(['auto', 'folder', 'workspace-1297741'])('keeps historical token %s readable', icon => {
    expect(validWorkspaceIcon(icon)).toBe(true);
    expect(libraryWorkspaceIcon(displayWorkspaceIcon(icon, 'Notes'))).toBe(true);
  });
});
