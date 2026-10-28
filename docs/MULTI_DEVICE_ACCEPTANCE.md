# Multi-device acceptance run

Automated tests cover the logic of desktop control (see [MULTI_DEVICE.md](MULTI_DEVICE.md)), including simulated
disconnects longer than the 90-second lease, revocation and owner-confirmed release. They cannot cover a real tailnet,
real sleep/wake, the system folder dialog or the window reload. This is the checklist for those. **Nothing here has been
run yet.** Do not call the feature beta-ready, or any OS pair supported, until the results table at the end has been
filled in from a real run.

## Before you start

- Two computers on the same tailnet, both running the same build from this branch (`werkbord version` on each;
  Settings → This computer in the app). Computer **A** is the controller ("home"); computer **B** is the laptop.
- Codex signed in on A, Claude Code signed in on B. A project whose Git repository is cloned on both.
- Note A's and B's operating systems and versions in the table. Repeat the whole run for each pair you want to call
  supported (macOS↔macOS, macOS↔Linux, macOS↔Windows, Linux↔Windows, and so on). Windows and Linux builds compile and
  pass vet (checked), but have not been exercised.
- Keep `werkbord logs` open on both. Do not share a screenshot or log that contains a pairing code or token.

## Steps

Each step lists what to do and what must be true. If anything differs, stop, record it, and keep the machine state for
inspection.

### 1. Guided pairing, control only
1. On A: Settings → Runners → **Pair desktop**. Copy the address and code.
2. On B (app): Settings → Runners → **Connect to existing Werkbord**. Enter address, code, name. Confirm the dialog.
3. Expect: B asks for confirmation, shows *Connecting…*, the window reloads onto A's board; the banner names A as the
   controller; B's own board is untouched (switch back later to confirm). The code is not visible anywhere afterwards.
4. Reuse the same code again from B: it is refused (single use). Wait five minutes with an unused code: it is refused.

### 2. Runner and clone binding
1. On A: **Add runner**, authorise the project. Back on B, switch back to its own controller (step 8), then repeat step 1 with
   *Also let that Werkbord run agents on this computer* and the runner code. (Or pair the runner first, then reconnect.)
2. Under *Where each project lives on this computer*, choose **Choose clone** and pick B's existing clone in the system
   folder dialog. Expect the dialog to be native, the path to appear, and nothing to be committed, pushed or copied.
3. A run for that project on B uses a worktree of that clone.

### 3. Simultaneous execution
1. From B's window start a Codex task on A's runner and a Claude Code task on B's runner at the same time.
2. Expect independent sessions, each labelled with its execution machine; answering a question in one does not affect the
   other.

### 4. Disconnect longer than 90 seconds
1. With both tasks running, cut B off from the tailnet (disconnect Tailscale or the network) for at least **3 minutes**.
2. Expect: B's agent keeps running; B's window shows the offline banner and says changes wait for the controller; no new
   run starts on B during the outage; A shows B as offline but its run still reserved.
3. Reconnect. Expect: output produced during the outage appears in order, no duplicate output, no second launch of the
   task, and the run finishes or waits for input normally.
4. Repeat while **restarting A's controller** during the outage. Expect the same, with no duplicate launches.

### 5. Sleep and wake
1. Sleep B for several minutes with a run in progress, then wake it. Record exactly what the agent process did (continued,
   was suspended and resumed, or was killed) and what the UI showed. Expect no duplicate launch and an honest state.
2. Sleep A (the controller) for several minutes and wake it. Expect B reconnects without being re-paired and without
   switching to a local board.
3. Quit and reopen B's app during a run. Expect the run to continue (closing the app does not stop the runtime) and the
   app to reconnect to A, never to a local controller.

### 6. Revocation
1. On A: revoke B's **desktop control**. Expect B's window loses requests and open streams, while B's runner keeps working.
2. Re-pair B with a fresh code.
3. With a run active on B, disconnect B, then on A revoke the **runner**. Expect the run to stay reserved: it is listed under
   **Work held on revoked machines**, and does not release after an hour. Inspect B, stop the process, tick the confirmation
   and choose **Release this work**. Expect the run to end as *owner verified execution stopped on revoked runner*.
   Confirm `werkbord runners held` on A agrees.

### 7. Terminals and Git on the other machine
1. Open a native terminal on B's runner from A's window. Expect it requires its own grant; input, resize and stop work.
2. In the project's Git tab choose B's machine: status, commit and push each need a confirmation naming that machine,
   repository and files. Change a file after the preview and confirm: the confirmation is refused as stale.

### 8. Switching back
1. With a run active on B started from A, choose **Use this computer's Werkbord** on B. Expect it to refuse.
2. Finish or stop the run, then switch back. Expect B's own board and history exactly as before step 1, and B's runner to stop
   participating in A.

### 9. Updates
1. In remote mode on B, try to update the installed backend. Expect it to be refused (backend updates and database restore are refused in remote mode). Switch back, then confirm an update is also refused while any run, terminal or device operation is unsettled.

## Results

Fill in one row per step per OS pair. *Pass* only if every expectation held.

| OS pair (controller → laptop) | Build | Step | Result | Notes (what happened, log excerpts without secrets) |
|---|---|---|---|---|
| | | 1 | | |
| | | 2 | | |
| | | 3 | | |
| | | 4 | | |
| | | 5 | | |
| | | 6 | | |
| | | 7 | | |
| | | 8 | | |
| | | 9 | | |

Only the OS pairs with every step passing may be described as supported.
