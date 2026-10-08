# Team production security and reliability gate

Review date: 2026-10-08. Product versions: Team **3.0.0**, individual Werkbord **1.4.0-preview.3**. This is an implementation and evidence report. **Production admission remains blocked** by the deployment, key-custody and dependency decisions below. Passing local tests is not a claim of complete security.

## 1. Architecture implemented

Customers operate Workspace Hosts, Connectivity Hosts, backups and local runners. Team has no vendor runtime registry, CA, relay, licensing endpoint or customer-data service. Individual Werkbord's existing tsnet support remains separate and passes the build/test isolation check with Team removed.

Production CLI creation defaults to the customer's Nebula network and administrator-approved enrollment. The local administration API binds literal loopback; the remote API has a separate Nebula listener. Enrollment uses pinned TLS 1.3, a TLS-exporter-bound device proof, signed single-use invitations and recipient-encrypted provisioning. Nebula uses a separate Ed25519 CA and X25519 node keys. Receiving firewalls default to deny, allow member API access, and reserve database/Raft traffic for Workspace Hosts. Ordinary runners have no inbound TCP rule and poll their mailbox. Nebula's optional SSH debugger is explicitly disabled.

Remote API authentication now requires both the device credential and an Ed25519 request proof binding workspace, user, device, destination, method, path/query, body hash, time and nonce. Mutating nonces survive host/handler restarts in replicated storage. Authentication and authenticated reads require a fresh linearizable fence; current membership, role and credentials are checked again in service transactions and during active long polls. An isolated stale host fails authorization closed.

Rqlite supplies crash-fault Raft replication. Team checks a consistent revision/hash history and maintains a local diagnostic read replica. Three voters tolerate one failure after election; loss of quorum refuses writes and remote authorization. There is no automatic quorum shrink or merge of divergent histories. Uncertain mutation responses are not blindly retried against another API host.

Canonical schema 2 Ed25519 licenses are verified offline and stored with replicated workspace data. They bind product, license/customer IDs, edition, seats, issuance and optional runtime/support dates. Only edition `team` is implemented. Owner/admin seats count as members; additional devices for the same member do not. Every member insertion, including invitation redemption and enrollment finalization, checks seats inside its transaction. Runtime expiry stops normal mutations; authenticated reads, owner renewal and permission-limited containment remain available. Containment can revoke devices, remove members, demote admins and replace existing member credentials, and cannot add seats or privileges.

Mac services use separate Keychain wrapping keys for ordinary device material and workspace root/CA/database authority material. Unsupported OS storage fails closed and requires a protected external passphrase file. The explicit adjacent-file mode is transition/evaluation support. Authority encryption separation does not protect against the same authorized service UID or root.

The runner verifies envelopes using sender keys approved and pinned locally. A Workspace Host cannot substitute a pin. The local bridge has five closed semantic operations: open a held ticket, start a locally approved task, cancel a mapped run, answer an existing question and fetch mapped status. Local start approvals are task-bound and spent durably before dispatch. No arbitrary shell, process, filesystem, HTTP proxy, SSH, PTY or Git command is exposed.

The offline vendor utility is separate source under `cmd/werkbord-team/vendor`, excluded from customer packages. It receives PKCS#8 private key material on stdin and writes a new owner-only license or release signature. License and release keys are distinct. CLI installers authenticate a product/tag-bound Ed25519 checksum manifest using an independently provisioned public PEM before extracting or executing a package. Online generic CI no longer publishes unsigned Team CLI archives. Mac DMGs retain the separate Apple Developer ID/notarization trust path.

## 2. Findings fixed and deviations

| Finding | Result |
| --- | --- |
| Bare or ambiguous bearer headers accepted | Require one actual `Bearer` header in both workspace and local device APIs |
| Remote bearer theft alone authorized actions | Device signatures required in production; remote legacy member tokens refused |
| Cached membership could revive revoked access | Fresh authorization reads and in-transaction/long-poll checks |
| API proofs could be replayed across hosts/restarts | Replicated expiring nonce records; live entries never evicted |
| Licensing existed only in desktop onboarding | CLI/server/daemon enforcement, replicated documents and guarded member insertion |
| License expiry could block incident containment | Narrow revocation/removal/demotion/credential-replacement recovery path |
| Editable macOS rqlite build record trusted executable bytes | Trusted build embeds the exact sidecar hash; verify before even `-version` |
| Downloaded checksums had no independent authenticity | Separate offline Ed25519 release signatures, product/tag binding and independent trust key |
| Host-to-runner incoming TCP was unnecessary | Remove it; delivery uses polling and local signature verification |
| Replay cache tied message ID and nonce as one tuple | Each identifier is independently unique; persisted legacy entries remain honored while live |
| Expired identifier reuse could grow nonce memory | Both halves of the memory cache count toward bounded cleanup/capacity |
| Signed timestamps could overflow duration checks | Compare bounded integer lifetimes before duration conversion |
| Identifier text could alter bridge URL paths/queries | Closed opaque identifier character set and strict payload EOF |
| Corrupt local state could enable unsafe settings | Bounded strict parsing, private regular files, reject symlinks, preserve corrupt state |
| New invitation clock checks broke pending enrollment recovery | Sealed redeemed journal uses signature/shape-checked saved parsing; new invitations still check time |
| Signature tampering test could leave its random input unchanged | Flip a decoded signature byte; the adversarial test passed 256 repetitions |

An agent-driven clean Linux install passed in a fresh Debian 13 ARM64 QEMU/HVF VM, including signed installation/reinstallation, licensed creation/serving and replaced-sidecar rejection. Native temporary-prefix installs and local processes are reported separately below. The prompt's clean Mac, independent three-machine and NAT/relay-only acceptance tests could not be completed in this workspace. Docker's daemon is unavailable; the disposable Linux VM was provisioned directly with QEMU and has been stopped. No privileged TUN interface, persistent OS service or customer installation was created.

No production vendor signing keys or Apple credentials were invented. Development packaging and signature tests use ad-hoc Apple signing or ephemeral fixture issuers. Keychain C bindings compile, and cryptographic separation is tested, but signed root-service Keychain access through boot/lock/update/recovery has not been accepted on a clean Mac. Linux Secret Service and automatic vault rewrapping are not implemented; protected external passphrases and replacement-host migration are documented alternatives.

Schema 0 expiring licenses and explicit adjacent-file vaults remain supported for the defined Team **3.x** transition window; the next major removes that compatibility. Older remote bearer clients must re-enroll/use the current signing client; older hosts refuse newer database schemas. There is no silent second Team networking system.

## 3. Boundaries and blast radius

The complete adversary matrix and key inventory are in [TEAM_SECURITY.md](TEAM_SECURITY.md). In particular:

- An ordinary member/runner gets no lateral TCP access through the Team overlay, even with its own outbound firewall edited. The individual product's separate network is outside this guarantee.
- A stolen device can use its own unlocked credentials and locally trusted semantic permissions until revocation. An Admin able to provision a Workspace Host can escalate to full workspace authority; treat that role accordingly.
- A compromised Workspace Host can disclose/falsify all coordination data, membership, root/CA and database authority. Raft is not Byzantine consensus. It still lacks other runner signing keys and target-local approvals. Host-controlled ticket content can contain malicious agent instructions.
- A Connectivity Host without a Workspace Host role can deny discovery/relay service and observe traffic metadata; it has no workspace CA or database credentials.
- Vendor servers hold no customer runtime records to disclose. A malicious accepted update can subsequently exfiltrate data with the app's privileges. Offline CLI signatures limit website compromise; the Apple CI signing path needs its own custody/release decision.

Architecture tests enforce Team imports, the closed bridge/actions/routes, sidecar launch sites, offline-only runtime configuration, private-key response exclusions, production security wiring and the narrow native Keychain API. These are regression guards, not a formal proof about arbitrary future code.

## 4. Remaining risks

Same-UID/root compromise, unlocked process memory, debugger/core dumps and full disk images can expose secrets. Key clearing is best effort. Authority wrapping keys do not remove that trust assumption. Dedicated protected Workspace Hosts and separately secured recovery material are necessary.

Nebula blocklist propagation is asynchronous; device API revocation uses current quorum state. Clock skew and clock rollback affect offline license/message windows. Restoring old membership, replay or approval state can reopen a live acceptance window; stop remote delivery, expire windows and reapply revocations before reconnecting.

Backups are plaintext SQLite unless the customer encrypts them externally. Integrity/checksum records detect corruption but are not signatures against an attacker replacing both files. Replication is not backup. Losing all authority unlock material requires a new workspace/network.

The CLI update stages sidecars and publishes the executable last; this is not an atomic multi-file update. Interruption can produce an old executable/new sidecar combination that refuses to start. Rerun the exact signed installer with the service stopped. Running supervisors use private verified copies.

There is no complete application DoS defense. High-entropy credentials, bounded requests/replay storage, timeouts and role checks do not substitute for capacity planning or rate limits. Full host authority can falsify legitimate task context; local automatic-start mode expands trust. Default `ask`/`off` policy is appropriate for a hostile-host model.

Offline licenses cannot be instantly revoked by the vendor without adding a vendor runtime dependency. Customers controlling clocks or modifying the program can bypass offline enforcement. Support entitlement expiry does not shut down an installed valid runtime.

## 5. Unsupported failures

No supported Byzantine-consensus/majority-corruption guarantee, automatic quorum shrink, automatic merge of independently writable histories, in-place compromised-root/CA/database-credential rotation, recovery without authority unlock material, Windows Team networking, Linux Secret Service integration, portable Keychain export or automatic vault rewrapping is provided.

A reverse proxy exposing/stripping provenance from the local member API is unsupported. Unknown protocol fields/versions and newer schemas fail closed rather than being silently interpreted. Losing every useful relay prevents relay-dependent peers from connecting; established direct paths need not depend on a lighthouse, but no universal NAT traversal guarantee is claimed.

## 6. Third-party review and scan results

| Component | Pin/license | Evidence |
| --- | --- | --- |
| Nebula | **1.11.2**, MIT | Upstream archive/binary SHA-256 pins; original universal Mac signature retained; notices in `third_party/nebula` |
| Rqlite | **10.5.2**, commit `a73dd2e63acb72080f5eb20881a03bb06a464f89`, MIT | Linux archive/binary pins; fresh Mac source builds; exact build/signature hash embedded in Team; `third_party/rqlite` notices |
| Go | **1.27.1** for local Team/rqlite builds, BSD-style license | Bundled Go notice; upstream Nebula has its own embedded dependency/toolchain versions |
| Wails | **2.16.0**, MIT | Separately versioned Team desktop module; native universal compile and desktop source scan |
| Go module dependencies | `go.mod`/`go.sum` and desktop module pins | Generated Nebula/rqlite dependency notices ship with bundles; no runtime dependency downloads |

Reviewed primary references include the [Nebula releases](https://github.com/slackhq/nebula/releases), [Nebula P256 blocklist advisory](https://github.com/slackhq/nebula/security/advisories/GHSA-69x3-g4r3-p962), [rqlite releases](https://github.com/rqlite/rqlite/releases) and Go's vulnerability database. Nebula 1.11.2 is outside the P256 advisory's affected range, and Team uses Ed25519 CA certificates. This limited upstream review is not a complete audit of either project.

`govulncheck` **1.8.0** found zero reachable advisories in the root source and desktop source, and none in the freshly built native rqlite or pinned Linux ARM64 rqlite binary. It reported the module-only [unmaintained OpenPGP advisory GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932); that package is not imported by these products.

The **shipped Nebula binaries** on Mac ARM64 and Linux ARM64 include `x/crypto` **0.54.0** and reported three optional-SSH symbols affected by [GO-2026-6355](https://pkg.go.dev/vuln/GO-2026-6355), [GO-2026-6354](https://pkg.go.dev/vuln/GO-2026-6354) and [GO-2026-6303](https://pkg.go.dev/vuln/GO-2026-6303). Team explicitly emits `sshd.enabled: false`, exposes no debugger option and tests that setting. Source inspection indicates those listener paths are disabled in Team; this is a configuration-based reachability assessment, not a clean binary scan. Obtain a patched reviewed upstream artifact or record an explicit release-security decision before production. The fat Mac binary had to be thinned for `govulncheck`; the installed artifact was not modified.

## 7. Tests actually performed

Commands below ran on this Apple Silicon Mac, except the installer and runtime checks explicitly identified as running inside the fresh Debian VM. Automated tests and agent-driven shell checks are distinct from human clean-machine acceptance. Logs are local under `/tmp/werkbord-gate-*.log`; browser screenshots are `/tmp/werkbord-gate-browser-screenshots/`. Fixture secrets/data were temporary and are not part of this commit.

| Check | Result and scope |
| --- | --- |
| `make check` | Passed all root Go tests, web unit tests, formatting/shell checks, Go vet, Svelte/ESLint, both desktop logic modules, workflow/notarization fixtures and builds |
| `make verify-isolation` | Passed individual build/vet/test/run/migrate in a copied tree without either Team directory |
| `make test-rqlite` | Passed real supervisor, replicated store and server suites, then service tests on real rqlite; local ports/processes, no independent machines |
| `WERKBORD_TEST_STORE=rqlite` service rerun with embedded build pin | Passed after containment changes, including atomic seat races, expiry/recovery and active-session revocation |
| `make test-nebula` | Passed pinned real process/configuration, handshake, missing-lighthouse, revocation/blocklist and supervisor tests; userspace firewall probes run without TUN |
| Race command below | Passed envelope/enrollment, API proof, licenses, durable device state, host client, narrow bridge, runner link, API, service, store and server packages |
| Six fuzz targets below | Passed 10 seconds each, two workers; approximately 2.6 million generated parser/auth inputs |
| `scripts/test-install-team.sh` | Passed nine installer checks: product/version selection, signatures, wrong tag/key, altered checksum, sidecar notices, refused package and interrupted switch/recovery; format fixtures contain stand-in sidecars |
| `make test-team-signed-release` | Passed actual native Nebula/rqlite bundling, separate vendor-tool license/release signatures, independently keyed temporary-prefix install, licensed default-Nebula create/serve, default admin approval and replaced-sidecar rejection before execution |
| Fresh Debian 13 ARM64 VM script below | Passed independently keyed signed Linux package install and reinstall on a fresh OS, notices/version checks, real rqlite licensed create/serve with protected external passphrase, current authentication, default admin approval and replaced-sidecar rejection; tunnel disabled, no persistent service |
| `node scripts/test-team-desktop-browser.cjs` | Passed actual daemon/API/database create, license import, QR/invitation join, admin approval, sender pinning, signed semantic handoff to an actual disposable individual controller and desktop/mobile navigation; tunnel dial mapped to loopback |
| Universal desktop packaging command below | Passed both architecture builds, exact rqlite hash stamping, app/component ad-hoc codesign checks, version checks and DMG creation; no installation/notarization |
| Dependency scans below | Source/desktop/rqlite results and Nebula findings reported in section 6; no claim of a globally clean dependency scan |

Race command (real clustered storage is tested separately):

```sh
WERKBORD_SKIP_RQLITE=1 go test -race -timeout 20m \
  ./internal/envelope ./internal/enrollment ./internal/team/authproof \
  ./internal/team/license ./internal/team/devicestate ./internal/team/hostclient \
  ./internal/team/localwerkbord ./internal/team/runnerlink ./internal/team/api \
  ./internal/team/service ./internal/team/store ./internal/team/server
```

The final bounded-cache change was additionally verified with `go test -race -timeout 3m ./internal/envelope`. Nebula's upstream userspace lab has a `!race` build tag; its transport is not claimed race-checked.

The corrected forgery check was run with `WERKBORD_SKIP_RQLITE=1 go test ./internal/team/service -run '^TestAWorkspaceHostCannotForgeAPersonsRequest$' -count 256 -timeout 3m` before the final successful `make check`.

Each fuzz run used `go test <package> -run '^$' -fuzz <target> -fuzztime 10s -parallel 2`:

| Package | Target |
| --- | --- |
| `./internal/enrollment` | `FuzzInvitationParser` |
| `./internal/envelope` | `FuzzSignedEnvelopeAndPayload` |
| `./internal/team/license` | `FuzzLicenseParser` |
| `./internal/team/authproof` | `FuzzProofParser` |
| `./internal/team/api` | `FuzzRemoteBearerCannotBecomeDeviceAuthentication` |
| `./internal/team/server` | `FuzzNodeConfigParser` |

Specific executed scenarios include seeded random host kills/restarts (seed 42/1701, four rounds), leader/follower loss, isolated leader/minority refusal, two-of-three loss/recovery, complete outage, stale host/snapshot catch-up, replica-to-voter promotion/removal, uncertain write answers, verified backup/restore/refusal, one-file migration/rollback, interrupted migration rollback and unknown newer schema refusal. Enrollment tests cover expiry/tampering/reuse, approval, restart from a sealed pending journal, wrong workspace/CA, certificate revocation, and seat exhaustion. Active-session tests cover device/member revocation and Admin demotion. The userspace lab stops a lighthouse/relay and confirms an already established **direct** peer path survives; it does not force a relay-only NAT path.

Agent-driven packaging and scan commands:

```sh
ARCH=universal GOFLAGS=-p=2 scripts/build-team-desktop.sh --package /tmp/werkbord-gate-package
scripts/check-team-desktop.sh --adhoc '/tmp/werkbord-gate-package/Werkbord Team.app'
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -show verbose ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -mode binary .cache/rqlite/10.5.2/darwin_arm64/rqlited
lipo .cache/nebula/1.11.2/darwin_arm64/nebula -thin arm64 -output /tmp/werkbord-gate-nebula-arm64
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -mode binary /tmp/werkbord-gate-nebula-arm64
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -mode binary .cache/nebula/1.11.2/linux_arm64/nebula
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -mode binary .cache/rqlite/10.5.2/linux_arm64/rqlited
# From cmd/werkbord-team/desktop:
CGO_ENABLED=1 CGO_LDFLAGS='-framework UniformTypeIdentifiers' \
  go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -tags desktop,production ./...
```

The clean Linux test was executed with `/tmp/werkbord-gate-linux.ZP1FMM2F/run-clean-linux.sh`, with output in `/tmp/werkbord-gate-clean-linux.log`. It downloaded the official [Debian cloud image](https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-arm64.qcow2) over HTTPS and verified its published SHA-512 (`d8470b8c6c38fead046c794b5800a5a7b96672d5bcf543cc230ceb0c4b8ace05ed341a0c8928045422243fde26b2f1f2f58e99244c65709ffda2e3d4b674dd5a`). The guest was Debian 13 ARM64 with OpenSSL 3.5.7. QEMU used HVF, no host-directory mount, and an SSH forward bound only to host loopback. Ephemeral SSH/license/release private keys and the VM disks were removed on exit. This verifies a fresh guest OS; it does not verify real network interfaces, customer key custody or a boot service.

No human-operated clean install, clean Mac install, real boot/unlock service test, Intel runtime test, production-key artifact, independent-host failover or forced relay-only NAT test was performed. The separate native Mac installer fixture is a fresh prefix on the current machine, not a fresh OS.

## 8. Production blockers and required acceptance

1. Provision separate production license/release keys with offline custody, independent public-key distribution, backup and rotation procedures. Verify both signatures using the actual release trust anchors. Publish no fixture issuer.
2. Build the exact reviewed production artifacts; require Developer ID/notarization for Mac. Decide how Apple CI signing credentials are protected against release-infrastructure compromise, or move that signing path offline. The CLI offline signature does not currently authenticate the Mac DMG.
3. Resolve the shipped Nebula optional-SSH advisory findings through a patched pinned artifact or an explicit reviewed security exception backed by the disabled-listener tests. Scan every shipping OS/architecture artifact and retain the evidence.
4. On clean Mac and Linux machines, verify downloaded signatures/pins/notices, install/create/join, upgrade and interrupted-upgrade recovery. For the root Mac service test boot before login, locked/unlocked Keychain, app identity changes on update, restart, uninstall safety and authority recovery. No adjacent-key fallback is acceptable.
5. Use three independently failing Workspace Hosts and at least two usable Connectivity Hosts. Kill a leader/follower, partition 1/2, lose quorum, restore it and bring a stale host back. Check successful acknowledged writes and hashes from every survivor. Exercise a forced relay-only NAT path, relay/lighthouse loss and alternative-path recovery; test application authorization/lateral probes on actual TUN interfaces.
6. Restore a customer-encrypted backup onto a replacement host with separate authority unlock material. Demonstrate stopped/fenced old hosts, correct fingerprints, expired replay windows, reapplied revocations and the expected data-loss boundary. Do not count a local scratch restore as this acceptance.

No commit or tag from this gate is permission to publish or deploy. See [TEAM_INSTALL.md](TEAM_INSTALL.md), [TEAM_BACKUP_RECOVERY.md](TEAM_BACKUP_RECOVERY.md), [TEAM_HOST_REPLACEMENT.md](TEAM_HOST_REPLACEMENT.md) and [TEAM_DISASTER_RECOVERY.md](TEAM_DISASTER_RECOVERY.md) for operator procedures.
