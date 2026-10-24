# Local demo data

## Generated workspaces, data library and members

Run `python3 scripts/seed-demo-workspaces.py --apply` against the running local
installation to add:

- Individual: **Demo · Focus planner** (a board, a chart, a calendar, weekly notes
  written in the document editor and small rituals), **Demo · Reading journal**
  (nested, linked reading notes beside the shelf) and **Demo · Developer cockpit**
  (shortcuts to the app's pages, a sprint board, copyable commands and runbook pages).
- Team: **Demo · Launch room** (launch documents, a deliverables board, due dates, a
  catalogue of launch material and a decision log) and **Demo · Research hub**
  (linked research notes, an interview question bank and the evidence), displayed in
  the first local Team workspace (`team:main`).
- Library collections with their sample records, including two unattached
  collections (**Kitchen experiments** and **Team risk register**) to inspect or
  reuse through the Data library.
- `scripts/demo-assets/` holds a sample PDF, picture and Markdown note
  (`generate.py` rewrites them). In **Demo · Launch room → Launch material**, open an
  entry and attach one to read it in the viewer. The file stays on this computer.
- Six fictional Team members with `(Demo)` names and reserved example email
  addresses, also added to existing active `Demo ·` projects with the Member role.

Generated workspaces displayed in Team remain **private local Individual data**.
They use the same Data library as the Individual workspaces; displaying a workspace
in Team does not share its records with other members. The demo member directory
entries are stored in the actual signed-in Team workspace. No invitations are sent
and their returned sign-in tokens are discarded.

The script backs up Individual's database and requests a Team storage backup
before adding data. The private `backups/before-demo-workspaces-<timestamp>` folder
contains the Individual snapshot, prior directory/library metadata, Team backup
receipt and a final manifest of sample IDs and counts. Both local services must
be available and Team backup must succeed. Writes use the existing authenticated
loopback APIs; the script never edits database rows directly.

Re-running preserves existing sample workspaces, members and record edits. Pass
`--replace` to bring workspaces made by an earlier version of the script up to date:
the same workspace (same ID, icon and display places) gets the new definition, the
earlier definition stays in its revision history and can be restored, and the earlier
sample collections remain, untouched, in the Data library. Run the script after the
app has been updated, because an older controller does not know the newer view kinds.
`--skip-team`, `--port` and `--db-name` allow a disposable controller. Missing
sample records are added using their stable title/participant/dish as identity;
renaming one of those identifiers means the original sample can be added again.
An incompatible existing sample collection schema stops the script. `--date
YYYY-MM-DD` sets the planning date anchor; `--data-dir` and `--team-key` override
the default Mac paths. Existing unrelated data is preserved. No agents, automatic
schedules, devices, network connections or Team services are created.

## Project boards and simulated history

`scripts/seed-demo-data.py --apply` adds marked sample projects to the existing
local Individual controller and the signed-in local Team workspace. Use Python
with SQLite 3.37 or newer. It reads credentials from private local files and
never prints them. `--data-dir` and `--team-key` override the default Mac paths.

The script backs up Individual's database under `backups/before-demo-<timestamp>`
and exports Team's previous overview there. Existing projects are preserved.
Re-running skips sample projects that already exist. Sample repository folders
live under the data directory's `demo-repositories`, outside personal checkouts.

Individual gets historical tasks, events and **simulated** terminal sessions,
with illustrative token/cost measurements marked `demo (simulated)`. They are
sample analytics, not actual spending or evidence of real agent execution.
Team gets human-work tickets with completed workflow history, current work and
future planning dates. Team records completion when the script runs; its past
planning dates do not backdate its activity audit.

The script never starts agents or creates automatic schedules. Future tasks
use date-only planning ranges. Every sample task has a `[Demo]` description,
and every sample project starts with `Demo ·`.

Projects can be archived in Project settings. Individual also supports
recoverable deletion. Settings → Project history restores the same project
identity, tasks, runs, memory and files. Individual's history retains dated
archive, delete and restore actions; Team records archive/restore in Activity.
Disable automatic schedules and stop active sessions before archiving or
deleting an Individual project. Neither action removes repository folders.
