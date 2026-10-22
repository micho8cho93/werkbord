import {issuer,verifyDocument,verifySnapshot} from "./verify.ts";

const project=Deno.env.get("SUPABASE_URL")!;
const publishable=Deno.env.get("SUPABASE_ANON_KEY")!;
const secret=Deno.env.get("SUPABASE_SERVICE_ROLE_KEY")!;
async function rpc(name:string,token:string,key:string,body:unknown):Promise<unknown>{
 const res=await fetch(project+"/rest/v1/rpc/"+name,{method:"POST",headers:{apikey:key,Authorization:token,"Content-Type":"application/json"},body:JSON.stringify(body),signal:AbortSignal.timeout(15000)});
 const data=await res.json();if(!res.ok)throw Error(data.message||"The register rejected the operation");return data;
}
export async function handler(req:Request):Promise<Response>{
 const headers={"Content-Type":"application/json","Cache-Control":"no-store","X-Content-Type-Options":"nosniff"};
 try{
  if(req.method!=="POST")return new Response(JSON.stringify({error:"POST required"}),{status:405,headers});
  const auth=req.headers.get("Authorization")||"";
  if(!auth.startsWith("Bearer ")||auth.length>8192)throw Error("Operator sign-in required");
  const userRes=await fetch(project+"/auth/v1/user",{headers:{apikey:publishable,Authorization:auth},signal:AbortSignal.timeout(10000)});
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
  }else if(input.action==="complete"){
   const claims=await verifyDocument(input.document);
   const op=await rpc("licensing_operation",auth,publishable,{p_id:input.operationId}) as {claims:unknown;state:string}|null;
   if(!op||!["pending","completed"].includes(op.state))throw Error("No pending authorised operation");
   // Compare structured terms without relying on JSONB object ordering.
   const approved=op.claims as Record<string,unknown>;
   if(Object.keys(approved).length!==Object.keys(claims).length||Object.keys(claims).some(k=>claims[k]!==approved[k]))throw Error("Signed claims differ from the reserved operation");
   result=await rpc("licensing_complete","Bearer "+secret,secret,{p_id:input.operationId,p_actor:user.id,p_session:tokenPayload.session_id,p_document:input.document,p_fingerprint:issuer.fingerprint});
  }else if(input.action==="import"){
   await verifyDocument(input.document);
   result=await rpc("licensing_import","Bearer "+secret,secret,{p_actor:user.id,p_session:tokenPayload.session_id,p_document:input.document,p_fingerprint:issuer.fingerprint,p_source:input.source});
  }else throw Error("Unsupported operator action");
  return new Response(JSON.stringify(result),{headers});
 }catch(e){return new Response(JSON.stringify({error:e instanceof Error?e.message:"Operation failed"}),{status:400,headers});}
}
Deno.serve(handler);
