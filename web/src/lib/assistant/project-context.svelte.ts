// A presentation hint from the authenticated parent shell, never an access grant.
export interface AssistantProjectContext { workspace: string; project: string; label: string }
export const assistantProjectContext = $state<{ current: AssistantProjectContext | null | undefined }>({ current: undefined });
export function projectContextPrompt(context: AssistantProjectContext | null | undefined): string {
  if (context === undefined) return '';
  return '\nThe application project selection is ' + (context === null ? 'All Projects (authorised accessible projects).' : JSON.stringify(context)) +
    '\nUse this as the default context for this turn. It grants no access: verify current permissions and availability through the existing tools. Never substitute another project if this one is inaccessible.';
}
