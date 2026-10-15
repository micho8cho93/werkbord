# Team phone grant security review

Reviewed 15 October 2026 for the Team phone lane. This is a focused source review and adversarial automated testing;
it is not an independent external assessment.

## Authority added

An authenticated local device user may issue a separate phone credential from Team Settings → This computer → Phone access.
The credential reads the member computer's Team summary, not its device API or the workspace API. The endpoint lives in
`werkbord-team`; it neither links nor contacts the Individual controller. Issuance is disabled unless the company explicitly
configures both its relay and connector on the member computer. Nothing starts listening on that member computer's network.

A grant is an X25519/AES-GCM relay pairing, stored privately in `local/phone-relay.json`. Its opaque client ID includes a
SHA-256 binding to the issuing workspace and member. Each decrypted HTTP request must prove a currently bound secret and
match the current member/workspace, with no leave transition underway. Only GET `/`, `/phone.js`, `/phone.css` and
`/api/summary` are admitted. HEAD, writes, the full console, device controls, execution, arbitrary forwarding and all other
API paths fail closed. Do not expand this list without a new review.

The summary reuses the member device's existing signed Workspace Host reads. Workspace membership, device revocation,
project permissions and licensing remain enforced at the host on those reads. Host credentials are never returned to the
phone. A disconnected computer returns state without cached tickets. Phone rendering uses text nodes; summary links are
not exposed as actionable links to privileged screens. Responses are no-store and have a restrictive CSP.

## Threats checked and findings resolved

- A phone reusing the local daemon credential cannot reach local administration through the tunnel: its handler is separate.
- A member change or workspace slot reuse cannot carry the old grant across. Beginning a leave blocks every new request.
- Revocation removes the encryption secret, cancels open tunnel requests and kicks the connection immediately. The original
  shared access store discarded save errors; Team now uses a checked revocation and reports failed persistence. A retry saves
  the removal even after the secret has left memory. A failed save must be retried before restart; it is never reported as success.
- Pairing and connection state are separate for Individual and Team. Switching uses distinct URL paths, IndexedDB entries,
  handshakes and live connections; it does not replace another lane's pairing. Reset invalidates outstanding connection attempts.
- The company relay reads only the protocol header and ciphertext. It retains SHA-256 join-key hashes, not traffic keys,
  workspace credentials or plaintext. Reopening it preserves room ownership. Wrong keys cannot take an existing room.
- The production Go relay and Worker share the conformance suite. Limits are eight phones per room, 64 KiB per message,
  50 binary messages/second (burst 200), 1024 claimed rooms and bounded writes. Both runner and phone sockets are rate-limited.
  WebSocket origins are unrestricted deliberately: the join key admits connections and the end-to-end grant authorises data.

## Explicit trust and limits

The pairing link is a reusable bearer credential until revoked; it is not a five-minute, single-use Individual enrollment code.
It appears only in a URL fragment, is removed by the connector, and is shown once on the member computer. Treat copies as
phone credentials. At most sixteen grants per workspace slot. Browser storage and the member computer's private file hold
secrets; a compromised endpoint can read that endpoint's grants. No phone names or hardware identity are asserted.

The connector origin executes trusted client code and stores both lanes' pairing secrets when both are paired there. Backend
credentials and routing are separate; same-origin browser scripts are not a security boundary between lanes. Use a connector
origin you trust with both lanes. A malicious connector operator can steal pairings. A malicious relay alone cannot decrypt
traffic, but a holder of a room join key can disrupt availability. Public first-runner room claiming matches the Worker:
1024 unauthorised claims can exhaust capacity. Restrict relay ingress to company use and protect the HTTPS proxy from abuse.
There is no automatic room expiry or join-key rotation; operators manage persisted room claims during decommissioning.

Already delivered data cannot be erased from a phone by revocation. An in-flight read may race a membership change; it has
only the authority of the member credential captured for that read. A dead computer or relay makes phone access unavailable.
The company's HTTPS proxy and static connector hosting are deployment prerequisites, not vendor services.

## Evidence

`TestTeamPhoneGrantIsReadOnlyScopedAndRevokedOnAnOpenConnection` uses the production relay, a real encrypted phone session,
a member-authenticated upstream, forbidden routes, membership substitution, leave, offline state and live revocation.
`TestNoCompanyRelayExplainsBlockedPhoneAccess` checks blocked copy and unauthenticated grant issuance.
`TestCompanyRelayUsesWorkerProtocol` runs the existing protocol conformance suite against the persisted Go relay.
`TestClaimSurvivesRestartAndOnlyHashIsStored` checks room ownership across restart.
Config tests reject partial, insecure and credential-bearing endpoint settings. Existing architecture tests continue to
forbid Team importing execution code; only neutral relay framing, crypto, transport and opaque bindings become shared.
