# Relay protocol (phone ↔ runner)

The relay lets a phone reach the controller on your computer without any VPN or app on the phone. It is a small Cloudflare Worker
(`relay/worker`) that forwards bytes between **one runner** and its **phones**. Everything that matters is encrypted
end to end between the phone and the runner; the relay sees ciphertext, holds no key and stores one hash.

Pieces: the protocol (this file), the Worker (`relay/worker`), the runner client (`internal/relay/client`), the
end-to-end crypto (`internal/relay/e2e`, and `relay/e2e/e2e.mjs` for the browser), shared conformance tests, the HTTP tunnel
and the runner's keys (`internal/relay/tunnel`, `internal/relay/access`), and the **connector**, the static page a phone opens
(`relay/connector`, [below](#the-connector-scan-and-go)).

## Nothing to set up: the relay that ships

A Werkbord install connects to a relay and connector built into the program (`config.DefaultRelayURL` and `DefaultConnectorURL`),
so Settings → Phones → Add a phone works at once, on the Workers Free plan described below, with nothing to deploy or configure.
The relay only forwards ciphertext. Your own replaces it: set `relay.url` and `relay.connectorUrl` in `config.json` (or
`WERKBORD_RELAY_URL` and `WERKBORD_CONNECTOR_URL`), both or neither. `relay.disabled` in `config.json`, or `WERKBORD_RELAY=off`,
turns the relay off, built-in addresses included. Test binaries never get the built-in pair, so a unit test cannot connect to it.
A code made while the computer is still reaching the relay works as soon as it is connected.

Moving to Workers Paid later changes none of this. It is the answer to the daily request cap when many people share one relay:
every install that uses the built-in relay spends the same Cloudflare account's free quota.

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

`GET /v1/room/<roomId>/status` with `Authorization: Bearer <joinKey>` → `{"runner":true|false,"phones":n}`, or `401` for a wrong or
missing key or a room no runner has claimed. It exists for browsers: a failed WebSocket upgrade tells a page nothing, not even
the status, so after a failure the connector asks this plain request *why*. Both this and `/v1/health` answer from any origin
(`Access-Control-Allow-Origin: *`; the join key is the gate, no cookie is involved) and `OPTIONS` with `204`. It is only asked
after a failure, so it costs nothing while things work.

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

**Not done**, by design: resuming a session without a new hello; messages larger than one frame. The `joinKey` is shared by the
runner's phones, so a revoked phone can still open a relay connection and be refused at the handshake. That costs quota, not
data. Rotating the `joinKey` is a later change.

## The connector: scan and go

A QR code is a plain link, `https://<your-connector>/connect/#wbr1.…`, so the phone's own camera opens it in its default
browser: **no app of any kind**. The pairing sits after the `#`, which a browser never sends to a server; the
page removes it from the address bar as soon as it has read it. The relay is the only way a phone comes in (`"via": "relay"` in
the answer of `POST /api/phones/codes`); Tailscale is used only by other computers joining as runners.

```
phone browser ── https ──▶ connector (Cloudflare Pages: static files, no code of yours runs there)
     │  /connect/  stores the pairing, installs the service worker, checks the whole way, opens the app
     ▼
service worker (/sw.js) ── wss, end-to-end encrypted ──▶ relay (Worker) ──▶ your computer's controller
     ▲  answers every request the app makes of its own origin by asking your computer; caches only /assets/*
```

**The app is served by your computer**, through the relay. Pages holds a dozen small static files and no copy of the app, so
the phone always runs the version your computer runs, and a Pages deploy is never needed when Werkbord updates. The phone's
service worker (`relay/connector/src/sw.js`) turns each fetch into a request on the tunnel (`internal/relay/tunnel`), which the
runner answers with the controller's phone-side handler. A phone therefore has exactly the access a phone has anywhere
else (`internal/api/phone.go`: a fixed, reviewed list), and the tunnel is not a second way in: it adds nothing the controller
does not already check, and passes only a few request headers (no `Origin`, `Cookie` or `Host`).

**Pairing and coming back.** The QR code carries the pairing and the enrollment code of the phone. `/connect/` keeps the pairing in
IndexedDB, connects, then opens `/#enroll=<code>`: the app spends the code as it does for any phone, gets the phone's own token,
and the runner binds the pairing's secret to that phone. From then on the phone reconnects by itself, with a new end-to-end hello
each time, for as long as the phone is not revoked. Before enrollment a secret works for five minutes, like the code. Revoking a
phone (Settings → Phones) deletes its secret and removes its connection from the relay at once. `relay.json` in the data
directory holds the runner's key, the room and these secrets (`0600`). Deleting it signs every phone out.

**Failures a person sees.** A page cannot see why a WebSocket failed, so the worker asks the relay (`/status`) and the screens say
what is true:

| What happened | The phone says |
| --- | --- |
| The relay answers and no runner is connected (the computer is asleep, off, or off the network) | **Your computer is offline.** It tries again by itself every few seconds while the page is on screen |
| The relay cannot be reached, answers 429 or 5xx (at its daily limit, or down), or a ninth phone is refused | **Temporarily unavailable.** Same retry |
| The computer closes the connection because it does not know this phone (revoked, or a code that ran out) | **This phone was signed out** / the code ran out: scan a new code |
| The pairing does not match the runner (`relay.json` was deleted) | **This link is out of date**: scan a new code |

While the app is open, its own requests get `503` with `{"error":{"code":"relay_offline"|"relay_unavailable",…}}`, which the app
shows as it shows any error. The worker backs off between the app's background retries (2 s doubling to 1 min) so a busy app
does not spend the relay's quota; opening or reloading the page always asks fresh.

**Free-plan cost.** A page load is a dozen requests on one WebSocket (1/20 of a Durable Object request each); `/assets/*` is
content-hashed and cached on the phone, so repeat loads are cheaper still. The runner's responses are outgoing messages, which
the free plan does not count.

### Tunnel format

Inside an end-to-end data frame (at most 60 KiB of plaintext): `type (1) ‖ request id (4, big-endian, chosen by the phone) ‖
payload`. A request is `ReqHead` (JSON `{m, p, h}`: method, path with query, a few lower-cased headers), any number of `ReqBody`
(32 KiB chunks), then `ReqEnd`; a response is `ResHead` (JSON `{s, h}`), `ResBody` chunks, `ResEnd`. `Cancel` (phone) and `Err`
(runner, JSON `{c, m}`) end a request early. Limits: 24 requests open at once per phone, a 16 KiB head, a 16 MiB request body.
A response is streamed as the handler writes it, which is what lets the live event stream work. Types and limits are in
`internal/relay/tunnel/tunnel.go`.

### Setting it up (your actions, once)

1. Deploy the relay (above) and note its address.
2. Build and deploy the connector to Cloudflare Pages (free; a `*.pages.dev` address is fine):
   ```
   cd relay/connector
   RELAY_URL=https://werkbord-relay.<your-subdomain>.workers.dev npm run build   # RELAY_URL only tightens the Content-Security-Policy
   npx wrangler pages deploy dist --project-name werkbord-connect                  # prints https://werkbord-connect.pages.dev
   ```
3. Tell the controller both addresses, in `config.json` or the environment, then restart it:
   ```json
   { "relay": { "url": "https://werkbord-relay.<you>.workers.dev", "connectorUrl": "https://werkbord-connect.<you>.pages.dev" } }
   ```
   `WERKBORD_RELAY_URL` and `WERKBORD_CONNECTOR_URL` do the same. Both or neither: one alone is refused. Plain `http` is accepted only for
   `localhost`, for trying it on one computer.
4. Settings → Phones → Add a phone. `GET /api/relay` (and the app) say whether the computer is connected.

Everything runs on your Cloudflare account and your computer. Nothing is sent to Werkbord's authors, and the connector holds no
secret: it is the same static files for everyone, and a pairing only ever lives in a phone and in `relay.json`.

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
  client end to end (handshake, encrypted traffic, single-use secret, kick, retry through 503s, reconnect, rejected key), the
  runner's key store, and `internal/relay/tunnel`, which runs the connector's own `link.js` under Node against the Go runner
  (request and response bodies of 200–300 KiB, a stream that arrives as it is written, concurrent requests, cancellation,
  what reaches the controller, and why a connection fails: removed, mismatched, relay unreachable, relay full, computer offline;
  skipped where Node is missing).
- `go test ./internal/controller -run Relay`: a phone enrolls through the relay with the code in its QR link, comes back with
  no new code, is held to the phone's reviewed routes, and is put off the relay when revoked.
- `node --test relay/e2e/e2e.test.mjs`: the browser crypto against the same vectors.
- `WERKBORD_RELAY_TEST_URL=… go test ./internal/relay/... -run Cloudflare`: the same suites against a real Worker.
- By hand, in a real browser with no Cloudflare account: `npm run dev` in `relay/worker`, `node build.mjs` and any static server on
  `relay/connector/dist` (port 8788, say), a controller started with `WERKBORD_RELAY_URL=http://localhost:8787` and
  `WERKBORD_CONNECTOR_URL=http://localhost:8788` and its own data directory, then open the link from Add a phone. This is how the
  pairing, return visits, sleep, relay-down and revoke were checked in Chromium.

## Team lane: company-owned relay

`werkbord-team phone-relay --addr 127.0.0.1:7432 --data-dir /var/lib/werkbord-phone-relay` runs the same relay protocol in
Go inside the Team executable. Put it behind your company's HTTPS proxy with WebSocket upgrades enabled. It retains room
join-key hashes across restart; it holds no workspace or encryption credentials. This is an opt-in company deployment.

Deploy the connector's static files as above. On member computers, set both `WERKBORD_TEAM_PHONE_RELAY_URL=https://relay.company.example`
and `WERKBORD_TEAM_PHONE_CONNECTOR_URL=https://connect.company.example` for the Team daemon, then restart it. The company
supplies these addresses; Werkbord operates neither service. Without them Team Settings says **Ask your admin to turn on phone access**.

In Team Settings → This computer → Phone access, choose **Pair a phone**, copy the private link to your phone and open it.
The member computer must remain online. The phone can read Needs you, My work and Calendar summaries; it cannot change
Team records or start work. Revoke individual grants in the same settings panel. A pairing link remains a credential until
revoked; keep it private. See [the separate grant security review](TEAM_PHONE_SECURITY.md).

The connector supplies the **Individual | Team** switch on both phone views. Pair both lanes at the same connector origin
to switch between them; each keeps its own pairing and connection. The default connector build accepts HTTPS relays from
both lanes; a `RELAY_URL`-pinned build accepts only that relay, so use a shared company relay or omit that pin when two relay
origins are needed. Trust the connector operator with every lane paired there. Individual's code and permissions are unchanged.

The local authenticated device API exposes `GET /api/device/v1/phones` (status and grant IDs), `POST /api/device/v1/phones`
(issue a link), and `DELETE /api/device/v1/phones/{id}` (revoke). These endpoints are never reachable through the phone tunnel.

## Individual native terminals (Phase 4)

Authorised terminal output and bounded input/resize/stop requests use the existing encrypted HTTP tunnel. There is no
terminal WebSocket upgrade: output is cursor-based, bounded HTTP long polling, and input carries duplicate-write receipts.
The same enrolled-device and per-session grants apply through the relay. Disconnects reattach output only;
uncertain input is never resent automatically. The relay's end-to-end encryption, framing, keys and revocation are unchanged.
Separately paired task runners expose no terminal capability; Team carries no terminal execution or shell access.
