# Desktop control and execution: Milestone 1

One fixed Personal controller owns the board, SQLite database and scheduling. A
second desktop can control that workspace while its separately paired runner
executes agents locally. No board database is created or replicated by the device
runtime. Team and the Cloudflare phone relay retain their existing trust boundaries.

## Connect another computer

Both computers need a build containing this milestone. Existing single-computer
installations retain their current startup and do not need Tailscale.

1. On the controller, enable the existing private network and sign in to your own
   tailnet. In Settings → Runners, pair the laptop runner if it should execute work.
   Use the existing `werkbord join` and `werkbord runner repo` commands to authorise
   projects and bind local clones, or explicitly allow cloning at both ends.
2. In the same Settings section, under Desktop control, choose **Pair desktop**.
   Copy the controller address. The desktop pairing code expires in five minutes.
3. On the laptop, run `werkbord device connect <controller-address> --runner`.
   Omit `--runner` for control only. Enter the desktop code when prompted; it is
   read from standard input, never a command-line argument. Sign in to the same
   tailnet if requested.
4. Open the existing Werkbord desktop app. It opens its normal loopback interface,
   backed by the selected controller. Tasks and run history remain on that controller.

The first milestone deliberately uses CLI connection and repository binding.
Guided first-run setup is a later milestone. Client control includes board edits,
runner selection, agent launch, managed output, questions and existing managed-run
commands. Device/credential administration, global configuration, Salie, private
Personal data, native terminals, and Git actions remain on the controller. The
interface identifies the controller and each execution machine.

Run authorisation retains existing clone/fetch/worktree preparation with each
runner's own Git credentials. No implicit commit, push, merge, working-directory
copy, or code synchronisation is added.

## Processes and identities

`werkbord device serve` runs outside Wails, using the existing runner login service
and `runner/tailscale` state directory. The one tsnet node dials for both the
loopback UI gateway and optional worker. `werkbord runner start/serve` adopt this
runtime when remote mode is selected. Never run another node against the same
state directory. Closing the app does not stop the runtime or agent processes.

Desktop grants use `wbc_` tokens, separate from owner, phone and local-program
tokens and runner signing keys. The controller stores grant hashes in
`clients.json`. Client codes live only in memory. The laptop holds its grant in
owner-only `device/client.json`; the webview receives only its own loopback token.
`device/state.json` records the selected controller and independent runner flag.
The gateway never forwards the laptop's loopback token to the controller.

The Tailscale listener dispatches signed runner join/sync to its existing handler
and desktop requests to a separate default-deny handler. The exact supported
method/path manifest is `internal/clientpolicy/policy.go`. The phone relay keeps
its original handler and cannot authenticate desktop grants. Event streams use
the existing durable log and resume cursor, with a separate event-type allow-list.
Unknown/private event types are omitted rather than inherited.

## Disconnects, revocation and recovery

- `werkbord device status` reports the selected mode and local service without
  printing credentials. `werkbord device start` restarts the gateway if needed.
- Losing the controller connection does not terminate already-started agents on
  runners that negotiated offline continuation. New agent launches still require
  fresh controller authorisation. Output remains bounded; questions and final
  results remain journalled and are replayed with acknowledgements on reconnect.
- Older controllers/runners keep the previous lease-expiry behavior. Upgrading
  both ends is required for the continuation guarantee. Once a runner has
  advertised continuation, the controller retains that safety property for its
  identity even if a later capability report omits it.
- Revoking **desktop control** closes open streams and prevents future requests;
  it does not revoke the device's separately paired runner. Use
  `werkbord device connect <same-controller> --pair [--runner]` with a fresh code
  to replace a client credential.
- Revoking an offline-capable **runner** fences its key immediately. Its active
  work remains reserved, because the controller cannot prove an offline process
  stopped. Inspect and stop execution on that machine before confirming recovery.
  The local controller owner can then call
  `POST /api/runners/{runnerId}/runs/{runId}/resolve` with
  `{"confirmStopped":true}` using the owner token. This assertion is audited in
  the resulting run state. Never make it based on elapsed time alone.
- Runner daemon termination/restart is different from a network disconnect:
  existing process recovery and uncertain-journal handling still apply. This
  milestone does not promise process survival across daemon termination.

## Switch back safely

`werkbord device local` refuses while the runner has unsettled work. It fences new
starts, stops the device runtime, restores the previous controller login-service
configuration and opens the preserved local database. Remote credentials remain
available for a future connection, but remote-runner participation is stopped.

Connecting the first time similarly fences controller dispatch and refuses while
controller-owned runs, native terminals or Salie turns are active. It disables
local-controller autostart without deleting the local database. A switching or
restoring record prevents accidental controller startup after an interrupted
transition. Repeat `device connect` for the same controller or `device local` to
recover; no unavailable remote controller triggers an automatic local fallback.

The remote gateway remains available locally when the controller is offline.
Mutations are not automatically retried: after an interrupted action, reconnect
and inspect its outcome before repeating it. Database restore quarantines runner
ownership under the existing recovery rules rather than relaunching work.

Installed backend updates and database restore are refused in remote mode.
Finish/resolve work and switch back to local mode before those operations, then
reconnect. The app's update guard still checks local runner journals.

## Validation and remaining milestones

Automated coverage includes independent credentials and revocation, stream
filtering/cancellation, proxy host/origin checks and token substitution, offline
completion replay, simultaneous worker sessions, controller recovery, safe mode
selection and launcher behavior without an online controller. Run `make check`
and `make desktop-check` before committing changes.

Real two-machine Tailscale/sleep-wake acceptance remains required before beta.
Milestone 2 adds native-terminal routing and confirmed Git operations on the
owning runner. Milestone 3 adds guided pairing, repository binding, and broader
mixed-OS and recovery acceptance. No Tailscale replacement, SQLite replication,
Werkbord-hosted control plane, or controller failover is introduced.
