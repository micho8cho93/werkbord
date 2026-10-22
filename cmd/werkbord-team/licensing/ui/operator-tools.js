import {Encrypter,Decrypter} from 'age-encryption';
import {verifySnapshot} from '../../../../supabase/functions/licensing-artifact/verify.ts';

export function qrImageURL(value){
 if(typeof value!=='string'||value.length>1024*1024)throw Error('The authenticator QR image is invalid. Use the manual setup key.');
 let svg=value;
 if(value.startsWith('data:image/svg+xml')){
  const comma=value.indexOf(',');if(comma<0)throw Error('The authenticator QR image is incomplete.');
  const metadata=value.slice(0,comma),payload=value.slice(comma+1);
  svg=metadata.includes(';base64')?atob(payload):payload.trimStart().startsWith('<')?payload:decodeURIComponent(payload);
 }
 if(!svg.trimStart().startsWith('<svg'))throw Error('The authenticator QR image is invalid. Use the manual setup key.');
 return 'data:image/svg+xml;charset=utf-8,'+encodeURIComponent(svg);
}

export async function encryptRegister(snapshot,passphrase){
 if(typeof passphrase!=='string'||passphrase.length<14)throw Error('Use a unique backup passphrase of at least 14 characters.');
 await verifySnapshot(snapshot);
 const e=new Encrypter();e.setScryptWorkFactor(16);e.setPassphrase(passphrase);
 return e.encrypt(JSON.stringify(snapshot));
}
export async function decryptRegister(encrypted,passphrase){
 if(encrypted.length>10*1024*1024)throw Error('Backup exceeds the 10 MiB recovery transfer limit.');
 // Refuse excessive scrypt work before invoking the library on untrusted files.
 const header=new TextDecoder().decode(encrypted.slice(0,1024));
 const match=header.match(/\n-> scrypt [A-Za-z0-9+/]+ (\d+)\n/);
 if(!match||Number(match[1])>18)throw Error('Choose an age passphrase backup with a supported work factor.');
 const d=new Decrypter();d.addPassphrase(passphrase);
 const raw=await d.decrypt(encrypted,'text'),snapshot=JSON.parse(raw);
 await verifySnapshot(snapshot);return snapshot;
}
