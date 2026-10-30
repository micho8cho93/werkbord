import { expect, it } from 'vitest';
import { parseAssistantRequest } from '../embed';
import { projectContextPrompt } from './project-context.svelte';
it('accepts only a bounded presentation context from the parent protocol', () => {
  const projectContext = { workspace: 'team:main', project: 'p1', label: 'App · Main' };
  expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', projectContext })?.projectContext).toEqual(projectContext);
  expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', projectContext: null })?.projectContext).toBeNull();
  for (const invalid of [[], 'project', {}, { ...projectContext, grant: true }, { ...projectContext, label: 'x'.repeat(501) }]) expect(parseAssistantRequest({ type: 'werkbord.assistant', action: 'open', projectContext: invalid })).toBeNull();
  expect(projectContextPrompt(projectContext)).toContain('grants no access');
  expect(projectContextPrompt(null)).toContain('All Projects');
  expect(projectContextPrompt(undefined)).toBe('');
});
