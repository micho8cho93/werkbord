# Werkbord Team: the customer-owned private network

A Team workspace can have its own private network, made and run by the workspace's own machines, with no account, no
service and no server of anyone else's. It replaces the need for a Tailscale account for a Team workspace. (The
individual Werkbord's Tailscale support is unchanged.)

The decision and its reasons are [ADR 0002](adr/0002-customer-owned-network.md). The threat model and what the tests
prove are in [TEAM_SECURITY.md](TEAM_SECURITY.md#the-private-network-team-25). This page is how it works and how to run it.

> **No Werkbord-operated infrastructure.** There is no relay, rendezvous server, control plane, discovery server,
> network registry or customer key service that Werkbord runs, and nothing in Team's code or settings can be pointed at
> one. Everything the network needs runs on machines the customer owns. What Werkbord publishes is signed software.

## The pieces

| | |
| --- | --- |
| **Workspace Host** | A machine that runs Team for the workspace. It holds the workspace's **signing keys** (below) and answers devices that are joining. A high-trust machine. |
| **Connectivity Host** | A machine of yours that can be reached from the Internet. It carries traffic for devices that cannot reach each other directly (a *relay*) and helps devices find each other. Often also a Workspace Host. |
| **Discovery host** | A host (a Workspace Host or a Connectivity Host) that says where it can be reached, so that devices can find the workspace's machines and each other; in Nebula's words a *lighthouse*. A workspace should have more than one. |
| **Member device** | A person's computer. It joins with an invitation, makes its own keys, and gets a certificate for the network. |
| **The network** | [Nebula](https://github.com/slackhq/nebula) (MIT), pinned to one release, run as a supervised program on each host. Team ships it and does not reimplement it. |

A device is on the network because it holds a certificate signed by the workspace's own authority. Being on the network
is never enough to be let in to anything: every request is checked again, by who is asking (the application's own
layer), and the network's own firewall is default-deny (below).

## Three families of keys, never the same key

| Key | What it is | Who holds it | What it does |
| --- | --- | --- | --- |
| **Workspace key** | Ed25519 | Workspace Hosts only | Is the workspace's identity: its fingerprint is what a joining device pins. Signs invitations and the certificates of the bootstrap endpoints. Nothing to do with the network. |
| **Network authority key** | Nebula's (Ed25519), in a Nebula certificate authority | Workspace Hosts only | Signs the certificates that let a device onto the network. The most sensitive secret a workspace has. |
| **A host's own keys** | an application key (Ed25519), a network key (X25519), a sealing key (X25519) | each host, for itself | The application key registers it as a device; the network key is the private half of what its certificate certifies; the sealing key is what the workspace's secrets are encrypted to when handed to it. |
| **A member device's keys** | its application key (the individual product's, never Team's) and its network key | that device, only | A device makes its own and sends only the public halves. The workspace never has a device's private key. |

On a Workspace Host the secret ones are in `<data dir>/pki/`, sealed with AES-256-GCM. macOS production builds use Keychain, with separate authority and device wrapping keys; root services use System Keychain. Unsupported OS-storage builds need an external owner-only passphrase file (Argon2id). A failed Keychain never falls back to an adjacent key. Explicit `WERKBORD_TEAM_KEY_STORAGE=file` is only 3.x evaluation/transition compatibility. Same-UID/root compromise can still read unlocked authority material. Protect host recovery/unlock material separately from ordinary device/database backups; see [TEAM_SECURITY.md](TEAM_SECURITY.md).

### A Workspace Host is a high-trust machine

Whoever holds the workspace key and the authority key can speak for the workspace and put any device on its network, in
any group, including the group that may reach the database. So a Workspace Host must be run, patched and physically
protected like the most important machine of the workspace. That is the price of having more than one: each host that can
issue certificates is a place where the authority can be lost. Do not make a host out of a laptop that travels.

A Workspace Host cannot run anything on a member's computer, read a member's files or impersonate a member's *application*
identity (it never has a device's private key); what it can do is act as the network's authority and as the workspace.
Team still starts no developer process anywhere ([PRODUCTS.md](PRODUCTS.md)).

## Starting a workspace's network

On the machine that will be the first Workspace Host:

```bash
export WERKBORD_TEAM_LICENSE_FILE=/customer/licenses/team.json
werkbord-team workspace create --name "Acme" --owner "Ada" \
    --endpoint team.example.org          # a name or address at which this machine can be reached from outside
werkbord-team serve
```

Nebula networking is the production default; workspace creation makes, on this host:

1. the workspace's own **trust identity** (the workspace key and its root certificate) and a random **workspace ID**;
2. the network's **certificate authority** (Nebula, certificate format v2, valid ten years) for a private range chosen at
   random (a /16 in 10.128.0.0/9; `--network-range` chooses another, any private /16–/24);
3. this host's **own keys**, registered as the owner's first device, a Workspace Host;
4. its address (the first in the range) and its first certificate;
5. a **discovery host** role, as soon as it says where it can be reached (`--endpoint`): on one LAN (`--endpoint 192.168.1.10`)
   that is all the network needs;
6. and, **if** an `--endpoint` it advertises could be reached from outside its own network (a public address or a DNS
   name), the **Connectivity Host** role as well, with relaying. `--connectivity yes|no` overrides; `yes` is
   refused if the address cannot be reached from outside. If it is not made one, `workspace create` says plainly that remote
   access cannot be guaranteed and what to do.

It prints the workspace **fingerprint**, which every device pins and which you read out when you invite someone.

`--endpoint` (or `WERKBORD_TEAM_ENDPOINTS=a,b`) takes hosts only, without a scheme or a port: the ports are
`WERKBORD_TEAM_BOOTSTRAP_ADDR` (TCP 7440, where devices join) and `WERKBORD_TEAM_NETWORK_PORT` (UDP 4242, the network).
Forward both to the host if it is behind a router.

### Settings

| Setting | Default | |
| --- | --- | --- |
| `WERKBORD_TEAM_ENDPOINTS` | none | hosts at which this machine can be reached from outside, comma separated |
| `WERKBORD_TEAM_BOOTSTRAP_ADDR` | `0.0.0.0:7440` | where this host answers devices that are joining (TLS 1.3) |
| `WERKBORD_TEAM_NETWORK_PORT` | `4242` | the network node's UDP port |
| `WERKBORD_TEAM_NETWORK_NODE` | on | `off` to not run the node on this host (it needs the privileges below) |
| `WERKBORD_TEAM_NEBULA_DIR` | none | extra directories the pinned program may be in; it is still checked against the pin |
| `WERKBORD_TEAM_PKI_PASSPHRASE_FILE` | none | seal this host's keys with a passphrase instead of a key beside the data |
| `WERKBORD_TEAM_KEY_STORAGE` | `os` | Keychain where supported; `file` is explicit evaluation/3.x transition only |

Every setting is an address or a file of the customer's own. A test fails the build if one is a URL, an account or a
service, or if any code that runs names an address that is not the customer's (`TestNoServiceURLIsBuiltIntoTheNetworkCode`).

### Privileges

A node with a network interface needs the right to create one:

- **Linux:** run Team as a dedicated user with `AmbientCapabilities=CAP_NET_ADMIN` (systemd), or as root.
- **macOS:** the node must run as root (a launchd daemon). Team then owns its data as root; Team checks the owner of what
  it runs and reads keys from.
- A **Connectivity Host that is only a Connectivity Host** (no Workspace Host, no runner) runs the node **without an
  interface** and needs no privileges at all: finding and relaying are done below the interface.

If the node cannot start, `serve` keeps running, says why (`werkbord-team network status` shows it), and tries again each
minute. It never loops starting a failing program.

## Joining: invitations and enrollment

An administrator makes an invitation:

```bash
werkbord-team network invite --label "Bo" --qr                       # a new member's device
werkbord-team network invite --for-member tmb_… --capability runner   # another device of a member
werkbord-team network invite --for-member tmb_… --capability workspace_host   # a new host
```

It prints a `werkbord://join/…` link (and, with `--qr`, a QR code) and the workspace fingerprint. **The link is the only
copy**: only a hash of its one-time credential is stored.

### What is in an invitation

Only what is needed to find and recognise the workspace and to prove one was invited:

| Field | |
| --- | --- |
| invitation ID, workspace ID, workspace name | which workspace; the ID is random and public |
| workspace public key and its fingerprint | what the device pins |
| bootstrap endpoints | the machines to try, in order: Connectivity Hosts and other reachable hosts first; more than one when there is more than one |
| a one-time enrollment credential | 256 random bits; spent when used |
| the role and capabilities | what the invitation is for (the workspace enforces its own record of them) |
| issued and expiry times | 24 hours by default, 14 days at most |
| a signature | by the workspace key, over the exact bytes |

**Not** in it, ever: a Nebula private key, a reusable certificate, a member token, a path or a credential. Changing any
byte (the role, an endpoint, the expiry) breaks the signature, and a changed credential is refused by the workspace.

What the signature does and does not prove: it shows the invitation was not altered after it was signed. It does not show
the invitation is the one the administrator meant, because anyone can make one with a key of their own; that is the
authenticity of the channel the link arrived over. So `device join --expect-fingerprint` (and the individual product's
equivalent) lets the person insist on a fingerprint they learned another way, and refuses any other workspace before a
byte is sent.

### What happens when a device joins

1. The device **parses and verifies** the invitation (format, signature, fingerprint, expiry).
2. It **makes its own keys** (its application key; its network key). Neither leaves it.
3. It connects to an invitation endpoint over **TLS 1.3**. The only trust anchor is the workspace key: the server presents
   a certificate that chains to a root with that key, valid for the address dialled, and nothing else is accepted. A wrong
   machine learns nothing, **not even the credential**: the device does not send it until the server has proved it is the
   workspace.
4. It sends the credential, its **public** keys and a signature, made with its application key, over the whole request and
   over a value both ends derive from this TLS connection (the exporter, RFC 8446 §7.5), so a recorded request is useless on
   any other connection, and a request cannot be made by someone who lacks the device's key. This is standard TLS and
   standard signatures; no handshake was invented.
5. The workspace checks the credential in constant time against its hash, that the invitation is open and unexpired, and
   that nothing the joiner chose clashes (a name; a device ID). Every way an invitation can be unusable — never existed,
   wrong credential, expired, withdrawn, spent — gets the same answer. A refused guess does not use the invitation up.
   The answer to the *joiner* is the same; the host's own log (`service.log` for the desktop service) records the reason,
   without any credential, so an administrator can tell a spent invitation from a mistaken one.
   Before connecting, the joining computer checks the invitation against its own clock and allows the two computers' clocks
   to differ by five minutes (the same allowance the network's certificates are issued with). Beyond that it says the
   clocks disagree, which is different from a link that is wrong or an invitation that has ended.
6. **Optionally an administrator approves** (`werkbord-team network approval admin` for the whole workspace, or the
   invitation's own `--require-approval`). The device waits; the administrator sees its name and the fingerprint of its key
   (`werkbord-team network pending`) to compare with what the person reads out, and approves or denies. Nothing exists for a
   pending device: no member, no address, no certificate.
7. The workspace makes it a device of a member, gives it an address, signs a certificate for **its** network key, and
   mints its API credential. The device **collects** them once; a lost credential is replaced by an administrator.
8. The invitation is **spent** at the moment the credential is accepted, in the same transaction.
9. The device installs the certificate and configuration and brings its node up.

The workspace's policy is chosen at `workspace create --approval admin|auto` and changed with `werkbord-team network approval`.

### Hosts that join

`werkbord-team device join <link> --expect-fingerprint …` (on the new host) makes it a host of the workspace. A member's
own computer joins with Werkbord, which holds that device's key; `device join` refuses an invitation for one.

## More than one Workspace Host

The first host's keys can be given to others, so that the workspace's authority does not live on one machine:

```bash
werkbord-team network invite --for-member tmb_… --capability workspace_host      # on the first host
werkbord-team device join <link> --expect-fingerprint …                          # on the new host
werkbord-team network node                                                       # on the new host: brings it onto the network
werkbord-team host promote <device id>                                           # an administrator
werkbord-team host collect                                                       # on the new host, over the network
```

`promote` seals the **minimum signing material** (the workspace key's seed, the authority's key and certificate) to the
new host's sealing key with HPKE (RFC 9180: DHKEM(X25519), HKDF-SHA256, ChaCha20-Poly1305), bound to the workspace and the
device ID, and stores the *ciphertext*. Only the host that holds the private half of the key it presented when it enrolled
can open it; `collect` fetches it with the host's own credential, refuses it unless the workspace key matches the
fingerprint it enrolled with and the authority matches the certificate it was given, stores it sealed, and tells the
workspace. The ciphertext is removed once collected, and when the device is revoked. Nothing here is in any other API
response.

**And, since Team 2.6, the data.** When the workspace keeps its data in a cluster (the default for a new workspace;
[TEAM_STORAGE.md](TEAM_STORAGE.md)), `promote` also seals what the host's database node needs to join the cluster, and
`collect` is followed by `werkbord-team serve`: the host's node joins as a read-only replica, takes the whole database, is
checked against the cluster's data, and only then votes and is recorded as an active Workspace Host. Until then the host is
*joining*. A host given the Workspace Host role after it enrolled needs a certificate that carries the `workspace-host` group,
which is what lets the others' firewalls admit it on the database's ports; it asks for a new one when it notices the groups in
its certificate are not the ones its roles call for. Three hosts are recommended. A workspace that still keeps its data in one
file has a second host that holds the authority and not the records, as before.

**Removing a host for cause.** A host that held the keys held the authority. Revoking it ends its device, but the keys it
held are not un-held. If the machine may have been compromised, treat the network as compromised: stand up a new
workspace network and move to it. Rotating the authority in place is not built.

## Connectivity Hosts, discovery and relays

Devices need something to introduce them, even on one LAN. Nebula's answer, and so Team's, is a **discovery host**: a
machine with a known address that devices report to and ask. Any Workspace Host or Connectivity Host that says where it can be
reached can be one. A device is told of **every** discovery host the workspace has
(`lighthouse.hosts` lists them all) and reports to all of them, so no host is the one authority, and a device that joins
while one is down still finds the network through the others. Where two devices cannot reach each other directly (both behind
strict NAT), a **relay** (also on a Connectivity Host) carries their traffic. (Upstream marks relays experimental; Team
therefore enables them on Connectivity Hosts and reports whether any exists, and does not promise they are enough.)

### What cannot be promised

**NAT traversal is not guaranteed.** Reliable connections between arbitrary machines on the Internet, with no vendor and no
third-party relay, need at least one machine **you own that can be reached from the Internet**. Two, so that one may be down.

If every host is behind a home router, a company firewall or **carrier-grade NAT** (an address in `100.64.0.0/10`, or a
router that does not forward ports) and none of yours is reachable, Team **says remote access cannot be guaranteed** and
does not try to hide it by relaying through anything of its own. A workspace whose members are all on one LAN does not need
a Connectivity Host.

### What Team reports

`werkbord-team network status` (and `GET /api/team/v1/network`) shows, for each host: whether it is online; whether it holds
the Workspace Host and Connectivity Host roles; whether it is a discovery host and a relay; each address it advertises and
what kind it is (**public**, **private**, **shared** (carrier-grade NAT), **loopback**, **link-local**, or a DNS **name**);
its **reachability** (`public`, `private only`, `unreachable`, `unknown`); and when it was last checked, and **when a check
from another device last succeeded**. And a verdict:

- **Remote access: available** only when a Connectivity Host has been reached from another device at an address that could be
  reached from outside. Otherwise **not guaranteed**, with the reason.
- **Warnings** in plain words: no Connectivity Host; none confirmed reachable; only one; only one discovery host; none
  relaying; no active Workspace Host.

How reachability is known, with no outside service: every Workspace Host, every five minutes, connects to the enrollment
endpoint of each host (and itself) and checks, with the pinned workspace key, that it is really the workspace. A check made by
a host of itself shows only that its endpoint answers; it never counts as "reachable from outside". A check from **another**
device, at a **public** address or a name, does. A private, shared or loopback address can never be reported public, whatever
answers. These checks test the TCP enrollment endpoint; **forward the UDP port too**: reaching one does not prove the other.

## What the network allows: default-deny policy

The network's firewall (enforced by each node on what reaches it and what it starts) denies everything that is not named, in
both directions, and names things by the *group* in the sender's certificate. Groups are in the certificate the authority
signed; a device cannot give itself another. There is no way to write an allow-anything rule: the policy generator refuses a
rule with no port, no group or an unknown group.

| A device that is… | may start | accepts |
| --- | --- | --- |
| a **member's device** (`member`, `runner`) | the workspace's API on a Workspace Host (TCP 7430), and ping to hosts | ping from hosts. **No inbound TCP, including from Workspace Hosts.** |
| a **Workspace Host** (`workspace-host`) | the API and replication on other Workspace Hosts | the API from members and hosts; the database's ports (TCP 4001–4002) **from Workspace Hosts only**; ping from hosts |
| a **Connectivity Host** (`connectivity-host`) | ping to hosts | ping from hosts. Finding and relaying happen below the firewall and need no rule. |

So a member **cannot** reach another member's runner, files or any port (the receiving device refuses it even if the sender's
own configuration was edited to allow everything); cannot reach the database from the member role; cannot reach SSH,
administration or arbitrary ports on a host; and a Connectivity Host exposes nothing but what it relays. Each of these is
tested against Nebula's own firewall, with real nodes (`internal/team/infra/overlay`: `TestThePolicyAsNebulaEnforcesIt`),
and a control proves that the same attempts succeed when nothing forbids them. Ports 7430/4001–4002 are the defaults. Port 7450 is closed; devices poll signed messages from the workspace API and need no inbound service.

The policy is enforced by the *receiver*. A node that edits its own configuration changes only what it accepts itself: it
cannot make another node accept anything. That is why the sensitive things (the database, the API) are guarded by the
Workspace Hosts' own rules.

## Revoking a device: two independent layers

Remote API authentication now requires a fresh linearizable fence. An isolated host cannot authorize a stale token/device/role; long polls recheck their actor every second and before a final response. Device-signed API proofs add application authorization to Nebula membership and persist mutating nonces across hosts/restarts.

`werkbord-team device revoke <id>` (or `POST /devices/{id}/revoke`, or removing the member, which revokes all their devices
first):

1. **The application layer is authoritative and immediate.** The device is marked revoked, its API credential is deleted, and
   `Authenticate` refuses it on the very next request. **A revoked device cannot use the workspace's API even if stale network
   configuration still lets its packets reach a host**: they arrive at an API that no longer knows who it is. Signed messages
   from it are refused too, and it can neither renew its certificate nor fetch the configuration.
2. **The network layer follows, as defence in depth.** Every certificate the workspace issued to it is marked revoked and its
   fingerprint goes on the **blocklist** each host gives its network node. A host applies a changed blocklist within about 15
   seconds, by reloading the node (no restart). Tested with the real program: the same device with the same certificate is
   refused (`certificate is in the block list`). The certificates of a removed member stay on the list after the member's
   records are gone, until each would have expired anyway.

Node certificates are short-lived (**30 days**), renewed at two thirds of their life, so a device whose membership ended falls
off the network by itself even if nothing else tells it to.

## Running the pinned Nebula

Werkbord ships **Nebula v1.11.2** (released 2026-09-22; MIT licence), pinned, and never downloads anything when Team runs.

| | |
| --- | --- |
| Pin | version, archive hashes and the hash of the program itself, per platform, in `internal/team/infra/nebula/manifest.go` |
| Where the hashes come from | the release's own `SHASUM256.txt`, kept in `third_party/nebula/SHASUM256-v1.11.2.txt`; a test checks every pin against it |
| Upstream provenance | the project publishes no signed attestations for its releases, so the pin rests on checksums taken from the release; re-verify them (below) when you review a change |
| Platforms | macOS (a universal binary) and Linux, amd64 and arm64. **Windows is not supported**: Nebula needs the third-party Wintun driver there, and its licence has terms this project has not taken on |
| Licences | `third_party/nebula/LICENSE` (MIT), `THIRD_PARTY_LICENSES.txt` (the 33 modules in the program, generated), `third_party/go/LICENSE`; they ship in the release archive and are installed in `share/doc/werkbord-team/licenses` |
| Where it is found | beside the executable (`libexec/werkbord-team/nebula`), in `<prefix>/libexec/werkbord-team`, or a directory named in `WERKBORD_TEAM_NEBULA_DIR`. **Never the PATH.** |
| Before every start | the file found is hashed against the pin and copied to a private directory; the copy is hashed again immediately before it is started |

### The supervisor starts one program and nothing else

`internal/team/infra/nebula` has one entry that starts a process, `StartNebula(NebulaConfig)`, which starts the verified,
pinned program with the arguments `-config <file>` that the package builds itself, in a private directory, with an empty
environment. There is no `Run(command, args)`; the typed configuration has no field for a program, an argument, an
environment variable or a path outside its data directory; the configuration it writes is rendered from a typed description,
never accepted as text. Architecture tests keep it so (`TestTheNetworkSupervisorIsNotAGeneralRunner`: the exported API is
exactly the reviewed list, no parameter or field means "something to run", no `[]string`, no shell, one `exec.CommandContext`
call with the literal program and the literal `-config`). It restarts the node with a growing delay if it dies, and reports,
instead of retrying forever, a node that cannot start.

### Bumping the pinned Nebula

A reviewed change, all together: the version and every hash in `manifest.go`; `third_party/nebula/` (the new `LICENSE`, the
new `SHASUM256-v<version>.txt`, `scripts/gen-nebula-notices.sh <version>`); the version in `go.mod` (the `cert` package is
linked in-process); and the tests, which fail until they agree. Fetch with `scripts/fetch-nebula.sh` (it refuses anything that
does not match the pin) and run `make test-nebula`.

## The network as a transport

`internal/transport` is the contract between Werkbord and whatever network carries its traffic, and names none.
`internal/team/infra/overlaynet` is the private network as a `transport.Transport`: it listens only on the host's own
address on the network (so nothing it accepts comes from outside), its `Dial` refuses any destination outside the workspace's
range, and its peers are the workspace's own devices. It passes the same conformance suite as the in-memory and Tailscale
transports. Code written against the contract never names the network program; the individual product keeps using the
Tailscale transport.

## Commands

| | |
| --- | --- |
| `werkbord-team workspace create … --network` | the workspace, its keys, its network, its first host |
| `werkbord-team serve` | the server; on a host with a network, also the enrollment endpoint, the node, and the API on the network |
| `werkbord-team network status` | hosts, reachability, warnings |
| `werkbord-team network invite` | an invitation (`--for-member`, `--capability`, `--hours`, `--require-approval`, `--qr`) |
| `werkbord-team network pending` / `approve <id>` / `deny <id>` / `approval auto\|admin` | devices waiting for approval, and whether they have to |
| `werkbord-team device join <link>` / `list` / `revoke <id>` | a host joining; the registry; revocation |
| `werkbord-team host promote <id>` / `collect` | handing the keys to another Workspace Host |
| `werkbord-team network node` | the node of a host that joined but does not hold the workspace's data |

The API is under `/api/team/v1` (`/network`, `/devices`, `/enrollment-invitations`, `/enrollments`); each route is in the
reviewed list in `internal/team/api/routes_test.go`.

## Backups and recovery

Current procedures: [TEAM_BACKUP_RECOVERY.md](TEAM_BACKUP_RECOVERY.md), [TEAM_HOST_REPLACEMENT.md](TEAM_HOST_REPLACEMENT.md) and [TEAM_DISASTER_RECOVERY.md](TEAM_DISASTER_RECOVERY.md). A compromised CA/root needs a fresh workspace/network and independently reapproved device pins; a removed host still retains secrets and data on its disk.

Back up the workspace's data ([TEAM_STORAGE.md](TEAM_STORAGE.md#backups): replication is not a backup; a file-based
workspace's data is `<data dir>/team.db`) **and** `<data dir>/pki/` together, with separately protected recovery access
to both OS wrapping keys or the external passphrase. Explicit legacy file mode also needs `<data dir>/secrets/sealing.key`.
Without `pki/` the workspace cannot sign for itself again and every device must be re-enrolled into a new network.
Without secure-store access (or the passphrase) `pki/` cannot be opened; `serve` then refuses to start, and says so, rather than
running without its network.

## Not built yet

- Rotating the **database's credentials**, and so evicting a host for cause without a new cluster ([TEAM_STORAGE.md](TEAM_STORAGE.md)).
- **Rotating** the network authority in place.
- A member device's own **runner** joining through the individual product (the protocol and the client package are shared and
  ready; the individual product does not use them yet).
- Linux Secret Service integration and Windows. macOS uses Keychain; Linux requires an external protected passphrase file.

## Synchronization and privileged networking

The synchronization with a member's own Individual (`internal/team/connector`) reuses enrolled device authentication and the existing workspace-only host client; it does not supervise a network node, and the Workspace Host and its network node never receive the Individual access grant. Keep TUN/root-required networking separate from it. See [INTEGRATION.md](INTEGRATION.md).
