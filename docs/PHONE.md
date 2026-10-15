# Phone access

Werkbord's controller runs on your computer. To use it from your phone it has to be reachable from there, and
Werkbord runs no servers of its own to do that. Instead it embeds a [Tailscale](https://tailscale.com) node
(`tailscale.com/tsnet`, as a library, in the controller process).

## What you do

1. Run `werkbord setup` (the installer does). When it asks, sign in to Tailscale in the browser it opens. A free
   account is enough; sign in with Google, GitHub, Microsoft or Apple.
2. Install the **Tailscale app on your phone** and sign in with the **same account**. This is the one thing Dev
   Board cannot do for you.
3. Scan the QR code (`werkbord phones add --qr`, `werkbord open --qr`, or Settings → Phones → Add a phone) with your
   phone's camera. It opens Werkbord in the phone's browser, already signed in. Then "Add to Home Screen".

The phone is signed in until you revoke it: no second scan, no token to type. The code in the QR works for one phone, for
five minutes.

You never deal with ports, tunnels, VPN configuration, certificates or NAT traversal.

## What it is

- The controller keeps listening on `127.0.0.1` for this computer, as before.
- Werkbord's embedded node joins **your own tailnet** as a device named `werkbord-<this computer>` (a computer that joined before the rename keeps `devboard-<this computer>`, so its address never changes) and serves the
  same app and API **only on that tailnet**. It runs in userspace: no root, no TUN device, no system Tailscale,
  and it does not touch the Tailscale you may already have installed.
- Its identity (keys) is in `<data dir>/tailscale/`, so it keeps the same address across restarts.
- When your tailnet has MagicDNS and HTTPS certificates turned on, the app is served at
  `https://werkbord-<name>.<tailnet>.ts.net/` with a real certificate (Tailscale provides it) and `http://` redirects
  there. Without them it is served over plain `http://` to the name or the `100.x` address: still encrypted by
  the tailnet, but a phone browser will not install it as an app or allow notifications from an `http://` page.
  Settings says so and links to the admin console switch (DNS → HTTPS Certificates).

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
analytics, Git as a reader, runners as a reader); create and edit tasks (not how they run); start a run **with the task's own
settings**, finish or stop it, answer an agent, accept or reject its work; fetch in Git; and use the **Assistant**.
It can read its own entry, rename itself and sign itself out.

**What a phone cannot do.** Change any setting, register or change a repository, read or rotate the controller's token, pair or
revoke a runner, choose an agent, model, policy or runner for a task or run, make codes or see and revoke phones, give the
Assistant folders on your computer or read its audit log, manage connections or MCP clients, and do anything in Git that cannot
be undone: merge, push, delete a branch, clean a worktree or open a pull request. Those are yours, at your computer. A phone
asking for one gets `403`, and the app says so.

The list is fixed and reviewed (`internal/api/phone.go`); a test fails when it changes without the change being read.

**How a phone is let in.** The QR code holds `<address>/#enroll=<code>`. The code is 256 random bits, used once, void after five
minutes, kept only as a hash, and a wrong, expired or used code all get the same answer. The browser never sends the part after
`#` to a server; the page spends the code, is given the phone's token once, keeps it in the browser's storage and removes the
code from the address. The phone's name comes from the browser ("iPhone (Safari)"); it can be changed on the phone and in
Settings. Enrollment is tried at most ten times a minute.

A phone's token is accepted only from the private network's listener and from this computer, never from any other address.

**Known limit (iOS).** Safari and a home-screen web app do not share storage on iPhone. A phone enrolled in Safari may need one
more scan after "Add to Home Screen", which then shows as a second phone: revoke the first.

## Security

- **Nothing is exposed to the Internet.** The node only accepts connections from devices in your tailnet. Werkbord never
  uses Tailscale Funnel, which is the feature that publishes to the Internet.
- **Reaching the tailnet is not authentication.** A sign-in is required on every API request over the private network,
  **even if you have set `requireToken=false` for loopback**; the private door never inherits an open one. A phone's is its own
  (above). The QR code carries a single-use enrollment code in the URL *fragment* (`#enroll=…`), never your controller token.
  It is worth a minute to whoever scans it first, so do not post a screenshot of it.
- Your controller token still works over the private network, for another computer on your tailnet that you sign in with
  `werkbord token --url`. The Assistant is not served to it there; it is served to enrolled phones.
- Everyone who can reach the device on your tailnet can see the sign-in page; only someone with a phone's sign-in (or your token)
  can use the API. If your tailnet is shared, use Tailscale ACLs to limit who can reach the Werkbord device.
- Werkbord has no account, relay or coordination server of its own. The third party is Tailscale's coordination
  service, which your device talks to for sign-in and to find your phone; traffic between your devices is
  end-to-end encrypted (relayed by Tailscale's DERP servers only when a direct connection is impossible).
- Tailscale's client normally uploads diagnostic logs to Tailscale. Werkbord turns that **off**
  (`TS_NO_LOGS_NO_SUPPORT`) unless you set that variable yourself.
- The sign-in link shown to you is a one-time credential for your account: it is shown in the terminal and the
  app, never logged, never in `werkbord doctor` or `status`.

## Turning it off, and other setups

- Settings → Phone access → **Turn off**, or `werkbord open --phone` to turn it on again. Your choice is
  remembered; `werkbord setup` will not override it.
- `network.enabled` in `config.json` (or `WERKBORD_NETWORK=true|false`) pins it on or off whatever the app says,
  for a server that must always join, or a computer that must never.
- **Headless computers:** create an auth key in your Tailscale admin console and start the controller with
  `WERKBORD_TS_AUTHKEY=tskey-…` (or `TS_AUTHKEY`). It is read from the environment only and never stored.
- **Self-hosted coordination (Headscale):** `network.controlUrl` in `config.json`.
- A different name: `network.hostname`.

## Troubleshooting

`werkbord doctor` reports the network's state in one line:

| It says | Meaning |
| --- | --- |
| off | nobody asked for it, or you turned it off |
| waiting for you to sign in | open Werkbord → Settings → Phone access → Sign in (or `werkbord open --phone`) |
| waiting for approval | your tailnet requires an admin to approve new devices |
| connected at … | working. If it says `http`, see the HTTPS note above |
| failed: … | `werkbord restart`; if it persists, turn it off and on in Settings |

If the phone cannot open the address, check that the Tailscale app on the phone is signed in to the same account
and connected, and that the address is the `…ts.net` name shown in Settings (or the `100.x` address, if MagicDNS
is off).
