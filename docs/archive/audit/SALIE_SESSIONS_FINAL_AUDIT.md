# Salie sessions and native terminals: final integration audit

An independent review of the combined system delivered in the four preceding phases: independent Salie session tabs and
coding-agent eligibility (`19b5b7d`), attached run sessions and the shared attention queue (`4897472`), persistent native
terminal tabs (`77c38db`) and authorised mobile terminal sessions (`a56f167`). Reviewed at `a56f167` (4.87.0-preview.1).
Fixes are in 4.87.1-preview.1. Design reference: `NATIVE_TERMINALS.md`, `ASSISTANT.md`.

## Method

Each trust boundary was traced from the request to the process rather than inferred from the phase documents: the HTTP
guard chain (`checkHost`, `checkOrigin`, `authenticate`), the phone route allow-list, `terminalReady` and `Access`, the
manager's lifecycle and persistence, the PTY and environment handed to a child, the cross-frame messages that carry the
attention queue, and the browser's use of storage. The existing suites were run under the race detector, then the findings
below were reproduced with tests before they were fixed.

## Confirmed controls (no change needed)

| Boundary | Finding |
| --- | --- |
| Request forgery | Authentication is a bearer header only: no cookie exists to ride, and `checkOrigin` refuses a foreign `Origin` on every state-changing method. A page in the person's browser cannot reach `input`, `resize` or `stop`. |
| Who may attach | An owner credential needs a loopback peer and a non-private listener; an owner token on the private-network listener is refused. A phone needs its own enrolled token **and** a per-session grant, rechecked after every poll and after decoding every mutation. |
| What a phone may do | Creation, the grant route and settings are not in `phoneRoutes`; the reviewed-routes test lists the five attachment routes exactly. A phone cannot change `directCLI` or `cliSignedIn`, so it cannot widen its own reach. |
| Other callers | Local-program grants, MCP, the runner wire protocol and Team expose no terminal route (`internal/archtest`, `internal/team/api`, `terminal_phone_test.go`). The controller still links no Team code. |
| What starts | Only `claude` or `codex`, with no arguments, from trusted configuration, in a registered repository whose resolved path and Git common directory still match. The request carries no command, path, environment or prompt. |
| Child environment | `agent.SanitizedEnv` removes the controller's secrets and Git redirection variables; the PTY master is close-on-exec, so no other child inherits it. |
| Process ownership | The child is a session leader (`kill(-pid)`); restart reaps only when PID, start time and group still match, and never relaunches or resends input. |
| Input | Bounded (16 KiB, eight queued), ordered, fenced by client and sequence with a content hash and rendered cursor; a failed request is never retried and the browser blocks further input until the person re-enables it. |
| Output | Bounded long poll (20 s, 64 KiB, 32 concurrent), base64 over JSON, gap signalled with `reset`; terminal queries replayed from history are consumed, not answered a second time. No OSC clipboard or link handlers. |
| Cross-frame messages | The shell believes only the frame it created (`id`, `origin`, `window`), validates run identifiers against `^[\w-]{1,120}$`, and accepts a count and flags from the assistant frame, never text. |
| Browser storage | `sessionStorage` keeps tab titles, drafts and terminal metadata; no token, output or input. |

## Findings fixed

### 1. A damaged `terminals.json` stopped the whole controller (reliability, medium)

`terminal.New` returned an error for a file it could not parse or that held more than 64 records, and
`controller.Start` returned that error. A write cut short by a crash or power loss therefore left the controller unable to
start, taking task execution with it, for the sake of metadata about terminals that could not be reattached anyway.

*Reproduced:* an empty file, a truncated array and a wrong-shaped document each made `New` fail.

*Fix:* the state is written to a temporary file, flushed and renamed. A file that is still unreadable is set aside as
`terminals.json.damaged`, logged, and the service starts with no terminals. More than 64 records keeps the newest 64, after
reaping every record still marked running so no old process outlives its record.

*Regression tests:* `TestDamagedStateIsSetAsideInsteadOfStoppingTheController`, `TestOversizedStateKeepsNewestRecords`.

### 2. Many browser tabs could lock a long-lived terminal out of input (reliability, medium)

Every page load generates a new input client identity, and a process kept at most 64 receipts with no eviction. A terminal
is now persistent across browser sessions, so after 64 distinct tabs or reconnects had typed into one, every new client was
refused with `409` for the rest of the process's life. "Enable new input" mints another identity and was refused too.

*Reproduced:* the sixty-fifth client was rejected indefinitely.

*Fix:* a receipt is given up only after an hour unused, only to admit a new client, and only once that client's input has
been accepted. While all 64 are fresh a new client is still refused (with a message saying why) rather than any receipt being
evicted. A duplicated HTTP or relayed request arrives within seconds, so the replay fence is unchanged in practice; the
documented guarantee is narrowed accordingly in `NATIVE_TERMINALS.md`.

*Regression test:* `TestInputReceiptsAreBoundedWithoutLockingAndNeverEvictedWhileFresh`.

## Reviewed and left as is

- **`Access` runs Git on every phone request** (about 27 ms on an idle Mac; two checks per input). It stops at the grant check
  first, so the cost scales with the sessions granted to that phone, not with all sessions. Caching it would weaken the
  documented "every request" revalidation for a latency that is not a problem; left alone.
- **`kill(-pid)` after the leader is reaped** can in theory name a recycled group. The window is the time to take a mutex, and
  a recycled PID becomes a group leader only if it then calls `setsid` or `setpgid`; accepted, as for any `os/exec` supervisor.
- **A child that creates its own session escapes the group kill.** Inherent to PTY supervision and already stated: the
  working directory is not a sandbox.
- **`postMessage(..., '*')` from the assistant frame** carries only a count and two flags to the parent; the parent verifies
  the sender. Unchanged, and not worth a new coupling to the shell origin.
- **The attention count is no longer capped at 99** in `embed.ts`. Only the shell badge is affected; cosmetic.
- **No physical-device coverage.** Mobile behaviour is verified in Chromium emulation only, as `NATIVE_TERMINALS.md` says.

## Verification

`go test -race` over `internal/terminal`, `internal/controller`, `internal/api` (terminal, phone, assistant, run-session and
selection tests), `internal/agentcatalog` and `internal/archtest`; the web unit tests, `svelte-check` and ESLint;
`scripts/browser-salie-terminals.cjs` and `scripts/browser-salie-sessions.cjs`. See the release commit for the full
`make check` result.
