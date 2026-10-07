# LifeLedger

LifeLedger is a private life-admin agent. It keeps a record of your bills, renewals, warranties, documents, and deadlines. It reminds you before each due date. It acts for you, and you approve each high-risk action.

## Overview

**Why it exists.** Life admin is a set of small dates and documents that are easy to forget: a card renewal, a warranty end date, an insurance premium. A missed date costs money or time. LifeLedger remembers these items and tells you about them on time.

**Who uses it.** One person who wants a private assistant for household and personal admin. You own your data. You can see, edit, and delete each item that LifeLedger remembers.

LifeLedger is an entry in the Personal AI track of the [Nebius x NVIDIA Global AI Hackathon](https://nebiusglobalaihackathon.devpost.com/).

## Key features

- NVIDIA Nemotron 3 models (Nano, Super, and Ultra) on Nebius Token Factory, with tool calling.
- Automatic retry and fallback to a smaller model when a model is unavailable.
- A daily spend limit that stops model calls before the account runs out of credit.
- HTTP service with a health endpoint for container hosts.

## Architecture

```text
Client ──HTTP──▶ lifeledger serve (Go)
                   └── GET /healthz

lifeledger ask ──▶ internal/llm ──HTTPS──▶ Nebius Token Factory
                   (tiers, retry,          ├── Nemotron 3 Nano
                    fallback, budget)      ├── Nemotron 3 Super
                                           └── Nemotron 3 Ultra
```

The service is one Go binary. Each command of the binary is a separate entry point. For the model tier decisions, see [ADR 0001](docs/adr/0001-model-tiers-and-spend-control.md).

## Prerequisites

- Go 1.24 or later.
- GNU Make.
- Git.
- A Nebius Token Factory account and API key. Create the key at [tokenfactory.nebius.com](https://tokenfactory.nebius.com).

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

3. Open `.env` and set `NEBIUS_API_KEY`.

4. Make sure that the configured models are in the Token Factory catalog:

   ```sh
   make build
   ./bin/lifeledger models
   ```

   The command marks each configured model with `*`. If a configured model is not in the catalog, the command shows an error.

5. Build the binary and start the server:

   ```sh
   make run
   ```

6. In a second terminal, send a request to the health endpoint:

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
| `NEBIUS_API_KEY` | none | Token Factory API key. The model commands need it. |
| `NEBIUS_BASE_URL` | `https://api.tokenfactory.nebius.com/v1/` | Token Factory endpoint. |
| `LIFELEDGER_MODEL_NANO` | `nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B` | Model for fast, low-cost calls. |
| `LIFELEDGER_MODEL_SUPER` | `nvidia/nemotron-3-super-120b-a12b` | Default model. |
| `LIFELEDGER_MODEL_ULTRA` | `nvidia/Nemotron-3-Ultra-550b-a55b` | Model for complex reasoning. |
| `LIFELEDGER_DAILY_BUDGET_USD` | `2` | Maximum model spend for each UTC day. `0` means no limit. |

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
| `make test` | Run all tests with the race detector. These tests do not call Token Factory. |
| `make test-live` | Run the tests that call Token Factory. They need `NEBIUS_API_KEY` in `.env` and cost a few cents. |
| `make check` | Run the format check, `go vet`, and the tests. Run it before each commit. |
| `make help` | Show all targets. |

Binary commands:

| Command | Purpose |
|---|---|
| `lifeledger serve` | Start the HTTP server. |
| `lifeledger models` | Show the Token Factory catalog and check the configured models. |
| `lifeledger ask [-tier nano\|super\|ultra] PROMPT` | Send one prompt to a Nemotron model. The default tier is `super`. |
| `lifeledger version` | Show the build version. |
| `lifeledger help` | Show the help text. |

## Project structure

```text
cmd/lifeledger/     # Binary entry point and commands
internal/config/    # Settings from environment variables and .env
internal/llm/       # Token Factory client: tiers, retry, fallback, spend limit
internal/server/    # HTTP server and routes
internal/version/   # Build version, set at build time
docs/adr/           # Architecture decision records
```

## Troubleshooting

| Problem | Cause | Solution |
|---|---|---|
| `address already in use` | A different process uses port 8080. | Set `LIFELEDGER_ADDR=:8081` in `.env`, or stop the other process. |
| `go: go.mod requires go >= 1.24` | Your Go version is too old. | Install Go 1.24 or later. |
| `config error: LIFELEDGER_ENV must be "dev" or "prod"` | A setting has a value that is not permitted. | Correct the value in `.env` or in the environment. |
| Values in `.env` have no effect | You started the binary from a different directory. | Start it from the repository root, or use `-env-file`. |
| `llm: NEBIUS_API_KEY is not set` | `.env` has no API key. | Set `NEBIUS_API_KEY` in `.env`. |
| `token factory returned 401` | The API key is not correct or it is expired. | Make a new key in Token Factory. |
| `Token Factory account is out of credits` | The account balance is zero (HTTP 402). | Add credit or apply a promo code in Token Factory. |
| `daily spend limit reached` | The spend for this UTC day is at `LIFELEDGER_DAILY_BUDGET_USD`. | Increase the limit, or wait until 00:00 UTC. |
| `configured ... model ... is not in the Token Factory catalog` | The model ID is not correct. Model IDs are case-sensitive. | Run `lifeledger models` and copy the exact ID into `.env`. |

## Related documentation

- [LICENSE](LICENSE): MIT license.
- [.env.example](.env.example): all settings with comments.
- [ADR 0001](docs/adr/0001-model-tiers-and-spend-control.md): model tiers, fallback, and spend control.
- [Token Factory documentation](https://docs.tokenfactory.nebius.com/quickstart).

## License

[MIT](LICENSE)
