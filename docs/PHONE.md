# Phone access

Werkbord's controller runs on your computer. To use it from your phone it has to be reachable from there, and a phone
behind a mobile network cannot dial your computer directly. The phone and your computer therefore both make an **outbound**
connection to a small **relay** (a Cloudflare Worker, [RELAY.md](RELAY.md)), which only passes along bytes the two ends
have encrypted for each other. Nothing is installed on the phone and nothing needs signing in to: the phone's own camera
opens a plain link.

## What you do

1. Run `werkbord setup` (the installer does). Your computer connects to the relay by itself.
2. Scan the QR code (`werkbord phones add --qr`, `werkbord open --qr`, or Settings → Phones → Add a phone) with your
   phone's camera. It opens Werkbord in the phone's browser, already signed in. Then "Add to Home Screen".

The phone is signed in until you revoke it: no second scan, no token to type. The code in the QR works for one phone, for
five minutes. You never deal with ports, tunnels, VPNs, certificates or accounts.

The phone shows "your computer is offline" when the computer is asleep and "temporarily unavailable" when the relay is at
its limit. A relay of your own can replace the built-in one ([RELAY.md](RELAY.md)).

## What it is

- The controller keeps listening on `127.0.0.1` for this computer, as before.
- It also keeps one outbound connection to the relay. Phones reach the same app and API through it, **end-to-end encrypted**
  between the phone and your computer: the relay sees ciphertext and holds no key.
- A request that arrives this way is marked as coming through the phone door, and only a phone's own sign-in is accepted
  on it (below).
- The private network (Tailscale) is **not** used for phones. It exists only so that your other computers can join as
  runners ([RUNNERS.md](RUNNERS.md)), is off until `werkbord network on`, and serves nothing but what a runner calls.

## Phones: what one can do, and how to revoke it

Every phone has a **sign-in of its own** (`wbp_…`), not your controller token. You see each one (name, when it was added, when
it was last seen) in **Settings → Phones**, or with `werkbord phones list`, and take any away at once:

```
werkbord phones list              the phones that can reach Werkbord, and when each was last seen
werkbord phones add [--qr]        make a code for one more phone
werkbord phones revoke <id>       sign a phone out, now
```

Revoking ends what the phone has open, including its live view, within a moment. The phone is then at the sign-in page and
needs a new code to come back. At most sixteen phones.

**What a phone can do.** See what the Control Deck and the task pages show (projects, tasks, runs, questions, calendar,
analytics, Git, runners); create and edit tasks (not how they run); start a run **with the task's own settings**, finish or
stop it, answer an agent, accept or reject its work; do everything the Git Control Center does (fetch, merge, push, delete a
branch, clean a worktree, open a pull request); and use the **Assistant**, all of it, as on the desktop. It can read its own
entry, rename itself and sign itself out.

**What a phone cannot do.** Change any setting, register or change a repository, read or rotate the controller's token, pair or
revoke a runner, change general task/run execution settings, make codes or see and revoke phones, or manage
connections or MCP clients. Those are yours, at your computer. The phone's screens do not show them (its Settings has only
Phones and display); the controller refuses them with `403` regardless.

**Configurable deck exception.** A phone may save only its own nine-position deck and choose an agent/model for one of six
fixed workflow buttons targeting a registered Individual repository. The dedicated deck routes do not permit policy changes,
runner selection, repository registration, general settings or arbitrary commands. Workflow starts use ordinary tasks/runs
and the same agent permission checks. See [MOBILE.md](MOBILE.md).

A phone also reads **suggested next steps** (`GET /api/projects/{}/recommendations`) for the Control Deck. The phone door does
not serve Individual memory (and a phone is never given it, wherever it connects from), so the answer there has the words of saved memory taken out (a constraint says that a saved policy
applied, and which record, not what it said), and a phone's *Useful / Not useful / Dismiss* is kept on the phone only: the
feedback route is not on its list. See [RECOMMENDATIONS.md](RECOMMENDATIONS.md).

A phone also opens the owner's generated personal workspaces and works with their records, and can put one on its Control Deck as a button (docs/PERSONAL_WORKSPACES.md, “Phone and the Control Deck”); it cannot change what a workspace is.

The list is fixed and reviewed (`internal/api/phone.go`); a test fails when it changes without the change being read.

**How a phone is let in.** The QR code is a plain `https://` link to the connector page. After the `#` it carries a
pairing: where the relay is, which room, the key for it, your computer's public key, a one-time secret and a single-use
enrollment code. The browser never sends the part after `#` to a server; the page uses it to open the encrypted channel,
spends the code, is given the phone's token once, keeps it in the browser's storage and removes the pairing from the
address. The code is 256 random bits, used once, void after five minutes, kept only as a hash, and a wrong, expired or used
code all get the same answer. The phone's name comes from the browser ("iPhone (Safari)"); it can be changed on the phone
and in Settings. Enrollment is tried at most ten times a minute.

A phone's token is accepted only through the relay and from this computer, never from any other address.

**Known limit (iOS).** Safari and a home-screen web app do not share storage on iPhone. A phone enrolled in Safari may need one
more scan after "Add to Home Screen", which then shows as a second phone: revoke the first.

## Security

- **Nothing on your computer is exposed to the Internet.** It only makes outbound connections. The relay cannot read or
  alter what passes through it, and holds one hash.
- **Reaching the relay is not authentication.** A phone is let in by its own sign-in on every API request. The QR code
  carries a single-use enrollment code, never your controller token. It is worth a minute to whoever scans it first, so
  do not post a screenshot of it.
- The Assistant is served to enrolled phones, not to the controller's own token on the phone door.
- Werkbord's built-in relay is a service Werkbord's author runs on a free tier; it sees ciphertext only. Set `relay.url`
  and `relay.connectorUrl` to your own to remove even that.
- A phone you revoke loses its relay secret at once and is put off the relay now, not at its next request.

## Turning it off, and other setups

- `relay.disabled` in `config.json` (or `WERKBORD_RELAY=off`) turns phone access off.
- `relay.url` and `relay.connectorUrl` (or `WERKBORD_RELAY_URL` and `WERKBORD_CONNECTOR_URL`), both or neither, point at a
  relay of your own.

## Troubleshooting

`werkbord doctor` and `werkbord status` report phone access in one line:

| It says | Meaning |
| --- | --- |
| ready through the relay | working |
| not connected to the relay yet | the computer is offline, or the relay is at its limit; it keeps trying by itself |
| off: the relay is turned off | `relay.disabled` or `WERKBORD_RELAY=off` is set |

If the phone cannot open the link, make a new code (`werkbord phones add --qr`): codes work once and for five minutes.

## Native terminal grants

Enrolment alone grants no standalone terminal control. At the execution computer, open the terminal in Assistant and use
**Phone access** to enable this phone for that session. The phone can then select it in Assistant, view output, send native
CLI input, resize it and explicitly terminate it through the encrypted relay connection. Creating
terminals and managing grants remain local-owner operations. Revoking the phone ends its open requests and relay access
immediately. See [Native terminal sessions](NATIVE_TERMINALS.md#mobile-session-access-phase-4).
