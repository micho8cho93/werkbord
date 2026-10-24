/**
 * Fixed user-editable draft. No saved content or record values become instructions.
 * A new workspace opens with a general statement and no example: whatever the person types after it is the type and style.
 */
export function personalWorkspaceDraft(id: string): string {
  if (id && !/^pws_[a-z2-7]{16}$/.test(id)) throw new Error('Invalid personal workspace ID.');
  return id
    ? `Open personal workspace ${id} from SQLite and help me modify its design. Preserve its collection records and bindings.`
    : 'We are about to create a new generative workspace. Everything I write after this line describes the type of workspace I want and the style it should have. Use it to design the workspace, and ask me where it should appear before you propose it.\n\nThe workspace I want: ';
}
