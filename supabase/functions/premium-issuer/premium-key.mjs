// Match internal/entitlement's canonical JSON, including Go's HTML escaping.
const encoder = new TextEncoder();
export function canonicalClaims(input) {
  if (input.schema !== 1 || input.product !== 'werkbord' || !/^[A-Za-z0-9_-]{1,64}$/.test(input.id ?? '') ||
      typeof input.issuedAt !== 'string' || !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/.test(input.issuedAt) ||
      !Number.isFinite(Date.parse(input.issuedAt)) || new Date(input.issuedAt).toISOString().replace('.000Z', 'Z') !== input.issuedAt ||
      (input.holder !== undefined && (typeof input.holder !== 'string' || encoder.encode(input.holder).length > 80 || /[\x00-\x1f\x7f]/.test(input.holder)))) {
    throw Error('The Premium key has invalid details.');
  }
  const claims = {schema: 1, product: 'werkbord', id: input.id};
  if (input.holder) claims.holder = input.holder;
  claims.issuedAt = input.issuedAt;
  if (Object.keys(input).some(k => !['schema', 'product', 'id', 'holder', 'issuedAt'].includes(k))) throw Error('Unexpected Premium key details.');
  return JSON.stringify(claims).replace(/[<>&\u2028\u2029]/g, c => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'));
}
export const encode64 = bytes => btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
export function decode64(text) {
  if (!/^[A-Za-z0-9_-]+$/.test(text)) throw Error('Invalid key encoding.');
  const bytes = Uint8Array.from(atob(text.replace(/-/g, '+').replace(/_/g, '/')), c => c.charCodeAt(0));
  if (encode64(bytes) !== text) throw Error('Invalid key encoding.');
  return bytes;
}
export const signedBytes = payload => encoder.encode('werkbord/premium/v1\0' + payload);
export async function verifyKey(text, publicKey) {
  if (typeof text !== 'string' || text.length > 1024) throw Error('Choose a Werkbord Premium unlock key.');
  const match = text.trim().match(/^wbpremium-([A-Za-z0-9_-]+)\.([A-Za-z0-9_-]+)$/);
  if (!match) throw Error('Choose a Werkbord Premium unlock key, not an old Team licence.');
  const raw = new TextDecoder('utf-8', {fatal: true}).decode(decode64(match[1]));
  const claims = JSON.parse(raw);
  if (canonicalClaims(claims) !== raw) throw Error('The Premium key details were changed.');
  const key = await crypto.subtle.importKey('raw', decode64(publicKey), 'Ed25519', false, ['verify']);
  if (!await crypto.subtle.verify('Ed25519', key, decode64(match[2]), signedBytes(raw))) throw Error('This key was not signed by the configured Premium issuer.');
  return claims;
}
