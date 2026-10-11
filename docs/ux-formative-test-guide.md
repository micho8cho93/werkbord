# Five-developer formative test guide

11 October 2026. Individual web app and unified desktop shell; disposable fixtures only.
This is an unconducted protocol. No outreach, participant identities or results are implied.

## People and preparation

Recruit five developers who steer coding agents: two primarily desk users, two who regularly check work on a phone,
and one who primarily uses the keyboard. Include a developer who uses assistive technology if possible; otherwise
conduct and report a separate assistive-technology session. Record actual device, browser, input method, theme,
text scale, familiarity with Werkbord and agent experience, with consent. Use anonymous P1–P5 identifiers.
Do not substitute viewport emulation for a physical phone or DOM assertions for VoiceOver/NVDA observations.

Build/start/seed using only docs/ux-excellence-prompts.md's guarded procedure. Confirm health version
`browser-fixture` before each session. Reset to a fresh seed between people and destructive tasks. Use the IDs in
that session's projects.json. Give people only the fixture address and disposable sign-in; never a real controller.
Keep a committed Review task, an unanswered question and Venue search's work tasks. Controlled browser response
mocks may simulate offline/lost-response states; label them in the facilitator record. No remote push is required.

Read aloud: “We are testing the interface. Work as you normally would and say what you expect to happen.
You may stop at any time. I will usually wait before helping. Everything here is disposable.”
Do not name the intended navigation destination or explain vocabulary before the relevant exercise.
Counterbalance tasks 2–5 across participants to reduce learning effects; do draft recovery last.
Use the assigned input method throughout, then allow a second method and record that as assistance.

## Tasks and neutral prompts

| Task | Prompt to participant | Observable completion / follow-up |
| --- | --- | --- |
| 1 Answer | “An agent needs a decision about committing its work. Find it, read the context and tell it No.” | Correct question/owner; one answer; recognize saved versus delivered/awaiting acknowledgment. Ask “What happened, and what is it doing now?” |
| 2 Review / merge | “The settings copy is ready for your inspection. Check what changed. If this fixture change looks acceptable, bring it into main.” | Inspect diff before confirmation; recognize source/test confidence; confirm local-only merge; task becomes Done. Ask “What evidence did you trust? Did anything go to a remote?” |
| 3 Verdict | Fresh Review fixture: “Record that this work is acceptable, while keeping the repository as it is.” | Uses verdict; no merge/archive/move. Ask before action “What will this change?” and afterwards “Where is the work now?” |
| 4 Plan / schedule | “Plan Book the venue for next Monday through Wednesday. Separately, arrange for the repository task Dark mode audit to start tomorrow at 10:00 in your timezone.” | Work dates on Timeline; execution schedule on Calendar/Schedule; explains distinction. No execution controls expected on work project. Record dates/timezone shown. |
| 5 Git navigation / Help | “Find the repository's branches, then its history. Return to where you started. Find guidance about an answer whose delivery is uncertain.” | Uses Git subsection, Back retains context, Help recovery guidance found and understood. Ask “Which next step is safe?” |
| 6 Draft recovery | “Begin a new task with this brief. The connection will drop before you save. Reload and recover the draft. Now a save response will be lost: decide what to do next.” | Draft survives reload in the same tab; checks Board before replay; does not duplicate an uncertain save. Ask “What do you know, and what remains unknown?” |

For keyboard sessions, observe skip link, route opening, task heading focus, Tab/Shift+Tab containment,
Escape/cancellation, focus return and shortcuts while typing. For physical phones observe targets, bottom navigation,
scrolling, on-screen keyboard, text zoom and portrait/landscape. Check at least light and dark once across the group.
For the assistive-technology session record actual speech/announcements: landmark/current location, question name and
choices, delivery state, validation/errors, modal name, background exclusion, focus restoration and draft recovery.
Test reduced motion with the person's system setting; record actual transitions observed.

## Measurement and assistance

Start a timer after each prompt; stop on completion, abandonment or the agreed five-minute cap. Record wall-clock
seconds, including recovery and assistance; mark infrastructure delays separately. Success means the correct end
state, not just a click. Unassisted success requires no hints or change of assigned input method.
A wrong turn is a destination/action that does not advance the task (record the actual route/control and recovery).
Record every hint verbatim. After 60 seconds of no progress, ask “What are you looking for?” without naming controls;
this is a facilitator intervention. If stuck, offer a directional hint, then explicit instruction, recording both.

Critical errors: wrong-owner answer, accidental repository mutation, mistaken remote-push belief followed by action,
duplicate uncertain save/run, lost unrecoverable draft, or inaccessible essential control that prevents completion.
Record near misses separately. Stop the exercise if the fixture identity or scope becomes uncertain.

| Participant | Device/input/AT | Task | Success | Unassisted | Wrong turns (count + detail) | Assistance (words) | Critical errors | Seconds | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| P1–P5 | Not observed | 1–6 | Not measured | Not measured | Not measured | Not measured | Not measured | Not measured | Protocol only |

Report each task as x/n observed successes and x/n unassisted, total wrong turns, number needing help, critical errors,
and median/range seconds with individual times retained. Report AT/device results separately. The diagnostic aim is
at least 4/5 unassisted on each core task and zero critical errors; five people do not establish population-level usability.
Today n=0: success counts, wrong turns, assistance, critical errors and time are **not measured**, not zero failures.
Missing participants, real phones, system zoom and assistive-technology observations remain open evidence needs.
