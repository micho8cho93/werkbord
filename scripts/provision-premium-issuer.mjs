// Run once on the vendor's release workstation. Never package this with the app.
// Private material goes to an owner-only file and Supabase via stdin, never argv/logs.
import {generateKeyPairSync, createPrivateKey, createPublicKey} from 'node:crypto';
import {mkdirSync, writeFileSync, readFileSync, existsSync, statSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import path from 'node:path';
const [project, directory] = process.argv.slice(2);
if (!/^[a-z]{20}$/.test(project || '') || !directory || !path.isAbsolute(directory)) throw Error('usage: node scripts/provision-premium-issuer.mjs PROJECT_REF ABSOLUTE_PRIVATE_DIRECTORY');
mkdirSync(directory, {recursive:true, mode:0o700});
if (statSync(directory).mode & 0o077) throw Error('The issuer directory must be owner-only (0700).');
const privatePath = path.join(directory, 'premium-issuer.pem');
let key;
if (existsSync(privatePath)) {
  if (statSync(privatePath).mode & 0o077) throw Error('The private key must be owner-only (0600).');
  key = createPrivateKey(readFileSync(privatePath));
} else {
  key = generateKeyPairSync('ed25519').privateKey;
  writeFileSync(privatePath, key.export({type:'pkcs8',format:'pem'}), {flag:'wx', mode:0o600});
}
if (key.asymmetricKeyType !== 'ed25519') throw Error('An Ed25519 Premium issuer is required.');
const pub = createPublicKey(key).export({type:'spki',format:'der'}).subarray(-32).toString('base64url');
const publicPath = path.join(directory, 'premium-issuer.pub');
if (existsSync(publicPath)) {
  if (readFileSync(publicPath,'utf8').trim() !== pub) throw Error('The retained public key differs; refusing to rotate the issuer.');
} else writeFileSync(publicPath, pub+'\n', {flag:'wx',mode:0o600});
const der = key.export({type:'pkcs8',format:'der'});
const result = spawnSync('supabase', ['secrets','set','--project-ref',project,'--env-file','/dev/stdin'], {input:`PREMIUM_SIGNING_KEY_PKCS8=${der.toString('base64')}\nPREMIUM_ISSUER_PUBLIC_KEY=${pub}\n`,encoding:'utf8'});
der.fill(0);
if (result.status !== 0) throw Error('Premium secret provisioning failed. The retained issuer is unchanged; check CLI authentication and retry.');
console.log('Premium issuer provisioned. Public key: '+pub);
console.log('Owner-only recovery primary: '+privatePath);
console.log('Set PREMIUM_ISSUER_PUBLIC_KEY for app releases to the public key above.');
