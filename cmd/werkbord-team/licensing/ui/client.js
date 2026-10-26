import {createClient} from '@supabase/supabase-js';
import {Encrypter} from 'age-encryption';
import {verifyKey} from './premium-key.mjs';

const root = document.querySelector('#app');
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;'}[c]));
const draftName = 'werkbord.premium.request';
let db, config, session, factor, enrolment, rows = [], selected, current, readiness, busy = false;
let draft;
try { draft = JSON.parse(sessionStorage.getItem(draftName)); } catch {}
if (!draft?.requestId) draft = {requestId: crypto.randomUUID(), holder: '', orderReference: ''};
const saveDraft = () => { try { sessionStorage.setItem(draftName, JSON.stringify(draft)); } catch {} };
const date = value => new Date(value).toLocaleString(undefined, {dateStyle:'medium', timeStyle:'short'});
function notice(message, error = false) {
  const node = document.querySelector('#notice');
  if (node) { node.textContent = message; node.className = message ? `notice ${error ? 'error' : 'success'}` : ''; node.setAttribute('role', error ? 'alert' : 'status'); }
}
async function api(action, values = {}) {
  const {data, error} = await db.auth.getSession();
  if (error || !data.session) throw Error('Sign in again to open the Premium register.');
  const response = await fetch(`${config.supabaseURL}/functions/v1/premium-issuer`, {method:'POST', headers:{'Content-Type':'application/json', apikey:config.publishableKey, Authorization:`Bearer ${data.session.access_token}`}, body:JSON.stringify({action, ...values}), signal:AbortSignal.timeout(30000)});
  const result = await response.json();
  if (!response.ok) throw Error(result.error || 'The operation failed. Refresh before retrying.');
  return result;
}
async function working(button, message, fn) {
  if (busy) return;
  busy = true; const label = button?.textContent;
  if (button) { button.disabled = true; button.textContent = message; }
  for (const el of root.querySelectorAll('button,input')) el.disabled = true;
  notice(message);
  try { await fn(); } catch (error) { notice(error.message + ' Your request reference is kept for a safe retry.', true); }
  finally {
    busy = false;
    for (const el of root.querySelectorAll('button,input')) el.disabled = false;
    if (button?.isConnected) button.textContent = label;
    const issue = document.querySelector('#issue button');
    if (issue && !readiness?.ready) issue.disabled = true;
  }
}
function login() {
  root.innerHTML = `<main class="auth"><div class="wordmark">werkbord</div><h1>Premium license operator</h1><p>Issue permanent Premium unlock keys, one per person. Sign in with your authorised operator account.</p><div id="notice" role="status"></div><form id="login" class="stack"><label>Email<input name="email" type="email" autocomplete="username" required></label><label>Password<input name="password" type="password" autocomplete="current-password" required></label><button class="primary">Sign in</button></form><details><summary>Create an operator account</summary><p>Only email addresses authorised by the administrator can access the register. Confirm your email before signing in.</p><form id="signup" class="stack"><label>Email<input name="email" type="email" autocomplete="username" required></label><label>New password<input name="password" type="password" autocomplete="new-password" minlength="14" required></label><button>Create account</button></form></details><p><small>Operator sign-in and your authenticator protect server signing. Customers paste their key into Settings → Plan; their app checks it offline.</small></p></main>`;
  for (const kind of ['login', 'signup']) document.querySelector(`#${kind}`).onsubmit = event => {
    event.preventDefault(); const form = event.currentTarget; const values = new FormData(form);
    working(form.querySelector('button'), kind === 'login' ? 'Signing in…' : 'Creating account…', async () => {
      const {error} = await db.auth[kind === 'login' ? 'signInWithPassword' : 'signUp']({email:values.get('email'), password:values.get('password')});
      if (error) throw error;
      form.reset();
      if (kind === 'signup') notice('Check your email to confirm the account, then sign in here.');
      else await establish();
    });
  };
}
async function mfa() {
  const {data, error} = await db.auth.mfa.listFactors(); if (error) throw error;
  const verified = data.totp.find(f => f.status === 'verified'); factor = verified?.id || factor;
  root.innerHTML = `<main class="auth"><div class="wordmark">werkbord</div><h1>${verified ? 'Verify your authenticator' : 'Set up your authenticator'}</h1><p>Verify your six-digit code to access Premium issuance.</p><div id="notice" role="status"></div>${!verified && !enrolment ? '<button class="primary" id="enrol">Set up authenticator</button>' : ''}${enrolment ? `<p>Add a time-based account to your authenticator using this setup key.</p><label>Manual setup key<input readonly id="setup" value="${esc(enrolment.secret)}" autocomplete="off" spellcheck="false"></label><button id="copy-setup">Copy setup key</button><p><small>Account: ${esc(session.user.email)} · issuer: Werkbord · six digits, 30-second interval.</small></p>` : ''}${factor ? '<form id="mfa" class="stack"><label>Authenticator code<input name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required></label><button class="primary">Verify and open register</button></form>' : ''}<button class="link" id="signout">Sign out</button></main>`;
  // Display the provider SVG as an image, never inject it as document markup.
  if (enrolment?.qr_code) {
    const image = document.createElement('img'); image.className = 'qrcode'; image.alt = 'Authenticator enrolment QR code';
    let svg = enrolment.qr_code;
    if (svg.startsWith('data:image/svg+xml')) {
      const comma = svg.indexOf(','); const metadata = svg.slice(0, comma); const payload = svg.slice(comma + 1);
      svg = metadata.includes(';base64') ? atob(payload) : payload.trimStart().startsWith('<') ? payload : decodeURIComponent(payload);
    }
    image.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
    image.onerror = () => image.remove(); document.querySelector('#setup').closest('label').before(image);
  }
  document.querySelector('#signout').onclick = signout;
  document.querySelector('#copy-setup')?.addEventListener('click', () => copyText(enrolment.secret, document.querySelector('#setup')));
  document.querySelector('#enrol')?.addEventListener('click', event => working(event.currentTarget, 'Preparing authenticator…', async () => {
    for (const old of data.all.filter(f => f.factor_type === 'totp' && f.status === 'unverified')) { const {error} = await db.auth.mfa.unenroll({factorId:old.id}); if (error) throw error; }
    const {data: result, error} = await db.auth.mfa.enroll({factorType:'totp', friendlyName:'Werkbord Premium', issuer:'Werkbord'});
    if (error) throw error; factor = result.id; enrolment = result.totp; await mfa();
  }));
  document.querySelector('#mfa')?.addEventListener('submit', event => {
    event.preventDefault(); const form = event.currentTarget; const code = new FormData(form).get('code');
    working(form.querySelector('button'), 'Verifying…', async () => {
      const {error} = await db.auth.mfa.challengeAndVerify({factorId:factor, code}); if (error) throw error;
      enrolment = null; await establish();
    });
  });
}
async function establish() {
  const {data} = await db.auth.getSession(); session = data.session;
  if (!session) { login(); return; }
  const {data: assurance, error} = await db.auth.mfa.getAuthenticatorAssuranceLevel(); if (error) throw error;
  if (assurance.currentLevel !== 'aal2') { await mfa(); return; }
  readiness = await api('status'); rows = await api('list'); render();
}
async function signout() {
  const {error} = await db.auth.signOut(); if (error) { notice(error.message, true); return; }
  session = null; current = null; rows = []; enrolment = null; factor = null; selected = null;
  draft = {requestId:crypto.randomUUID(), holder:'', orderReference:''}; sessionStorage.removeItem(draftName); login();
}
function register() {
  const search = document.querySelector('#search').value.toLowerCase();
  const list = rows.filter(row => `${row.holder} ${row.id} ${row.order_reference}`.toLowerCase().includes(search));
  document.querySelector('#register').innerHTML = list.length ? `<table><thead><tr><th scope="col">Person</th><th scope="col">Status</th></tr></thead><tbody>${list.map(row => `<tr class="${selected === row.id ? 'selected' : ''}"><td><button data-select="${esc(row.id)}" aria-pressed="${selected === row.id}">${esc(row.holder)}</button><small class="mono">${esc(row.id)}</small></td><td>${row.completed_at ? 'Issued' : 'Prepared'}</td></tr>`).join('')}</tbody></table>` : `<p class="empty">${search ? 'No people match your search.' : 'No Premium keys yet. Issue a key for the first person using the form beside this register.'}</p>`;
  for (const button of root.querySelectorAll('[data-select]')) button.onclick = () => working(button, 'Opening…', async () => {
    const row = await api('read', {id:button.dataset.select});
    if (!row) throw Error('This key could not be found. Refresh the register.');
    if (row.unlock_key) await verifyKey(row.unlock_key, row.issuer_public_key);
    selected = row.id; current = row; register(); detail(); notice('');
  });
}
function render() {
  root.innerHTML = `<div class="shell"><header class="topbar"><div><span class="wordmark">werkbord</span><small>Premium · operator console</small></div><div class="row"><span class="muted">Protected server signing</span><button id="backup">Back up Premium register</button><button id="signout">Sign out</button></div></header><div class="toolbar row between"><h1>Premium keys</h1><div class="row"><label class="sr-only" for="search">Search people, key IDs or orders</label><input id="search" placeholder="Search person, key ID or order"><button id="refresh">Refresh</button><button class="primary" id="new">New Premium key</button></div></div><div class="plan-summary"><p><strong>Free:</strong> personal work and teams of up to 2 people. <strong>Premium:</strong> teams of 3 or more, with a separate key for every member, including the owner.</p><p class="muted">One permanent key per person, usable across their teams. No seats, expiry or renewal. A key cannot be shared by two members of the same team.</p></div><div id="notice" role="status"></div>${!readiness.ready ? '<p class="notice error" role="alert">Premium signing is not configured. The administrator must provision the Premium issuer and include its public key in app releases before keys can be used.</p>' : ''}<main class="workspace"><section class="register" aria-label="Premium key register"><div id="register"></div></section><section class="detail" id="detail" aria-label="Premium key details"></section></main><section id="backup-form" class="backup-panel" hidden></section></div>`;
  document.querySelector('#signout').onclick = signout;
  document.querySelector('#search').oninput = register;
  document.querySelector('#new').onclick = () => { if (busy) return; if (current) { draft = {requestId:crypto.randomUUID(), holder:'', orderReference:''}; saveDraft(); } current = null; selected = null; register(); detail(); document.querySelector('#holder').focus(); };
  document.querySelector('#refresh').onclick = event => working(event.currentTarget, 'Refreshing…', async () => { rows = await api('list'); register(); notice('Register refreshed.'); });
  document.querySelector('#backup').onclick = backupForm;
  register(); detail();
}
function detail() {
  const panel = document.querySelector('#detail');
  if (current?.unlock_key) {
    panel.innerHTML = `<h2 tabindex="-1">${esc(current.holder)}</h2><p><span class="badge">Premium · permanent</span></p><dl class="terms"><div><dt>Key ID</dt><dd class="mono">${esc(current.id)}</dd></div><div><dt>Issued</dt><dd>${esc(date(current.issued_at))}</dd></div><div><dt>Order reference</dt><dd>${esc(current.order_reference || 'Not supplied')}</dd></div></dl><h3>Give this key to ${esc(current.holder)}</h3><label>Premium unlock key<textarea id="unlock-key" class="mono" readonly spellcheck="false" autocomplete="off">${esc(current.unlock_key)}</textarea></label><div class="row actions"><button class="primary" id="copy-key">Copy key</button><button id="download">Download key</button></div><p>In Werkbord, open <strong>Settings → Plan</strong>, paste the key and choose <strong>Unlock Premium</strong>. Use a release configured with this Premium issuer.</p><p class="muted">The app checks the key offline. In teams of 3 or more, members without their own key have read-only access. Downloading or copying does not confirm delivery to the person.</p><details><summary>Issuer public key</summary><code>${esc(current.issuer_public_key)}</code></details>`;
    document.querySelector('#copy-key').onclick = () => copyText(current.unlock_key, document.querySelector('#unlock-key'));
    document.querySelector('#download').onclick = () => { download(current.unlock_key + '\n', `${current.id}.txt`, 'text/plain'); notice('Key download requested. Send it privately to its holder.'); };
    panel.querySelector('h2').focus(); return;
  }
  if (current) draft = {requestId:current.request_id, holder:current.holder, orderReference:current.order_reference};
  saveDraft();
  panel.innerHTML = `<h2>${current ? 'Finish prepared key' : 'Issue a Premium key'}</h2><p>Issue a separate key to each person who needs to create or join a team of 3 or more.</p><form class="stack" id="issue"><label>Person’s name or email<input id="holder" name="holder" value="${esc(draft.holder)}" required maxlength="80" autocomplete="off" aria-describedby="holder-help"></label><p class="muted" id="holder-help">Shown in their app so they can recognise their key. Maximum 80 UTF-8 bytes.</p><label>Order reference <span class="muted">(optional)</span><input name="orderReference" value="${esc(draft.orderReference)}" maxlength="256" autocomplete="off"></label><p>Premium is permanent. There is no team, seat allowance, expiry date or support term to set.</p><p class="muted">Request <code>${esc(draft.requestId)}</code>. If interrupted, retry this request to recover the same key.</p><button class="primary" ${!readiness.ready ? 'disabled' : ''}>${current ? 'Finish issuing key' : 'Issue Premium key'}</button></form>`;
  const form = document.querySelector('#issue');
  form.oninput = () => { const values = new FormData(form); draft.holder = values.get('holder'); draft.orderReference = values.get('orderReference'); saveDraft(); };
  form.onsubmit = event => {
    event.preventDefault();
    if (new TextEncoder().encode(draft.holder.trim()).length > 80) { notice('Use a holder label of at most 80 UTF-8 bytes.', true); document.querySelector('#holder').focus(); return; }
    working(form.querySelector('button'), 'Issuing Premium key…', async () => {
      const row = await api('issue', draft); await verifyKey(row.unlock_key, row.issuer_public_key);
      current = row; selected = row.id;
      draft = {requestId:crypto.randomUUID(), holder:'', orderReference:''}; saveDraft();
      // Retain the successful result even if refreshing the register is interrupted.
      rows = [row, ...rows.filter(old => old.id !== row.id)]; register(); detail();
      notice('Premium key issued. Copy or download it and send it privately to its holder.');
    });
  };
}
async function copyText(text, fallback) {
  try { await navigator.clipboard.writeText(text); notice('Copied. Send the key privately to its holder.'); }
  catch { fallback?.focus(); fallback?.select(); notice('Copy is unavailable. The text is selected; copy it manually.', true); }
}
function download(content, filename, type) {
  const url = URL.createObjectURL(new Blob([content], {type})); const link = document.createElement('a'); link.href = url; link.download = filename; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function backupForm() {
  const panel = document.querySelector('#backup-form'); panel.hidden = false;
  panel.innerHTML = '<h2>Back up Premium register</h2><p>The encrypted backup includes issued keys. Keep the passphrase separately from the file.</p><form class="stack" id="export"><label>Backup passphrase<input name="passphrase" type="password" autocomplete="new-password" minlength="14" required></label><button class="primary">Download encrypted backup</button></form>';
  panel.querySelector('input').focus();
  panel.querySelector('form').onsubmit = event => {
    event.preventDefault(); const form = event.currentTarget; const passphrase = new FormData(form).get('passphrase');
    working(form.querySelector('button'), 'Encrypting backup…', async () => {
      const records = await api('list'); const keys = [];
      for (const row of records) {
        const full = await api('read', {id:row.id});
        if (full.unlock_key) await verifyKey(full.unlock_key, full.issuer_public_key);
        keys.push(full);
      }
      const encryptor = new Encrypter(); encryptor.setScryptWorkFactor(16); encryptor.setPassphrase(passphrase);
      const bytes = await encryptor.encrypt(JSON.stringify({schema:1, product:'werkbord-premium-register', exportedAt:new Date().toISOString(), keys}));
      download(bytes, `werkbord-premium-${new Date().toISOString().slice(0,10)}.json.age`, 'application/octet-stream');
      form.reset(); panel.hidden = true; notice('Encrypted backup download requested. Retain it with your recovery material.');
    });
  };
}
async function boot() {
  try {
    const response = await fetch('./config.json', {cache:'no-store'}); if (!response.ok) throw Error('Operator configuration could not be loaded.');
    config = await response.json();
    db = createClient(config.supabaseURL, config.publishableKey, {auth:{persistSession:false, autoRefreshToken:true, detectSessionInUrl:false}});
    await establish();
  } catch (error) {
    root.innerHTML = `<main class="auth"><h1>Unable to open Premium licensing</h1><p role="alert">${esc(error.message)}</p><p>Reload the console to sign in again. An interrupted issue keeps its request reference.</p><button id="reload">Reload console</button></main>`;
    document.querySelector('#reload').onclick = () => location.reload();
  }
}
await boot();
