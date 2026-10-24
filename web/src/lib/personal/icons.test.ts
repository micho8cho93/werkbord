import { describe, expect, it } from 'vitest';
import { displayWorkspaceIcon, generatedWorkspaceIcon, workspaceIconCells } from './icons';

describe('workspace glyph contract', () => {
  it.each([
    ['Notes', 'auto', 'workspace-1297741'],
    ['Weekly planner', 'check', 'workspace-101a1aa'],
    ['Café 🌿', 'calendar', 'workspace-0c9f71f'],
  ])('matches the server glyph for %s', (name, old, expected) => {
    expect(displayWorkspaceIcon(old, name)).toBe(expected);
    expect(displayWorkspaceIcon(expected, 'Renamed')).toBe(expected);
    expect(workspaceIconCells(expected, name).length).toBeGreaterThanOrEqual(8);
  });
  it.each(['check', 'calendar', 'workspace-0000000', 'workspace-1ffffff', 'workspace-fffffff', '<svg/>', 'https://example.com/icon.svg'])('rejects native or unsafe glyph %s', icon => {
    expect(generatedWorkspaceIcon(icon)).toBe(false);
  });
});
