# GitHub release display sequence

> **One release.** From `werkbord-v4.0.0-preview.1` there is a single release series, so a single display sequence. The
> Individual and Team sequences below are history; continue the **Individual** sequence for the unified releases and
> reserve no more Team numbers.

GitHub release titles use one display sequence for Werkbord, continuing the historical Individual sequence below. The original
per-product sequences began at `0.1.0`; those published records stay unchanged. This is a presentation convention, not the
application's semantic version. Product tags, committed VERSION files, archive names,
embedded versions, checksums, update feeds and installer version checks keep their
original build versions. Never renumber a Git tag to match a display number.

New titles use `Werkbord — Release <sequence>`. Historical `Werkbord Individual` and `Werkbord Team` titles remain records of
their publication; neither names a current separate product. Previews include `-preview.N`; additional
qualifiers such as `unsigned Mac desktop` can follow the title.

Before publishing, reserve the display number in the table below. Keep patch releases
in their display minor series, advance the display minor for a feature release or new
product generation, and advance the preview counter for another preview of the same
release. Count published releases, not every local commit or unpublished tag. Stable
publication of a preview uses the same display base without the preview suffix.

Each release description begins with:

```markdown
**Release sequence:** <sequence> · **Original build version:** <build version>

The release sequence is a display label for this product’s published history. Downloads and installed apps use the original build version; the Git tag is `<product tag>`.

---
```

Preserve the existing release notes below that introduction. The publishing workflow
reads the reserved display sequence from this table when assembling the draft.
The original build version remains derived from the tag.
Editing presentation must preserve tags, publication dates, prerelease status, assets
and latest selection. Only stable unified Werkbord releases may become latest. The `werkbord-preview-feed` release (the update feed
preview apps read, docs/DESKTOP.md) is not a product release and has no display number.

The first beta invitation scope is macOS. Linux publication is opt-in only after
its separate acceptance; Windows publication remains deferred. See
[BETA_READINESS.md](BETA_READINESS.md).
Stable unified releases are published only after the signed, notarized universal macOS app
and public download and signed update-feed checks succeed. The GUI app carries the Team
service and installer; do not attach standalone Team Host CLI archives. A pushed tag
is not proof that a release is installable.

## Recorded and reserved numbers

| Product | Git tag / original build version | Display sequence | Status |
| --- | --- | --- | --- |
| Individual | `werkbord-v0.8.2` | `0.1.0` | Published |
| Individual | `werkbord-v0.9.0` | `0.2.0` | Published |
| Individual | `werkbord-v0.9.1` | `0.2.1` | Published |
| Individual | `werkbord-v0.9.2` | `0.2.2` | Published |
| Individual | `werkbord-v0.10.0` | `0.3.0` | Published |
| Individual | `werkbord-v0.11.0` | `0.4.0` | Published |
| Individual | `werkbord-v1.0.0` | `0.5.0` | Published |
| Individual | `werkbord-v1.3.1-preview.1` | `0.6.0-preview.1` | Published |
| Individual | `werkbord-v1.3.1-preview.2` | `0.6.0-preview.2` | Published |
| Individual | `werkbord-v1.4.0-preview.2` | `0.7.0-preview.1` | Published |
| Team | `werkbord-team-v2.0.0` | `0.1.0` | Published |
| Team | `werkbord-team-v2.1.0` | `0.2.0` | Published |
| Team | `werkbord-team-v2.1.1` | `0.2.1` | Published |
| Team | `werkbord-team-v2.1.2` | `0.2.2` | Published |
| Team | `werkbord-team-v2.1.3` | `0.2.3` | Published |
| Team | `werkbord-team-v2.2.0` | `0.3.0` | Published |
| Team | `werkbord-team-v2.2.1` | `0.3.1` | Published |
| Team | `werkbord-team-v2.3.0` | `0.4.0` | Published |
| Team | `werkbord-team-v2.5.0` | `0.5.0` | Published |
| Team | `werkbord-team-v2.6.1` | `0.6.0` | Published |
| Team | `werkbord-team-v2.8.0` | `0.7.0` | Published |
| Team | `werkbord-team-v2.8.1` | `0.7.1` | Published |
| Individual | `werkbord-v1.4.0-preview.3` | `0.7.0-preview.2` | Published CLI preview |
| Team | `werkbord-team-v3.2.0` | `0.8.0` | Abandoned empty draft, deleted; superseded by the unified release. Do not reuse |
| Individual | `werkbord-v4.5.4-preview.1` | `0.8.0-preview.1` | Published unified CLI preview; no Mac app asset |
| Individual | `werkbord-v4.24.6` | `0.8.0` | Published stable; signed Mac app and macOS/Linux/Windows CLI archives |
| Individual | `werkbord-v4.25.0` | — | Unpublished; superseded by 4.28.4 |
| Individual | `werkbord-v4.28.4` | `0.9.0` | Unpublished; superseded by 4.28.5-preview.1; stable display number remains reserved |
| Individual | `werkbord-v4.28.5-preview.1` | — | Unpublished; CI browser fixture lacked a Git committer identity; superseded by preview.2 |
| Individual | `werkbord-v4.28.5-preview.2` | — | Unpublished; desktop browser fixture used an obsolete invitation label; superseded by preview.3 |
| Individual | `werkbord-v4.28.5-preview.3` | — | Unpublished signed draft; publish job lacked repository context; superseded by preview.4 |
| Individual | `werkbord-v4.28.5-preview.4` | — | Unpublished; timing-sensitive Git evidence assertion; superseded by preview.5 |
| Individual | `werkbord-v4.28.5-preview.5` | — | Unpublished; public verifier did not retry an Apple ticket-service network error; superseded by preview.6 |
| Individual | `werkbord-v4.28.5-preview.6` | `0.9.0-preview.1` | Published public signed macOS preview; Team production admission remains blocked (previous status: Reserved public signed preview) |
| Individual | `werkbord-v4.29.0-preview.1` | — | Unpublished; empty Team workspace analytics encountered a null project list; superseded by preview.2 |
| Individual | `werkbord-v4.29.0-preview.2` | `0.10.0-preview.1` | Published public signed macOS preview; focused analytics views and Universal startup; public Mac download and signed update-feed verification passed; Team production admission remains blocked |
| Individual | `werkbord-v4.37.0-preview.1` | — | Unpublished; in-process relay connection-registration races and obsolete Assistant phone browser expectations; superseded by 4.37.1-preview.1 |
| Individual | `werkbord-v4.37.1-preview.1` | — | Unpublished; Assistant dictation fixture emitted speech before asynchronous recognizer startup completed; superseded by 4.37.2-preview.1 |
| Individual | `werkbord-v4.37.2-preview.1` | `0.11.0-preview.1` | Published public signed macOS preview; phone identity, phone Git and Assistant access, encrypted relay and connector; public Mac download and signed preview-feed verification passed; Team production admission remains blocked |
| Individual | `werkbord-v4.49.1-preview.1` | — | Unpublished; release browser suite still expected the old setup disclosure; superseded by 4.49.2-preview.1 |
| Individual | `werkbord-v4.49.2-preview.1` | `0.12.0-preview.1` | Published public signed macOS preview; configurable deck workflows, interactive onboarding, compact deck controls and roomier deck spacing; Team production admission remains blocked |
| Individual | `werkbord-v4.64.0-preview.1` | — | Unpublished; the release job's browser suite failed because the Control Deck test pressed Shift+F10 on a key that was still disabled (a timing dependence that appeared on a slower runner); superseded by 4.64.1-preview.1 |
| Individual | `werkbord-v4.64.1-preview.1` | — | Unpublished; the Team comments browser suite runs in WebKit and the workflows installed only Chromium; superseded by 4.64.2-preview.1 |
| Individual | `werkbord-v4.64.2-preview.1` | `0.13.0-preview.1` | Reserved public signed macOS preview; persistent Individual and Team memory, Git-driven updates, memory-aware Salie and suggestions, repository relationships and Team initiatives, with the audit in docs/MEMORY_IMPLEMENTATION_AUDIT.md; Team production admission remains blocked |
| Individual | `werkbord-v4.65.0-preview.1` | — | Unpublished; the Team browser suite still looked for "Cancel new task" after the Team button became "Cancel new ticket", and a Go network test read a random invitation signature beginning "ai-" as a domain name; superseded by 4.65.1-preview.1 |
| Individual | `werkbord-v4.65.1-preview.1` | — | Unpublished; the Settings -> Phones browser step navigated before the app had taken its sign-in token and rewritten the address, so on a slower runner the phone list never appeared; superseded by 4.65.2-preview.1 |
| Individual | `werkbord-v4.65.2-preview.1` | `0.14.0-preview.1` | Reserved public signed macOS preview; one Settings screen for Universal, Individual and Team, phones working with the built-in relay, native Mac alerts, a floating Help button, Individual board In progress column and the Team project header and board layout; Team production admission remains blocked |
| Individual | `werkbord-v4.66.0-preview.1` | `0.15.0-preview.1` | Reserved public signed macOS preview; preview apps update themselves from their own update feed (the `werkbord-preview-feed` release), and a copy running from a disk image says how to move to Applications instead of failing to update; Team production admission remains blocked |
| Individual | `werkbord-v4.67.0-preview.2` | — | Unpublished; unified desktop startup browser assertion still forbade the hidden Individual setup-check frame; superseded by 4.67.0-preview.3. Previously reserved public signed macOS preview; guided onboarding with verified requirements, explicit optional choices, honest pause and resume, completion review, and desktop viewport/navigation fixes; Team production admission remains blocked |
| Individual | `werkbord-v4.67.0-preview.3` | `0.16.0-preview.1` | Reserved public signed macOS preview; guided onboarding and desktop viewport fixes, corrected hidden startup-frame verification, and onboarding browser checks in CI and release; Team production admission remains blocked |
| Individual | `werkbord-v4.68.0-preview.1` | — | Unpublished; replicated store tests still expected Available after claiming a ticket; superseded by 4.68.1-preview.1 |
| Individual | `werkbord-v4.68.1-preview.1` | — | Unpublished; the unified desktop browser test matched board-card Start agent buttons as well as the private execution confirmation; superseded by 4.68.2-preview.1 |
| Individual | `werkbord-v4.68.2-preview.1` | `0.17.0-preview.1` | Reserved public signed macOS preview; Team Available-first claims, private agent controls and automatic review for completed agent work, visible ticket dates and comments, screen-local host compatibility notices, and working phone Control Deck taps without workflow subtitles; corrected replicated claim assertions and scoped private agent confirmation checks unblock the Mac release build; Team production admission remains blocked |
| Individual | `werkbord-v4.68.3-preview.1` | `0.17.1-preview.1` | Reserved public signed macOS preview; one-tap task progression, phone parent navigation and chat wrapping, lighter ticket context, portable MCP connection settings, configuration import and setup help; Team production admission remains blocked |
| Individual | `werkbord-v4.69.0-preview.1` | — | Local initial motion implementation; unpublished, superseded by 4.69.1-preview.1 |
| Individual | `werkbord-v4.69.1-preview.1` | — | Unpublished; CI and release both asserted editor removal before the new sheet exit transition completed; superseded by 4.69.1-preview.2 |
| Individual | `werkbord-v4.69.1-preview.2` | `0.18.0-preview.1` | Reserved public signed macOS preview; workflow motion and Salie streaming feedback, six approved animated marks, Git fetch/push feedback and confirmed results, attachment readiness and recovery; browser verification waits for animated editors to finish closing and targets the named phone switcher, and Salie keeps new approval cards and Confirm all in view; Team production admission remains blocked |
| Individual | `werkbord-v4.69.2-preview.1` | `0.18.1-preview.1` | Reserved public signed macOS preview; recover offline Team license issuance with a replacement public issuer while preserving the previous public issuer and isolating local test issuers; Team production admission remains blocked |
| Individual | `werkbord-v4.71.0-preview.1` | — | Unpublished; hosted startup CSP omitted same-origin configuration fetches; superseded by 4.71.1-preview.1. Previously reserved public signed macOS preview; hosted licensing operator console, corrected authenticator QR/manual setup, protected server signing and customer-verifier-compatible issuance; Team production admission remains blocked |
| Individual | `werkbord-v4.71.1-preview.1` | — | Unpublished; live authenticator images include an XML declaration/comment before the SVG; superseded by 4.71.2-preview.1. Previously reserved public signed macOS preview; hosted licensing, corrected authenticator QR/manual setup, protected server signing and customer-compatible issuance; permits same-origin public configuration loading and shows recoverable startup failures; Team production admission remains blocked |
| Individual | `werkbord-v4.71.2-preview.1` | — | Unpublished; native network fixture could select the HTTP API port again for TLS enrollment; superseded by 4.71.3-preview.1. Previously reserved public signed macOS preview; hosted licensing with XML-compatible authenticator QR/manual setup, protected signing, encrypted browser backups, perpetual-term preservation and public-app-compatible license files; Team production admission remains blocked |
| Individual | `werkbord-v4.71.3-preview.1` | — | Unpublished; superseded by 4.71.4-preview.1 to correct the customer app’s perpetual-license display. Previously reserved public signed macOS preview; hosted licensing, working authenticator QR/manual setup, protected signing and verified 3-to-10 seat issuance; network fixtures reserve distinct HTTP/TLS ports; Team production admission remains blocked |
| Individual | `werkbord-v4.71.4-preview.1` | `0.19.0-preview.1` | Reserved public signed macOS preview; hosted issuance, working authenticator setup, protected signing, verified seat expansion, distinct network fixture ports and correct perpetual-license display/renewal; Team production admission remains blocked |
| Individual | `werkbord-v4.72.0-preview.1` | — | Unpublished; the unified desktop browser suite timed out after 90 seconds waiting for a second Team workspace to join on a loaded CI runner (it passes on a developer Mac); superseded by 4.73.0-preview.1 |
| Individual | `werkbord-v4.73.0-preview.1` | `0.20.0-preview.1` | Reserved public signed macOS preview; personal generative workspaces saved on this computer with a Personal data library; phones open them, work with their records, see new ones live and can put them on the Control Deck; Create with Salie is offered first in Individual and Team (not Universal); bordered panels with a shaded filter band for table, list and checklist views, and tables as cards on a phone; Team production admission remains blocked |
| Individual | `werkbord-v4.83.0-preview.1` | — | Unpublished; the release browser suite still expected an always-visible Agent field in the Control Deck workflow dialog; superseded by 4.83.1-preview.1 |
| Individual | `werkbord-v4.83.1-preview.1` | — | Unpublished; the Team and Analytics browser tests predated the new Workspace layout and the All projects default; superseded by 4.83.2-preview.1 |
| Individual | `werkbord-v4.83.2-preview.1` | — | Unpublished; the Team desktop, unified desktop and assistant browser tests predated the sidebar theme, collapsible project join code, Data library in every view and sidebar Salie; superseded by 4.83.3-preview.1 |
| Individual | `werkbord-v4.83.3-preview.1` | — | Unpublished; signed and notarized, but no display sequence was reserved for the tag; superseded by 4.83.4-preview.1 |
| Individual | `werkbord-v4.83.4-preview.1` | — | Unpublished; two release browser steps timed out on a loaded runner (the assistant attachment step, then the phone task step), each passing on another run; superseded by 4.83.5-preview.1 |
| Individual | `werkbord-v4.83.5-preview.1` | `0.21.0-preview.1` | Reserved public signed macOS preview; refined Team layouts (Analytics, Reviews, Projects, Workspace, repository-only project headers, per-card Archive), configurable Salie placement, expanded CLI agent catalogue, and backup-verified maintenance for an enrolled Team service (`Werkbord --maintain`); Team production admission remains blocked |
| Individual | `werkbord-v4.89.0-preview.1` | — | Unpublished; release validation stopped before signing on stale Salie theme/session browser expectations; superseded by 4.89.1-preview.1. |
| Individual | `werkbord-v4.89.1-preview.1` | `0.22.0-preview.1` | Reserved candidate validation; patched source-built infrastructure and dependencies, authenticated release manifests, protected exact-byte candidate review and opt-in Linux preview packaging; browser checks follow the current theme, per-conversation storage and pending-request popup. Mac first, Linux disabled pending acceptance, Windows deferred. Production admission remains blocked; no publication approval is implied. |
