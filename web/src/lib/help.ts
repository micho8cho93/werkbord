import { globalHref, projectHref, type GlobalView, type ProjectSection } from './location';

export type HelpDestination =
  | { kind: 'global'; view: GlobalView }
  | { kind: 'settings'; section?: string }
  | { kind: 'project'; repository: ProjectSection; work: ProjectSection };

export interface HelpArticle {
  id: string;
  title: string;
  summary: string;
  example: string;
  steps: readonly string[];
  recovery: string;
  searchTerms: readonly string[];
  destination: HelpDestination;
  linkLabel: string;
}

export const HELP_ARTICLES: readonly HelpArticle[] = [
  {
    id: 'mcp', title: 'Setting up an MCP server',
    summary: 'MCP lets a client use tools from a server. Connect Werkbord to an external server, or connect any compatible client to Werkbord’s built-in board server.',
    example: 'A notes service supplies an MCP URL for Werkbord to use. A separate planning client instead connects to Werkbord to read selected projects and propose changes.',
    steps: [
      'Open Settings → Connections → MCP services. To use another service, paste its single-server MCP JSON configuration or choose Add connection and enter its URL or local executable. Use the authentication method documented by that service.',
      'Review the imported fields and save. Test and discover checks the server and lists its tools; OAuth opens sign-in if required. Then enable the connection and choose Salie or Coding agents. Review each tool’s permission before use.',
      'To let another client use Werkbord, go to Connect a client to Werkbord on the same page. Give it a name and select only the projects and operations it needs. Create its credential and copy the generated connection settings while they are visible.',
      'For a client on another computer, use a reachable private controller address ending in /mcp. For stdio-only clients, select the local process option and install the werkbord command on that client’s computer. Paste the generated settings into your client and reconnect it.',
      'Ask the client to list projects to verify the connection. Personal board changes wait in Pending changes for your approval. Team task plans wait in Plans; direct Team ticket writes follow the issued permissions. Credentials are specific to your own computer or to a Team workspace.',
    ],
    recovery: 'If connection fails, check which side is the client, the server address, network access, and authentication. localhost means the computer running the client. Environment references name variables available to the controller process; they are not secret values. Never paste credentials into a chat. If you lose a new credential, revoke it and create another.',
    searchTerms: ['mcp', 'server', 'client', 'integration', 'stdio', 'streamable http', 'bearer', 'oauth', 'configuration'],
    destination: { kind: 'settings', section: 'mcp' }, linkLabel: 'Open MCP setup',
  },
  {
    id: 'answer',
    title: 'Answer an agent',
    summary: 'Workspace collects questions and other interruptions. Open the task to see its project and the exact request before choosing.',
    example: 'An agent asks, “Commit the fixture work?” Open the question, review its context and options, then choose the answer that matches your intent.',
    steps: [
      'Open Workspace or the question on its task card. The task title and project stay with the question.',
      'Read the prompt and available choices. Werkbord saves the answer, then the runner delivers it to the agent.',
      '“Answer saved. Waiting for the agent to receive it” means delivery is not confirmed yet. “Answer delivered” means the runner confirmed receipt.',
    ],
    recovery: 'If the answer status is uncertain, reconnect and check the question first. If Werkbord offers a retry, use the same answer; each question accepts one answer. The app disables answers when its report is stale or offline.',
    searchTerms: ['question', 'reply', 'needs attention', 'needs you', 'awaiting acknowledgment', 'awaiting acknowledgement', 'waiting for the agent to receive it', 'stale', 'offline'],
    destination: { kind: 'global', view: 'control' },
    linkLabel: 'Open Workspace',
  },
  {
    id: 'review',
    title: 'Inspect and merge work',
    summary: 'Review means the work is ready for your inspection. Accept work and Reject work record your verdict; Merge… changes the repository after confirmation.',
    example: 'A task reaches Review with a branch and reported test notes. Inspect the changes, treat those notes as reported rather than verified, then use Merge… only if you want to merge locally.',
    steps: [
      'Open the task in Review and choose Review changes to inspect its branch or pull request evidence.',
      'Accept work or Reject work records your assessment. It does not merge the branch or move the task.',
      'Merge… shows Werkbord’s repository check. Confirm only when the displayed evidence is current and the proposed merge is what you want.',
      'When your review is finished, Mark done moves the task to Done. This changes its board status; it does not merge a branch. The next-step button also moves tasks from Backlog to In progress and then to Review.',
    ],
    recovery: 'If evidence changes or Werkbord refuses the merge, return to the task and inspect the latest Git state. A successful local merge moves the task to Done and offers Undo for the task move.',
    searchTerms: ['review', 'merge', 'verdict', 'accept', 'reject', 'reported tests', 'verified tests', 'branch', 'pull request'],
    destination: { kind: 'project', repository: 'runs', work: 'board' },
    linkLabel: 'Open project work',
  },
  {
    id: 'continue',
    title: 'Continue a run',
    summary: 'A task keeps its run history and its current state. Open an In progress task to see the latest run and use the available reply, answer or run-again action.',
    example: 'If a run finished without changes and the task remains In progress, open the task and start another run when the task is ready.',
    steps: [
      'Open the task from the Board or Runs and read the latest reported state.',
      'If it is waiting for a decision, answer the question in place. If it is still running, use the task message field to send a follow-up.',
      'When a finished task offers Run again, review its current execution choices before starting another run.',
    ],
    recovery: 'A lost start response can leave the outcome uncertain. Check the task and Runs before trying again; do not replay a start when the app says it cannot tell whether it succeeded.',
    searchTerms: ['continue', 'run again', 'doing', 'message', 'follow-up', 'lost response', 'uncertain'],
    destination: { kind: 'project', repository: 'runs', work: 'board' },
    linkLabel: 'Open project work',
  },
  {
    id: 'plan-schedule',
    title: 'Plan dates or schedule a run',
    summary: 'Timeline dates plan when work should happen. Calendar schedules when Werkbord starts agent work. They answer different questions.',
    example: 'Put “Book the venue” on next week’s Timeline to plan the work. Add an execution schedule in Calendar only if an agent should start it at a chosen time.',
    steps: [
      'Use Timeline for planned task dates, milestones and dependencies. Setting a date there does not start an agent.',
      'Use Calendar for agent execution schedules. A scheduled task starts only under the configured runner and execution rules.',
      'Work projects have Timeline planning, but no Calendar or agent execution controls.',
    ],
    recovery: 'If you meant to plan work rather than launch an agent, edit the task on Timeline. If you meant to schedule execution, check the task’s Calendar entry and runner availability.',
    searchTerms: ['plan', 'planning', 'timeline', 'schedule', 'calendar', 'date', 'milestone', 'work project'],
    destination: { kind: 'project', repository: 'calendar', work: 'timeline' },
    linkLabel: 'Open planning or schedule',
  },
  {
    id: 'runner',
    title: 'Choose a runner',
    summary: 'A runner is the computer where an agent works. Project defaults and routing rules can choose it for new runs; task run settings can override the defaults. Opening a Team ticket in your Werkbord creates a local task for the handoff.',
    example: 'If the only eligible runner is Busy, wait for a slot or stop a run. An installed agent on a full runner is busy, not missing.',
    steps: [
      'Open Settings → Runners to check whether a runner is Idle, Working, Busy, Offline or Disabled.',
      'Use project Task defaults or Routing rules for the usual choice. Change run settings on a task when that run needs a different agent or runner.',
      'Before starting, confirm the selected runner is online and has capacity.',
    ],
    recovery: 'If no runner can take the task, wait for capacity or stop a run you no longer need. If it is Offline or Disabled, use the runner’s existing Settings controls to investigate its connection or configuration.',
    searchTerms: ['runner', 'machine', 'busy', 'capacity', 'routing', 'agent', 'offline', 'disabled'],
    destination: { kind: 'settings', section: 'runners' },
    linkLabel: 'Open Settings → Runners',
  },
  {
    id: 'disconnect',
    title: 'Recover after disconnection',
    summary: 'Werkbord may keep working while a browser is disconnected. A last reported state is not proof of what is happening now.',
    example: 'If a task save reports an uncertain outcome, reconnect and check the Board or use View saved task before creating it again.',
    steps: [
      'Reconnect to the Werkbord that owns the project and wait for a fresh report.',
      'Check the task or Runs to see what was saved or started before taking the action again.',
      'If answer delivery is still unconfirmed and Werkbord offers a retry, retry the same answer. Do not submit a different answer.',
    ],
    recovery: 'When the app says delivery or save is uncertain, check first instead of repeating the mutation. A stale or offline report keeps actions disabled until current evidence returns.',
    searchTerms: ['disconnect', 'offline', 'reconnect', 'last reported', 'draft', 'uncertain', 'saved task'],
    destination: { kind: 'global', view: 'control' },
    linkLabel: 'Open Workspace',
  },
  {
    id: 'counts',
    title: 'Understand Workspace counts',
    summary: 'Interruption badges count questions, blocked work and failures. They exclude tasks ready for Review; the Workspace page’s All list includes Review work.',
    example: 'A project can show one interruption while also having two tasks in Review. Open Workspace → All to see both kinds of work.',
    steps: [
      'Use the badge to spot work that needs a response or intervention.',
      'Use Workspace → All when you want the full list, including work ready for review.',
      'Open a task to see its project and source run before acting.',
    ],
    recovery: 'If a badge does not match the rows you expected, refresh the report and compare its scope: badges exclude Review, while All includes it.',
    searchTerms: ['count', 'badge', 'all', 'review', 'interruption', 'questions', 'blocked', 'failures'],
    destination: { kind: 'global', view: 'control' },
    linkLabel: 'Open Workspace',
  },
  {
    id: 'keyboard',
    title: 'Navigate with the keyboard',
    summary: 'Jump to is available from the page and from ⌘K on Mac or Ctrl+K elsewhere. It searches bundled destinations and tasks in the current project.',
    example: 'Press Ctrl+K, type “Runs”, then press Enter. Press N from a project page to open New task.',
    steps: [
      'Press ⌘K or Ctrl+K to open Jump to, type a project, section or task, then use the arrow keys and Enter.',
      'Press N on a project page to open New task. Press Escape to close a task panel or sheet.',
      'Tab through links and controls; shortcuts pause while you type in a field. Use the visible Skip to main content link to move past navigation.',
    ],
    recovery: 'If a shortcut does not open, first finish or leave the focused field or menu. Jump to is also a visible button in the page heading.',
    searchTerms: ['keyboard', 'shortcut', 'shortcuts', 'jump to', 'command k', 'control k', 'ctrl k', 'escape', 'skip'],
    destination: { kind: 'project', repository: 'board', work: 'board' },
    linkLabel: 'Open the current Board',
  },
];

export function searchHelp(query: string): HelpArticle[] {
  const terms = query.trim().toLocaleLowerCase();
  if (!terms) return [...HELP_ARTICLES];
  return HELP_ARTICLES.filter((article) => [
    article.title,
    article.summary,
    article.example,
    ...article.steps,
    article.recovery,
    ...article.searchTerms,
  ].some((text) => text.toLocaleLowerCase().includes(terms)));
}

export function helpHref(destination: HelpDestination, projectId: string, projectKind: string | undefined): string {
  if (destination.kind === 'global') return globalHref(destination.view);
  if (destination.kind === 'settings') return `${globalHref('settings')}${destination.section ? `?${destination.section}` : ''}`;
  if (!projectId) return globalHref('projects');
  return projectHref(projectId, projectKind === 'work' ? destination.work : destination.repository);
}
