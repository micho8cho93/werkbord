# One platform: Personal and Team projects, Free and Premium

Status: **plan, not started.** Nothing in this document has been implemented. It records the decisions made on 2026-10-25 and
the order of work. Mockups of the result are in this folder (`01`–`06`, PNG).

## What changes

1. **One platform.** The Individual / Team / Universal switcher goes. One window, one sidebar. The distinction that remains is
   per project: a project is **Personal** (only you, on your computer) or a **Team** project (shared, in a team you belong to).
   Anyone can create a team and anyone can join one.
2. **No licensing, two plans.** The per-team signed licence system is deleted. **Free** and **Premium** replace it.
   Free: everything Personal, plus teams of up to **2 people** (you and a partner). Premium: teams of any size, as many as you
   like, bought once, kept forever.

## Decisions

| Question | Decision |
| --- | --- |
| Premium gate | One build for everyone. Premium is unlocked locally by a **per-person unlock key**, checked offline against a public key. No account, no server, no expiry, no seats, no renewal. |
| Who needs Premium | **Every member** of a team of 3 or more, host included. |
| Team hosting | Unchanged. A team lives on a **customer-owned Workspace Host** over a customer-owned private network. Werkbord runs no cloud service. |
| Navigation | **One sidebar with sections**: Home, Calendar, then PERSONAL (projects), then one section per team (projects, People, Reviews). The switcher is replaced by a go-to palette (⌘K). |
| How deep | **One shell, two engines.** One window, one sidebar, one project model. The local controller (runs your agents, holds your credentials) and the Team service (root-owned, coordination only) stay separate processes. No screen rewrite in this change; screens converge later. |
| Over the cap | **Block the add.** A free build cannot invite or approve a 3rd person. A free member of a team that is already 3 or more keeps **read-only** access with an upgrade prompt; nothing is hidden or lost. |
| Cross-links | All four: a team ticket becomes my local task (kept); move a project personal ↔ team; Home shows everything; Personal is private by default. |
| Naming | **Personal / Team.** "Individual" leaves the interface. "Workspace" stays an internal word. |
| Strictness | **The host verifies the key.** A joiner presents their signed key at enrolment; the host refuses a 3rd member without valid proof from the host and the joiner. |
| Salie | **Personal write, team read-only**, through the existing Team bridge. **Two entry points, both kept:** the Salie button in the sidebar and the floating Salie button (draggable, bottom centre by default, with its Needs-you count). Both open the same Salie. |
| Builds and releases | **One build, one release, one product, always.** There is no Premium build, edition build, flavour or second download. Free and Premium differ only by whether a valid unlock key has been entered. Every change to the app ships through the one existing release process (one `VERSION`, one tag series, one DMG). |
| Joining a team | **One flow, one confirm**: creating or joining ends with a single "Connect this computer to run my tickets with my own agent" choice. Every run still asks locally. |

### Considered and rejected

- **Check the key only when admitting the 3rd member** (no read-only state): rejected. Every member needs Premium, and a free member of a 3+ team is read-only.
- **One key per team, held by the host**: rejected. Premium stays per person.
- A Premium build, a second download or any edition flag: rejected. One build; the key is the only difference.

## Assumptions I made (say if any is wrong)

- A free person may belong to **any number** of teams, as long as each has 2 people or fewer. The only limit is team size.
- Pending invitations reserve a place, so a free team cannot have two outstanding invites that would make it 3.
- Extra devices of the same person do not count as people (as seats work today).
- A team that falls back to 2 people returns everyone to full access.
- The price, the purchase page and the sales channel are decided later. "Get Premium" opens a configurable address that is
  empty until then.
- The version moves to **5.0.0-preview.1** at the stage where the visible product changes (stage 3). Earlier stages are minor bumps.

## One release process

The policy already says Werkbord is one product with one version and one release; this plan keeps it and closes the one door
that could reopen it. Concretely: no `-tags premium`, no linker flag that selects an edition, no second artifact in the release
workflow, and an architecture test that fails if one appears. The unlock key is the only difference between Free and Premium.
The tool that signs keys is run by you, offline, and is not part of any build you ship.

## The trust rules stay

These are about credentials, not packaging, and `internal/archtest` keeps enforcing them:

- A team coordinates; it never executes, never holds Git, GitHub or agent credentials, and never reaches into a member's machine.
- The controller links no Team code. The desktop app links neither Team code nor the execution engine.
- Personal data is never written into a team, and team data never into Personal storage, without an explicit action.
- Werkbord operates no service that Team points at (architecture rules 15 and 16 stay true).

## Design

### The project model

The shell gets one **project registry** with a `kind`: `personal` or `team`, and for a team project the team it belongs to. Today
the shell asks each workspace for its summary and lists projects per workspace; the registry is the same data presented as one
list, grouped into sections. Personal projects are read from the controller, team projects from each team's service, exactly as now.
Nothing is copied between them. The wire values `"personal"` and the workspace kind stay as they are, with their comment.

### Plans

A small shared package, `internal/entitlement`, standard library only (it is added to the Team allow-list and obeys rule 10):

- `Premium` state and one rule function: `AllowsTeamSize(premium bool, size int) bool` (free: size ≤ 2).
- Verification of an unlock key: Ed25519 over a fixed prefix and canonical JSON `{schema, product, id, issuedAt}`. No expiry,
  no seats, no customer binding, no support date. A leaked key is the known weakness; a signed blocklist shipped inside app
  releases is the later remedy, not part of this change.
- The key is stored in the desktop app's per-user configuration (mode 0600) and presented to the local Team service over the
  locally authenticated device API, which records it as a verified fact about that member.

The Team service enforces transactionally, in the same guarded insertion that counts seats today:

- Admitting member N ≥ 3 needs the host's proof and the joiner's proof.
- When a team has 3 or more people, a member without proof is **read-only**: mutations are refused with a typed
  "premium required" error that the UI turns into the upgrade prompt. Reads and permission-limited containment
  (revoking devices, removing members) stay available, as they are today after a licence expires.
- When the team is back to 2, the restriction lifts.

The frontend only displays; it never unlocks anything. The offline limit is stated honestly: a person who modifies their own
build can bypass this. The aim is a fair, low-friction boundary, not copy protection.

### The sidebar

Home and Calendar (everything, across Personal and every team, tagged by colour), then the PERSONAL section with its projects
and "New personal project", then one section per team showing its name, headcount (or "read-only"), its projects, People and
Reviews. At the bottom: "New team or join…", Settings, and a plan chip (Free · Upgrade, or Premium · Active). The
present switcher, "Everywhere" heading and workspace tags are removed. A section collapses; the open one is remembered per device.

### Creating and joining

"New team or join…" opens one dialog (mock 04): create (name, hosted on this computer, plan note, connect-runner choice) or join
with a link. The host install still needs one macOS administrator authorisation, and the dialog says so plainly.

## Stages

Each stage is its own commit with its version bump, tag (`make tag`, `make verify-tag`) and a passing `make check`. Nothing is
pushed.

| # | Stage | Result | Version |
| --- | --- | --- | --- |
| 1 | **Done in 4.90.0-preview.1.** **Edition rule replaces licences.** Delete team licensing: `internal/team/license`, the licence gate and API, `cmd/werkbord-team/vendor` licence issuing and `licensing/`, `supabase/migrations/…licensing_operator`, `TEAM_LICENSE.md`, `LICENSE_OPERATIONS.md`, `PRODUCTION_LICENSING.md`, the console licence screens and the archtest licence rules. Add `internal/entitlement` and the size rule in the Team service, with a **test-only** way to set Premium. The `workspace_licenses` table is left in the schema, unread (hosts of different versions can share a database during maintenance, so no migration drops it). The test-only seam is `config.Config.Premium` and, for the browser scripts, `-X main.testPremium=1`; an architecture test keeps every release script free of it. See [../PLANS.md](../PLANS.md). | Teams are capped at 2; no licence anywhere | 4.90.0 |
| 2 | **Premium key.** Key format and offline verification; Settings → Plan (mock 06); the issuing tool for you, kept offline and outside the repository's customer builds; host-verified proof at enrolment; read-only state and the upgrade prompts (mock 05). | Premium really unlocks | 4.91.0 |
| 3 | **Unified shell.** Project registry, new sidebar, Home and Calendar across everything, ⌘K go-to, the switcher removed, "Individual" → "Personal" in the interface (mocks 01–03). | One platform you can see | **5.0.0-preview.1** |
| 4 | **Create and join flow.** The one dialog, the single runner-connect confirmation, first-run changes so a new person starts in Personal. | Mock 04 | 5.1.0 |
| 5 | **Cross-links.** Move a project personal ↔ team with a review of exactly what leaves the computer; Salie reading team projects read-only; Home polish. | | 5.2.0 |
| 6 | **Docs, naming and acceptance.** `STRUCTURE`, `ARCHITECTURE` §22, `UNIFICATION`, `README`, the website and release acceptance rewritten around one product with two plans. | | 5.2.1 |

Stage 1 first, because it removes the most code, makes the cap real before anything user-visible changes, and leaves the tree
simpler for the rest.

## Risks and limits to know about

- **One host per computer.** A computer can host only one team today (fixed ports) and belong to up to 16. "Everybody can create a
  team" is true, but a second team you host needs another computer. Lifting that is separate work (per-team ports and services).
- **Administrator password.** Hosting a team on a Mac installs a privileged service once. That cannot be avoided without moving
  the team to a cloud, which was decided against.
- **Platforms.** The Team installer exists on macOS only. Windows and Linux get Personal and joining; hosting there is not covered here.
- **Key sharing.** Offline keys can be shared or leaked. The remedy (a blocklist inside releases) is not in the first version.
- **Your own installs.** The real install has a Team host (3.0.1) enrolled with a licence. That service is replaced only through
  coordinated maintenance, and tests never run setup, stop or update against it. Existing teams larger than 2 will become
  read-only for a free member until a key is entered; the development issuer's test key covers local testing.
- **The live licence register.** The repository's Supabase licensing files are deleted in stage 1. The hosted Supabase project is
  not touched without your explicit say-so.
- **Move a project (stage 5)** is the least defined. A promoted project sends text only (name, repository reference, tickets);
  runs, history and agent output stay local. It needs its own design pass before building.

## Not in this change

A payment system, pricing, a purchase page, a Werkbord-hosted cloud for teams, merged screens (one native board for both kinds),
merged processes, and a key blocklist.
