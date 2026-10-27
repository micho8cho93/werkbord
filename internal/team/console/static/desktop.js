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
  const res = await fetch(BASE + '/api/device/v1' + path, { method, headers: { Authorization: 'Bearer ' + state.token, ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined });
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
    const name = h('input', { name: 'setup-team', required: true, maxlength: 80, autocomplete: 'organization', placeholder: 'Your team name' });
    const owner = h('input', { name: 'setup-owner', required: true, maxlength: 80, autocomplete: 'name' });
    const email = h('input', { name: 'setup-email', type: 'email', maxlength: 254, autocomplete: 'email' });
    return setupShell(setupBack(), h('h1', {}, 'Create your workspace'), h('p', { class: 'muted' }, 'Start with this computer as your only host. Keep it online while your team works. Invite your teammates after setup; the device service enables daily backups by default.'), h('p', { class: 'muted' }, 'The Free plan allows a team of two people: you and a partner. A team of three or more needs Premium.'),
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
  return setupShell(h('h1', {}, serviceRunning ? 'Reconnecting to your workspace' : 'Cannot reach the Team service'),
    h('p', { class: 'muted', role: 'status' }, serviceRunning ? 'Your Team service is running. Keep a Workspace Host online so your workspace stays available.' : 'Check the connection. If the Team service is stopped, start it to reconnect.'),
    state.error ? h('p', { class: 'error', role: 'alert' }, state.error) : '',
    h('div', { class: 'actions' }, h('button', { class: 'primary', onclick: () => render() }, 'Try again'), !serviceRunning && nativeApp() ? h('button', { class: 'plain', onclick: () => act(() => nativeApp().Service('start')) }, 'Start Team service') : ''), alwaysOnHelp());
}

function alwaysOnHelp() {
  return h('details', { class: 'always-on' }, h('summary', {}, 'Advanced hosting'),
    h('p', {}, 'Runners perform work; Workspace Hosts keep Team available; Connectivity Hosts enable remote access.'),
    h('p', {}, 'One host is enough to start. Add two more hosts if your team needs to keep working when one goes offline. Sleeping computers pause hosting and runner work.'));
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
  const firstHost = r.workspaceHosts.configured === 1 ? h('p', { class: 'advice' }, 'One host is enough to start. Keep it online while your team works. ', h('span', {}, 'For availability when a host goes offline, you can add two more. '), h('button', { class: 'link', onclick: () => go('hosts') }, 'Manage hosts')) : '';
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
  // The directory uses reported device presence only; it never probes a member's runner.
  const content = membersPanels(members);
  const note = planNote();
  if (can('members.manage') && state.desktop) content.append(h('aside', { class: 'member-tools' }, await invitationsPanel(members)));
  return note ? h('div', { class: 'stack' }, note, content) : content;
}

let createdInvitation = null;
async function invitationsPanel(members) {
  const label = h('input', { name: 'enrollment-label', maxlength: 120, required: true, placeholder: 'Who is this for?' });
  const person = h('select', { name: 'enrollment-person' }, h('option', { value: '' }, 'A new teammate'), members.map(m => h('option', { value: m.id }, m.name)));
  const role = h('select', { name: 'enrollment-role' }, h('option', { value: 'member' }, 'Member'), can('admins.manage') ? h('option', { value: 'admin' }, 'Admin') : '');
  const invites = await api('GET', '/enrollment-invitations');
  return h('section', { class: 'panel' }, h('h2', {}, 'Invite a person or device'), h('p', { class: 'muted' }, 'Send a private invitation link. Opening it in the Team app sets up the connection. Administrators approve joining computers in Devices.'),
    h('form', { onsubmit: e => { e.preventDefault(); act(async () => { createdInvitation = await api('POST', '/enrollment-invitations', { label: label.value, forMemberId: person.value, role: role.value, capabilities: [], requireApproval: true, expiresInHours: 24 }); }); } }, field('Invite label', label), field('Who is joining?', person), field('Role for a new teammate', role), h('button', { class: 'primary', disabled: !!createdInvitation }, 'Create invitation')),
    createdInvitation ? h('div', { class: 'invite-result' }, h('strong', {}, 'Your invitation is ready'), h('p', { class: 'muted' }, 'Shown once. Copy it before closing this page. Scan the code on a device with Werkbord Team installed.'), createdInvitation.qr ? h('img', { src: createdInvitation.qr, width: 288, height: 288, alt: 'QR invitation to open Werkbord Team', class: 'invite-qr' }) : '', h('code', {}, createdInvitation.link), h('div', { class: 'actions' }, copy(createdInvitation.link, 'Copy invitation'), h('a', { href: createdInvitation.link.startsWith("werkbord://join/") ? createdInvitation.link : "#" }, 'Open in Team'), h('button', { class: 'plain', onclick: () => { createdInvitation = null; render(); } }, 'Done'))) : '',
    invites.length ? h('div', {}, h('h3', {}, 'Recent invitations'), invites.map(i => h('div', { class: 'row' }, h('div', { class: 'grow' }, i.label || 'Device invitation'), h('span', { class: 'muted' }, i.state || (i.withdrawnAt ? 'revoked' : 'expires ' + new Date(i.expiresAt).toLocaleDateString())), !i.withdrawnAt && !['expired', 'consumed', 'revoked'].includes(i.state) && new Date(i.expiresAt).getTime() > Date.now() ? h('button', { class: 'danger', onclick: () => act(() => api('DELETE', '/enrollment-invitations/' + i.id)) }, 'Revoke') : ''))) : '');
}

async function administrationView(tab) {
  switch (tab) {
    case 'devices': return devicesView(false);
    case 'hosts': return devicesView(true);
    case 'connectivity': return connectivityView();
    case 'backups': return backupsView();
    case 'settings': return settingsView();
    case 'mcp': return mcpClientsView();
    case 'labels': return labelsView();
  }
}

// The workspace's shared labels. Every member can use them; only people with labels.manage (the owner and admins) get here.
const LABEL_COLORS = ['#d14d41', '#da702c', '#d0a215', '#879a39', '#3aa99f', '#4385be', '#8b7ec8', '#ce5d97', '#6f6e69'];
let editingLabel = null;
async function labelsView() {
  if (!can('labels.manage')) return h('section', { class: 'panel' }, h('h2', {}, 'Labels'), h('p', { class: 'muted' }, 'Labels are made by the workspace owner and admins. You can put any of them on a ticket you may edit.'));
  const { labels } = await api('GET', '/labels');
  const name = h('input', { name: 'label-name', maxlength: 40, required: true, placeholder: 'Q4 launch, Waiting on legal, Design…' });
  const desc = h('input', { name: 'label-desc', maxlength: 200, placeholder: 'What it is for (optional)' });
  const color = h('input', { name: 'label-color', type: 'color', value: LABEL_COLORS[5], 'aria-label': 'Colour' });
  const swatches = h('div', { class: 'swatches', role: 'group', 'aria-label': 'Suggested colours' }, LABEL_COLORS.map((c) => { const b = h('button', { type: 'button', class: 'sw', 'aria-label': 'Colour ' + c, onclick: () => { color.value = c; } }); b.style.background = c; return b; }));
  const rows = labels.map((l) => {
    if (editingLabel === l.id) {
      const n = h('input', { name: 'edit-label-name', value: l.name, maxlength: 40, required: true });
      const ds = h('input', { name: 'edit-label-desc', value: l.description || '', maxlength: 200 });
      const co = h('input', { name: 'edit-label-color', type: 'color', value: WerkbordTimeline.safeColor(l.color), 'aria-label': 'Colour' });
      return h('form', { class: 'row label-row', onsubmit: (e) => { e.preventDefault(); act(async () => { await api('PATCH', '/labels/' + l.id, { name: n.value, description: ds.value, color: co.value, version: l.version }); editingLabel = null; }); } },
        h('div', { class: 'grow stack' }, field('Name', n), field('What it is for', ds), field('Colour', co)),
        h('button', { class: 'primary small' }, 'Save'), h('button', { class: 'plain small', type: 'button', onclick: () => { editingLabel = null; render(); } }, 'Cancel'));
    }
    return h('div', { class: 'row label-row' }, h('div', { class: 'grow' }, labelChip(l), l.description ? h('p', { class: 'muted small' }, l.description) : ''),
      h('span', { class: 'muted small' }, plural(l.tickets, 'open ticket')),
      h('button', { class: 'plain small', onclick: () => { editingLabel = l.id; render(); } }, 'Edit'),
      h('button', { class: 'danger small', onclick: () => confirm('Delete “' + l.name + '”? It comes off ' + plural(l.tickets, 'ticket') + ' and nothing else changes.') && act(() => api('DELETE', '/labels/' + l.id)) }, 'Delete'));
  });
  return h('div', {}, h('section', { class: 'panel' }, h('h2', {}, 'Labels'),
    h('p', { class: 'muted' }, 'Names and colours of your own choosing, shared by every project in this workspace. A label never changes how a ticket moves; who does the work is a separate setting on the ticket.'),
    h('form', { class: 'stack', onsubmit: (e) => { e.preventDefault(); act(async () => { await api('POST', '/labels', { name: name.value, description: desc.value, color: color.value }); name.value = ''; desc.value = ''; }); } },
      field('Name', name), field('What it is for', desc), h('div', { class: 'inline' }, field('Colour', color), swatches), h('button', { class: 'primary' }, 'Add label'))),
    h('section', { class: 'panel' }, h('h2', {}, 'In this workspace'), rows.length ? rows : h('p', { class: 'muted' }, 'No labels yet.')));
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
      fit?.profile?.offerWorkspaceHost && !isHost ? h('p', { class: 'ok' }, 'Owner volunteered for Workspace Host; administrator approval required.') : '',
      fit?.profile?.offerConnectivityHost && !d.capabilities.includes('connectivity_host') ? h('p', { class: 'ok' }, 'Owner volunteered for Connectivity Host; administrator approval required.') : '',
      fit?.hostFit.reasons?.map(reason => h('p', { class: 'muted' }, reason)) || '',
      admins && isHost && !fit?.removal?.allowed ? h('p', { class: 'advice' }, fit?.removal?.reason || 'Team cannot confirm that this Host can be safely removed yet.') : ''),
      isHost ? h('span', { class: 'badge' }, d.hostStatus) : '',
      ownRunner && state.desktop ? h('button', { class: 'plain', disabled: requestPending(), onclick: () => act(async () => { activeRequest = await deviceApi('POST', '/requests', { target: d.id, action: 'fetch_runner_status', payload: {} }); runnerStatus = null; await pollRequest(); }) }, 'Runner status') : '',
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
    devices.filter(d => !d.revokedAt).map(d => { const enabled = d.capabilities.includes('connectivity_host'); return h('div', { class: 'row' }, h('div', { class: 'grow' }, d.name), h('span', { class: 'badge' }, enabled ? 'Connectivity Host' : 'Team device'), can('devices.manage') ? h('button', { class: 'plain', onclick: () => confirm(enabled ? 'Disable this Connectivity Host? Devices may lose remote access. Keep another reachable Connectivity Host online before replacing it.' : 'Enable this Connectivity Host? It must stay online and be reachable from outside your network.') && act(async () => {
      const caps = enabled ? d.capabilities.filter(c => c !== 'connectivity_host') : [...d.capabilities, 'connectivity_host'];
      const connection = await api('GET', '/devices/' + d.id + '/network');
      if (!enabled && !connection.networkEndpoints.length) throw new Error('This computer has not connected yet. Open Team on it, then enable connectivity here.');
      await api('PUT', '/devices/' + d.id + '/capabilities', { capabilities: caps });
      try { await api('PUT', '/devices/' + d.id + '/network', { discovery: !enabled, relay: !enabled }); } catch (e) { await api('PUT', '/devices/' + d.id + '/capabilities', { capabilities: d.capabilities }); throw e; }
      state.info = enabled ? 'Connectivity Host disabled.' : 'Connectivity Host enabled. Remote access will be available when reachability is confirmed.';
    }) }, enabled ? 'Disable' : 'Enable Connectivity Host') : ''); })), alwaysOnHelp());
}

function startupBackupText(backup) {
  return !backup ? 'Backup status could not be checked. Open Backups to try again.'
    : !backup.configured ? 'Set up a backup destination in Backups.'
    : backup.lastError ? 'The last backup failed. Open Backups to check it.'
    : backup.automatic ? (backup.lastOk ? 'Automatic backups are on; the latest backup was checked.' : 'Automatic backups are on; the first backup is being prepared.')
    : backup.automatic === false ? 'Automatic backups are off. You can take a backup from Backups.'
    : 'A backup folder is configured. Open Backups to check the schedule.';
}
async function startupBackup() {
  try { return (await api('GET', '/storage')).backup; } catch (_) { return undefined; }
}
// Use the existing health tick to update the result without rebuilding focused buttons.
async function refreshTeamStart() {
  const status = app.querySelector('[data-startup-backup]');
  if (!status) return;
  const backup = await startupBackup();
  if (!status.isConnected) return;
  const text = startupBackupText(backup);
  if (status.textContent !== text) status.textContent = text;
  status.className = backup?.lastError ? 'error' : 'muted';
}
// The first workspace has a short path from creation to people and backup checks.
async function teamStart() {
  if (!state.desktop || !can('members.manage') || state.ov.projects.some(p => !p.project.archived)) return '';
  const backup = await startupBackup();
  return h('section', { class: 'team-start', 'aria-label': 'Set up your team' },
    h('h2', {}, 'Your team is ready'), h('p', { class: 'muted' }, 'Invite people, approve their computers in Devices, then let each person connect their own runner. Keep a host online while your team works.'),
    h('p', { class: backup?.lastError ? 'error' : 'muted', role: 'status', 'data-startup-backup': true }, startupBackupText(backup)),
    h('div', { class: 'actions' }, h('button', { class: 'primary', onclick: () => go('members') }, 'Invite people'),
      h('button', { class: 'plain', onclick: () => go('devices') }, 'Devices'), h('button', { class: 'plain', onclick: () => go('backups') }, 'Check backups')));
}

async function backupsView() {
  const s = await api('GET', '/storage'); const b = s.backup;
  return h('section', { class: 'panel' }, h('h2', {}, 'Workspace backups'),
    h('p', { role: 'status' }, b?.lastOk ? 'The last backup succeeded ' + ago(new Date(b.lastAt).toISOString()) + '.' : b?.lastError || 'No successful backup has been taken yet.'),
    h('p', { class: 'muted' }, !b?.configured ? 'Backups have not been configured on this host. The desktop device service sets up a backup folder by default.'
      : b.automatic ? 'Automatic backups are on. Each backup is read back and checked. Keep an additional copy on a separate disk.'
      : 'Automatic backups are off. You can take a backup now. Keep an additional copy on a separate disk.'),
    b?.configured ? h('p', { class: 'muted' }, 'Backup folder: ' + b.destination + ' · ' + b.count + ' retained') : '',
    can('devices.manage') ? h('button', { class: 'primary', disabled: !b?.configured, onclick: () => act(async () => { await api('POST', '/storage/backup'); state.info = 'Your backup was taken and checked.'; }) }, 'Back up now') : '');
}

let nativeTransport;
// The desktop app shows this console in a frame beside a person's other workspaces. A framed page cannot reach the app's
// runtime, so it asks the shell that framed it (the bridge does that on its own), and the shell offers it only what a Team
// workspace may ask: to connect the person's own runner to this workspace, to start or stop the Team service, to open the
// invitation the app was started with, and to open a web link in the browser.
function nativeApp() {
  if (window.go?.main?.App) return window.go.main.App;
  const framed = window.parent !== window;
  if (!framed && !window.webkit?.messageHandlers?.external) return null;
  if (!nativeTransport) nativeTransport = import(BASE + '/native-bridge.js').then(m => m.createNativeBridge('main.App'));
  const invoke = (method, ...args) => nativeTransport.then(call => call(method, args, 0));
  if (framed) return { SetupRunner: () => invoke('ConnectRunner'), Service: action => invoke('TeamService', action), PendingInvitation: () => invoke('PendingInvitation'), Info: () => invoke('Info'), OpenExternal: address => invoke('OpenExternal', address) };
  return { SetupRunner: () => invoke('SetupRunner'), Service: action => invoke('Service', action), PendingInvitation: () => invoke('PendingInvitation'), Info: () => invoke('Info'), OpenExternal: address => invoke('OpenExternal', address) };
}
function runnerSetup() {
  const runner = state.device.runner;
  return h('section', { class: 'panel' }, h('h2', {}, 'Your runner on this computer'), h('p', { class: 'muted', role: 'status' }, runner.connected ? 'Connected to your own Werkbord. Runner setup is complete. It keeps its normal execution policies and approvals.' : runner.configured ? 'Your runner is already configured. Its connection is currently unavailable; reconnect without repeating setup.' : 'The free Werkbord app runs your agents with your credentials. Team communicates with it only on this computer.'),
    h('div', { class: 'actions' }, h('button', { class: runner.connected ? 'plain' : 'primary', disabled: runner.connected, onclick: () => act(async () => { if (window.parent !== window) await nativeApp().SetupRunner(); else await deviceApi('POST', '/runner/connect'); state.info = 'Runner connection saved. Checking its reported status…'; }) }, runner.connected ? 'Runner connected' : runner.configured ? 'Reconnect runner' : 'Connect existing runner'),
      !runner.connected && !runner.configured && window.parent === window ? (nativeApp() ? h('button', { class: 'plain', onclick: () => act(async () => { await nativeApp().SetupRunner(); await deviceApi('POST', '/runner/connect'); state.info = 'Runner setup saved. Checking its reported status…'; }) }, 'Install / set up free runner') : extLink('https://github.com/micho8cho93/werkbord/releases', 'Download free Werkbord')) : ''));
}

// Where a held ticket stands in the person's own Werkbord, once their runner is connected: the Team service on this
// computer copies every held ticket of a synchronized project into it as a task (never starting it). Inside the desktop
// app the ticket then opens there; until it can be copied, this says why.
function individualActions(projectId, ticketId, small) {
  const sync = state.desktop && state.device?.sync;
  if (!sync?.active) return '';
  const t = (sync.tickets || []).find(x => x.projectId === projectId && x.ticketId === ticketId);
  const size = small ? ' small' : '';
  if (!t) {
    if ((state.device.settings.syncOff || []).includes(projectId)) return '';
    scheduleDesktopRefresh();
    return h('p', { class: 'muted', role: 'status' }, 'Adding this ticket as a task in Werkbord…');
  }
  if (t.taskId) {
    const open = FRAMED ? h('button', { class: 'primary' + size, 'data-testid': 'open-in-individual', onclick: () => openInIndividual('#/p/' + encodeURIComponent(t.localProjectId) + '/task/' + encodeURIComponent(t.taskId)) }, 'Open in Werkbord')
      : h('p', { class: 'muted' }, 'In your Werkbord as a task, ready to start.');
    return h('span', { class: 'inline individual' }, open, t.conflict ? h('p', { class: 'muted' }, 'The task was changed in Werkbord, so this ticket’s newer text was not applied there.') : '');
  }
  if (t.waiting) return h('span', { class: 'inline individual' }, h('p', { class: 'muted', 'data-testid': 'waiting-for-repository' }, 'Werkbord has no project for this repository yet.'),
    FRAMED ? h('button', { class: 'plain' + size, onclick: () => openInIndividual('#/control') }, 'Choose a project in Werkbord') : '');
  if (t.problem) return h('p', { class: 'muted' }, 'Not in Werkbord yet: ' + t.problem);
  scheduleDesktopRefresh();
  return h('p', { class: 'muted', role: 'status' }, 'Adding this ticket as a task in Werkbord…');
}

// Which projects' held tickets are copied into the person's own Werkbord. All of them, unless turned off here.
function syncPanel() {
  if (!state.device.runner.configured) return '';
  const s = state.device.settings;
  const off = new Set(s.syncOff || []);
  const projects = state.ov.projects.filter(p => p.member && !p.project.archived);
  const save = (id, on) => act(async () => {
    const next = new Set(off);
    if (on) next.delete(id); else next.add(id);
    await deviceApi('PUT', '/settings', { ...s, syncOff: [...next] });
  });
  const problem = state.device.sync?.problem;
  return h('section', { class: 'panel', 'aria-labelledby': 'sync-h' }, h('h2', { id: 'sync-h' }, 'Team handoffs to Werkbord'),
    h('p', { class: 'muted' }, 'Claimed and assigned tickets become local tasks; claiming never starts an agent.'),
    projects.length ? projects.map(p => h('label', { class: 'check-row' }, h('input', { type: 'checkbox', name: 'sync-' + p.project.id, checked: !off.has(p.project.id), onchange: e => save(p.project.id, e.target.checked) }), p.project.name))
      : h('p', { class: 'muted' }, 'You are not on any project yet.'),
    problem ? h('p', { class: 'advice' }, problem) : '');
}

// This computer, its phones and its memory are three sections of Settings: the window lists them, and shows one of them at a time
// (a hosted page). Opened on its own in a browser, this page keeps all three together.
async function settingsView() {
  const only = HOSTED ? (['phones', 'memory', 'history'].includes(state.settingsPart) ? state.settingsPart : 'computer') : '';
  const has = (part) => !only || only === part;
  if (only === 'history') return projectHistoryPanel();
  if (only === 'phones') return h('div', {}, state.desktop ? await phoneSettings() : h('section', { class: 'panel' }, h('h2', {}, 'Phone access'), h('p', { class: 'muted' }, 'Phones are paired in the Team desktop app.')));
  if (only === 'memory') return h('div', {}, await memorySharingPanel());
  if (!state.desktop) return h('div', {}, has('memory') ? await memorySharingPanel() : '', h('section', { class: 'panel' }, h('h2', {}, 'Device settings'), themeButton(), HOSTED ? '' : styleChoice(), h('p', { class: 'muted' }, 'Manage this computer in the Team desktop app.'),
    h('div', { class: 'actions' }, h('button', { class: 'plain', onclick: signOut }, 'Sign out of workspace'))));
  const s = state.device.settings;
  const form = h('select', { name: 'computer-kind' }, [['', 'Choose…'], ['desktop', 'Desktop / Mac mini'], ['laptop', 'Laptop'], ['server', 'Server / NAS']].map(([value, label]) => h('option', { value, selected: s.form === value || (!s.form && !value) }, label)));
  const policy = h('select', { name: 'remote-policy' }, [['ask', 'Ask each time'], ['off', 'Off'], ['auto', 'Allow from trusted devices']].map(([value, label]) => h('option', { value, selected: s.remoteStart === value }, label)));
  const open = h('input', { name: 'remote-open', type: 'checkbox', checked: s.remoteOpen });
  const sleeps = h('input', { name: 'computer-sleeps', type: 'checkbox', checked: s.sleeps || s.form === 'laptop' });
  const hostOffer = h('input', { name: 'offer-workspace-host', type: 'checkbox', checked: s.offerWorkspaceHost });
  const connectivityOffer = h('input', { name: 'offer-connectivity-host', type: 'checkbox', checked: s.offerConnectivityHost });
  const plan = await deviceApi('GET', '/removal');
  return h('div', {}, runnerSetup(), syncPanel(), !only ? await memorySharingPanel() : '', !only ? await phoneSettings() : '', h('section', { class: 'panel' }, h('h2', {}, 'Computer settings'), !HOSTED ? themeButton() : '', !HOSTED ? styleChoice() : '', h('form', { class: 'device-settings', onsubmit: e => { e.preventDefault(); act(() => deviceApi('PUT', '/settings', { ...s, form: form.value, sleeps: sleeps.checked, offerWorkspaceHost: hostOffer.checked, offerConnectivityHost: connectivityOffer.checked, remoteStart: policy.value, remoteOpen: open.checked })); } },
    h('fieldset', { class: 'setting-group' }, h('legend', {}, 'Computer'), field('Type', form), field('Sleeps automatically', sleeps)),
    h('fieldset', { class: 'setting-group' }, h('legend', {}, 'Hosting'), field('Workspace Host', hostOffer), field('Connectivity Host', connectivityOffer),
      h('p', { class: 'muted' }, 'Hosting starts after administrator approval; sleep pauses this computer’s work.'),
      h('details', {}, h('summary', {}, 'Details'), h('p', { class: 'muted' }, 'Workspace Hosts keep Team available. Connectivity Hosts enable remote access. Three Workspace Hosts allow Team to keep working if one goes offline.'))),
    h('fieldset', { class: 'setting-group' }, h('legend', {}, 'Trusted devices'), field('Remote starts', policy), field('Allow trusted devices to open tickets here', open),
      h('p', { class: 'muted' }, 'Compare device identities before granting trust.'),
      h('details', {}, h('summary', {}, 'Details'), h('p', { class: 'muted' }, 'A trusted device can ask this computer to open tickets on its runner. Starting work still follows this computer’s local approvals.')),
      h('p', { class: 'identity' }, 'This computer · ', h('code', {}, state.device.identity)),
      (state.device.senders || []).map(d => h('div', { class: 'row' }, h('div', { class: 'grow' }, d.name, h('code', {}, d.identity)), h('button', { class: 'plain', onclick: () => act(() => deviceApi('POST', '/senders/' + d.deviceId + (d.approved ? '/revoke' : '/trust'), d.approved ? {} : { publicKey: d.publicKey })) }, d.approved ? 'Stop trusting' : 'Trust this device')))),
    changedSubmit('Save settings', [form, sleeps, hostOffer, connectivityOffer, policy, open], undefined, 'primary'))),
    h('section', { class: 'panel' }, h('h2', {}, 'Execution approvals'), h('p', { class: 'muted' }, 'Choose a runner and review its permissions in the ticket’s Agent controls.')),
    h('section', { class: 'panel' }, h('h2', {}, 'Service and data'), h('p', { class: 'muted' }, 'Removing the GUI keeps the Team service and workspace running. Leaving can keep an archive or erase local data.'),
      plan.reason ? h('p', { class: 'advice' }, plan.reason) : '',
      h('div', { class: 'actions' }, nativeApp() ? h('button', { class: 'plain', onclick: () => act(() => nativeApp().Service('stop')) }, 'Stop Team service, keep data') : '',
        h('button', { class: 'danger', disabled: !plan.canLeave, onclick: () => confirm('Leave this workspace? A local archive is kept. Your free runner is unaffected.') && act(() => deviceApi('POST', '/leave', { confirm: 'leave workspace', removeData: false })) }, 'Leave workspace, keep archive'),
        h('button', { class: 'danger', disabled: !plan.canRemoveData, onclick: () => confirm('Leave and permanently remove this workspace’s local secrets and data? Team requires another safe workspace copy first.') && act(() => deviceApi('POST', '/leave', { confirm: 'leave workspace', removeData: true })) }, 'Leave and remove local data')),
      h('p', { class: 'muted' }, 'Move Werkbord Team.app to the Trash to remove only the GUI. Stopping the service preserves data; reopen the app to start it.')));
}

function runnerHandoff(hf) {
  const box = h('div', { class: 'handoff' }, h('strong', {}, 'Open ' + hf.ticket.key + ' on your runner'), h('p', { class: 'muted' }, 'Run this ticket on your computer using your agent. The chosen runner opens it as a backlog task, and its usual approvals still apply.'));
  api('GET', '/devices').then(ds => {
    const mine = ds.filter(d => d.memberId === state.me.member.id && d.capabilities.includes('runner') && !d.revokedAt);
    if (!mine.length) { box.append(h('p', { class: 'muted' }, 'Connect your free runner in Settings first.'), h('button', { class: 'plain', onclick: () => go('settings', 'computer') }, 'Set up my runner')); return; }
    const pick = h('select', { name: 'handoff-runner', 'aria-label': 'Your runner' }, mine.map(d => h('option', { value: d.id }, d.name)));
    box.append(h('div', { class: 'actions' }, pick, h('button', { class: 'primary', disabled: requestPending(), onclick: () => act(async () => { activeRequest = await deviceApi('POST', '/requests', { target: pick.value, action: 'open_ticket_on_runner', payload: { projectId: hf.project.id, ticketId: hf.ticket.id } }); await pollRequest(); }) }, 'Open ticket on runner')), activeRequest ? requestPanel() : '');
  }).catch(e => { box.append(h('p', { class: 'error' }, e.message)); });
  return box;
}

function requestPending() { return !!activeRequest && ['queued', 'delivered'].includes(activeRequest.state); }

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
function runnerStatusPanel() { const policy = { ask: 'Each task needs approval on this computer.', off: 'Remote starts are turned off on this computer.', auto: 'Tickets from your trusted devices can be started here.' }[runnerStatus.remoteStart] || 'Check this computer’s approvals in Settings.'; return h('section', { class: 'panel' }, h('h2', {}, 'Your runner'), h('p', { class: 'muted' }, policy), (runnerStatus.executions || []).map(a => h('div', { class: 'row' }, h('div', { class: 'grow' }, a.title || 'Approved task'), h('button', { class: 'primary', disabled: requestPending(), onclick: () => act(async () => { activeRequest = await deviceApi('POST', '/requests', { target: activeRequest.toDeviceId, action: 'start_authorized_run', payload: { projectId: a.teamProjectId, ticketId: a.ticketId, executionId: a.executionId, fenceId: a.fence } }); runnerStatus = null; await pollRequest(); }) }, 'Start approved task')))); }

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
/** How many of the owner's agent questions on this ticket wait for an answer: the number on the Chat tab. */
function ownAgentPending(k) {
  return (executionViews.get(k.projectId + ':' + k.id)?.detail?.questions || []).filter(q => q.state === 'pending').length;
}

/** The owner's private agent, in two parts that share one view of it: `controls` (what to run and how, and starting it) and `chat`
 *  (its runs, what it asks, what it writes and what you say back). The ticket's tabs choose which one is on screen. */
function ownExecutionPanel(k, part = 'controls') {
  const key = k.projectId + ':' + k.id;
  let view = executionViews.get(key);
  const refresh = async () => {
    if (view.loading) return;
    view.loading = true;
    try {
      const detail = await deviceApi('GET', '/execution/detail?projectId=' + encodeURIComponent(k.projectId) + '&ticketId=' + encodeURIComponent(k.id) + '&agentId=' + encodeURIComponent(view.selection?.agentId || '') + '&runId=' + encodeURIComponent(view.selectedRunId || ''));
      const schedules = await api('GET', '/projects/' + k.projectId + '/schedules');
      const scheduled = schedules.find(s => s.ticketId === k.id && !['completed', 'canceled'].includes(s.state));
      const before = new Set((view.detail?.questions || []).map(q => q.id));
      if (view.detail && detail.questions.some(q => q.state === 'pending' && !before.has(q.id))) toast(k.key + ': your agent needs an answer.');
      view.detail = detail; view.schedule = scheduled; view.error = '';
      // A ticket whose agent is running or waiting for an answer opens on its conversation, unless the person has chosen a view.
      if (state.ticketId === k.id && state.projectId === k.projectId && !state.ticketTabChosen && (detail.questions.some(q => q.state === 'pending') || detail.runs.some(r => ['starting', 'running', 'waiting_for_user', 'blocked'].includes(r.state)))) state.ticketTab = 'chat';
      if (!view.selection) view.selection = { projectId: k.projectId, ticketId: k.id, executionId: scheduled?.executionId || 'exe_' + crypto.randomUUID().replaceAll('-', ''), runnerId: detail.runners.find(r => r.kind === 'local' && !r.disabled && !r.removed)?.id || '', agentId: detail.options?.agentId || detail.agents.find(a => a.available)?.id || '', model: 'default', reasoning: 'default', interaction: 'interactive' };
      if (scheduled && view.selection.executionId !== scheduled.executionId) { view.selection.executionId = scheduled.executionId; view.preview = null; view.approval = null; }
      view.updatedAt = Date.now();
    } catch (error) { view.error = error.message; }
    finally { view.loading = false; }
  };
  if (!view) {
    view = { loading: false }; executionViews.set(key, view);
    void refresh().then(() => { if (state.ticketId === k.id && state.projectId === k.projectId) render(); });
  }
  view.refresh = refresh;
  if (!view.detail) return h('section', { class: 'handoff', 'aria-label': 'Your private agent' }, h('h3', {}, 'Your private agent'),
    h('p', { class: 'muted', role: 'status' }, view.error || 'Loading your runner and agent choices…'),
    view.error ? h('button', { class: 'plain', onclick: () => act(refresh) }, 'Retry agent controls') : '');
  const selection = view.selection;
  const change = (field, value) => { selection[field] = value; view.preview = null; view.approval = null; if (field === 'agentId') { selection.model = 'default'; selection.reasoning = 'default'; act(refresh); } else render(); };
  const agent = h('select', { name: 'agent-' + k.id, onchange: e => change('agentId', e.target.value) }, view.detail.agents.map(a => h('option', { value: a.id, selected: a.id === selection.agentId, disabled: !a.available }, a.name + (a.available ? '' : ' · unavailable'))));
  const runner = h('select', { name: 'runner-' + k.id, onchange: e => change('runnerId', e.target.value) }, view.detail.runners.filter(r => r.kind === 'local' && !r.removed && !r.disabled).map(r => h('option', { value: r.id, selected: r.id === selection.runnerId }, r.name + (r.online ? ' · online' : ' · offline'))));
  const options = view.detail.options?.agentId === selection.agentId ? view.detail.options : null;
  const modelChoices = options?.models || [{ id: 'default', name: 'Agent default' }];
  const model = options?.customModels ? h('input', { name: 'model-' + k.id, list: 'models-' + k.id, value: selection.model, maxlength: 100, onchange: e => change('model', e.target.value), placeholder: 'default' })
    : h('select', { name: 'model-' + k.id, onchange: e => { selection.reasoning = 'default'; change('model', e.target.value); } }, modelChoices.map(m => h('option', { value: m.id, selected: m.id === selection.model }, m.name)));
  const modelList = h('datalist', { id: 'models-' + k.id }, modelChoices.map(m => h('option', { value: m.id }, m.name)));
  const allowed = modelChoices.find(m => m.id === selection.model)?.reasoning || [];
  const reasoningChoices = (options?.reasoning || [{ id: 'default', name: 'Agent default' }]).filter(r => r.id === 'default' || !allowed.length || allowed.includes(r.id));
  if (!reasoningChoices.some(r => r.id === selection.reasoning)) selection.reasoning = 'default';
  const reasoning = h('select', { name: 'reasoning-' + k.id, onchange: e => change('reasoning', e.target.value) }, reasoningChoices.map(r => h('option', { value: r.id, selected: r.id === selection.reasoning }, r.name)));
  const interaction = h('select', { name: 'interaction-' + k.id, onchange: e => change('interaction', e.target.value) }, [['interactive', 'Ask me when needed'], ['autonomous', 'Investigate independently'], ['autonomous_stop_if_blocked', 'Stop if blocked']].map(([value, label]) => h('option', { value, selected: value === selection.interaction }, label)));
  const preapprove = h('input', { type: 'checkbox', name: 'preapprove-' + k.id, checked: !!selection.preapprove, onchange: e => change('preapprove', e.target.checked) });
  const request = () => ({ ...selection, digest: view.preview?.digest || '' });
  // Start agent on the board's card opens this with the policy already laid out, so one more press starts it.
  if (state.agentFocus === k.id) {
    state.agentFocus = '';
    if (!view.preview && !view.approval && !view.error && selection.agentId && selection.runnerId && !view.detail.runs.some(r => ['starting', 'running', 'waiting_for_user', 'blocked'].includes(r.state))) {
      setTimeout(() => act(async () => { view.preview = await deviceApi('POST', '/execution/preview', request()); view.approval = null; }).then(() => document.querySelector('[aria-label="Your private agent"]')?.scrollIntoView({ block: 'start' })), 0);
    }
  }
  const control = (action, run, question, option, reply) => act(async () => { await deviceApi('POST', '/execution/' + action, { projectId: k.projectId, ticketId: k.id, runId: run, questionId: question || '', option: option || '', reply: reply || '' }); await refresh(); });
  const questions = view.detail.questions.filter(q => q.state === 'pending').map(q => {
    const answer = h('input', { name: 'agent-reply-' + q.id, maxlength: 2000, 'aria-label': 'Your answer' });
    return h('div', { class: 'execution-row' }, h('strong', {}, q.kind === 'approval' ? 'Permission requested' : 'Your agent asks'), h('p', { class: 'prose' }, q.prompt), q.context ? h('pre', {}, q.context) : '',
      q.options?.length ? h('div', { class: 'actions' }, q.options.map(o => h('button', { class: 'plain', onclick: () => control('answer', q.runId, q.id, o) }, o))) : '',
      !q.options?.length || q.allowFreeText ? h('form', { class: 'actions', onsubmit: e => { e.preventDefault(); control('answer', q.runId, q.id, '', answer.value); } }, answer, h('button', { class: 'primary', disabled: !!view.error }, 'Reply')) : '');
  });
  const runs = view.detail.runs.map(run => h('div', { class: 'execution-row' }, h('strong', {}, run.agentId + ' · ' + run.state), h('button', { class: 'plain small', 'aria-pressed': view.detail.selectedRunId === run.id, onclick: () => act(async () => { view.selectedRunId = run.id; await refresh(); }) }, 'View activity'), run.branch ? h('p', {}, 'Branch: ', h('code', {}, run.branch)) : '',
    ['starting', 'running', 'waiting_for_user', 'blocked'].includes(run.state) ? h('button', { class: 'danger', onclick: () => control('stop', run.id) }, 'Stop my run') : '',
    run.handoff ? h('details', {}, h('summary', {}, ['completed','failed','stopped'].includes(run.state) ? 'Review final handoff' : 'Review handoff'), h('pre', { class: 'prose' }, JSON.stringify(run.handoff, null, 2))) : ''));
  const message = h('input', { name: 'agent-message-' + k.id, maxlength: 20000, required: true, 'aria-label': 'Message your agent' });
  const currentRun = view.detail.runs.find(r => r.id === view.detail.selectedRunId);
  const activity = view.detail.activity ? h('section', { class: 'execution-activity', 'aria-label': 'Private agent activity' }, h('h4', {}, 'Agent activity'),
    view.detail.activity.hasMore ? h('p', { class: 'muted small' }, 'Showing the latest 200 events. Open in Werkbord for the full history.') : '',
    h('div', { class: 'execution-output', 'data-scroll-key': 'agent-output-' + k.id }, (view.detail.activity.events || []).filter(e => e.type === 'agent.output').map(e => h('div', { class: 'execution-message' }, h('strong', {}, e.payload.stream), h('pre', { class: 'prose' }, e.payload.text))),
      !(view.detail.activity.events || []).some(e => e.type === 'agent.output') ? h('p', { class: 'muted' }, 'No agent output yet.') : ''),
    currentRun && ['running', 'waiting_for_user'].includes(currentRun.state) ? h('form', { class: 'actions', onsubmit: e => { e.preventDefault(); act(async () => { await deviceApi('POST', '/execution/input', { projectId: k.projectId, ticketId: k.id, runId: currentRun.id, text: message.value }); message.value = ''; delete message.dataset.dirty; await refresh(); }); } }, message, h('button', { class: 'plain' }, 'Send message')) : '') : '';
  const failed = view.error ? h('p', { class: 'error', role: 'alert' }, view.error + ' Showing the last loaded details; refresh to reconnect.') : '';
  if (part === 'chat') return h('section', { class: 'handoff', 'aria-label': 'Agent chat' }, h('h3', {}, 'Agent chat'), h('p', { class: 'muted' }, 'Your conversation and output stay on this computer.'), failed, questions, runs, view.detail.activityError ? h('p', { class: 'error' }, view.detail.activityError) : '', activity);
  return h('section', { class: 'handoff', 'aria-label': 'Your private agent' }, h('h3', {}, 'Your private agent'), h('p', { class: 'muted' }, 'Your choices stay on this computer. The ticket is already In Progress while it is yours. ' + (k.workMode === 'hybrid' ? 'When the agent finishes, complete your own steps and submit it for review.' : 'It moves to Review by itself when your agent finishes.')), 
    view.error ? h('p', { class: 'error', role: 'alert' }, view.error + ' Showing the last loaded details; refresh to reconnect.') : '',
    !view.preview ? h('div', { class: 'agent-choices' }, field('Runner on this computer', runner), field('Agent', agent), field('Model', model), modelList, field('Reasoning', reasoning), field('Conversation policy', interaction)) : '', view.detail.optionsError ? h('p', { class: 'muted' }, view.detail.optionsError) : '',

    view.schedule ? h('p', {}, 'Scheduled request: ' + view.schedule.state + ' · ' + new Date(view.schedule.at).toLocaleString() + ' · ' + view.schedule.timezone) : '',
    !view.preview ? h('details', {}, h('summary', {}, 'Schedule or use another device'), h('p', { class: 'muted' }, 'To use another of your devices, approve the execution there first, then use Open in my runner.'), h('label', { class: 'execution-preapproval' }, preapprove, ' Allow this run once, within 30 days')) : '',
    h('div', { class: 'actions' }, !view.preview ? h('button', { class: 'primary', disabled: !!view.error || !selection.agentId || !selection.runnerId || (!view.schedule && view.detail.runs.some(r => ['starting', 'running', 'waiting_for_user', 'blocked'].includes(r.state))), onclick: () => act(async () => { view.preview = await deviceApi('POST', '/execution/preview', request()); view.approval = null; }) }, 'Review and start agent') : '', h('button', { class: 'plain', onclick: () => act(refresh) }, 'Refresh progress')),
    view.preview ? h('div', {}, h('h4', {}, 'Permissions for this run'), h('p', {}, (view.detail.agents.find(a => a.id === selection.agentId)?.name || selection.agentId) + ' · ' + selection.model + ' · ' + selection.reasoning + ' reasoning'), h('p', { class: 'muted' }, 'Runs on this computer using your agent and its permissions. The agent is asked to push a pull request for team review.'), h('details', {}, h('summary', {}, 'Runner permissions'), h('pre', {}, JSON.stringify(view.preview.runtimePolicy, null, 2))),
      h('details', {}, h('summary', {}, 'Task instructions'), h('p', { class: 'muted' }, 'Ticket text does not grant permissions.'), h('pre', { class: 'prose' }, view.preview.title + '\n' + view.preview.description)),
      !view.approval ? h('button', { class: 'plain', onclick: () => { view.preview = null; render(); } }, 'Change settings') : '',
      h('button', { class: 'primary', disabled: !!view.error || (!!view.approval && (!!view.schedule || !!selection.preapprove || view.detail.runs.some(r => ['starting', 'running', 'waiting_for_user', 'blocked'].includes(r.state)))), onclick: () => act(async () => {
        if (!view.approval) view.approval = await deviceApi('POST', '/execution/approve', request());
        if (!view.schedule && !selection.preapprove) {
          try { await deviceApi('POST', '/execution/start', request()); }
          catch (error) { await refresh().catch(() => {}); throw error; }
          view.preview = null; view.approval = null; selection.executionId = 'exe_' + crypto.randomUUID().replaceAll('-', ''); await refresh(); state.ticketTab = 'chat';
        }
      }) }, view.schedule || selection.preapprove ? 'Approve this execution once' : view.approval ? 'Start my approved run' : 'Start agent')) : '',
    view.approval ? h('p', { role: 'status' }, 'Authorized once until ' + new Date(view.approval.expiresAt).toLocaleString() + '. The scheduled request will wait for eligibility and this runner.') : '');
}


async function phoneSettings() {
  const status = await deviceApi('GET', '/phones');
  const panel = h('section', { class: 'panel' }, h('h2', {}, 'Phone access'));
  if (!status.enabled) { panel.append(h('p', { class: 'muted' }, 'Ask your admin to turn on phone access')); return panel; }
  panel.append(h('p', { class: 'muted' }, status.connected ? 'Connected to your company relay. Phones can read your Team summary.' : 'The company relay is reconnecting. Keep this computer online.'));
  panel.append(h('button', { class: 'primary', onclick: () => act(async () => {
    const grant = await deviceApi('POST', '/phones');
    state.secret = { kind: 'phone', token: grant.url, name: 'Team phone' };
  }) }, 'Pair a phone'));
  for (const id of status.grants || []) panel.append(h('div', { class: 'row' }, h('code', { class: 'grow' }, 'Phone ' + id.slice(-8)), h('button', { class: 'plain', onclick: () => act(() => deviceApi('DELETE', '/phones/' + encodeURIComponent(id))) }, 'Revoke phone')));
  return panel;
}

setInterval(async () => {
  if (!state.desktop || !state.ticketId || actionBusy || document.visibilityState !== 'visible') return;
  const key = state.projectId + ':' + state.ticketId;
  const view = executionViews.get(key);
  if (!view?.refresh || view.loading) return;
  await view.refresh();
  if (key === state.projectId + ':' + state.ticketId && !actionBusy) await render();
}, 5000);

// Sharing choices belong to the local controller; this surface holds only its
// separately authorised workspace-bound journal grant and Team-owned records.
const MEMORY_STATE = { suggested: 'Waiting for approval', approved: 'Approved, not shared yet', published: 'Shared with the team' };
const MEMORY_SOURCE = { git: 'From Git activity', user: 'Written by a person', run: 'From an agent run', handoff: 'From a handoff', import: 'Imported' };
// The repository a project's memory is filed under: host and path, as the controller canonicalises it. A project may have none.
function memoryRepository(remote) {
  if (!remote) return '';
  if (remote.startsWith('git@') && !remote.includes('://')) remote = 'ssh://' + remote.replace(':', '/');
  let address;
  try { address = new URL(remote); } catch { return ''; }
  let host = address.hostname.toLowerCase().replace(/^\[|\]$/g, '');
  const defaultPort = { 'https:': '443', 'ssh:': '22', 'git:': '9418' }[address.protocol];
  if (address.port && address.port !== defaultPort) host = (host.includes(':') ? '[' + host + ']' : host) + ':' + address.port;
  return host + '/' + address.pathname.replace(/^\//, '').replace(/\/$/, '').replace(/\.git$/, '');
}
async function memorySharingPanel() {
  const projects = state.ov.projects.filter(p => p.member && !p.project.archived).map(p => p.project);
  const box = h('section', { class: 'panel memory-panel', 'aria-labelledby': 'memory-sharing-h' }, h('h2', { id: 'memory-sharing-h' }, 'Team memory'),
    h('p', { class: 'muted' }, 'This workspace stores up to 10,000 memory revisions. Changes and publication steps consume revisions; a curator can remove obsolete memories when capacity is reached.'),
    h('p', { class: 'muted' }, 'Short notes about each project that the team has agreed to keep. Nothing is shared until you turn it on for a project in Personal → Settings → Personal memory, and another person approves it.'),
    state.device?.sync?.problem ? h('p', { class: 'advice' }, state.device.sync.problem) : '');
  if (state.desktop) {
    const token = h('input', { type: 'password', name: 'memory-grant', autocomplete: 'off', required: true });
    box.append(h('details', {}, h('summary', {}, 'Connect memory sharing'),
      h('form', { onsubmit: e => { e.preventDefault(); act(async () => { await deviceApi('POST', '/memory/grant', { token: token.value }); token.value = ''; state.info = 'Memory sharing connected.'; }); } },
        field('Grant from Personal', token), h('button', { class: 'primary', type: 'submit' }, 'Connect'))));
  }
  const results = h('div', { class: 'memory-list', 'aria-live': 'polite' });
  const choose = h('select', { name: 'memory-project', 'aria-label': 'Project' }, projects.map(p => h('option', { value: p.id }, p.name)));
  async function load() {
    results.replaceChildren();
    const p = projects.find(p => p.id === choose.value);
    if (!p) { results.append(h('p', { class: 'muted' }, 'You are not on any project yet.')); return; }
    const repository = memoryRepository(p.repository);
    if (!repository) { results.append(h('p', { class: 'muted' }, p.name + ' has no repository connected, so it has no shared memory. Connect one in the project’s settings.')); return; }
    // The service validates the canonical repository binding before returning data.
    const scope = { workspaceId: state.me.workspace.id, kind: 'repository', projectId: p.id, repository };
    let after = '';
    const records = [];
    do {
      const page = await api('POST', '/memory/query', { schema: 'werkbord.memory/v1', scope, after, published: false });
      records.push(...(page.records || []));
      after = page.more ? page.next : '';
    } while (after && records.length < 200);
    if (!records.length) { results.append(h('p', { class: 'muted' }, 'Nothing has been proposed for ' + p.name + ' yet.')); return; }
    const waiting = records.filter(r => r.state === 'suggested').length;
    results.append(h('p', { class: 'memory-count' }, plural(records.length, 'note') + (waiting ? ' · ' + waiting + ' waiting for approval' : '')));
    for (const r of records) {
      const command = action => api('POST', '/memory/' + r.id, { schema: r.schema, scope: r.scope, version: r.version, action }).then(load).catch(e => { state.error = e.message; render(); });
      const source = MEMORY_SOURCE[r.content?.source?.kind] || 'From this project';
      results.append(h('article', { class: 'memory-card' },
        h('header', {}, h('span', { class: 'badge' + (r.state === 'published' ? '' : ' warn') }, MEMORY_STATE[r.state] || r.state), h('span', { class: 'muted small' }, source)),
        h('p', { class: 'memory-summary' }, r.content?.summary || 'No summary'),
        h('div', { class: 'actions' },
          r.state === 'suggested' && r.authorId !== state.me.member.id ? h('button', { class: 'plain small', onclick: () => command('approve') }, 'Approve') : '',
          r.state === 'approved' ? h('button', { class: 'plain small', onclick: () => command('publish') }, 'Share with team') : '',
          h('button', { class: 'danger small', onclick: () => confirm('Revoke this shared note? Copies already read, exported or backed up elsewhere cannot be erased.') && command('delete') }, 'Revoke'))));
    }
    if (after) results.append(h('p', { class: 'muted' }, 'Showing the first 200 notes.'));
  }
  const show = () => load().catch(e => results.replaceChildren(h('p', { class: 'error' }, e.message || 'These notes could not be loaded.')));
  choose.addEventListener('change', show);
  box.append(projects.length ? field('Project', choose) : '', results,
    h('details', {}, h('summary', {}, 'How sharing works'),
      h('p', { class: 'muted' }, 'Only validated Git metadata from projects you enable can be proposed. Summaries and decisions need their own choice and a review. Sessions, transcripts, code, credentials and paths are never shared. Sharing needs another person’s approval, and revoking removes access once acknowledged; copies already read or backed up may remain.')),
    projects.length ? h('details', {}, h('summary', {}, 'Identifiers for Personal'),
      h('p', { class: 'muted' }, 'Copy these into Personal → Settings → Personal memory.'),
      h('p', { class: 'identity' }, state.me.workspace.id),
      projects.map(p => h('p', { class: 'identity' }, p.name + ' · ' + p.id + (p.repository ? ' · ' + p.repository : '')))) : '');
  if (projects.length) show();
  return box;
}

// ---- the plan (docs/PLANS.md) ----
// A team of two people is free; a team of more than that needs every member on Premium. The Workspace Host decides: this only says
// what it said. The unlock key is entered in the Werkbord window's Settings, under Plan, which this page asks the window to open.

function askForPlan() {
  if (FRAMED) window.parent.postMessage({ type: 'werkbord.frame', event: 'plan' }, '*');
  else { state.upgrade = ''; state.info = 'Open Werkbord on this computer and enter your unlock key in Settings, under Plan.'; render(); }
}

// Shown above the page when this member can read the team and not change it.
function planBanner() {
  const p = state.plan;
  if (!p || !p.enforced || !p.readOnly) return '';
  return h('div', { class: 'plan-banner', role: 'status', 'data-testid': 'plan-read-only' },
    h('span', {}, 'This team has ' + p.people + ' people, so everyone in it needs Premium. You can read it, but not change it, until you unlock Premium.'),
    h('button', { class: 'primary', type: 'button', onclick: askForPlan }, 'Unlock Premium'));
}

// One line on the people page: how full the Free plan is, while the team is still on it.
function planNote() {
  const p = state.plan;
  if (!p || !p.enforced || p.ownerPremium || p.people > p.freeLimit) return null;
  return h('p', { class: 'muted plan-note', 'data-testid': 'plan-note' }, p.people + ' of ' + p.freeLimit + ' people on the Free plan. To add another person, the team’s owner and that person need Premium.');
}

// The answer to a change the plan refused: what it said, and a way to the place the key is entered.
function upgradeDialog() {
  if (!state.upgrade) return '';
  const close = () => { state.upgrade = ''; render(); };
  return h('dialog', { class: 'upgrade-dialog', 'aria-labelledby': 'upgrade-title', onclose: () => { if (state.upgrade) close(); }, 'data-testid': 'upgrade-dialog' },
    h('h2', { id: 'upgrade-title' }, 'Teams of 3 or more need Premium'),
    h('p', {}, state.upgrade.charAt(0).toUpperCase() + state.upgrade.slice(1)),
    h('p', { class: 'muted' }, 'Nothing is lost if you stay on Free: this team keeps working, and so does everything you do on your own.'),
    h('div', { class: 'actions' }, h('button', { class: 'plain', type: 'button', onclick: close }, 'Not now'),
      h('button', { class: 'primary', type: 'button', onclick: () => { askForPlan(); close(); } }, FRAMED ? 'Enter unlock key' : 'How to unlock')));
}
