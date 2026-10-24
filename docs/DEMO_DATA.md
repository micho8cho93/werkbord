# Local demo data

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
