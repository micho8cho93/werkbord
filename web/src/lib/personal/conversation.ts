/** Fixed user-editable draft. No saved content or record values become instructions. */
export function personalWorkspaceDraft(id: string): string {
  if (id && !/^pws_[a-z2-7]{16}$/.test(id)) throw new Error('Invalid personal workspace ID.');
  return id
    ? `Open personal workspace ${id} from SQLite and help me modify its design. Preserve its collection records and bindings.`
    : 'Create a weekly to-do workspace with deadlines and priorities. Help me choose where it should appear.';
}
