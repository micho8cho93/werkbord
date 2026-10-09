// Team's device experience. Same tokens and coordination views; a separately installed, authenticated device service.
'use strict';
let setupView = 'welcome';
let desktopRefresh = null;
let invitationPreview = null;
let invitationLink = '';
let activeRequest = null;
let runnerStatus = null;
let requestRefresh = null;

async function deviceApi(method, path, body) {
  const res = await fetch('/api/device/v1' + path, { method, headers: { Authorization: 'Bearer ' + state.token, ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined });
  if (res.status === 204) return null;
  const data = await res.json();
  if (!res.ok) throw new Error(data.error?.message || 'The device service could not complete this step.');
  return data;
}

function scheduleDesktopRefresh() {
  if (desktopRefresh) return;
  desktopRefresh = setTimeout(() => { desktopRefresh = null; render(); }, 2500);
}

async function desktopGate() {
  if (state.serviceRemoved) { app.replaceChildren(setupShell(h('h1', {}, 'Team service removed'), h('p', {}, 'Your runner is unaffected. Any retained workspace archives and backups are still on this Mac. Move Werkbord Team.app to the Trash to remove the desktop app.'))); return true; }
  if (!state.token || state.desktop === false) return false;
  try { state.device = await deviceApi('GET', '/state'); state.desktop = true; }
  catch (_) { if (state.desktop === null) { state.desktop = false; return false; } app.replaceChildren(desktopConnecting(false)); scheduleDesktopRefresh(); return true; }
  if (state.joinLink) {
    if (state.device.enrolled) state.error = 'This computer already belongs to a workspace. Leave it in Settings before using another invitation.';
    else { invitationLink = state.joinLink; setupView = 'join'; invitationPreview = null; }
    state.joinLink = null;
  }
  const d = state.device;
  if (d.enrolled && !d.operation && !d.pending) { setupView = 'welcome'; return false; }
  stopSync();
  if (d.operation || d.pending) {
    app.replaceChildren(setupShell(h('h1', {}, d.operation || 'Waiting for your administrator'),
      h('p', { class: 'muted', role: 'status' }, d.pending ? 'Your invitation was verified. An administrator will approve this computer in Devices. You can close this window and come back later.' : 'Your workspace is being prepared. This continues if you close the window.'),
      d.error ? h('p', { class: 'error', role: 'alert' }, d.error) : '',
      d.pending && !d.operation ? h('button', { class: 'plain', onclick: () => confirm('Cancel this enrollment? Ask your administrator for a new invitation to try again.') && act(() => deviceApi('POST', '/join/cancel', {})) }, 'Cancel enrollment') : '', alwaysOnHelp()));
    scheduleDesktopRefresh(); return true;
  }
  const previous = snapshot(app);
  app.replaceChildren(onboardingScreen()); restore(app, previous, true);
  return true;
}

function setupShell(...content) { return h('div', { class: 'gate desktop-gate' }, h('header', { class: 'top' }, brand(), themeButton()), h('div', { class: 'panel setup-panel' }, content)); }
function setupBack() { return h('button', { class: 'link', type: 'button', onclick: () => { setupView = 'welcome'; state.error = ''; render(); } }, 'Back'); }

function onboardingScreen() {
  const error = state.error || state.device?.error;
  if (setupView === 'create') {
    if (!state.device.license) return setupShell(setupBack(), h('h1', {}, 'Activate Werkbord Team'), h('p', { class: 'muted' }, 'Import the license supplied with your Team purchase. It is verified on this computer; no networking account is needed.'), error ? h('p', { class: 'error', role: 'alert' }, error) : '', licenseImport(), alwaysOnHelp());
    const name = h('input', { name: 'setup-team', required: true, maxlength: 80, autocomplete: 'organization', placeholder: 'Your team name' });
    const owner = h('input', { name: 'setup-owner', required: true, maxlength: 80, autocomplete: 'name' });
    const email = h('input', { name: 'setup-email', type: 'email', maxlength: 254, autocomplete: 'email' });
    return setupShell(setupBack(), h('h1', {}, 'Create your workspace'), h('p', { class: 'muted' }, 'This computer becomes your first Workspace Host. You will be the owner. Invite your teammates after setup.'),
      error ? h('p', { class: 'error', role: 'alert' }, error) : '',
      h('form', { onsubmit: e => { e.preventDefault(); act(() => deviceApi('POST', '/create', { name: name.value, owner: owner.value, email: email.value })); } }, field('Team name', name), field('Your name', owner), field('Email (optional)', email), h('button', { class: 'primary' }, 'Create workspace')), alwaysOnHelp());
  }
  if (setupView === 'join') {
    const link = h('textarea', { name: 'setup-invitation', rows: 3, required: true, autocomplete: 'off', placeholder: 'Paste your invitation link' }, invitationLink);
    const name = h('input', { name: 'join-member', required: true, maxlength: 80, autocomplete: 'name' });
    const device = h('input', { name: 'join-device', required: true, maxlength: 80, placeholder: 'e.g. My office Mac' });
    const check = async () => { invitationPreview = await deviceApi('POST', '/invitation', { link: link.value.trim() }); invitationLink = link.value.trim(); };
    return setupShell(setupBack(), h('h1', {}, invitationPreview ? 'Join ' + invitationPreview.name : 'Join your team'),
      h('p', { class: 'muted' }, 'Open an invitation from your administrator or paste its link. Team verifies it and sets up the connection for you.'),
      error ? h('p', { class: 'error', role: 'alert' }, error) : '',
      invitationPreview ? [h('p', { class: 'ok' }, 'Invitation verified. Expires ' + new Date(invitationPreview.expiresAt).toLocaleDateString() + '.'),
        h('form', { onsubmit: e => { e.preventDefault(); act(() => deviceApi('POST', '/join', { link: invitationLink, name: name.value, deviceName: device.value })); } }, field('Your name', name), field('Name for this computer', device), h('button', { class: 'primary' }, 'Join workspace'))]
        : h('form', { onsubmit: e => { e.preventDefault(); act(check); } }, field('Invitation link', link), h('button', { class: 'primary' }, 'Verify invitation')),
      alwaysOnHelp());
  }
  return setupShell(h('h1', {}, 'Set up Team'),
    error ? h('p', { class: 'error', role: 'alert' }, error) : '',
    h('div', { class: 'actions setup-actions' }, h('button', { class: 'primary', onclick: () => { setupView = 'create'; render(); } }, 'Create Team'), h('button', { class: 'plain', onclick: () => { setupView = 'join'; render(); } }, 'Join Team')),
    alwaysOnHelp(), nativeApp() ? h('button', { class: 'link', onclick: () => act(async () => { await nativeApp().Service('uninstall'); state.serviceRemoved = true; }) }, 'Remove Team service and local settings') : '');
}

function desktopConnecting(serviceRunning = true) {
  return setupShell(h('h1', {}, serviceRunning ? 'Reconnecting to your workspace' : 'Connect to your Team service'),
    h('p', { class: 'muted', role: 'status' }, serviceRunning ? 'Your Team service is running. Keep a Workspace Host online so your workspace stays available.' : 'The Team service is not answering on this computer. Start it again to reconnect.'),
    state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
    h('div', { class: 'actions' }, h('button', { class: 'primary', onclick: () => render() }, 'Try again'), !serviceRunning && nativeApp() ? h('button', { class: 'plain', onclick: () => act(() => nativeApp().Service('start')) }, 'Start Team service') : ''), alwaysOnHelp());
}

function licenseImport() {
  const file = h('input', { type: 'file', accept: '.json,.werkbord-license,application/json', required: true, name: 'license-file' });
  return h('form', { onsubmit: e => { e.preventDefault(); act(async () => { if (!file.files.length) throw new Error('Choose your license file.'); if (file.files[0].size > 16384) throw new Error('This license file is too large.'); const document = await file.files[0].text(); try { JSON.parse(document); } catch (_) { throw new Error('Choose a valid Werkbord Team license file.'); } await deviceApi('POST', '/license', { document }); state.device = await deviceApi('GET', '/state'); state.info = 'Your Team license is active.'; }); } }, field('License file', file), h('button', { class: 'primary' }, 'Activate license'));
}

function alwaysOnHelp() {
  return h('details', { class: 'always-on' }, h('summary', {}, 'What needs to stay online?'),
    h('p', {}, 'Runners perform the work. Scheduled or remote work requires at least one of your runners to stay online.'),
    h('p', {}, 'Workspace Hosts keep Team available. Three Workspace Hosts are recommended so Team can keep working when one goes offline.'),
    h('p', {}, 'Connectivity Hosts make remote access possible. Team checks whether a host is reachable and tells you when remote access cannot be guaranteed.'),
    h('p', {}, 'Desktop computers, Mac minis, office servers and server-class NAS systems are better Host and Runner choices than laptops that sleep or travel. Closing an app window keeps its service running; sleeping the computer pauses its work.'));
}

function availabilityText(r) {
  if (!r.writable) return 'Read-only until enough Hosts reconnect';
  if (r.workspaceHosts.configured === 1) return 'Available while this Host stays online';
  if (r.workspaceHosts.configured === 2) return 'Both Hosts must stay online to save changes';
  return 'Available — enough Hosts can save changes';
}
function healthWords(text) { return text.replace(/\bquorum returns\b/g, 'enough Hosts reconnect').replace(/\bquorum\b/g, 'enough Hosts to agree on changes').replace(/\bfault tolerance\b/g, 'protection when a Host goes offline'); }
async function resiliencePanel(compact = false) {
  const r = await api('GET', '/resilience');
  const firstHost = r.workspaceHosts.configured === 1 ? h('p', { class: 'advice' }, 'Your workspace currently has one host. Add two more hosts for fault tolerance. ', h('span', {}, 'Three Hosts keep Team available if one goes offline. '), h('button', { class: 'link', onclick: () => go('hosts') }, 'Add hosts')) : '';
  if (compact) return h('section', { class: 'panel resilience compact-health', 'aria-label': 'Workspace health summary' },
    h('div', { class: 'actions' }, h('strong', { class: 'check-' + (r.writable ? 'ok' : 'bad') }, r.writable ? 'Workspace available' : 'Workspace is read-only'), h('span', { class: 'muted' }, r.hosts.value), h('button', { class: 'link', onclick: () => go('hosts') }, 'View workspace health')),
    firstHost,
    r.remoteAccess.state !== 'ok' ? h('p', { class: 'muted' }, 'Remote access needs attention. ', h('button', { class: 'link', onclick: () => go('connectivity') }, 'Review connectivity')) : '');
  const checks = [r.hosts, { ...r.quorum, label: 'Workspace availability', value: availabilityText(r) }, r.database, r.connectivityHosts, r.remoteAccess, r.backups];
  return h('section', { class: 'panel resilience', 'aria-label': 'Workspace resilience' }, h('h2', {}, 'Workspace resilience'),
    h('dl', { class: 'health-checks' }, checks.map(c => [h('dt', {}, c.label), h('dd', { class: 'check-' + c.state }, c.value)])),
    firstHost,
    r.advice.filter(a => a.code !== 'one_workspace_host').map(a => h('div', { class: 'advice' }, h('strong', {}, healthWords(a.title)), a.detail ? h('p', { class: 'muted' }, healthWords(a.detail)) : '', a.action ? h('button', { class: 'link', onclick: () => go(a.action.kind.includes('backup') ? 'backups' : a.action.kind.includes('connectivity') ? 'connectivity' : 'hosts') }, a.action.label) : '')));
}

async function membersView() {
  const members = await api('GET', '/members');
  const content = membersPanels(members);
  if (can('members.manage') && state.desktop) content.append(h('aside', { class: 'member-tools' }, await invitationsPanel(members)));
  return content;
}

let createdInvitation = null;
async function invitationsPanel(members) {
  const label = h('input', { name: 'enrollment-label', maxlength: 120, required: true, placeholder: 'Who is this for?' });
  const person = h('select', { name: 'enrollment-person' }, h('option', { value: '' }, 'A new teammate'), members.map(m => h('option', { value: m.id }, m.name)));
  const role = h('select', { name: 'enrollment-role' }, h('option', { value: 'member' }, 'Member'), can('admins.manage') ? h('option', { value: 'admin' }, 'Admin') : '');
  const invites = await api('GET', '/enrollment-invitations');
  return h('section', { class: 'panel' }, h('h2', {}, 'Invite a person or device'), h('p', { class: 'muted' }, 'Send a private invitation link. Opening it in the Team app sets up the connection. Administrators approve joining computers in Devices.'),
    h('form', { onsubmit: e => { e.preventDefault(); act(async () => { createdInvitation = await api('POST', '/enrollment-invitations', { label: label.value, forMemberId: person.value, role: role.value, capabilities: [], requireApproval: true, expiresInHours: 24 }); }); } }, field('Invite label', label), field('Who is joining?', person), field('Role for a new teammate', role), h('button', { class: 'primary' }, 'Create invitation')),
    createdInvitation ? h('div', { class: 'invite-result' }, h('strong', {}, 'Your invitation is ready'), h('p', { class: 'muted' }, 'Shown once. Copy it before closing this page. Scan the code on a device with Werkbord Team installed.'), createdInvitation.qr ? h('img', { src: createdInvitation.qr, width: 288, height: 288, alt: 'QR invitation to open Werkbord Team', class: 'invite-qr' }) : '', h('code', {}, createdInvitation.link), h('div', { class: 'actions' }, copy(createdInvitation.link, 'Copy invitation'), h('a', { href: createdInvitation.link.startsWith("werkbord://join/") ? createdInvitation.link : "#" }, 'Open in Team'), h('button', { class: 'plain', onclick: () => { createdInvitation = null; render(); } }, 'Done'))) : '',
    invites.length ? h('div', {}, h('h3', {}, 'Recent invitations'), invites.map(i => h('div', { class: 'row' }, h('div', { class: 'grow' }, i.label || 'Device invitation'), h('span', { class: 'muted' }, i.state || (i.withdrawnAt ? 'revoked' : 'expires ' + new Date(i.expiresAt).toLocaleDateString())), !i.withdrawnAt ? h('button', { class: 'danger', onclick: () => act(() => api('DELETE', '/enrollment-invitations/' + i.id)) }, 'Revoke') : ''))) : '');
}

async function administrationView(tab) {
  switch (tab) {
    case 'devices': return devicesView(false);
    case 'hosts': return devicesView(true);
    case 'connectivity': return connectivityView();
    case 'backups': return backupsView();
    case 'license': return licenseView();
    case 'settings': return settingsView();
  }
}

async function devicesView(hostsOnly) {
  const devices = await api('GET', '/devices');
  const admins = can('devices.manage');
  const resilience = admins ? await api('GET', '/resilience') : null;
  const fits = new Map(resilience?.devices.map(d => [d.deviceId, d]) || []);
  const rows = devices.filter(d => !d.revokedAt).map(d => {
    const isHost = d.capabilities.includes('workspace_host');
    const fit = fits.get(d.id);
    const ownRunner = d.memberId === state.me.member.id && d.capabilities.includes('runner');
    return h('article', { class: 'row device-row' }, h('div', { class: 'grow' }, h('strong', {}, d.name), d.id === state.device?.deviceId ? ' · this computer' : '',
      h('p', { class: 'muted' }, d.capabilities.map(c => ({ workspace_host: 'Workspace Host', connectivity_host: 'Connectivity Host', runner: 'Runner' })[c]).join(' · ') || 'Team device'),
      h('p', { class: 'muted' }, fit?.online ? 'Online' : d.lastSeenAt ? 'Last seen ' + ago(d.lastSeenAt) : 'Waiting for its first connection'),
      fit?.hostFit.reasons?.map(reason => h('p', { class: 'muted' }, reason)) || '',
      admins && isHost && !fit?.removal?.allowed ? h('p', { class: 'advice' }, fit?.removal?.reason || 'Team cannot confirm that this Host can be safely removed yet.') : ''),
      isHost ? h('span', { class: 'badge' }, d.hostStatus) : '',
      ownRunner && state.desktop ? h('button', { class: 'plain', onclick: () => act(async () => { activeRequest = await deviceApi('POST', '/requests', { target: d.id, action: 'fetch_runner_status', payload: {} }); runnerStatus = null; await pollRequest(); }) }, 'Runner status') : '',
      admins && !isHost && fit?.hostFit.possible !== false ? h('button', { class: 'plain', onclick: () => confirm('Make ' + d.name + ' a Workspace Host? It will keep a workspace copy and should stay online.') && act(async () => { await api('PUT', '/devices/' + d.id + '/capabilities', { capabilities: [...d.capabilities, 'workspace_host'] }); await api('POST', '/devices/' + d.id + '/provision'); state.info = 'The device will become a Host automatically when its Team service next connects.'; }) }, 'Make Workspace Host') : '',
      admins && isHost && d.hostStatus === 'joining' ? h('button', { class: 'plain', onclick: () => act(() => api('POST', '/devices/' + d.id + '/provision')) }, 'Retry host setup') : '',
      admins && isHost ? h('button', { class: 'danger', disabled: !fit?.removal?.allowed, onclick: () => confirm('Remove ' + d.name + ' from the Workspace Hosts? Team will refuse if the remaining hosts cannot safely keep the workspace.') && act(async () => { await api('DELETE', '/devices/' + d.id + '/replica'); }) }, 'Remove host') : '',
      !isHost && (admins || d.memberId === state.me.member.id) ? h('button', { class: 'danger', onclick: () => confirm('Revoke ' + d.name + '? It will lose workspace access.') && act(() => api('POST', '/devices/' + d.id + '/revoke')) }, 'Revoke device') : '');
  });
  const pending = admins ? await api('GET', '/enrollments') : [];
  return h('div', {}, hostsOnly ? await resiliencePanel() : '', h('section', { class: 'panel' }, h('h2', {}, hostsOnly ? 'Choose Workspace Hosts' : 'Registered devices'),
    hostsOnly ? h('p', { class: 'muted' }, 'Keep three Workspace Hosts online to tolerate one outage.') : '',
    rows.length ? rows : h('p', { class: 'muted' }, 'No devices are registered yet.')),
    pending.length ? h('section', { class: 'panel' }, h('h2', {}, 'Waiting for approval'), pending.map(p => h('div', { class: 'row' }, h('div', { class: 'grow' }, h('strong', {}, p.deviceName), h('p', { class: 'muted' }, p.memberName || 'Existing member')), h('button', { class: 'primary', onclick: () => act(() => api('POST', '/enrollments/' + p.id + '/approve')) }, 'Approve device'), h('button', { class: 'danger', onclick: () => act(() => api('POST', '/enrollments/' + p.id + '/deny')) }, 'Deny')))) : '',
    activeRequest ? requestPanel() : '', runnerStatus ? runnerStatusPanel() : '', alwaysOnHelp());
}

async function connectivityView() {
  const [devices, health] = await Promise.all([api('GET', '/devices'), api('GET', '/network')]);
  return h('div', {}, h('section', { class: 'panel' }, h('h2', {}, 'Remote access'), h('p', {}, health.remoteAccess === 'available' ? 'Remote access is available.' : 'No Connectivity Host is confirmed reachable from outside your network. Local use works, but remote access cannot be guaranteed.'),
    h('p', { class: 'muted' }, 'Use an always-on machine that your team can reach from outside the office. Team checks reachability; enabling this role alone does not make an unreachable computer accessible.'),
    devices.filter(d => !d.revokedAt).map(d => { const enabled = d.capabilities.includes('connectivity_host'); return h('div', { class: 'row' }, h('div', { class: 'grow' }, d.name), h('span', { class: 'badge' }, enabled ? 'Connectivity Host' : 'Team device'), can('devices.manage') ? h('button', { class: 'plain', onclick: () => act(async () => {
      const caps = enabled ? d.capabilities.filter(c => c !== 'connectivity_host') : [...d.capabilities, 'connectivity_host'];
      const connection = await api('GET', '/devices/' + d.id + '/network');
      if (!enabled && !connection.networkEndpoints.length) throw new Error('This computer has not connected yet. Open Team on it, then enable connectivity here.');
      await api('PUT', '/devices/' + d.id + '/capabilities', { capabilities: caps });
      try { await api('PUT', '/devices/' + d.id + '/network', { discovery: !enabled, relay: !enabled }); } catch (e) { await api('PUT', '/devices/' + d.id + '/capabilities', { capabilities: d.capabilities }); throw e; }
      state.info = enabled ? 'Connectivity Host disabled.' : 'Connectivity Host enabled. Remote access will be available when reachability is confirmed.';
    }) }, enabled ? 'Disable' : 'Enable Connectivity Host') : ''); })), alwaysOnHelp());
}

async function backupsView() {
  const s = await api('GET', '/storage'); const b = s.backup;
  return h('section', { class: 'panel' }, h('h2', {}, 'Workspace backups'),
    h('p', {}, b?.lastOk ? 'The last backup succeeded ' + ago(new Date(b.lastAt).toISOString()) + '.' : b?.lastError || 'No successful backup has been taken yet.'),
    h('p', { class: 'muted' }, b?.configured ? 'This host takes backups automatically. Replication keeps Team available; backups protect against accidental changes and disk loss. Keep an additional copy on a separate disk.' : 'Backups have not been configured on this host. Use the desktop service to enable automatic backups.'),
    b?.configured ? h('p', { class: 'muted' }, 'Backup folder: ' + b.destination + ' · ' + b.count + ' retained') : '',
    can('devices.manage') ? h('button', { class: 'primary', onclick: () => act(async () => { await api('POST', '/storage/backup'); state.info = 'Your backup was taken and checked.'; }) }, 'Back up now') : '');
}

function licenseView() {
  if (!state.desktop) return h('section', { class: 'panel' }, h('h2', {}, 'Team license'), h('p', { class: 'muted' }, 'Open the separately installed Werkbord Team app to manage this computer’s license.'));
  const l = state.device.license;
  return h('section', { class: 'panel' }, h('h2', {}, 'Werkbord Team license'), l ? [h('p', { class: 'ok' }, 'Active for ' + l.customer), h('p', { class: 'muted' }, l.seats + ' seats · expires ' + new Date(l.expiresAt).toLocaleDateString())] : h('p', { class: 'muted' }, state.me.member.role === 'owner' ? state.device.licenseError : 'This workspace’s owner manages the Team license.'),
    state.me.member.role === 'owner' ? licenseImport() : h('p', { class: 'muted' }, 'The workspace owner manages its license.'));
}

let nativeTransport;
function nativeApp() {
  if (window.go?.main?.App) return window.go.main.App;
  if (!window.webkit?.messageHandlers?.external) return null;
  if (!nativeTransport) nativeTransport = import('/native-bridge.js').then(m => m.createNativeBridge('main.App'));
  const invoke = (method, ...args) => nativeTransport.then(call => call(method, args, 0));
  return { SetupRunner: () => invoke('SetupRunner'), Service: action => invoke('Service', action), PendingInvitation: () => invoke('PendingInvitation'), Info: () => invoke('Info'), OpenExternal: address => invoke('OpenExternal', address) };
}
function runnerSetup() {
  return h('section', { class: 'panel' }, h('h2', {}, 'Your runner on this computer'), h('p', { class: 'muted' }, state.device.runner.connected ? 'Connected to your own Werkbord. It keeps its normal execution policies and approvals.' : 'The free Werkbord app runs your agents with your credentials. Team communicates with it only on this computer.'),
    h('div', { class: 'actions' }, h('button', { class: 'primary', onclick: () => act(async () => { await deviceApi('POST', '/runner/connect'); state.info = 'Your runner is connected.'; }) }, state.device.runner.connected ? 'Reconnect runner' : 'Connect existing runner'),
      nativeApp() ? h('button', { class: 'plain', onclick: () => act(async () => { await nativeApp().SetupRunner(); await deviceApi('POST', '/runner/connect'); state.info = 'Your runner is ready.'; }) }, 'Install / set up free runner') : extLink('https://github.com/micho8cho93/werkbord/releases', 'Download free Werkbord')));
}

async function settingsView() {
  if (!state.desktop) return h('section', { class: 'panel' }, h('h2', {}, 'Device settings'), h('p', { class: 'muted' }, 'Manage this computer’s service and runner approvals in the Team desktop app.'),
    h('div', { class: 'actions' }, h('button', { class: 'plain', onclick: signOut }, 'Sign out of workspace')));
  const s = state.device.settings;
  const form = h('select', { name: 'computer-kind' }, [['', 'Choose…'], ['desktop', 'Desktop / Mac mini'], ['laptop', 'Laptop'], ['server', 'Server / NAS']].map(([value, label]) => h('option', { value, selected: s.form === value || (!s.form && !value) }, label)));
  const policy = h('select', { name: 'remote-policy' }, [['ask', 'Approve each task on this computer'], ['off', 'Do not allow remote starts'], ['auto', 'Allow starts for tickets from my trusted devices']].map(([value, label]) => h('option', { value, selected: s.remoteStart === value }, label)));
  const open = h('input', { name: 'remote-open', type: 'checkbox', checked: s.remoteOpen });
  const plan = await deviceApi('GET', '/removal');
  return h('div', {}, runnerSetup(), h('section', { class: 'panel' }, h('h2', {}, 'This computer'), h('form', { onsubmit: e => { e.preventDefault(); act(() => deviceApi('PUT', '/settings', { ...s, form: form.value, remoteStart: policy.value, remoteOpen: open.checked })); } }, field('Kind of computer', form), field('Starting work from my other devices', policy), field('Allow my trusted devices to open tickets here', open), h('button', { class: 'primary' }, 'Save settings')),
    h('p', { class: 'muted' }, 'Changes here belong to this computer. Workspace Hosts cannot change your approvals or execution policies.')),
    h('section', { class: 'panel' }, h('h2', {}, 'Trust your other devices'), h('p', { class: 'muted' }, 'Approve a device here before it can ask your runner to act. Compare its identity with the value shown in Settings on that device.'),
      h('p', { class: 'muted' }, 'This computer’s identity: ', h('code', {}, state.device.identity)),
      (state.device.senders || []).map(d => h('div', { class: 'row' }, h('div', { class: 'grow' }, d.name, h('code', {}, d.identity)), h('button', { class: 'plain', onclick: () => act(() => deviceApi('POST', '/senders/' + d.deviceId + (d.approved ? '/revoke' : '/trust'), d.approved ? {} : { publicKey: d.publicKey })) }, d.approved ? 'Stop trusting' : 'Trust this device')))),
    h('section', { class: 'panel' }, h('h2', {}, 'Execution approvals'), h('p', { class: 'muted' }, 'Open a held ticket’s Agent controls to select its runner, agent, model and policy. Review the effective runtime permissions before approving a single execution.')),
    h('section', { class: 'panel' }, h('h2', {}, 'Service and uninstall'), h('p', { class: 'muted' }, 'Removing the GUI leaves the Team service, your runner and your workspace running. You can keep or stop the Team service separately. Leaving the workspace can keep a local archive or remove its local secrets and data.'),
      plan.reason ? h('p', { class: 'advice' }, plan.reason) : '',
      h('div', { class: 'actions' }, nativeApp() ? h('button', { class: 'plain', onclick: () => act(() => nativeApp().Service('stop')) }, 'Stop Team service, keep data') : '',
        h('button', { class: 'danger', disabled: !plan.canLeave, onclick: () => confirm('Leave this workspace? A local archive is kept. Your free runner is unaffected.') && act(() => deviceApi('POST', '/leave', { confirm: 'leave workspace', removeData: false })) }, 'Leave workspace, keep archive'),
        h('button', { class: 'danger', disabled: !plan.canRemoveData, onclick: () => confirm('Leave and permanently remove this workspace’s local secrets and data? Team requires another safe workspace copy first.') && act(() => deviceApi('POST', '/leave', { confirm: 'leave workspace', removeData: true })) }, 'Leave and remove local data')),
      h('p', { class: 'muted' }, 'To remove only the GUI, move Werkbord Team.app to the Trash. Keep the service to continue hosting. Stopping it preserves data and can be reversed by opening the app.')), alwaysOnHelp());
}

function runnerHandoff(hf) {
  const box = h('div', { class: 'handoff' }, h('strong', {}, 'Open ' + hf.ticket.key + ' on your runner'), h('p', { class: 'muted' }, 'Your request is signed on this computer. The selected device verifies it and asks your own Werkbord to open a backlog task. Its normal approvals still apply.'));
  api('GET', '/devices').then(ds => {
    const mine = ds.filter(d => d.memberId === state.me.member.id && d.capabilities.includes('runner') && !d.revokedAt);
    if (!mine.length) { box.append(h('p', { class: 'muted' }, 'Connect your free runner in Settings first.'), h('button', { class: 'plain', onclick: () => go('settings') }, 'Set up my runner')); return; }
    const pick = h('select', { name: 'handoff-runner', 'aria-label': 'Your runner' }, mine.map(d => h('option', { value: d.id }, d.name)));
    box.append(h('div', { class: 'actions' }, pick, h('button', { class: 'primary', onclick: () => act(async () => { activeRequest = await deviceApi('POST', '/requests', { target: pick.value, action: 'open_ticket_on_runner', payload: { projectId: hf.project.id, ticketId: hf.ticket.id } }); await pollRequest(); }) }, 'Open ticket on runner')), activeRequest ? requestPanel() : '');
  }).catch(e => { box.append(h('p', { class: 'error' }, e.message)); });
  return box;
}

async function pollRequest() {
  if (!activeRequest) return;
  const id = activeRequest.id;
  const message = await api('GET', '/messages/' + id);
  if (activeRequest?.id !== id) return;
  activeRequest = message;
  if (message.state === 'done' && message.result?.status) runnerStatus = message.result;
  if (requestRefresh) clearTimeout(requestRefresh);
  requestRefresh = null;
  if (message.state === 'queued' || message.state === 'delivered') requestRefresh = setTimeout(() => {
    requestRefresh = null;
    pollRequest().then(render).catch(e => { state.error = e.message; render(); });
  }, 2500);
}

function requestPanel() {
  const m = activeRequest;
  return h('div', { class: 'request-status', role: 'status' }, h('p', {}, ({ queued: 'Your request is queued. Your runner will receive it when it is online.', delivered: 'Your runner is processing the request.', done: 'Your runner completed the request.', refused: m.result?.reason || 'Your runner refused the request. Check its local approvals in Settings.', expired: 'The request expired before your runner handled it. Try again when it is online.' })[m.state] || m.state),
    m.result?.approvalId && m.result?.taskId ? h('button', { class: 'primary', onclick: () => act(async () => { activeRequest = await deviceApi('POST', '/requests', { target: m.toDeviceId, action: 'start_approved_run', payload: { taskId: m.result.taskId, approvalId: m.result.approvalId } }); await pollRequest(); }) }, 'Start approved task') : '');
}
function runnerStatusPanel() { const policy = { ask: 'Each task needs approval on this computer.', off: 'Remote starts are turned off on this computer.', auto: 'Tickets from your trusted devices can be started here.' }[runnerStatus.remoteStart] || 'Check this computer’s approvals in Settings.'; return h('section', { class: 'panel' }, h('h2', {}, 'Your runner'), h('p', { class: 'muted' }, policy), (runnerStatus.executions || []).map(a => h('div', { class: 'row' }, h('div', { class: 'grow' }, a.title || 'Approved task'), h('button', { class: 'primary', onclick: () => act(async () => { activeRequest = await deviceApi('POST', '/requests', { target: activeRequest.toDeviceId, action: 'start_authorized_run', payload: { projectId: a.teamProjectId, ticketId: a.ticketId, executionId: a.executionId, fenceId: a.fence } }); runnerStatus = null; await pollRequest(); }) }, 'Start approved task')))); }

window.addEventListener('load', () => {
  const native = nativeApp();
  if (!native) return;
  async function invitations() {
    try { const link = await native.PendingInvitation(); if (link) { state.joinLink = link; render(); } } catch (_) { /* The window may be quitting. */ }
    setTimeout(invitations, 2500);
  }
  invitations();
  // External web links belong in the user's browser; the native app rejects unsupported schemes.
  document.addEventListener('click', e => {
    const link = e.target.closest?.('a[href]');
    if (!link || !isHTTPS(link.href) || new URL(link.href).origin === location.origin) return;
    e.preventDefault();
    native.OpenExternal(link.href).catch(err => { state.error = err.message; render(); });
  });
});

const executionViews = new Map();
function ownExecutionPanel(k) {
  const key = k.projectId + ':' + k.id;
  let view = executionViews.get(key);
  const refresh = async () => {
    const detail = await deviceApi('GET', '/execution/detail?projectId=' + encodeURIComponent(k.projectId) + '&ticketId=' + encodeURIComponent(k.id));
    const schedules = await api('GET', '/projects/' + k.projectId + '/schedules');
    const scheduled = schedules.find(s => s.ticketId === k.id && !['completed', 'canceled'].includes(s.state));
    view = { ...view, detail, schedule: scheduled };
    if (!view.selection) view.selection = { projectId: k.projectId, ticketId: k.id, executionId: scheduled?.executionId || 'exe_' + crypto.randomUUID().replaceAll('-', ''), runnerId: detail.runners.find(r => r.kind === 'local' && !r.disabled && !r.removed)?.id || '', agentId: detail.agents.find(a => a.available)?.id || 'codex', model: 'default', reasoning: 'default', interaction: 'interactive' };
    if (scheduled && view.selection.executionId !== scheduled.executionId) { view.selection.executionId = scheduled.executionId; view.preview = null; }
    executionViews.set(key, view);
  };
  if (!view) return h('section', { class: 'handoff' }, h('h3', {}, 'Your agent'), h('p', { class: 'muted' }, 'Choose your own runner and review its effective policy before approving a run.'), h('button', { class: 'primary', onclick: () => act(refresh) }, 'Agent controls'));
  const selection = view.selection;
  const change = (field, value) => { selection[field] = value; view.preview = null; view.approval = null; };
  const agent = h('select', { name: 'agent-' + k.id, onchange: e => change('agentId', e.target.value) }, view.detail.agents.map(a => h('option', { value: a.id, selected: a.id === selection.agentId }, a.name + (a.available ? '' : ' · unavailable'))));
  const runner = h('select', { name: 'runner-' + k.id, onchange: e => change('runnerId', e.target.value) }, view.detail.runners.filter(r => r.kind === 'local' && !r.removed && !r.disabled).map(r => h('option', { value: r.id, selected: r.id === selection.runnerId }, r.name + (r.online ? ' · online' : ' · offline'))));
  const model = h('input', { name: 'model-' + k.id, value: selection.model, maxlength: 100, oninput: e => change('model', e.target.value), placeholder: 'default' });
  const reasoning = h('input', { name: 'reasoning-' + k.id, value: selection.reasoning, maxlength: 40, oninput: e => change('reasoning', e.target.value), placeholder: 'default' });
  const interaction = h('select', { name: 'interaction-' + k.id, onchange: e => change('interaction', e.target.value) }, [['interactive', 'Ask me when needed'], ['autonomous', 'Investigate independently'], ['autonomous_stop_if_blocked', 'Stop if blocked']].map(([value, label]) => h('option', { value, selected: value === selection.interaction }, label)));
  const preapprove = h('input', { type: 'checkbox', name: 'preapprove-' + k.id, checked: !!selection.preapprove, onchange: e => { selection.preapprove = e.target.checked; } });
  const request = () => ({ ...selection, digest: view.preview?.digest || '' });
  const control = (action, run, question, option, reply) => act(async () => { await deviceApi('POST', '/execution/' + action, { projectId: k.projectId, ticketId: k.id, runId: run, questionId: question || '', option: option || '', reply: reply || '' }); await refresh(); });
  const questions = view.detail.questions.filter(q => q.state === 'pending').map(q => {
    const answer = h('input', { name: 'agent-reply-' + q.id, maxlength: 2000, 'aria-label': 'Your answer' });
    return h('div', { class: 'execution-row' }, h('strong', {}, q.kind === 'approval' ? 'Permission requested' : 'Your agent asks'), h('p', { class: 'prose' }, q.prompt), q.context ? h('pre', {}, q.context) : '',
      q.options?.length ? h('div', { class: 'actions' }, q.options.map(o => h('button', { class: 'plain', onclick: () => control('answer', q.runId, q.id, o) }, o))) : h('div', { class: 'actions' }, answer, h('button', { class: 'primary', onclick: () => control('answer', q.runId, q.id, '', answer.value) }, 'Reply')));
  });
  const runs = view.detail.runs.map(run => h('div', { class: 'execution-row' }, h('strong', {}, run.agentId + ' · ' + run.state), run.branch ? h('p', {}, 'Branch: ', h('code', {}, run.branch)) : '',
    ['starting', 'running', 'waiting_for_user'].includes(run.state) ? h('button', { class: 'danger', onclick: () => control('stop', run.id) }, 'Stop my run') : '',
    run.handoff ? h('details', {}, h('summary', {}, ['completed','failed','stopped','blocked'].includes(run.state) ? 'Review final handoff' : 'Review handoff'), h('pre', { class: 'prose' }, JSON.stringify(run.handoff, null, 2))) : ''));
  return h('section', { class: 'handoff' }, h('h3', {}, 'Your agent'), h('p', { class: 'muted' }, 'Stop/restart is supported. Live pause is unavailable. Agent permissions remain configured on this computer.'),
    field('Runner on this computer', runner), field('Agent', agent), field('Model', model), field('Reasoning', reasoning), field('Conversation policy', interaction),
    h('p', { class: 'muted' }, 'To use another enrolled device, open this ticket there and approve its exact execution locally; use “Open in my runner” to send the signed request.'),
    view.schedule ? h('p', {}, 'Scheduled request: ' + view.schedule.state + ' · ' + new Date(view.schedule.at).toLocaleString() + ' · ' + view.schedule.timezone) : '',
    h('label', { class: 'execution-preapproval' }, preapprove, ' Keep this exact one-shot authorization for up to 30 days'),
    h('div', { class: 'actions' }, h('button', { class: 'plain', onclick: () => act(async () => { view.preview = await deviceApi('POST', '/execution/preview', request()); view.approval = null; }) }, 'Review execution policy'), h('button', { class: 'plain', onclick: () => act(refresh) }, 'Refresh progress')),
    view.preview ? h('div', {}, h('h4', {}, 'Effective policy'), h('pre', {}, JSON.stringify({ runner: view.preview.request.runnerId, agent: view.preview.request.agentId, model: view.preview.request.model, interaction: view.preview.request.interaction, ...view.preview.runtimePolicy }, null, 2)),
      h('details', {}, h('summary', {}, 'Untrusted task context'), h('pre', { class: 'prose' }, view.preview.title + '\n' + view.preview.description)),
      h('button', { class: 'primary', onclick: () => act(async () => { view.approval = await deviceApi('POST', '/execution/approve', request()); if (!view.schedule && !selection.preapprove) { await deviceApi('POST', '/execution/start', request()); view.preview = null; view.approval = null; selection.executionId = 'exe_' + crypto.randomUUID().replaceAll('-', ''); await refresh(); } }) }, view.schedule || selection.preapprove ? 'Approve this execution once' : 'Approve and start my run')) : '',
    view.approval ? h('p', { role: 'status' }, 'Authorized once until ' + new Date(view.approval.expiresAt).toLocaleString() + '. The scheduled request will wait for eligibility and this runner.') : '', questions, runs);
}
