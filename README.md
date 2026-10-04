# Devboard

A local-first remote control for coding agents. The controller runs on your computer and
owns your repositories, credentials, database and agent sessions. Your phone, tablet or
browser connects to it. There is no hosted backend and no account.

**Status: foundation.** You can register local Git repositories, keep a four-column board
(Backlog, Doing, Review, Done) per project, and watch changes sync live across devices.
Agent execution and advanced Git features are not built yet. See
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Requirements

- Go 1.26+ (no C toolchain needed: SQLite is pure Go)
- Node 20+ and npm, to build the web app
- `git` on `PATH`

## Build and run

```bash
make build
```

```bash
./bin/devboard serve
```

Open http://127.0.0.1:7420. In another terminal, register a repository:

```bash
./bin/devboard project add ~/code/my-app
```

Other commands: `devboard project list`, `devboard migrate`, `devboard version`. Run
`devboard <command> -h` for flags.

## Development

Run the controller and the Vite dev server (with hot reload, proxying `/api`) in two
terminals:

```bash
make dev-api
```

```bash
make dev-web
```

Then open http://127.0.0.1:5173.

Checks (Go tests, `go vet`, `gofmt`, `svelte-check`, ESLint, both builds):

```bash
make check
```

## Using it from your phone

By default the controller only listens on `127.0.0.1`. To reach it from a phone, put both
devices on a private network such as Tailscale and bind to that address, e.g.
`--addr 100.x.y.z:7420` (or `0.0.0.0:7420`). Off loopback a token is required: it is
generated into `<data dir>/token` on first start. Open the app on the phone and paste it, or
open `http://<host>:7420/#token=<token>` once. Then use "Add to Home Screen".

## Configuration

Settings come from defaults, then `<data dir>/config.json`, then environment variables,
then flags.

| Setting | Env | Flag | Default |
| --- | --- | --- | --- |
| `addr` | `DEVBOARD_ADDR` | `--addr` | `127.0.0.1:7420` |
| `dataDir` | `DEVBOARD_DATA_DIR` | `--data-dir` | OS config dir + `/devboard` |
| `logLevel` | `DEVBOARD_LOG_LEVEL` | `--log-level` | `info` |
| `logFormat` | `DEVBOARD_LOG_FORMAT` | `--log-format` | `text` (or `json`) |
| `token` | `DEVBOARD_TOKEN` | — | generated when needed |
| `requireToken` | `DEVBOARD_REQUIRE_TOKEN` | `--require-token` | `false` (always on off loopback) |
| `allowedHosts` | — | — | `[]` |
| `shutdownTimeout` | — | — | `10s` |

## Layout

```
cmd/devboard          CLI and daemon entry point
internal/domain       entities, states, rules (no dependencies)
internal/store        persistence interfaces; sqlite/ implementation and migrations
internal/service      use cases
internal/api          HTTP API, SSE, auth and security middleware
internal/events       live event fan-out
internal/gitrepo      Git boundary (repository inspection)
internal/agent        agent adapter boundary (no adapters yet)
internal/controller   wiring and lifecycle
internal/webui        embedded PWA
web/                  Svelte 5 + TypeScript PWA
docs/                 architecture
```
