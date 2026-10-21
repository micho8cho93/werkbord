import { describe, expect, it } from 'vitest';
import { clientConfig, importConnection } from './mcp-setup';

describe('portable MCP setup', () => {
  it('generates HTTP settings that can be imported without losing authentication', () => {
    const config = clientConfig('https://private.example/mcp', 'test-token');
    expect(importConnection(config)).toMatchObject({ name: 'werkbord', transport: 'streamable_http', url: 'https://private.example/mcp', authMode: 'bearer', bearerToken: 'test-token' });
  });
  it('generates the installed stdio bridge with the correct environment', () => {
    const config = JSON.parse(clientConfig('http://localhost:7420/mcp', 'test-token', true));
    expect(config.mcpServers.werkbord).toEqual({ command: 'werkbord', args: ['mcp', '--url', 'http://localhost:7420/mcp'], env: { WERKBORD_MCP_TOKEN: 'test-token' } });
  });
  it('imports local process arguments without shell splitting', () => {
    expect(importConnection(JSON.stringify({ mcpServers: { local: { command: 'node', args: ['/a path/server.js', '--read-only'] } } }))).toMatchObject({ transport: 'stdio', args: '/a path/server.js\n--read-only' });
  });
  it('refuses ambiguous servers, unsupported authentication and environment secrets', () => {
    for (const value of [{ mcpServers: { old: { url: 'https://service.test', type: 'sse' } } }, {}, { mcpServers: { a: {}, b: {} } }, { mcpServers: { a: { url: 'https://service.test', headers: { 'X-Key': 'secret' } } } }, { mcpServers: { a: { command: 'node', env: { TOKEN: 'secret' } } } }, { mcpServers: { a: { command: 'node', args: ['a\nb'] } } }]) expect(() => importConnection(JSON.stringify(value))).toThrow();
  });
  it('rejects non-HTTP endpoints and embedded credentials', () => {
    for (const url of ['file:///tmp/mcp', 'https://secret:token@example.com/mcp']) {
      expect(() => clientConfig(url, 'test')).toThrow();
      expect(() => importConnection(JSON.stringify({ mcpServers: { a: { url } } }))).toThrow();
    }
  });
});
