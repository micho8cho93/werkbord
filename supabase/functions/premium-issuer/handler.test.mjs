import test from 'node:test';
import assert from 'node:assert/strict';
import {generateKeyPairSync} from 'node:crypto';
import {createHandler} from './handler.mjs';
import {verifyKey} from './premium-key.mjs';
const actor = '00000000-0000-4000-8000-000000000001', session = '00000000-0000-4000-8000-000000000002';
const token = (changes={}) => 'x.' + Buffer.from(JSON.stringify({sub:actor, aal:'aal2', session_id:session, ...changes})).toString('base64url') + '.x';
function fixture(options={}) {
  const pair = generateKeyPairSync('ed25519');
  const signingKey = pair.privateKey.export({type:'pkcs8',format:'der'}).toString('base64');
  const publicKey = pair.publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64url');
  const settings = {project:'https://fixture.test',publishable:'public',secret:'service',signingKey,publicKey,allowedOrigins:['https://micho8cho93.github.io'],...options};
  const row = {id:'pk_fixture',request_id:'00000000-0000-4000-8000-000000000003',holder:'Ada <&> Zoë',order_reference:'order_1',issued_at:'2026-10-26T08:00:00+00:00',actor,unlock_key:null};
  const calls=[];
  const handler=createHandler(settings, async (url, request) => {
    const body=request.body ? JSON.parse(request.body) : null;
    calls.push({url,request,body});
    if(url.endsWith('/auth/v1/user')) return Response.json({id:actor},{status:options.authStatus||200});
    if(url.endsWith('/licensing_whoami')) return Response.json(options.operator === false ? null : actor);
    if(url.endsWith('/premium_prepare')) return Response.json(row);
    if(url.endsWith('/premium_complete')) {
      assert.equal(request.headers.Authorization,'Bearer service');
      assert.equal(body.p_actor,actor);assert.equal(body.p_session,session);
      row.unlock_key=body.p_key;row.issuer_public_key=body.p_issuer;row.completed_at='2026-10-26T08:00:01Z';return Response.json(row);
    }
    if(url.endsWith('/premium_list')) return Response.json([row]);
    if(url.endsWith('/premium_read')) return Response.json(row);
    throw Error('Unexpected request: '+url);
  });
  const request=(input,auth=token(),origin='https://micho8cho93.github.io')=>handler(new Request('https://fixture.test/functions/v1/premium-issuer',{method:'POST',headers:{Authorization:'Bearer '+auth,Origin:origin,'Content-Type':'application/json'},body:JSON.stringify(input)}));
  return {request,row,calls,publicKey,handler};
}
test('protected issue signs the customer format and retry recovers the same key',async()=>{
  const f=fixture();const input={action:'issue',requestId:f.row.request_id,holder:f.row.holder,orderReference:'order_1'};
  const first=await f.request(input);assert.equal(first.status,200);const issued=await first.json();
  assert.equal((await verifyKey(issued.unlock_key,f.publicKey)).holder,f.row.holder);
  assert.equal((await (await f.request(input)).json()).unlock_key,issued.unlock_key);
  assert.equal(f.calls.filter(c=>c.url.endsWith('/premium_complete')).length,1);
  assert.equal(first.headers.get('Cache-Control'),'no-store');
});
test('rejects unauthenticated, non-MFA, non-operator and wrong-origin callers before prepare',async()=>{
  for(const opts of [{authStatus:401},{operator:false},{aal:'aal1'},{sub:'someone-else'},{origin:'https://evil.test'}]) {
    const f=fixture(opts);const res=await f.request({action:'issue'},token({aal:opts.aal||'aal2',sub:opts.sub||actor}),opts.origin||'https://micho8cho93.github.io');
    assert.ok([401,403].includes(res.status));assert.equal(f.calls.some(c=>c.url.endsWith('/premium_prepare')),false);
  }
});
test('missing or mismatched issuer never completes a key',async()=>{
  for(const options of [{signingKey:undefined},{publicKey:'A'.repeat(43)}]) {
    const f=fixture(options);const response=await f.request({action:'issue',requestId:f.row.request_id,holder:'Ada',orderReference:''});
    assert.equal(response.status,400);assert.equal(f.calls.some(c=>c.url.endsWith('/premium_complete')),false);
  }
});
test('rejects legacy actions, oversized input and cross-origin preflight',async()=>{
  const f=fixture();
  for(const input of [{action:'expand'},{action:'issue',holder:'a'.repeat(5000)}]) assert.equal((await f.request(input)).status,400);
  assert.equal((await f.handler(new Request('https://fixture.test',{method:'OPTIONS',headers:{Origin:'https://evil.test'}}))).status,403);
  assert.equal((await f.handler(new Request('https://fixture.test',{method:'GET'}))).status,405);
});
