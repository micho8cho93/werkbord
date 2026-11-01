import { describe, expect, it } from 'vitest';
import { CONNECTOR_REQUEST_URL, isAdded, MCP_CATALOG } from './mcp-catalog';

describe('the connection catalog', () => {
  it('lists five to ten services, each once', () => {
    expect(MCP_CATALOG.length).toBeGreaterThanOrEqual(5);
    expect(MCP_CATALOG.length).toBeLessThanOrEqual(10);
    expect(new Set(MCP_CATALOG.map(s => s.id)).size).toBe(MCP_CATALOG.length);
    expect(new Set(MCP_CATALOG.map(s => s.name)).size).toBe(MCP_CATALOG.length);
  });
  it('offers only hosted HTTPS servers, with no credential in an address, as the connection form itself requires', () => {
    for (const s of MCP_CATALOG) {
      const u = new URL(s.url);
      expect(u.protocol, s.id).toBe('https:');
      expect(u.username + u.password, s.id).toBe('');
      expect(['oauth', 'bearer']).toContain(s.auth);
      if (s.docs) expect(new URL(s.docs).protocol).toBe('https:');
    }
  });
  it('says what a token is for when the service takes one instead of a sign-in', () => {
    for (const s of MCP_CATALOG.filter(x => x.auth === 'bearer')) expect(s.note, s.id).toMatch(/token/i);
  });
  it('adds Supabase read-only, so a change is something the person chooses', () => {
    expect(MCP_CATALOG.find(s => s.id === 'supabase')?.url).toContain('read_only=true');
  });
  it('knows a service is already connected by its address, not its name', () => {
    const linear = MCP_CATALOG.find(s => s.id === 'linear')!;
    expect(isAdded(linear, ['https://mcp.linear.app/mcp/'])).toBe(true);
    expect(isAdded(linear, ['https://mcp.linear.app/other', undefined])).toBe(false);
    expect(isAdded(MCP_CATALOG.find(s => s.id === 'supabase')!, ['https://mcp.supabase.com/mcp'])).toBe(true);
  });
  it('asks for a request through a public page the person writes', () => {
    expect(new URL(CONNECTOR_REQUEST_URL).hostname).toBe('github.com');
  });
});
