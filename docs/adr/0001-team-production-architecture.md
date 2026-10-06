# ADR 0001: The production architecture of Werkbord Team

- **Status:** accepted. Phase 1 (the foundation) and phase 2 (the customer-owned network: [ADR 0002](0002-customer-owned-network.md)) are implemented; rqlite and the runner message path are not.
- **Applies to:** Werkbord Team. The individual product shares the packages named below and is otherwise unchanged.
- **Read with:** [PRODUCTS.md](../PRODUCTS.md), [TEAM.md](../TEAM.md), [TEAM_SECURITY.md](../TEAM_SECURITY.md).

## Context

Team today is one process with one SQLite file, reached over plain HTTP on a network the operator provides. That is
enough to coordinate people on one machine or one LAN. It is not a production design for a distributed team: the data has
one copy, the network is someone else's problem, and "who is this device" has no answer beyond a member's bearer token.

The constraints that shape the answer are Werkbord's, not technical fashion:

1. A Team workspace **coordinates; it never executes** (PRODUCTS.md). That does not change.
2. The customer's projects, tickets, code, prompts, credentials and runtime data must not travel through anything Werkbord
   operates, and Werkbord must not be required to be *up* for a customer's team to work.
3. No third-party account may be required (so not Tailscale's, not a cloud provider's).

## Decision

### 1. Zero vendor runtime infrastructure

Werkbord operates **no** relay, rendezvous server, workspace server, runner registry, customer database, control plane or
licensing service that a customer's team needs in order to work. What the vendor publishes is **signed software** (releases
and updates) and **signed licenses**. Everything a running workspace needs is run by the customer.

### 2. A private network the customer owns: Nebula

Devices reach one another over a [Nebula](https://github.com/slackhq/nebula) overlay network that the customer's
workspace creates and administers: its own certificate authority, its own addressing, its own policy. No Werkbord or
third-party coordination service is involved. Nebula's own pieces are customer-run:

- **Lighthouses** (discovery) and **relays** (traffic for pairs that cannot connect directly) run on customer-owned
  **Connectivity Hosts**.
- Devices behind NAT connect directly when they can and through a Connectivity Host's relay when they cannot.

**The unavoidable limitation.** Reliable connectivity between arbitrary machines on the Internet, with no vendor and no
third-party relay, needs at least one **publicly reachable machine that the customer owns**. Two is better, so that one
can be down. A workspace whose members are all on one LAN does not need one; a distributed team does. Werkbord will not
hide this by quietly relaying through anything of its own, because that would break constraint 2.

### 3. Replicated workspace state: rqlite

The workspace's data lives in [rqlite](https://rqlite.io) (SQLite replicated with Raft) across **Workspace Hosts**. Three
Workspace Hosts are the recommendation: a Raft cluster needs a majority, so three tolerate the loss of one and two tolerate
nothing. One host remains a supported, honest "single node" mode for small teams and for evaluation, with no fault tolerance.
Team keeps SQL and its migrations; what changes is that more than one machine holds the data.

### 4. Roles are separate: people versus devices

| Concept | What it is | Where it is decided |
| --- | --- | --- |
| **Owner** | One person per workspace: ownership authority and the licence | `RoleOwner`, `PermOwnership` |
| **Admin** | A person who administers members, projects and devices; no ownership authority | `RoleAdmin`, permission table |
| **Member** | A person who takes part | `RoleMember` |
| **Workspace Host** | A *device* that holds a replica of the workspace | `Capability` on a device |
| **Connectivity Host** | A *device* that runs a lighthouse/relay for the workspace | `Capability` on a device |
| **Runner** | A *device* that executes its owner's work | `Capability` on a device |

They are independent on purpose. An Admin is not necessarily a host and a host is not necessarily owned by an Admin: a
member's always-on machine can be a Workspace Host without that member being able to do anything more than before, and an
Admin need own no infrastructure. People get permissions from a table (`Role.Can`); devices get capabilities granted to
*the device*, by someone who may manage devices. Nothing derives one from the other.

### 5. Two identities, two layers of authorization

A device has a **network identity** (its Nebula certificate) and a separate **application identity** (an Ed25519 key,
`internal/deviceid`). They are different keys and never substituted for each other.

- The network decides **which machines may talk** to which.
- The application decides **who is asking and whether it is allowed**: every security-sensitive cross-device request is a
  signed **envelope** (`internal/envelope`) from a registered, unrevoked device of a named user in a named workspace, for a
  named target device, with an expiry, a nonce and a signed payload hash, checked and replay-protected by whoever acts on
  it. Being on the network is never enough.

Revoking a device in the registry revokes its application identity at once, and the network's certificate can then be
dropped independently.

### 6. Team never executes developer work

Team may *route* authenticated messages between devices. It does not start agent, Git, shell or model-provider processes,
holds no member's credentials, and does not reach into anyone's machine; the **device that receives** an envelope decides,
under its own policy, whether to act, and a runner is the individual product's. The protocol has **semantic actions**
(`OpenTicketOnRunner`, `StartApprovedRun`, `CancelRun`, `RespondToAgentQuestion`, `FetchRunnerStatus`), never "run this
string": a payload names things by ID and has no field for a command, path or environment, and a test keeps it so.

A Team deployment will need *infrastructure* supervision (starting its own network node and database). That is not
developer execution, and the architecture tests now distinguish them (`internal/archtest/infra_test.go`): the rule against
starting processes is unchanged, with one narrow named exception under `internal/team/infra/`, empty today, in which a
package may start only programs it is granted by name, from constants, never a shell, Git, an agent or a runtime, and may
not import anything that handles a member's request.

### 7. The transport is an interface

`internal/transport` is the contract (start/stop, local node, dial/listen, peers and their reachability, connection
metadata) and names no network. The embedded Tailscale node (`internal/netprivate`) is adapted to it and keeps working;
Nebula will be a second implementation; a future one (for example a mature Tailcat implementation) would be a third. One
conformance suite (`transport/transporttest`) runs against each, and an architecture test keeps vendor names out of code
written against the contract.

### 8. Licensing is offline

A licence is a document signed by the vendor's key, verified by the product against a public key it ships with. It needs no
call home, so an expired internet connection never stops a team. (Not built yet; this fixes the shape.)

## What phase 1 built

| | |
| --- | --- |
| `internal/transport` (+ `memtransport`, `transporttest`) | the contract, an in-memory implementation, the conformance suite |
| `internal/netprivate` | the Tailscale node, adapted to the contract; unchanged otherwise |
| `internal/deviceid`, `internal/deviceid/localidentity` | device ID and public key and checks (Team links this); private-key custody and file persistence (Team never links it) |
| `internal/envelope` | the signed format, actions and typed payloads, signing, verification, expiry, replay |
| `internal/team/domain`, `store`, `service` | the Admin role; the device registry (non-secret only); `store.Store`/`store.Tx` as interfaces |
| `internal/archtest` | the infrastructure/developer-execution line; the shared packages stay neutral; the private key stays off Team; no vendor names above the contract |

Device identity is stored with restrictive file permissions behind a `Store` interface; an OS keychain (macOS Keychain,
Windows DPAPI, the Secret Service) is a second implementation, not yet written.

## Consequences and open questions

- **rqlite and `Store.Update`.** The service layer runs a function inside one transaction with interleaved reads and
  writes. rqlite executes a transaction as a single request of statements, not an interactive transaction. Putting rqlite
  under `store.Store` unchanged is therefore not free: either the `Tx` methods that guard (claim a ticket, use an invite)
  are already atomic statements, as they were written to be, and the rest of a use case is made safe by optimistic checks
  with a retry (which is why `Store`'s contract says a function may run more than once), or those use cases become single
  commands. Phase 2 must settle this with tests before any data moves. The interface removed the coupling to `database/sql`;
  it did not make this question go away.
- **Who holds the Nebula CA key**: decided in [ADR 0002](0002-customer-owned-network.md): Workspace Hosts only, sealed. Rotating it
  in place is not built.
- **Registering a host**: decided in ADR 0002: a signed invitation and the enrollment protocol (`internal/enrollment`).
- **Clock skew** matters to envelopes; the tolerance is 30 seconds, and hosts need a sane clock.
- **The Mac app and the individual product** gain no Team behaviour from this work.
