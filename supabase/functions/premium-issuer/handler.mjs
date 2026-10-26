import {canonicalClaims, encode64, decode64, signedBytes, verifyKey} from './premium-key.mjs';

export function createHandler(settings, fetcher = fetch) {
  const {project, publishable, secret, signingKey, publicKey, allowedOrigins} = settings;
  async function rpc(name, auth, apiKey, body) {
    const response = await fetcher(`${project}/rest/v1/rpc/${name}`, {method: 'POST', headers: {apikey: apiKey, Authorization: auth, 'Content-Type': 'application/json'}, body: JSON.stringify(body), signal: AbortSignal.timeout(15000)});
    const data = await response.json();
    if (!response.ok) throw Error(data.message || 'The Premium register rejected the operation. Refresh before retrying.');
    return data;
  }
  return async req => {
    const origin = req.headers.get('Origin');
    const headers = {'Content-Type': 'application/json', 'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff', Vary: 'Origin'};
    const json = (data, status = 200) => new Response(JSON.stringify(data), {status, headers});
    if (origin && !allowedOrigins.includes(origin)) return json({error: 'This operator origin is not permitted.'}, 403);
    if (origin) Object.assign(headers, {'Access-Control-Allow-Origin': origin, 'Access-Control-Allow-Headers': 'authorization, apikey, content-type, x-client-info', 'Access-Control-Allow-Methods': 'POST, OPTIONS'});
    if (req.method === 'OPTIONS') return new Response(null, {status: 204, headers});
    if (req.method !== 'POST') return json({error: 'POST required.'}, 405);
    try {
      const auth = req.headers.get('Authorization') || '';
      if (!auth.startsWith('Bearer ') || auth.length > 8192) return json({error: 'Operator sign-in required.'}, 401);
      // Auth validates the JWT; the database also checks allow-list, MFA and live session.
      const userRes = await fetcher(`${project}/auth/v1/user`, {headers: {apikey: publishable, Authorization: auth}, signal: AbortSignal.timeout(10000)});
      if (!userRes.ok) return json({error: 'Sign in again before issuing Premium keys.'}, 401);
      const user = await userRes.json();
      const claims = JSON.parse(new TextDecoder().decode(decode64(auth.slice(7).split('.')[1])));
      if (claims.sub !== user.id || claims.aal !== 'aal2' || !claims.session_id) return json({error: 'Verify your authenticator code first.'}, 403);
      if (await rpc('licensing_whoami', auth, publishable, {}) !== user.id) return json({error: 'Operator authorisation required.'}, 403);
      const reader = req.body?.getReader();
      if (!reader) throw Error('Request body required.');
      const chunks = []; let size = 0;
      for (;;) { const {done, value} = await reader.read(); if (done) break; size += value.length; if (size > 4096) { await reader.cancel(); throw Error('Request too large.'); } chunks.push(value); }
      const bytes = new Uint8Array(size); let offset = 0;
      for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
      const input = JSON.parse(new TextDecoder('utf-8', {fatal: true}).decode(bytes));
      if (input.action === 'status') return json({ready: Boolean(signingKey && publicKey), publicKey: publicKey || ''});
      if (input.action === 'list') return json(await rpc('premium_list', auth, publishable, {}));
      if (input.action === 'read') return json(await rpc('premium_read', auth, publishable, {p_id: input.id}));
      if (input.action !== 'issue') throw Error('Unsupported Premium operation.');
      if (!signingKey || !publicKey) throw Error('Premium signing is not configured. Contact the operator administrator.');
      if (typeof input.requestId !== 'string' || !/^[0-9a-f-]{36}$/.test(input.requestId) || typeof input.holder !== 'string' || typeof input.orderReference !== 'string') throw Error('Check the request reference and holder.');
      const row = await rpc('premium_prepare', auth, publishable, {p_request: input.requestId, p_holder: input.holder, p_order: input.orderReference});
      if (row.unlock_key) { await verifyKey(row.unlock_key, row.issuer_public_key); return json(row); }
      const payload = canonicalClaims({schema: 1, product: 'werkbord', id: row.id, holder: row.holder, issuedAt: new Date(row.issued_at).toISOString().replace('.000Z', 'Z')});
      const der = Uint8Array.from(atob(signingKey), c => c.charCodeAt(0));
      let key;
      try { key = await crypto.subtle.importKey('pkcs8', der, 'Ed25519', false, ['sign']); } finally { der.fill(0); }
      const signature = new Uint8Array(await crypto.subtle.sign('Ed25519', key, signedBytes(payload)));
      const text = `wbpremium-${encode64(new TextEncoder().encode(payload))}.${encode64(signature)}`;
      await verifyKey(text, publicKey);
      return json(await rpc('premium_complete', `Bearer ${secret}`, secret, {p_id: row.id, p_actor: user.id, p_session: claims.session_id, p_key: text, p_issuer: publicKey}));
    } catch (error) {
      // No stack, signing material or upstream response body is logged.
      return json({error: error instanceof Error ? error.message : 'Premium issuance failed. Refresh before retrying.'}, 400);
    }
  };
}
