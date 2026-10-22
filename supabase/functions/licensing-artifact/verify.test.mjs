import test from 'node:test';
import assert from 'node:assert/strict';
import {generateKeyPairSync,sign,createHash} from 'node:crypto';
import {verifyDocument,verifySnapshot,signDocument} from './verify.ts';

const {privateKey,publicKey}=generateKeyPairSync('ed25519');
const rawKey=publicKey.export({format:'jwk'}).x;
const fp=createHash('sha256').update(Buffer.from(rawKey,'base64url')).digest('hex');
const claims={schema:2,product:'werkbord-team',id:'lic_protocol_fixture',customer:'Research & <Development>\u2028',edition:'team',seats:3,issuedAt:'2026-10-01T00:00:00Z',expiresAt:'2027-10-01T00:00:00Z'};
const canonical=c=>JSON.stringify(c).replace(/[<>&\u2028\u2029]/g,c=>'\\u'+c.charCodeAt(0).toString(16).padStart(4,'0'));
function document(c=claims,domain='werkbord-team/license/v2\0'){
 const raw=canonical(c),sig=sign(null,Buffer.from(domain+raw),privateKey).toString('base64url');
 return `{"claims":${raw},"signature":"${sig}"}\n`;
}
test('canonical Ed25519 files preserve Go escaping and optional dates',async()=>{
 assert.deepEqual(await verifyDocument(document(),rawKey),claims);
 const perpetual={...claims};delete perpetual.expiresAt;assert.deepEqual(await verifyDocument(document(perpetual),rawKey),perpetual);
});
test('production default rejects a different issuer',async()=>{await assert.rejects(verifyDocument(document()),/production issuer/);});
test('tampering, duplicate fields, whitespace changes and wrong domains fail',async()=>{
 for(const raw of [document().replace('"seats":3','"seats":10'),document().replace('"schema":2','"schema":2,"schema":2'),document().replace('"claims":','"claims": '),document(claims,'another-product\0')])await assert.rejects(verifyDocument(raw,rawKey));
});
test('unsupported or inconsistent terms fail even when signed',async()=>{
 for(const c of [{...claims,seats:0},{...claims,edition:'other'},{...claims,schema:3},{...claims,expiresAt:claims.issuedAt},{...claims,issuedAt:'2026-02-30T00:00:00Z'},{...claims,unknown:true}])await assert.rejects(verifyDocument(document(c),rawKey));
});
function snapshot(){const doc=document();return {schema:1,product:'werkbord-licensing-register',exportedAt:'2026-10-22T00:00:00Z',issuers:[{fingerprint:fp,public_key:rawKey,can_issue:true}],entitlements:[{id:'ent_fixture',current_revision:1,status:'active'}],operations:[{id:'op_fixture',request_reference:'manual-fixture',entitlement_id:'ent_fixture',revision:1,state:'completed'}],artifacts:[{license_id:claims.id,entitlement_id:'ent_fixture',revision:1,document:doc,claims,sha256:createHash('sha256').update(doc).digest('hex'),issuer_fingerprint:fp,operation_id:'op_fixture'}],events:[{id:1,entitlement_id:'ent_fixture',operation_id:'op_fixture'}]};}
test('complete snapshot verifies and altered history is refused',async()=>{
 assert.equal(await verifySnapshot(snapshot(),rawKey),1);
 for(const change of [s=>s.artifacts[0].sha256='0'.repeat(64),s=>s.artifacts[0].claims={...claims,seats:10},s=>s.operations=[],s=>s.artifacts=[],s=>s.events[0].operation_id='missing',s=>s.operations.push({...s.operations[0]})]){const s=snapshot();change(s);await assert.rejects(verifySnapshot(s,rawKey));}
});

test('hosted signer is deterministic, matches Go canonical terms and rejects an untrusted issuer',async()=>{
 const der=new Uint8Array(privateKey.export({format:'der',type:'pkcs8'}));
 const signed=await signDocument({...claims},der,rawKey);
 assert.equal(signed,document());assert.equal(await signDocument({...claims},der,rawKey),signed);
 await assert.rejects(signDocument(claims,der),/production issuer/);
 await assert.rejects(signDocument({...claims,customer:'é'.repeat(129)},der,rawKey),/UTF-8/);
 await assert.rejects(signDocument({...claims,customer:'broken\ud800'},der,rawKey),/UTF-8/);
});
