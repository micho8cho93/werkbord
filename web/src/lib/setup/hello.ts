import type { Run, Task } from '../types';

/** The example ticket: small, quick, and a page anyone can see when it is done. */
export const EXAMPLE_TITLE = 'Build a “Getting started with Werkbord” page';

export const EXAMPLE_DESCRIPTION = [
  'Create a single static web page, index.html, in the root of this repository. It should be a simple, good-looking page with the heading “I’m getting started with Werkbord”. No build step, no dependencies; the page styles itself.',
  'Also add .github/workflows/pages.yml: a GitHub Actions workflow that publishes the repository root with GitHub Pages (actions/configure-pages, actions/upload-pages-artifact and actions/deploy-pages), on every push to main and on manual run.',
  'Commit your work. Do not change anything else.',
].join('\n\n');

/** Where the example ticket stands, from its task and latest run. */
export type ExampleStage =
  | 'no-ticket' // no task yet
  | 'not-started' // the task is saved but nothing has run
  | 'working' // the assistant is on it
  | 'needs-you' // it is waiting for an answer or is blocked
  | 'failed' // it stopped, or finished without changes
  | 'review' // it finished with changes to look at
  | 'done'; // the task is closed

export function exampleStage(task: Task | undefined, run: Run | undefined): ExampleStage {
  if (!task) return 'no-ticket';
  if (task.state === 'done') return 'done';
  if (!run) return 'not-started';
  switch (run.state) {
    case 'starting':
    case 'running':
      return 'working';
    case 'waiting_for_user':
    case 'blocked':
      return 'needs-you';
    case 'completed':
      return run.branch ? 'review' : 'failed';
    default:
      return 'failed';
  }
}

/** One line for each stage, in the words the rest of the app uses. */
export function stageText(stage: ExampleStage, run?: Run): string {
  switch (stage) {
    case 'no-ticket': return 'No task yet.';
    case 'not-started': return 'The task is saved. The agent has not started.';
    case 'working': return run?.activity ? `Working: ${run.activity}` : 'The agent is working on it.';
    case 'needs-you': return 'The agent needs your attention. Open the task to respond.';
    case 'failed': return run?.state === 'completed' ? 'The agent finished without making changes.' : 'The agent stopped before finishing.';
    case 'review': return 'Done. The changes are ready for you to look at.';
    case 'done': return 'The task is done.';
  }
}

/** The steps after the repository, in order. The publish step reads differently with no GitHub. */
export function publishLabel(variant: 'github' | 'local' | undefined): string {
  return variant === 'local' ? 'Merge the page' : 'Publish the page';
}
