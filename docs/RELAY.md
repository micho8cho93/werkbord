# Relay protocol (phone ↔ runner)

The relay lets a phone reach the controller on your computer without Tailscale. It is a small Cloudflare Worker
(`relay/worker`) that forwards bytes between **one runner** and its **phones**. Everything that matters is encrypted
end to end between the phone and the runner; the relay sees ciphertext, holds no key and stores one hash.

Pieces: the protocol (this file), the Worker (`relay/worker`), the runner client (`internal/relay/client`), the
end-to-end crypto (`internal/relay/e2e`, and `relay/e2e/e2e.mjs` for the browser), and shared conformance tests.

## Free plan first

This is built and tested for the **Workers Free** plan; moving to Workers Paid later changes nothing in the code.

- The Durable Object class is declared with `new_sqlite_classes`. The free plan only offers SQLite-backed objects.
- One object per runner (`idFromName(roomId)`), using the **WebSocket Hibernation API**: an idle room is evicted from memory
  and accrues no duration. The keep-alive `ping` is answered by `setWebSocketAutoResponse`, which neither wakes the object
  nor costs duration.
- Free limits that matter (checked October 2026; Cloudflare may change them), and what the design does about each:

| Free limit | Design |
| --- | --- |
| 100,000 Durable Object requests/day, shared by all rooms; incoming WebSocket messages count 20:1 | one WebSocket per runner; no polling; no per-message acknowledgements; a room costs about one request per connection plus 1/20 per message |
| 100,000 Worker requests/day | each connection upgrade is one request; the client's backoff is jittered, 1 s up to 5 min |
| 13,000 GB-s/day duration (128 MB per object) | hibernation + auto-response ping; the Worker does no crypto beyond one SHA-256 |
| 10 ms CPU per Worker invocation | forwarding only; no parsing beyond a 9-byte header |
| 100,000 SQLite rows written/day | the room writes one row, once, when it is claimed |
| Past a limit, operations fail until 00:00 UTC | the client treats 429 and 5xx as "relay is full": it waits at least 30 s and backs off to 5 min, and reconnects on its own. The phone sees "runner offline" until then |

A rough budget: a runner and one phone that stay connected and exchange 100 messages an hour is a few thousand requests a
day. Several runners, or a chatty stream, would reach the 100,000 requests/day cap; that is the signal to move to Workers
Paid ($5/month), not to change the code.

## Rooms and keys

A **room** is one runner: `roomId` is 16 random bytes in hex (32 characters). Three secrets are involved, none of which the
relay can use to read traffic:

| | Held by | Purpose |
| --- | --- | --- |
| `joinKey` (32 random bytes, base64url) | runner and its phones | admission to the relay. The relay stores `SHA-256(joinKey)`, set by the first runner that connects (trust on first use). It keeps strangers from using your room; it protects no data |
| runner key (X25519) | runner private, phone public | the phone verifies it is talking to *your* runner |
| `secret` (32 random bytes) | runner and one phone | the runner verifies it is talking to *that* phone. Single-use for enrollment: spent when the phone first proves it |

The QR code carries a **pairing** in the URL fragment (never sent to any server):
`wbr1.` + base64url(JSON `{v:1,u:relayURL,r:roomId,j:joinKey,k:runnerPublicKey,s:secret}`).

## Relay transport

WebSocket, **binary** messages only (plus the text `ping`/`pong`). `GET /v1/room/<roomId>?role=runner|phone`, offering the
subprotocols `werkbord.relay.v1` and `wb-auth.<joinKey>` (a browser cannot set headers, and a query string would be logged).

| Result | When |
| --- | --- |
| `101` | accepted |
| `400` | role is neither `runner` nor `phone` |
| `401` | missing/malformed key; wrong key; a phone for a room no runner has claimed |
| `404` | path is not `/v1/health` or a well-formed room |
| `426` | not a WebSocket request |
| `429` | a ninth phone |
| `503` | phone, but the runner is offline |

`GET /v1/health` → `{"ok":true,"protocol":1,"maxFrame":65536}`.

**Phone connection**: raw payloads both ways, nothing added. **Runner connection**: every message has a 9-byte header, the
type and an 8-byte phone id the relay assigned:

| Type | Direction | Frame |
| --- | --- | --- |
| `0x01` data | both | `01 ‖ peer ‖ payload` |
| `0x02` event | relay → runner | `02 ‖ peer ‖ 01` phone connected, or `02` phone disconnected |
| `0x03` kick | runner → relay | `03 ‖ peer` |

Rules: one runner connection per room (a new one closes the old with `4001` and closes every phone with `4410`); when the
runner goes, phones are closed `4410`; a kicked phone is closed `4403`; a phone whose runner is gone is closed `4404`; at most
8 phones; frames are at most 64 KiB (`1009` otherwise); a socket sending more than 50 messages a second sustained (burst 200) is
closed `4429`. Text messages other than `ping` are ignored. `ping` is answered `pong`; send one every 45 s, since quiet
sockets are dropped after about 100 s.

## End-to-end encryption

All numbers big-endian. X25519, HKDF-SHA-256, AES-256-GCM. `relay/conformance/e2e-vectors.json` pins every byte.

**Hello** (phone → runner, 42 bytes): `01 ‖ 01 (version) ‖ secretId ‖ Pe`, where `secretId = SHA-256("werkbord-relay-psk-id" ‖ secret)[:8]`
and `Pe` is the phone's fresh ephemeral public key.

**Accept** (runner → phone): `02 ‖ Re ‖ seal(counter 0, "wb-ok")` with `Re` the runner's fresh ephemeral key.

**Keys**: `h = SHA-256("werkbord-relay-e2e-v1" ‖ hello ‖ Re ‖ Rs)` (`Rs` the runner's static public key),
`ikm = X25519(Pe,Rs) ‖ X25519(Pe,Re) ‖ secret`, `okm = HKDF-SHA-256(ikm, salt = h, info = "werkbord-relay-keys", 64 bytes)`.
The first 32 bytes encrypt phone → runner, the last 32 runner → phone.

**Data** (both ways): `03 ‖ counter(8) ‖ AES-GCM(plaintext)`; nonce = 4 zero bytes ‖ counter; AAD = the first 9 bytes. A counter
must be greater than the last accepted one. The runner's accept used counter 0, so its data starts at 1; the phone's starts
at 0. At most 61,440 bytes of plaintext per frame.

Properties: the phone knows it reached the holder of the runner key (a different runner cannot produce a valid accept); the
runner knows it reached a holder of the secret; a recorded session stays closed if either long-term secret later leaks
(fresh ephemerals on both sides); the relay can drop, delay or replay-at-most-once but not read or alter. A session lasts as long as the
relay connection of that phone; any disconnect is a new hello.

**Not in this step**, by design: how the secret becomes long-lived after enrollment and how revoking a phone rotates it (the
phone identity layer, `internal/phoneaccess`, owns that); resuming a session without a new hello; messages larger than one
frame. Revoking a phone today means `Kick` and deleting its secret from the store; the `joinKey` is shared by the runner's
phones, so a revoked phone can still open a relay connection and be refused at the handshake. That costs quota, not data.
Rotating the `joinKey` is a later change.

## Deploying the relay (your action, once)

Needs a Cloudflare account; the free plan is enough. From `relay/worker`:

```
npm install
npx wrangler login        # opens a browser
npx wrangler deploy       # prints https://werkbord-relay.<your-subdomain>.workers.dev
```

Check it: `curl https://werkbord-relay.<your-subdomain>.workers.dev/v1/health`. Then run the conformance suite against the
deployed Worker (it creates throwaway rooms and costs a few hundred requests):

```
WERKBORD_RELAY_TEST_URL=https://werkbord-relay.<your-subdomain>.workers.dev go test ./internal/relay/... -run Cloudflare -v
```

Locally, with no account: `npm run dev` in `relay/worker` serves the same Worker on `http://localhost:8787`.

When you upgrade to Workers Paid nothing changes. Worth doing then: a billing alert in the Cloudflare dashboard.

## Tests

- `go test ./internal/relay/...`: the e2e crypto and vectors, the conformance suite against the in-process relay, the runner
  client end to end (handshake, encrypted traffic, single-use secret, kick, retry through 503s, reconnect, rejected key).
- `node --test relay/e2e/e2e.test.mjs`: the browser crypto against the same vectors.
- `WERKBORD_RELAY_TEST_URL=… go test ./internal/relay/... -run Cloudflare`: the same suites against a real Worker.
