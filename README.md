# LifeLedger

LifeLedger is a private life-admin agent. It keeps a record of your bills, renewals, warranties, documents, and deadlines. It reminds you before each due date. It acts for you, and you approve each high-risk action.

## Overview

**Why it exists.** Life admin is a set of small dates and documents that are easy to forget: a card renewal, a warranty end date, an insurance premium. A missed date costs money or time. LifeLedger remembers these items and tells you about them on time.

**Who uses it.** One person who wants a private assistant for household and personal admin. You own your data. You can see, edit, and delete each item that LifeLedger remembers.

LifeLedger is an entry in the Personal AI track of the [Nebius x NVIDIA Global AI Hackathon](https://nebiusglobalaihackathon.devpost.com/).

## Key features

- HTTP service with a health endpoint for container hosts.
- Configuration from environment variables or a local `.env` file.
- Graceful shutdown on `SIGINT` and `SIGTERM`.

## Architecture

```text
Client ──HTTP──▶ lifeledger serve (Go)
                   └── GET /healthz
```

The service is one Go binary. Each command of the binary is a separate entry point.

## Prerequisites

- Go 1.24 or later.
- GNU Make.
- Git.

## Local setup

1. Clone the repository:

   ```sh
   git clone https://github.com/rohitshukla001/lifeledger.git
   cd lifeledger
   ```

2. Copy the example settings file:

   ```sh
   cp .env.example .env
   ```

3. Build the binary and start the server:

   ```sh
   make run
   ```

4. In a second terminal, send a request to the health endpoint:

   ```sh
   curl -s localhost:8080/healthz
   ```

   The server sends a response like this:

   ```json
   {"status":"ok","version":"dev (none)","uptime_seconds":0}
   ```

## Configuration

LifeLedger reads its settings from environment variables. It also reads a `.env` file from the current directory. A variable in the process environment has priority over the same variable in `.env`.

| Variable | Default | Purpose |
|---|---|---|
| `LIFELEDGER_ADDR` | `:8080` | HTTP listen address. If it is empty, LifeLedger uses `PORT`. |
| `PORT` | none | Listen port that container hosts set. LifeLedger uses it only if `LIFELEDGER_ADDR` is empty. |
| `LIFELEDGER_ENV` | `dev` | `dev` gives text logs. `prod` gives JSON logs. |
| `LIFELEDGER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |

To use a different settings file, use the `-env-file` flag:

```sh
./bin/lifeledger -env-file ./local.env serve
```

Do not put API keys or passwords in the repository. Keep them in `.env`. Git ignores `.env`.

## Running and testing

| Command | Purpose |
|---|---|
| `make run` | Build the binary and start the server. |
| `make build` | Build the binary into `./bin`. |
| `make test` | Run all tests with the race detector. |
| `make check` | Run the format check, `go vet`, and the tests. Run it before each commit. |
| `make help` | Show all targets. |

Binary commands:

| Command | Purpose |
|---|---|
| `lifeledger serve` | Start the HTTP server. |
| `lifeledger version` | Show the build version. |
| `lifeledger help` | Show the help text. |

## Project structure

```text
cmd/lifeledger/     # Binary entry point and commands
internal/config/    # Settings from environment variables and .env
internal/server/    # HTTP server and routes
internal/version/   # Build version, set at build time
```

## Troubleshooting

| Problem | Cause | Solution |
|---|---|---|
| `address already in use` | A different process uses port 8080. | Set `LIFELEDGER_ADDR=:8081` in `.env`, or stop the other process. |
| `go: go.mod requires go >= 1.24` | Your Go version is too old. | Install Go 1.24 or later. |
| `config error: LIFELEDGER_ENV must be "dev" or "prod"` | A setting has a value that is not permitted. | Correct the value in `.env` or in the environment. |
| Values in `.env` have no effect | You started the binary from a different directory. | Start it from the repository root, or use `-env-file`. |

## Related documentation

- [LICENSE](LICENSE): MIT license.
- [.env.example](.env.example): all settings with comments.

## License

[MIT](LICENSE)
