// Vendor administration only. Never package this or a signing key in a customer app.
import {readFileSync,writeFileSync,mkdtempSync,rmSync,statSync} from 'node:fs';
import {createPrivateKey,createPublicKey,createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import path from 'node:path';
const args=process.argv.slice(2),value=name=>args[args.indexOf(name)+1];
if(args.length!==4||!args.includes('--project-ref')||!args.includes('--key-file'))throw Error('Usage: node provision-signing-key.mjs --project-ref REF --key-file PRIVATE_OWNER_ONLY_PEM');
const ref=value('--project-ref'),file=value('--key-file');
if(!/^[a-z]{20}$/.test(ref))throw Error('Check the destination Supabase project reference.');
if((statSync(file).mode&0o077)!==0)throw Error('The key input must be owner-only (chmod 600).');
const raw=readFileSync(file);let key;
try{key=createPrivateKey(raw);}finally{raw.fill(0);}
const pub=Buffer.from(createPublicKey(key).export({format:'jwk'}).x,'base64url');
if(createHash('sha256').update(pub).digest('hex')!=='b66484e825f6148e826f46e48f7134d7d00bb2ea139a700182dca1ef5ddd502b')throw Error('This is not the pinned production issuer.');
const dir=mkdtempSync(path.join(tmpdir(),'werkbord-licensing-secret-'));
try{
 const der=key.export({format:'der',type:'pkcs8'}),envFile=path.join(dir,'secrets.env');
 try{writeFileSync(envFile,'WERKBORD_LICENSE_SIGNING_PKCS8='+der.toString('base64')+'\nWERKBORD_LICENSING_ORIGINS=https://micho8cho93.github.io\n',{mode:0o600});}finally{der.fill(0);}
 execFileSync('supabase',['secrets','set','--env-file',envFile,'--project-ref',ref],{stdio:['ignore','pipe','pipe']});
 console.log('Pinned issuer provisioned in project '+ref+'. No private key was printed.');
}catch{throw Error('Provisioning failed. Confirm CLI administrative access and destination project; no secret values are logged.');}
finally{rmSync(dir,{recursive:true,force:true});}
