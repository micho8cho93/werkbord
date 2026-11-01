// The services Werkbord offers to connect in one step. Each is a hosted MCP server the service's own company runs (Werkbord does not
// run or vouch for any of them): choosing one only fills in the connection form, which the person reviews and saves like any other.
// A new connection starts disabled, its tools start at "ask first", and credentials stay with the controller. Addresses were checked
// against each service's own documentation and by asking the server (it answers with a sign-in challenge); a service that cannot be
// signed in to without registering an app of your own (Slack) or that limits which programs may connect (Vercel) is not listed:
// "Add your own" covers it. Local servers that run a package on this computer are not listed either: they run code, so they are
// added by hand, where the command is in front of the person.

export interface CatalogService {
  id: string;
  name: string;
  /** What it gives an agent, in a few words. */
  about: string;
  url: string;
  /** How the person signs in: the service's own sign-in page (OAuth), or an access token they make there and paste. */
  auth: 'oauth' | 'bearer';
  /** One line after the form is filled in, about what is different for this service. */
  note?: string;
  /** The service's own page about its MCP server. */
  docs?: string;
}

export const MCP_CATALOG: readonly CatalogService[] = [
  { id: 'linear', name: 'Linear', about: 'Issues, projects and cycles', url: 'https://mcp.linear.app/mcp', auth: 'oauth', docs: 'https://linear.app/docs/mcp' },
  { id: 'atlassian', name: 'Jira and Confluence', about: 'Issues, boards and pages (Atlassian)', url: 'https://mcp.atlassian.com/v1/mcp', auth: 'oauth', note: 'Atlassian asks which site to share when you sign in.' },
  { id: 'notion', name: 'Notion', about: 'Pages, databases and docs', url: 'https://mcp.notion.com/mcp', auth: 'oauth', docs: 'https://developers.notion.com/docs/get-started-with-mcp' },
  { id: 'sentry', name: 'Sentry', about: 'Errors, issues and releases', url: 'https://mcp.sentry.dev/mcp', auth: 'oauth', docs: 'https://docs.sentry.io/ai/mcp/' },
  { id: 'figma', name: 'Figma', about: 'Design files and components', url: 'https://mcp.figma.com/mcp', auth: 'oauth', docs: 'https://help.figma.com/hc/en-us/articles/35281350665623' },
  { id: 'trello', name: 'Trello', about: 'Boards, lists and cards', url: 'https://mcp.trello.com/v1', auth: 'oauth', note: 'Trello asks which workspace to share, and lets you allow read, write and search separately.' },
  { id: 'supabase', name: 'Supabase', about: 'Postgres, storage and project settings', url: 'https://mcp.supabase.com/mcp?read_only=true', auth: 'oauth', note: 'Added read-only, so it cannot change your data. Remove “?read_only=true” from the address to allow changes.' },
  { id: 'github', name: 'GitHub', about: 'Repositories, issues and pull requests', url: 'https://api.githubcopilot.com/mcp/', auth: 'bearer', note: 'Paste a personal access token (made in GitHub’s settings, with only the access you want agents to have).', docs: 'https://docs.github.com/en/copilot/how-tos/context/model-context-protocol/using-the-github-mcp-server' },
];

/** Whether a saved connection is this service, by its address (ignoring a trailing slash and the query). */
export function isAdded(service: CatalogService, urls: readonly (string | undefined)[]): boolean {
  const key = (u: string) => { try { const x = new URL(u); return x.origin + x.pathname.replace(/\/$/, ''); } catch { return u; } };
  return urls.some(u => !!u && key(u) === key(service.url));
}

/** The catalog service a saved connection is, by its address, if it is one (so its mark can be shown beside it). */
export function serviceFor(url: string | undefined): CatalogService | undefined {
  return url ? MCP_CATALOG.find(s => isAdded(s, [url])) : undefined;
}

/** Where a request for a service that is not listed goes: a public issue the person writes themselves, so nothing is sent for them. */
export const CONNECTOR_REQUEST_URL = 'https://github.com/micho8cho93/werkbord/issues/new?title=' + encodeURIComponent('Connector request: ');
