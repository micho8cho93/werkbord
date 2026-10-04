# Phone access

Dev Board's controller runs on your computer. To use it from your phone it has to be reachable from there, and
Dev Board runs no servers of its own to do that. Instead it embeds a [Tailscale](https://tailscale.com) node
(`tailscale.com/tsnet`, as a library, in the controller process).

## What you do

1. Run `devboard setup` (the installer does). When it asks, sign in to Tailscale in the browser it opens. A free
   account is enough; sign in with Google, GitHub, Microsoft or Apple.
2. Install the **Tailscale app on your phone** and sign in with the **same account**. This is the one thing Dev
   Board cannot do for you.
3. Scan the QR code (`devboard open --qr`, or Settings → Phone access). It opens Dev Board on your phone already
   signed in. Then "Add to Home Screen".

You never deal with ports, tunnels, VPN configuration, certificates or NAT traversal.

## What it is

- The controller keeps listening on `127.0.0.1` for this computer, as before.
- Dev Board's embedded node joins **your own tailnet** as a device named `devboard-<this computer>` and serves the
  same app and API **only on that tailnet**. It runs in userspace: no root, no TUN device, no system Tailscale,
  and it does not touch the Tailscale you may already have installed.
- Its identity (keys) is in `<data dir>/tailscale/`, so it keeps the same address across restarts.
- When your tailnet has MagicDNS and HTTPS certificates turned on, the app is served at
  `https://devboard-<name>.<tailnet>.ts.net/` with a real certificate (Tailscale provides it) and `http://` redirects
  there. Without them it is served over plain `http://` to the name or the `100.x` address: still encrypted by
  the tailnet, but a phone browser will not install it as an app or allow notifications from an `http://` page.
  Settings says so and links to the admin console switch (DNS → HTTPS Certificates).

## Security

- **Nothing is exposed to the Internet.** The node only accepts connections from devices in your tailnet. Dev
  Board never uses Tailscale Funnel, which is the feature that publishes to the Internet.
- **Reaching the tailnet is not authentication.** The access token is required on every API request over the
  private network, **even if you have set `requireToken=false` for loopback**; the private door never inherits
  an open one. The QR code and "Copy" link carry the token in the URL *fragment* (`#token=…`), which a browser
  never sends to a server. Treat the QR code like a password and do not screenshot it.
- Everyone who can reach the device on your tailnet can see the sign-in page; only someone with the token can use
  the API. If your tailnet is shared, use Tailscale ACLs to limit who can reach the Dev Board device.
- Dev Board has no account, relay or coordination server of its own. The third party is Tailscale's coordination
  service, which your device talks to for sign-in and to find your phone; traffic between your devices is
  end-to-end encrypted (relayed by Tailscale's DERP servers only when a direct connection is impossible).
- Tailscale's client normally uploads diagnostic logs to Tailscale. Dev Board turns that **off**
  (`TS_NO_LOGS_NO_SUPPORT`) unless you set that variable yourself.
- The sign-in link shown to you is a one-time credential for your account: it is shown in the terminal and the
  app, never logged, never in `devboard doctor` or `status`.

## Turning it off, and other setups

- Settings → Phone access → **Turn off**, or `devboard open --phone` to turn it on again. Your choice is
  remembered; `devboard setup` will not override it.
- `network.enabled` in `config.json` (or `DEVBOARD_NETWORK=true|false`) pins it on or off whatever the app says,
  for a server that must always join, or a computer that must never.
- **Headless computers:** create an auth key in your Tailscale admin console and start the controller with
  `DEVBOARD_TS_AUTHKEY=tskey-…` (or `TS_AUTHKEY`). It is read from the environment only and never stored.
- **Self-hosted coordination (Headscale):** `network.controlUrl` in `config.json`.
- A different name: `network.hostname`.

## Troubleshooting

`devboard doctor` reports the network's state in one line:

| It says | Meaning |
| --- | --- |
| off | nobody asked for it, or you turned it off |
| waiting for you to sign in | open Dev Board → Settings → Phone access → Sign in (or `devboard open --phone`) |
| waiting for approval | your tailnet requires an admin to approve new devices |
| connected at … | working. If it says `http`, see the HTTPS note above |
| failed: … | `devboard restart`; if it persists, turn it off and on in Settings |

If the phone cannot open the address, check that the Tailscale app on the phone is signed in to the same account
and connected, and that the address is the `…ts.net` name shown in Settings (or the `100.x` address, if MagicDNS
is off).
