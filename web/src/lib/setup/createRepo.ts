/** The default name for the example repository. */
export const DEFAULT_REPO_NAME = 'werkbord-hello';

const NAME = /^[A-Za-z0-9._-]{1,100}$/;

/** Why a repository name cannot be used, or an empty string when it can. */
export function repoNameProblem(name: string): string {
  const n = name.trim();
  if (!n) return 'Give the repository a name.';
  if (!NAME.test(n) || n === '.' || n === '..' || n.endsWith('.git')) return 'Use letters, numbers, dashes, dots or underscores.';
  return '';
}

/** What the confirmation says will happen, in the order it happens, so nothing is created that was not shown. */
export function createPlan(login: string, name: string, isPrivate: boolean): string[] {
  const full = `${login}/${name.trim()}`;
  return [
    `Create ${isPrivate ? 'a private' : 'a public'} repository ${full} on GitHub`,
    isPrivate ? 'Try to turn on GitHub Pages (private repositories need a paid GitHub plan for this)' : 'Turn on GitHub Pages for it',
    'Copy it to this computer and add it to Werkbord as a project',
  ];
}
