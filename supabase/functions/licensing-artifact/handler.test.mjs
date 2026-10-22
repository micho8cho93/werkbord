import test from 'node:test';
import assert from 'node:assert/strict';
import {createHandler} from './handler.ts';
const origin='https://micho8cho93.github.io',actor='operator-fixture',session='fixture-session';
const claims={schema:2,product:'werkbord-team',id:'lic_fixture',customer:'Fixture',edition:'team',seats:10,issuedAt:'2026-10-22T00:00:00Z'};
function fixture({aal='aal2',allowed=true,verified=true,pending=true}={}){
 const calls=[];
 const payload=Buffer.from(JSON.stringify({sub:actor,aal,session_id:session})).toString('base64url');
 const token='Bearer header.'+payload+'.verified-by-auth';
 const fetcher=async(url,init)=>{
  const name=url.split('/').pop();calls.push({name,body:init.body&&JSON.parse(init.body),auth:init.headers.Authorization});
  if(name==='user')return Response.json(verified?{id:actor}:{message:'Invalid token'},{status:verified?200:401});
  if(name==='licensing_whoami')return Response.json(allowed?actor:{message:'Operator authorisation denied'},{status:allowed?200:403});
  if(name==='licensing_operation')return Response.json(pending?{claims,state:'pending'}:null);
  if(name==='licensing_complete')return Response.json({license_id:claims.id,document:JSON.parse(init.body).p_document});
  throw Error('Unexpected network operation: '+name);
 };
 const signed=JSON.stringify({claims,signature:'fixture'});
 const tools={signDocument:async c=>{assert.deepEqual(c,claims);return signed;},verifyDocument:async document=>{assert.equal(document,signed);return claims;}};
 const handler=createHandler({project:'https://fixture.supabase.co',publishable:'public',secret:'server-only',signingKey:btoa('fixture-key'),allowedOrigins:[origin]},fetcher,tools);
 const request=(body={action:'sign',operationId:'fixture-op'},source=origin)=>new Request('https://fixture.supabase.co/functions/v1/licensing-artifact',{method:'POST',headers:{Origin:source,Authorization:token,'Content-Type':'application/json'},body:JSON.stringify(body)});
 return {calls,handler,request};
}
test('hosted issuance fetches reserved terms and completes only after live MFA/operator verification',async()=>{
 const f=fixture(),res=await f.handler(f.request({action:'sign',operationId:'fixture-op',claims:{seats:999}}));
 assert.equal(res.status,200);assert.equal(res.headers.get('Access-Control-Allow-Origin'),origin);
 assert.deepEqual(f.calls.map(c=>c.name),['user','licensing_whoami','licensing_operation','licensing_complete']);
 assert.equal(f.calls.at(-1).body.p_actor,actor);assert.equal(f.calls.at(-1).body.p_session,session);
 assert.equal(f.calls.at(-1).auth,'Bearer server-only');
 assert.equal(JSON.parse((await res.json()).document).claims.seats,10);
});
test('invalid login, missing MFA, disabled operator and missing operation cannot complete issuance',async()=>{
 for(const options of [{verified:false},{aal:'aal1'},{allowed:false},{pending:false}]){
  const f=fixture(options),res=await f.handler(f.request());assert.equal(res.status,400);
  assert.equal(f.calls.some(c=>c.name==='licensing_complete'),false);
 }
});
test('unexpected origins fail before authentication and permitted preflight exposes no credentials',async()=>{
 const f=fixture(),res=await f.handler(f.request(undefined,'https://evil.example'));
 assert.equal(res.status,403);assert.equal(f.calls.length,0);assert.equal(res.headers.get('Access-Control-Allow-Origin'),null);
 const preflight=await f.handler(new Request('https://fixture.example',{method:'OPTIONS',headers:{Origin:origin}}));
 assert.equal(preflight.status,204);assert.equal(preflight.headers.get('Access-Control-Allow-Origin'),origin);assert.equal(f.calls.length,0);
});
