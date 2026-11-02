// Where the person's own project stands in Git, for the header. Read once when the project in view changes and again when the window is
// focused after a while: never on an interval, so the header adds no polling. A project that is not the person's own has nothing here
// (a Team's Git is what its members reported, and the header reads that from the summary).
import { ask } from './native';

export interface Head { branch: string; commit: string; detached?: boolean; unborn?: boolean }

const REFRESH_AFTER = 15_000;

class GitHead {
  head = $state<Head | null>(null);
  private project = '';
  private at = 0;
  private serial = 0;

  /** Read the head of `project` (a Personal project id), or clear it when there is none. A read already fresh for the same project is not repeated. */
  async load(project: string | null, now = Date.now()): Promise<void> {
    if (!project) { this.project = ''; this.head = null; this.serial++; return; }
    if (project === this.project && now - this.at < REFRESH_AFTER) return;
    if (project !== this.project) this.head = null;
    this.project = project;
    this.at = now;
    const serial = ++this.serial;
    try {
      const head = await ask((a) => a.ProjectHead(project));
      if (serial === this.serial) this.head = head;
    } catch {
      // No repository, or Personal cannot answer: the header simply says nothing about Git.
      if (serial === this.serial) this.head = null;
    }
  }
}

export const gitHead = new GitHead();
