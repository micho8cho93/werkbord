import {issuer,signDocument,verifyDocument,verifySnapshot} from "./verify.ts";

export function createHandler(settings:{project:string;publishable:string;secret:string;signingKey?:string;allowedOrigins:string[]},fetcher:typeof fetch=fetch,cryptoTools={signDocument,verifyDocument}){
const {project,publishable,secret}=settings;
async function rpc(name:string,token:string,key:string,body:unknown):Promise<unknown>{
 const res=await fetcher(project+"/rest/v1/rpc/"+name,{method:"POST",headers:{apikey:key,Authorization:token,"Content-Type":"application/json"},body:JSON.stringify(body),signal:AbortSignal.timeout(15000)});
 const data=await res.json();if(!res.ok)throw Error(data.message||"The register rejected the operation");return data;
}
return async function handler(req:Request):Promise<Response>{
 const origin=req.headers.get("Origin");
 const headers:Record<string,string>={"Content-Type":"application/json","Cache-Control":"no-store","X-Content-Type-Options":"nosniff","Vary":"Origin"};
 if(origin){
  if(!settings.allowedOrigins.includes(origin))return new Response(JSON.stringify({error:"This operator origin is not permitted"}),{status:403,headers});
  headers["Access-Control-Allow-Origin"]=origin;
  headers["Access-Control-Allow-Headers"]="authorization, apikey, content-type, x-client-info, x-werkbord-operator";
  headers["Access-Control-Allow-Methods"]="POST, OPTIONS";
 }
 if(req.method==="OPTIONS")return new Response(null,{status:204,headers});
 try{
  if(req.method!=="POST")return new Response(JSON.stringify({error:"POST required"}),{status:405,headers});
  const auth=req.headers.get("Authorization")||"";
  if(!auth.startsWith("Bearer ")||auth.length>8192)throw Error("Operator sign-in required");
  const userRes=await fetcher(project+"/auth/v1/user",{headers:{apikey:publishable,Authorization:auth},signal:AbortSignal.timeout(10000)});
  if(!userRes.ok)throw Error("Sign in again before issuing licenses");
  const user=await userRes.json();
  // The gateway and Auth have verified the token. The database rechecks MFA,
  // current operator authorisation and the live session on every operation.
  const tokenPayload=JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(auth.split(".")[1].replace(/-/g,"+").replace(/_/g,"/")),c=>c.charCodeAt(0))));
  if(tokenPayload.sub!==user.id||tokenPayload.aal!=="aal2"||!tokenPayload.session_id)throw Error("Verify your authenticator code first");
  const operator=await rpc("licensing_whoami",auth,publishable,{});
  if(operator!==user.id)throw Error("Operator authorisation required");
  const reader=req.body?.getReader();if(!reader)throw Error("Request body required");
  let size=0;const chunks:Uint8Array[]=[];
  for(;;){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>10*1024*1024){await reader.cancel();throw Error("Request too large");}chunks.push(value);}
  const joined=new Uint8Array(size);let offset=0;for(const chunk of chunks){joined.set(chunk,offset);offset+=chunk.length;}
  const input=JSON.parse(new TextDecoder("utf-8",{fatal:true}).decode(joined));
  let result;
  if(input.action==="restore"){
   await verifySnapshot(input.snapshot);
   result=await rpc("licensing_restore","Bearer "+secret,secret,{p_actor:user.id,p_session:tokenPayload.session_id,p_snapshot:input.snapshot,p_source_sha256:input.sourceSHA256});
  }else if(input.action==="complete"||input.action==="sign"){
   const op=await rpc("licensing_operation",auth,publishable,{p_id:input.operationId}) as {claims:Record<string,unknown>;state:string}|null;
   if(!op||!["pending","completed"].includes(op.state))throw Error("No pending authorised operation");
   let document=input.document;
   if(input.action==="sign"){
    if(!settings.signingKey)throw Error("Hosted signing is not configured; contact the operator administrator");
    const der=Uint8Array.from(atob(settings.signingKey),c=>c.charCodeAt(0));
    try{document=await cryptoTools.signDocument(op.claims,der);}finally{der.fill(0);}
   }
   const claims=await cryptoTools.verifyDocument(document);
   // Compare structured terms without relying on JSONB object ordering.
   const approved=op.claims as Record<string,unknown>;
   if(Object.keys(approved).length!==Object.keys(claims).length||Object.keys(claims).some(k=>claims[k]!==approved[k]))throw Error("Signed claims differ from the reserved operation");
   result=await rpc("licensing_complete","Bearer "+secret,secret,{p_id:input.operationId,p_actor:user.id,p_session:tokenPayload.session_id,p_document:document,p_fingerprint:issuer.fingerprint});
  }else if(input.action==="import"){
   await cryptoTools.verifyDocument(input.document);
   result=await rpc("licensing_import","Bearer "+secret,secret,{p_actor:user.id,p_session:tokenPayload.session_id,p_document:input.document,p_fingerprint:issuer.fingerprint,p_source:input.source});
  }else throw Error("Unsupported operator action");
  return new Response(JSON.stringify(result),{headers});
 }catch(e){return new Response(JSON.stringify({error:e instanceof Error?e.message:"Operation failed"}),{status:400,headers});}
}
}
