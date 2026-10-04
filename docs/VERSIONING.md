# Versioning

Each milestone (one prompt's worth of work) is a **minor version**, committed as one commit and tagged with an
annotated tag `v0.N.0`. A fix made afterwards to the same milestone is `v0.N.1`, and so on. The tag message is the
version and the commit subject. `make build` stamps the binary with `git describe --tags`, so `devboard version`
reports it.

| Version | Commit | Milestone |
| --- | --- | --- |
| v0.1.0 | `dabf24a` | The local-first controller and PWA foundation |
| v0.2.0 | `bab1f07` | Hardened Git inspection and worktree records; token required by default |
| v0.3.0 | `553e904` | The local coding-agent runtime |
| v0.4.0 | `516a780` | Project as the scope of the app; per-task execution policies |
| v0.5.0 | `e85f596` | The Git Control Center |
| v0.6.0 | the "Add repository health" commit | Repository health; the Control Center for exceptions |

## Tagging a new version

After committing a milestone:

```bash
make tag VERSION=v0.7.0        # annotated tag on HEAD, message = version + commit subject
git push origin v0.7.0
```

`make tag` refuses a malformed version, a tag that exists, and a dirty working tree.

## Recreating the historical tags

Tags are ordinary Git refs: if they are missing in a clone, recreate them from the table (the commit IDs are stable):

```bash
git tag -a v0.1.0 dabf24a -m "v0.1.0 — Build the local-first controller and PWA foundation"
git tag -a v0.2.0 bab1f07 -m "v0.2.0 — Harden Git inspection and worktree records; require API token by default"
git tag -a v0.3.0 553e904 -m "v0.3.0 — Add the local coding-agent runtime"
git tag -a v0.4.0 516a780 -m "v0.4.0 — Make Project the scope of the app and add per-task execution policies"
git tag -a v0.5.0 e85f596 -m "v0.5.0 — Add the Git Control Center"
git push origin --tags
```
