# Salie native terminal sessions (Phases 3–4)

Salie's New tab menu can open Claude Code and Codex directly, without a task or a Salie conversation. Enable **Direct CLI** in Settings → Agents; detection must report the CLI installed and available. A verified sign-in, or an explicit owner sign-in declaration when detection is unknown, is required. A known signed-out agent is always refused. ACP support alone never grants terminal support.

Select an active registered local repository, then Open terminal. Both CLIs launch their native interactive UI with **no arguments**, in that repository. Their own permission prompts, credentials and configuration apply. The working directory is **not a filesystem sandbox**: an agent can access the operating system according to the local user's permissions. This flow does not alter task execution, assistant provider restrictions, worktrees or task policies.

## Ownership and reconnection

`internal/terminal` is an Individual execution service owned by the controller. Its PTYs and child processes do not depend on browser requests. Closing a tab, hiding Salie or disconnecting the browser detaches only the frontend. Reopen closed tabs to attach again. On a new browser session Salie discovers existing service sessions and offers them under Reopen closed tabs, even when its browser tab index has been lost. Terminate explicitly kills the owned process group.

A controller shutdown stops its terminals. A controller or execution-device restart marks previously running records **interrupted** and safely reaps an old process only when its PID, start identity and process group still match. It never relaunches an agent or resubmits instructions. Completed/stopped metadata remains discoverable; terminal output is memory-only and is not restored after a restart.

The current transport operates on the local controller's own registered execution device. Paired task runners do not advertise native terminal support: open their own authenticated local controller to use a terminal there. Enrolled phones can attach through the private-network listener or encrypted relay only after the local owner grants that phone access to that session. Local-program grants, MCP, the runner wire protocol and Team expose no terminal control. Team never executes terminals.

## PTY and renderer choice

The service uses the maintained [`creack/pty`](https://github.com/creack/pty) library already present in Werkbord's dependency graph. It creates a controlling native PTY on macOS and Linux; input, ANSI output and resize ioctls go directly to that PTY. Ordinary stdout is never parsed into a simulated CLI. The browser uses the maintained [`@xterm/xterm`](https://github.com/xtermjs/xterm.js) renderer and Fit addon, with local bundled assets, a bounded scrollback, selection/copy and browser paste. No OSC clipboard or URL-opening addons are enabled.

`creack/pty` does not implement Windows ConPTY. A cross-platform alternative, [CrossPTY](https://github.com/Kodecable/crosspty), provides ConPTY and lifecycle APIs but would add a second process abstraction. This phase retains the existing Unix implementation and fails closed on other operating systems. Windows native-terminal support requires a separately validated ConPTY backend; pipes are never used as a fallback.

Reviewed native entry points: [`claude`](https://docs.anthropic.com/en/docs/claude-code/cli-usage) starts Claude Code's interactive REPL; [`codex`](https://learn.chatgpt.com/docs/cli/reference) starts Codex's terminal UI. The explicit allow-list is separate from the much larger ACP catalogue. Executable overrides come only from the controller's existing trusted configuration and are resolved with the existing installation detection. Requests cannot specify a command, path, environment, working directory, arguments, resume handle or initial prompt.

## API and limits

All endpoints sit behind the existing host, origin and owner authentication middleware. Native terminals are unavailable when controller authentication has been explicitly disabled; the default authenticated configuration enables them. They require either a local owner connection or an enrolled phone with an explicit session grant, and a terminal service enabled on that listener. An owner token on the private-network listener cannot attach. Session IDs are random 192-bit identifiers. Every operation verifies both project and session ID; another project's path cannot attach, resize, stop or inject input into a session. An owner credential authorises the owner's sessions; there is no multi-user terminal API.

- `GET /api/terminals`: metadata only, with no PID, executable, environment or credentials.
- `POST /api/projects/{pid}/terminals`: only `agentId`, `device`, `cols`, `rows`.
- `GET /api/projects/{pid}/terminals/{id}/output?after=N`: authenticated bounded HTTP long polling, compatible with the existing fetch-based streaming model. Returns base64 PTY bytes, a byte cursor and current session metadata. Polls wait at most 20 seconds, return at most 64 KiB, and are cancelled when the frontend detaches.
- `POST .../{id}/input`: `data`, up to 16 KiB UTF-8, and `client`/`sequence` receipts plus the `rendered` byte cursor (required for phones); an eight-entry per-session queue bounds outstanding input. Input is delivered in order. A failed request is never automatically retried.
- `POST .../{id}/resize`: only `cols` (2–500) and `rows` (2–300).
- `POST .../{id}/stop`: explicit process termination.

At most eight active terminal processes, 32 concurrent output polls, 64 retained metadata records, 512 KiB of raw output per session and 5,000 frontend scrollback lines are retained. If a client falls behind, `reset` signals an output gap and the renderer clears before replaying the retained suffix. These are service memory/process-count limits, not operating-system CPU or filesystem confinement. CLI output is visible to the authenticated owner as in any terminal; the API never serialises the local user's credential files or environment.

Metadata is stored in a mode-0600 `terminals.json` within the controller's private data directory. Output is not logged to disk by this service. Process ownership metadata is private and never returned by the terminal API.

## Validation

`go test -race ./internal/terminal ./internal/api` covers real PTY descriptors, ANSI bytes, input/output isolation, resizing, cancellation/reconnection, process exit/termination, limits, restart recovery, unsupported launch rejection, unknown command fields and owner/program/phone/project authorisation. Frontend tests cover service discovery and tab persistence without launching task or conversation sessions.

`node scripts/browser-salie-terminals.cjs` exercises the authenticated API, a real PTY fixture process and xterm in Chromium. It captures desktop and narrow screenshots under `output/playwright/salie-terminals/`. `WERKBORD_NATIVE_SMOKE=1 node scripts/browser-salie-terminals.cjs` additionally starts the installed Claude Code and Codex CLIs simultaneously in a disposable repository, verifies terminal output and explicitly terminates both without sending instructions.

For the full API suite under the race detector, allow its SQLite-backed fixtures sufficient time: `go test -race ./internal/api -timeout 10m`. The focused terminal authorisation tests can also be run with `go test -race ./internal/api -run TestTerminal`.

## Mobile session access (Phase 4)

Control Deck and Assistant remain the two phone destinations. Below 900px the Assistant uses a native **Session** select
instead of a horizontal tab bar. It includes Salie conversations, opened task sessions and discoverable authorised CLI
terminals, including hidden sessions. New tab retains New Salie Conversation; phones cannot create native terminals or
manage agents. Open Session on a task retains the existing run adapter, questions, input and Stop behaviour.

At the computer, open a terminal's **Phone access** disclosure and explicitly select an enrolled phone. This grants
that phone output, input, resize and termination of **that session only**. Other terminals remain absent from discovery
and their control routes refuse access. Grant management and native process creation remain authenticated local-owner
operations. Revocation retains the existing phone-token checks, request cancellation and encrypted-relay kick; each
terminal request additionally verifies the local runner remains authorised and the registered, active repository still
matches the captured repository root and Git common directory. Output authorisation is checked again after every poll.
Session grants persist with the private terminal metadata, and grant identifiers are not returned to phone callers.

A phone can tap **Keyboard** to focus the native terminal input. Escape, Tab, one-shot Ctrl and arrow keys send real PTY
control bytes. The Text control selects 12–18px terminal type; Fit and the existing Assistant visual-viewport sizing
respond to keyboard and orientation changes. Controls have 44px touch targets and wrap on narrow screens. The phone
runs only the renderer: the CLI, sign-in and permissions remain on the computer. A native CLI's repository directory is
not a filesystem sandbox; enable remote control only for a phone trusted to use that CLI's existing permissions.

The encrypted connector tunnels the existing authenticated, bounded HTTP requests unchanged. Output uses the 20-second
long poll and byte cursor, with 512 KiB per-process buffering and explicit gap notification. No terminal WebSocket
upgrade, new relay protocol, network service, Team route or credential forwarding is introduced. Fresh handshakes and
HTTP reconnection attach to the same process; a bounded `wait=0` output probe restores connection status without waiting for fresh CLI bytes; background retries every three seconds, online events and visibility
changes recover the output stream. Closing either UI never sends Stop. The controller service must stay running on the
Mac mini: quitting the desktop UI leaves that service running; stopping/restarting the controller or sleeping/shutting
down the execution computer cannot promise live connectivity or process survival. Processes launched outside Werkbord's
terminal manager cannot be adopted.

Input is sequential, bounded and coalesced behind an in-flight request so network latency does not lose ordinary typing.
A client identifier and increasing sequence fence duplicate writes. The rendered output cursor marks acknowledged history; replay consumes historical terminal queries without sending their replies again. A lost output response does not acknowledge those bytes. Repeating the last receipt with different bytes or
sending a stale/out-of-order sequence is refused. Up to 64 input clients are retained per process, with no receipt eviction
that could permit a replay. Switching views retains the browser's input queue and uncertainty guard. A delivery failure
clears pending input, blocks further input and asks the person to check output before explicitly enabling new input with
a new client identity. Neither reconnection nor wake resends a mutation. Server restart does not replay receipts or input:
previous processes are interrupted, as in Phase 3.

### Separately paired runners

`internal/runnerwire/protocol.go` v1 carries signed, repository-scoped task jobs, reports and semantic send/respond/finish/stop
commands. It has no native-terminal capability, PTY identity, output cursor or independent terminal input acknowledgement.
Those task commands must not become an arbitrary shell tunnel. This phase therefore supports a controller and execution
runner on the same always-on computer. For another computer, connect to its own controller and enrol/grant the phone there.
A future runner extension needs an explicitly advertised and owner-authorised terminal capability, repository grants,
signed session/sequence fencing, bounded output recovery and revocation on both sides before it can be enabled. Team
continues to coordinate only.

`node scripts/browser-salie-terminals.cjs` also exercises a separately enrolled phone after the desktop UI closes, explicit
grants, CLI input and controls, 320/430px portrait and 844px landscape in both themes, network recovery, uncertain-input
blocking, session switching, Salie conversations, termination and revocation. These are Chromium mobile emulation tests;
physical iOS/Android keyboards and an external live tailnet are not covered. `TestPhoneTerminalOverEncryptedRelayAndPrivateNetwork`
uses the real controller, PTY, encrypted relay tunnel and a simulated private-network listener to verify grants, duplicate
input, reconnection, cross-repository refusal and revocation. Existing relay conformance and architecture tests remain intact.
