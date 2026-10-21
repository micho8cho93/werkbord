/** Portable settings; clients may use different names for their HTTP transport field. */
export function clientConfig(endpoint: string, token: string, stdio = false): string {
  const url = new URL(endpoint);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('Enter an HTTP or HTTPS endpoint without credentials in the URL.');
  return JSON.stringify({ mcpServers: { werkbord: stdio
    ? { command: 'werkbord', args: ['mcp', '--url', url.href], env: { WERKBORD_MCP_TOKEN: token } }
    : { url: url.href, headers: { Authorization: `Bearer ${token}` } } } }, null, 2);
}

export function importConnection(text: string) {
  const parsed = JSON.parse(text);
  const servers = parsed.mcpServers;
  if (!servers || typeof servers !== 'object' || Array.isArray(servers) || Object.keys(servers).length !== 1) throw new Error('Paste a configuration with exactly one entry inside mcpServers.');
  const [name, value] = Object.entries(servers)[0] as [string, Record<string, unknown>];
  if (!value || typeof value !== 'object') throw new Error('The server entry must be an object.');
  if (Object.keys(value).some(key => !['command', 'args', 'url', 'headers', 'type', 'transport', 'env'].includes(key))) throw new Error('This configuration has unsupported options. Enter it manually so no settings are lost.');
  if (value.env && (typeof value.env !== 'object' || Object.keys(value.env).length)) throw new Error('This configuration includes environment values. Enter it manually and use environment references instead of pasting secrets.');
  if (value.url !== undefined && value.command !== undefined) throw new Error('Choose one transport: a service URL or a local executable.');
  const transport = value.type ?? value.transport;
  if (value.type && value.transport && value.type !== value.transport) throw new Error('The configuration declares two different transports. Enter it manually.');
  if (transport !== undefined && !(typeof value.url === 'string' ? ['http', 'streamable-http', 'streamable_http'] : ['stdio']).includes(String(transport))) throw new Error('Use Streamable HTTP or stdio. This configuration declares an unsupported transport.');
  if (typeof value.url === 'string') {
    const url = new URL(value.url);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('Use an HTTP or HTTPS service URL without embedded credentials.');
    const headers = value.headers;
    let bearerToken = '';
    if (headers) {
      if (typeof headers !== 'object' || Array.isArray(headers)) throw new Error('Headers must be an object.');
      for (const [key, val] of Object.entries(headers)) {
        if (key.toLowerCase() !== 'authorization' || typeof val !== 'string' || !/^Bearer \S+$/i.test(val)) throw new Error('Only a Bearer Authorization header can be imported. Configure other authentication manually.');
        bearerToken = val.slice(7);
      }
    }
    return { name, transport: 'streamable_http' as const, url: url.href, command: '', args: '', authMode: bearerToken ? 'bearer' as const : 'none' as const, bearerToken };
  }
  if (typeof value.command !== 'string' || !value.command.trim() || (value.args !== undefined && (!Array.isArray(value.args) || value.args.some(x => typeof x !== 'string' || /[\r\n]/.test(x))))) throw new Error('Provide an executable and a list of string arguments, or a service URL.');
  return { name, transport: 'stdio' as const, command: value.command, args: ((value.args ?? []) as string[]).join('\n'), url: '', authMode: 'none' as const, bearerToken: '' };
}
