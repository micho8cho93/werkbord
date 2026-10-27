# Desktop control and execution: Milestones 1–2

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

Connection and repository binding currently use the CLI.
Guided first-run setup is a later milestone. Client control includes board edits,
runner selection, agent launch, managed output, questions and existing managed-run
commands. Device/credential administration, global configuration, Salie and private
Personal data remain on the controller. Git inspection and explicitly confirmed
commit/push operations run on the chosen execution machine. Native terminals
require separate per-session grants. The interface identifies the controller and each execution machine.

Run authorisation retains existing clone/fetch/worktree preparation with each
runner's own Git credentials. No implicit commit, push, merge, working-directory
copy, or code synchronisation is added.

## Git on the owning machine

Open a project's **Git** tab on a connected desktop, or expand **Git on another
execution machine** in the controller's Git view. Select a machine and its
registered repository or an ended run's worktree. Inspect the checkout, select
exact files and enter a message, then choose **Review commit**. The separate
confirmation names the machine, checkout, branch, HEAD, files and message.
**Review push to origin** publishes the displayed exact commit to the same
branch on the registered origin; it never forces a push.

Confirmations last five minutes and belong to the requesting desktop, machine,
project and checkout. Execution rechecks permission and repository state under
the existing repository lock. Changed selected bytes, index, branch or HEAD
require another review. Active execution, detached/unborn HEAD, conflicts,
unfinished Git operations, filtered files and oversized paths are refused.
Unrelated staged changes are preserved. Repository hooks and signing programs
are disabled for these operations. Origin must match the controller's registered
credential-free SSH/HTTPS remote. No remote URL, shell command or filesystem path
can be supplied by the desktop client.

Requests retain one identity and a durable receipt. **Check outcome** retrieves
that receipt; **Recover unacknowledged request** resubmits the original saved
request identity after a lost response. Neither a reconnect nor an unknown
outcome generates a new write. An uncertain result requires inspection on the
owning machine before a fresh review. Terminal input bytes are removed from the
controller's completed queue records; their request hashes remain for deduplication.

## Native terminal sessions

Managed-run output, questions, approvals, send/finish/stop commands continue to
use the existing run/session APIs and event log. Native CLI terminals reuse the
existing PTY manager and byte offsets. They are available on macOS and Linux;
unsupported platforms continue to support their existing managed agents.

On a remote runner, explicitly enable each installed, locally signed-in CLI:

```sh
werkbord runner terminals codex --enable --signed-in
werkbord runner terminals claude-code --enable --signed-in
```

Omit `--enable` to withdraw local permission. On the controller, existing native
CLI settings still apply. In **Settings → Runners → Native agent terminals**, the
controller owner can start a terminal on a chosen authorised machine and grant
or revoke a selected desktop's access to that specific session. A paired desktop
sees only its granted sessions and can view output, type, resize and terminate
them. Pairing a desktop alone never grants terminal access or process creation.
Phone grants retain their separate existing local-terminal path.

Terminal IDs include their owning runner; only that runner resolves its local
repository binding. Disconnecting a viewer or losing the controller connection
does not terminate the PTY. Output reconnects from its byte offset, with bounded
history. Input uses ordered client sequence numbers and durable receipts; an
uncertain input is never automatically replayed. Reattaching does not start a
second process. A daemon restart remains different from a network disconnect.

Git and terminal commands are optional fields in the existing signed runner
heartbeat. They add no listener, second synchronisation service or execution
engine. Upgrade both ends for these capabilities; older runners are unavailable
for device operations until upgraded.

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

If an operation is interrupted during daemon termination, its executing receipt
fences the request permanently. Stop the device explicitly when needed with
`werkbord device stop --force` (this interrupts local processes), inspect Git and
process state on that machine, then acknowledge only that inspection:

```sh
werkbord runner operation <operation-id> --confirm-inspected
```

For an operation executed by the local controller, stop that controller first
and use the same command with `--controller`. This does not repeat or declare a
write successful; its request stays fenced. Start the runtime again and inspect
the controller's outcome before issuing a new review. Preserve operation receipts
when recovering data; deleting them removes the duplicate-execution fence.

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
reconnect. The app's update guard checks runner journals, running native terminals, interrupted device receipts and pending controller operations.

## Validation and remaining milestones

Automated coverage includes independent credentials and revocation, stream
filtering/cancellation, proxy host/origin checks and token substitution, offline
completion replay, simultaneous worker sessions, controller recovery, safe mode
selection and launcher behavior without an online controller. Run `make check`
and `make desktop-check` before committing changes.

Real two-machine Tailscale/sleep-wake acceptance remains required before beta.
Milestone 2 includes native-terminal routing and confirmed Git operations on the
owning runner. Automated tests cover stale/bound confirmations, hooks/filter
refusal, staged-file preservation, commit and push, interrupted/replayed receipts,
terminal input deduplication, independent grants and late result recovery.
Milestone 3 remains: guided pairing, repository binding, and broader
mixed-OS and recovery acceptance. No Tailscale replacement, SQLite replication,
Werkbord-hosted control plane, or controller failover is introduced.
