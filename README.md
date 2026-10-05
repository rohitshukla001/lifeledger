# LifeLedger

LifeLedger is a private life-admin agent. It keeps a record of your bills, renewals, warranties, documents, and deadlines. It reminds you on a schedule. It acts for you: it drafts emails, makes calendar events, and checks facts on the web. You can see and edit everything that it remembers. You must approve each high-risk action.

LifeLedger is an entry in the Personal AI track of the [Nebius x NVIDIA Global AI Hackathon](https://nebiusglobalaihackathon.devpost.com/).

> **Status:** Work in progress. This README grows with each build task.

## Technology

| Part | Technology |
|---|---|
| Models | NVIDIA Nemotron open models (added in Task 2) |
| Inference | Nebius Token Factory (added in Task 2) |
| Hosting | Nebius Serverless (added in Task 12) |
| Backend | Go 1.24 |
| Web UI | Angular (added in Task 11) |

## Requirements

- Go 1.24 or later.
- GNU Make.

## Quick start

1. Copy the example settings file:

   ```sh
   cp .env.example .env
   ```

2. Build the binary and start the server:

   ```sh
   make run
   ```

3. In a second terminal, send a request to the health endpoint:

   ```sh
   curl -s localhost:8080/healthz
   ```

   The server sends this response:

   ```json
   {"status":"ok","version":"dev (none)","uptime_seconds":0}
   ```

4. Push `Ctrl+C` to stop the server.

## Configuration

LifeLedger reads its settings from environment variables. For local work, it also reads a `.env` file. A variable in the process environment has priority over the same variable in `.env`.

| Variable | Default | Description |
|---|---|---|
| `LIFELEDGER_ADDR` | `:8080` | HTTP listen address. If it is empty, LifeLedger uses `PORT`. |
| `LIFELEDGER_ENV` | `dev` | `dev` gives text logs. `prod` gives JSON logs. |
| `LIFELEDGER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |

To use a different settings file, use the `-env-file` flag:

```sh
./bin/lifeledger -env-file ./local.env serve
```

## Commands

| Command | Description |
|---|---|
| `lifeledger serve` | Start the HTTP server. |
| `lifeledger version` | Show the build version. |
| `lifeledger help` | Show the help text. |

## Development

| Make target | Description |
|---|---|
| `make build` | Build the binary into `./bin`. |
| `make run` | Build the binary and start the server. |
| `make test` | Run all tests with the race detector. |
| `make check` | Do the format check, `go vet`, and the tests. Do this before each commit. |
| `make help` | Show all targets. |

## Security

Do not put API keys in the repository. Keep keys in `.env`. Git ignores `.env`.

## License

[MIT](LICENSE)
