# ADR 0002: The customer-owned private network

- **Status:** accepted and implemented in Team 2.5.0 (phase 2 of the architecture in [ADR 0001](0001-team-production-architecture.md)). Replicating the workspace's *data* across hosts is not part of it.
- **Applies to:** Werkbord Team. The individual product keeps its Tailscale transport and gains no behaviour; it shares the new `internal/enrollment` package, which it does not yet use.
- **Read with:** [TEAM_NETWORK.md](../TEAM_NETWORK.md) (how it works and how to run it), [TEAM_SECURITY.md](../TEAM_SECURITY.md).

## Context

ADR 0001 decided that a Team workspace's devices reach one another over a private network the customer owns, with no
Werkbord-operated relay, rendezvous server, control plane, discovery server, network registry or customer key service,
and left open who holds the network's certificate authority, how a device is enrolled, and what is shipped. This ADR
answers those, from the upstream project's documentation and source, checked on 2026-10-06.

## Decision

### 1. Ship Nebula; do not reimplement it

[Nebula](https://github.com/slackhq/nebula) is the network. It is MIT-licensed, maintained, runs on every platform Team's
installer supports, and does what the design needs: a certificate authority the customer holds, **discovery hosts**
("lighthouses") that more than one machine may be, **relays**, and a default-deny firewall written in groups carried in each
certificate. Re-implementing any of that would put security-critical network code in Werkbord's hands for no benefit.

- **Pinned.** One release, v1.11.2 (2026-09-22), with the SHA-256 of its archive and of the program inside it for each
  supported platform, in `internal/team/infra/nebula/manifest.go`. The program is never fetched when Team runs: a build script
  (`scripts/fetch-nebula.sh`) gets it, refuses anything that does not match the pin, and the release archive carries it.
  A test checks the pins against the checksum file the release itself published, kept in `third_party/nebula/`.
- **Licence material.** `third_party/nebula/LICENSE` (MIT), the licences of the 33 modules statically linked into the program
  (generated from its module graph by `scripts/gen-nebula-notices.sh`) and Go's, shipped in every archive and installed beside
  the program. Nebula has no NOTICE file.
- **What upstream does not give.** It publishes no signed attestations, so the pin rests on checksums taken from the release;
  its relay support is documented as experimental; there is no Windows build Team can ship (Wintun's licence); and creating a
  network interface needs privileges (a Connectivity Host that only relays and finds runs without an interface and needs none).
  Each is written down in TEAM_NETWORK.md rather than worked around.
- **Library use.** The `cert` package is imported, in-process, to make and check certificates, so that no `nebula-cert`
  program is run and no key is ever passed as an argument. It adds `github.com/slackhq/nebula` and two small dependencies (`protobuf`, `bigmod`) to
  Team's build, none to the individual product's (`teamOnlyModules` checks). Nebula's userspace mode is used **in tests only**,
  to run the generated firewall policy against Nebula's real firewall without privileges.

### 2. A supervisor that can start one thing

`internal/team/infra/nebula` has the one grant in `teamInfraExec` (rule 9). Its only process-starting entry is
`StartNebula(NebulaConfig)`. It verifies the executable's hash against the pin, runs a private copy from a directory it owns
(checking owner and mode), passes `-config <file>` where the file is rendered from a typed description by
`internal/team/infra/overlay`, runs it with an empty environment, and watches it. There is no function that takes a command,
an argument list, an environment or a path to run; rule 14 reads the package and fails if its exported API, its field names
or its one `exec.CommandContext` call change shape. The rule's own tests run it on packages that break it.

### 3. Who holds the authority: Workspace Hosts, sealed, and only them

The authority's private key (and the workspace's own key) are held by **Workspace Hosts only**, sealed on disk
(AES-256-GCM under a key kept apart from the sealed files, or Argon2id from a passphrase), never in the database, an API
response, a log or a backup of the database. Member devices never hold either. Making another host a Workspace Host seals
exactly that material to the new host's sealing key with HPKE (RFC 9180, from Go's standard library) bound to the workspace
and device, stores only the ciphertext, and has the receiving host refuse it unless it matches what it enrolled with. A
Workspace Host is therefore a **high-trust machine**, and the documentation says so wherever it recommends one.

**Rejected:** a CA key on every member device (any device could mint certificates); an offline CA with certificates issued by
hand (fails the "a team can enroll its own people" goal); splitting the key with threshold signatures (invented machinery for a
problem a small number of well-protected hosts solves). **Not built:** rotating the authority in place, and recovering from
a compromised host other than by moving to a new network.

### 4. Two identities, three families of keys

The workspace key (a Werkbord identity: what devices pin, what signs invitations and bootstrap certificates), the network
authority's key, and each host's own keys (application, network, sealing) are different keys with different jobs, and a device
never hands over a private key. A member's device has its own application key, which Team never links (rule 11); a host's keys
are held by `internal/team/infra/pki`, which only Team's wiring may import (rule 9).

### 5. Invitations are bootstrap artifacts, and enrollment uses standard parts

An invitation is a signed, expiring, single-use statement of where the workspace's own machines are, which workspace key to
trust and a one-time credential, carried as `werkbord://join/…` (a link or a QR code) in `internal/enrollment`, a shared
package with no private key in it. A device pins the workspace key (not a vendor, not a certificate authority of the Web) and
joins over TLS 1.3, sends its credential only to a server that has proved it holds that key, and signs its request with its own
key bound to the TLS session. No cryptographic handshake was invented: TLS 1.3, X.509, Ed25519, HPKE, Argon2id, AES-GCM and
SHA-256, all from the standard library or `golang.org/x/crypto`. The honest limit, documented: an invitation proves it was not
altered, not that it was meant, so devices can insist on a fingerprint learned elsewhere.

### 6. Revocation in two layers that do not depend on each other

The application layer (the registry and the device's API credential) is authoritative and immediate. The network layer (a
blocklist of certificate fingerprints in every host's node configuration, and 30-day certificates) follows. Neither is a
substitute for the other, and a test with the real program shows each works alone.

### 7. A default-deny policy, generated, checked against the real firewall

Policy is generated from the certificate's groups by one function (`overlay.PolicyFor`) that cannot write an allow-all rule.
It is enforced by the receiving node, so it does not depend on the sender behaving. The policy table in TEAM_NETWORK.md is
exactly what is tested, with real Nebula nodes and a control.

### 8. Reachability is reported, not promised

Team classifies every advertised address, checks reachability with the workspace's own machines (never an outside service),
separates a host's check of itself from another device's check, and says "remote access cannot be guaranteed" when no
Connectivity Host has been confirmed reachable from outside. It does not claim to defeat NAT.

### 9. The transport contract is kept

Team's network is a `transport.Transport` (`internal/team/infra/overlaynet`), which passes the shared conformance suite and
refuses to dial outside the workspace's range. The individual product keeps the Tailscale transport. Nothing above the contract
names a network (rule 12 now covers `internal/enrollment` too).

## Consequences

- Team's build grows by Nebula's `cert` package (and `protobuf`, `bigmod`); its release archives for macOS and Linux grow by the
  Nebula binary (roughly 20 to 40 MB compressed, by platform). `go.sum` gains the dependencies of Nebula's userspace mode for tests.
- Rule 7 (the server never reaches out) gained three narrow, named allowances: `os`, `syscall` and `net` in the infrastructure
  tree (to read and write its own files, check who owns them and signal its own node), `os`/`filepath` in the server's wiring
  (the key vault and the passphrase file the configuration names), and `net.Dialer` in `overlaynet` alone (a transport that
  can only dial the workspace's own range). Each is in the rule with its reason.
- The workspace's data is still on one host. A second Workspace Host can hold the authority but cannot yet serve the
  workspace. That is phase 3 (the replicated store), whose open question about rqlite's transactions is unchanged.
- A host's loss for cause means a new network. Rotation is future work.
