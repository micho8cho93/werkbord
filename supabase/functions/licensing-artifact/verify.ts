// Match Go encoding/json and license.CanonicalClaims exactly. Public keys only.
export const issuer = { publicKey:"ChctbT2EmgTYU37lb0e1_KkzSO9GR32pkRdcTR-UoYU", fingerprint:"b66484e825f6148e826f46e48f7134d7d00bb2ea139a700182dca1ef5ddd502b" };
function b64(s:string):Uint8Array {if(!/^[A-Za-z0-9_-]+$/.test(s))throw Error("Invalid signature encoding");return Uint8Array.from(atob(s.replace(/-/g,"+").replace(/_/g,"/")),c=>c.charCodeAt(0));}
function json(v:unknown):string{return JSON.stringify(v).replace(/[<>&\u2028\u2029]/g,c=>"\\u"+c.charCodeAt(0).toString(16).padStart(4,"0"));}
export function canonicalClaims(c:Record<string,unknown>):string{
 const ordered:Record<string,unknown>={schema:c.schema,product:c.product,id:c.id,customer:c.customer,edition:c.edition,seats:c.seats,issuedAt:c.issuedAt};
 if(c.expiresAt!==undefined)ordered.expiresAt=c.expiresAt;if(c.supportEndsAt!==undefined)ordered.supportEndsAt=c.supportEndsAt;
 if(Object.keys(c).length!==Object.keys(ordered).length)throw Error("Unexpected reserved license terms");
 return json(ordered);
}
export async function signDocument(claims:Record<string,unknown>,pkcs8:Uint8Array,publicKey=issuer.publicKey):Promise<string>{
 const canonical=canonicalClaims(claims);
 const key=await crypto.subtle.importKey("pkcs8",pkcs8 as BufferSource,{name:"Ed25519"},false,["sign"]);
 const signature=new Uint8Array(await crypto.subtle.sign("Ed25519",key,new TextEncoder().encode("werkbord-team/license/v2\0"+canonical)));
 const encoded=btoa(String.fromCharCode(...signature)).replace(/\+/g,"-").replace(/\//g,"_").replace(/=+$/g,"");
 const document=`{"claims":${canonical},"signature":"${encoded}"}\n`;
 await verifyDocument(document,publicKey);
 return document;
}
export async function verifyDocument(document:string,publicKey=issuer.publicKey):Promise<Record<string,unknown>>{
 if(new TextEncoder().encode(document).length>16384)throw Error("License file is too large");
 const parsed=JSON.parse(document),c=parsed.claims;
 if(!c||Object.keys(parsed).sort().join(",")!=="claims,signature")throw Error("Choose a signed license file");
 const allowed=["schema","product","id","customer","edition","seats","issuedAt","expiresAt","supportEndsAt"];
 if(Object.keys(c).some(k=>!allowed.includes(k))||c.schema!==2||c.product!=="werkbord-team"||c.edition!=="team"||typeof c.id!=="string"||c.id.length<1||c.id.length>128||typeof c.customer!=="string"||c.customer.length<1||c.customer.length>256||!Number.isInteger(c.seats)||c.seats<1||c.seats>1000000)throw Error("Invalid Team license claims");
 const encoder=new TextEncoder(),decoder=new TextDecoder();
 if(encoder.encode(c.id).length>128||encoder.encode(c.customer).length>256||decoder.decode(encoder.encode(c.id))!==c.id||decoder.decode(encoder.encode(c.customer))!==c.customer)throw Error("License names must fit the customer app's UTF-8 limits");
 function date(v:unknown):number{if(typeof v!=="string"||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(v)||!Number.isFinite(Date.parse(v))||new Date(v).toISOString().replace(".000Z","Z")!==v)throw Error("Invalid license date");return Date.parse(v);}
 const start=date(c.issuedAt);
 if(c.expiresAt!==undefined&&date(c.expiresAt)<=start)throw Error("Invalid expiry");
 if(c.supportEndsAt!==undefined&&date(c.supportEndsAt)<start)throw Error("Invalid support end");
 const canonical:Record<string,unknown>={schema:2,product:"werkbord-team",id:c.id,customer:c.customer,edition:"team",seats:c.seats,issuedAt:c.issuedAt};
 if(c.expiresAt!==undefined)canonical.expiresAt=c.expiresAt;if(c.supportEndsAt!==undefined)canonical.supportEndsAt=c.supportEndsAt;
 const claims=json(canonical),expected=`{"claims":${claims},"signature":${json(parsed.signature)}}`;
 if(document!==expected&&document!==expected+"\n")throw Error("The signed file is not canonical; preserve the original bytes");
 const sig=b64(parsed.signature);if(sig.length!==64)throw Error("Invalid signature length");
 const pub=await crypto.subtle.importKey("raw",b64(publicKey),{name:"Ed25519"},false,["verify"]);
 if(!await crypto.subtle.verify("Ed25519",pub,sig,new TextEncoder().encode("werkbord-team/license/v2\0"+claims)))throw Error("The license was not signed by the production issuer");
 return canonical;
}

export async function verifySnapshot(snapshot:Record<string,any>,publicKey=issuer.publicKey):Promise<number>{
 const expectedFingerprint=[...new Uint8Array(await crypto.subtle.digest("SHA-256",b64(publicKey)))].map(b=>b.toString(16).padStart(2,"0")).join("");
 if(snapshot.schema!==1||snapshot.product!=="werkbord-licensing-register"||!Number.isFinite(Date.parse(snapshot.exportedAt)))throw Error("Unsupported register backup");
 const arrays=["entitlements","operations","artifacts","events","issuers"];
 if(arrays.some(k=>!Array.isArray(snapshot[k])))throw Error("Incomplete register backup");
 const issuerIds=new Set();
 for(const i of snapshot.issuers){if(i.public_key!==publicKey||i.fingerprint!==expectedFingerprint||issuerIds.has(i.fingerprint))throw Error("Invalid pinned issuer registry");issuerIds.add(i.fingerprint);}
 if(!issuerIds.size)throw Error("Missing issuer registry");
 const ents=new Map<string,number>(),ops=new Map<string,any>(),refs=new Set(),ids=new Set(),revs=new Set(),events=new Set();
 for(const e of snapshot.entitlements){if(typeof e.id!=="string"||ents.has(e.id)||!Number.isInteger(e.current_revision)||e.current_revision<0||!["active","pending","closed"].includes(e.status))throw Error("Invalid entitlement history");ents.set(e.id,e.current_revision);}
 for(const op of snapshot.operations){if(!ents.has(op.entitlement_id)||!op.id||ops.has(op.id)||!op.request_reference||refs.has(op.request_reference))throw Error("Invalid operation history");ops.set(op.id,op);refs.add(op.request_reference);}
 for(const a of snapshot.artifacts){
   const claims=await verifyDocument(a.document,publicKey);
   const hash=[...new Uint8Array(await crypto.subtle.digest("SHA-256",new TextEncoder().encode(a.document)))].map(b=>b.toString(16).padStart(2,"0")).join("");
   const op=ops.get(a.operation_id),rev=a.entitlement_id+"/"+a.revision;
   if(a.sha256!==hash||a.license_id!==claims.id||a.issuer_fingerprint!==expectedFingerprint||!op||op.entitlement_id!==a.entitlement_id||op.revision!==a.revision||op.state!=="completed"||!Number.isInteger(a.revision)||a.revision<1||ids.has(a.license_id)||revs.has(rev))throw Error("Backup artifact metadata differs from the signed file");
   if(Object.keys(a.claims).length!==Object.keys(claims).length||Object.keys(claims).some(k=>a.claims[k]!==claims[k]))throw Error("Backup claims differ from their signed original");
   ids.add(a.license_id);revs.add(rev);
 }
 for(const [id,revision]of ents)for(let n=1;n<=revision;n++)if(!revs.has(id+"/"+n))throw Error("Missing signed revision");
 for(const ev of snapshot.events){const op=ops.get(ev.operation_id);if(!Number.isSafeInteger(ev.id)||ev.id<1||events.has(ev.id)||!op||op.entitlement_id!==ev.entitlement_id)throw Error("Invalid audit history");events.add(ev.id);}
 return snapshot.artifacts.length;
}
