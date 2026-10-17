# LifeLedger

LifeLedger is a private life-admin agent. It keeps a record of your bills, renewals, warranties, documents, and deadlines. It reminds you before each due date. It acts for you, and you approve each high-risk action.

## Overview

**Why it exists.** Life admin is a set of small dates and documents that are easy to forget: a card renewal, a warranty end date, an insurance premium. A missed date costs money or time. LifeLedger remembers these items and tells you about them on time.

**Who uses it.** One person who wants a private assistant for household and personal admin. You own your data. You can see, edit, and delete each item that LifeLedger remembers.

LifeLedger is an entry in the Personal AI track of the [Nebius x NVIDIA Global AI Hackathon](https://nebiusglobalaihackathon.devpost.com/).

## Key features

- A chat agent on NVIDIA Nemotron 3 Super that records and updates your obligations through tool calls and answers from stored data only.
- Memory across conversations: each new conversation starts with the facts that are relevant to your message.
- Document ingest: give it an email or a PDF bill, and Nemotron 3 Nano extracts the due dates and amounts as proposals that you accept or reject.
- Actions: the agent marks bills paid (a recurring bill moves to its next due date), snoozes reminders, and makes calendar files at once. It drafts emails, but sends an email only after you approve it.
- Web research with Tavily: the agent finds current public facts, such as official deadlines and provider policies, gives the source URL for each fact, and can save a fact to memory with its source. It does not put your personal data in search queries.
- An audit log of each action: who asked for it, who approved it, and the result.
- NVIDIA Nemotron 3 models (Nano, Super, and Ultra) on Nebius Token Factory.
- Automatic retry and fallback to a smaller model when a model is unavailable.
- A daily spend limit that stops model calls before the account runs out of credit.
- Memory that you can see, edit, and delete. Recall finds memories by meaning (embeddings) and by keywords, and it still works without network access.
- Local SQLite storage for obligations, memories, and conversations, with automatic schema migrations.
- HTTP service with a health endpoint that also checks the database.

## Architecture

```text
Client ──HTTP──▶ lifeledger serve (Go) ──▶ internal/store ──▶ SQLite (data/lifeledger.db)
                   └── GET /healthz

lifeledger ingest ──▶ internal/ingest ──▶ Nemotron 3 Nano (JSON Schema output)
   (email, PDF)          │  validate, dedupe
                         └──▶ proposals ──accept──▶ obligations

lifeledger chat ──▶ internal/agent ──▶ Nemotron 3 Super (tool calls, up to 8 steps)
                      │  tools: remember, recall, add_obligation, list_obligations,
                      │         update_obligation, mark_paid, snooze_obligation,
                      │         add_calendar_reminder, draft_email, web_search
                      ├──▶ internal/tavily ──HTTPS──▶ Tavily Search API
                      ├──▶ internal/memory
                      ├──▶ internal/actions ──▶ policy: run now, or wait for approval
                      │                      ├──▶ SMTP (approved emails only)
                      │                      └──▶ audit log
                      └──▶ internal/store

lifeledger ask ──▶ internal/llm ──HTTPS──▶ Nebius Token Factory
                   (tiers, retry,          ├── Nemotron 3 Nano
                    fallback, budget)      ├── Nemotron 3 Super
                                           ├── Nemotron 3 Ultra
                                           └── Qwen3 Embedding 8B
                        ▲
lifeledger memory ──▶ internal/memory ──▶ internal/store ──▶ SQLite
                      (remember, recall,
                       edit, forget)
```

The service is one Go binary. Each command of the binary is a separate entry point. For the model tier decisions, see [ADR 0001](docs/adr/0001-model-tiers-and-spend-control.md). For the storage decisions, see [ADR 0002](docs/adr/0002-sqlite-storage.md). For memory recall, see [ADR 0003](docs/adr/0003-memory-recall.md). For the agent loop, see [ADR 0004](docs/adr/0004-agent-loop.md). For document ingest, see [ADR 0005](docs/adr/0005-document-ingest.md). For actions and approvals, see [ADR 0006](docs/adr/0006-actions-and-approvals.md). For web research, see [ADR 0007](docs/adr/0007-web-research.md).

## Prerequisites

- Go 1.24.1 or later, with CGO enabled (the default).
- A C compiler for the SQLite driver. On macOS, install the Xcode Command Line Tools with `xcode-select --install`.
- GNU Make.
- Git.
- A Nebius Token Factory account and API key. Create the key at [tokenfactory.nebius.com](https://tokenfactory.nebius.com).
- Optional: a Tavily API key for web research. Create the key at [app.tavily.com](https://app.tavily.com).

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

3. Open `.env` and set `NEBIUS_API_KEY`. To turn on web research, also set `TAVILY_API_KEY`.

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
   {"status":"ok","database":"ok","version":"dev (none)","uptime_seconds":0}
   ```

   On the first start, LifeLedger makes the database file at `data/lifeledger.db`.

## Configuration

LifeLedger reads its settings from environment variables. It also reads a `.env` file from the current directory. A variable in the process environment has priority over the same variable in `.env`.

| Variable | Default | Purpose |
|---|---|---|
| `LIFELEDGER_ADDR` | `:8080` | HTTP listen address. If it is empty, LifeLedger uses `PORT`. |
| `PORT` | none | Listen port that container hosts set. LifeLedger uses it only if `LIFELEDGER_ADDR` is empty. |
| `LIFELEDGER_ENV` | `dev` | `dev` gives text logs. `prod` gives JSON logs. |
| `LIFELEDGER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `LIFELEDGER_DB_PATH` | `data/lifeledger.db` | SQLite database file. LifeLedger makes the directory if it does not exist. |
| `LIFELEDGER_TIMEZONE` | `UTC` | IANA time zone of the user, for example `Asia/Kolkata`. It sets the date "today" for the agent. |
| `LIFELEDGER_CURRENCY` | `USD` | Default ISO 4217 currency for amounts without a currency, for example `INR`. |
| `NEBIUS_API_KEY` | none | Token Factory API key. The model commands need it. |
| `NEBIUS_BASE_URL` | `https://api.tokenfactory.nebius.com/v1/` | Token Factory endpoint. |
| `LIFELEDGER_MODEL_NANO` | `nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B` | Model for fast, low-cost calls. |
| `LIFELEDGER_MODEL_SUPER` | `nvidia/nemotron-3-super-120b-a12b` | Default model. |
| `LIFELEDGER_MODEL_ULTRA` | `nvidia/Nemotron-3-Ultra-550b-a55b` | Model for complex reasoning. |
| `LIFELEDGER_MODEL_EMBED` | `Qwen/Qwen3-Embedding-8B` | Embedding model for memory recall. |
| `LIFELEDGER_DAILY_BUDGET_USD` | `2` | Maximum model spend for each UTC day. `0` means no limit. |
| `TAVILY_API_KEY` | none | Tavily API key. If it is empty, the agent has no `web_search` tool. |
| `TAVILY_BASE_URL` | `https://api.tavily.com` | Tavily endpoint. |
| `LIFELEDGER_SMTP_HOST` | none | SMTP server for approved emails. If it is empty, email is off. |
| `LIFELEDGER_SMTP_PORT` | `587` | `587` uses STARTTLS. `465` uses TLS. |
| `LIFELEDGER_SMTP_USERNAME` | none | SMTP user name. If it is empty, LifeLedger does not log in. |
| `LIFELEDGER_SMTP_PASSWORD` | none | SMTP password. For Gmail, use an app password. |
| `LIFELEDGER_SMTP_FROM` | none | Sender address, for example `Rohit <rohit@example.com>`. Necessary when `LIFELEDGER_SMTP_HOST` is set. |

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
| `make test-live` | Run the tests that call Token Factory and Tavily, including a two-conversation agent test, an email ingest test, and a web research test. They need `NEBIUS_API_KEY` in `.env` and cost a few cents. The Tavily tests use about 2 credits and skip if `TAVILY_API_KEY` is empty. |
| `make check` | Run the format check, `go vet`, and the tests. Run it before each commit. |
| `make help` | Show all targets. |

Binary commands:

| Command | Purpose |
|---|---|
| `lifeledger serve` | Start the HTTP server. |
| `lifeledger models` | Show the Token Factory catalog and check the configured models. |
| `lifeledger ask [-tier nano\|super\|ultra] PROMPT` | Send one prompt to a Nemotron model. The default tier is `super`. |
| `lifeledger chat [-c ID]` | Talk to the agent in the terminal. `-c` continues a conversation. Type `/new` for a new conversation, `/approve ID` or `/reject ID` to decide an action, and `/quit` to exit. |
| `lifeledger actions` | Show the actions that wait for approval. |
| `lifeledger actions list [-n N]` | Show recent actions. |
| `lifeledger actions show ID` | Show one action with its payload and result. |
| `lifeledger actions approve ID...` | Approve and run actions. |
| `lifeledger actions reject ID...` | Reject actions. |
| `lifeledger actions ics ID [-o FILE]` | Write the calendar file of a calendar action. |
| `lifeledger audit [-n N]` | Show the audit log, oldest first. |
| `lifeledger ingest add [-name NAME] FILE` | Extract obligations from an email, text, or PDF file as proposals. Use `-` to read from stdin. |
| `lifeledger ingest pending` | Show the proposals that wait for a decision. |
| `lifeledger ingest accept ID...` | Turn proposals into obligations. |
| `lifeledger ingest reject ID...` | Discard proposals. |
| `lifeledger memory add [-kind fact\|preference\|note] TEXT` | Save a memory. |
| `lifeledger memory list` | Show all memories, newest first. |
| `lifeledger memory recall [-n N] QUERY` | Show the memories that match a query, with a score. |
| `lifeledger memory edit ID TEXT` | Change the text of a memory. |
| `lifeledger memory forget ID` | Delete a memory permanently. |
| `lifeledger memory reindex` | Make embeddings for the memories that have none. |
| `lifeledger search [-topic T] [-range R] [-n N] [-site D] QUERY` | Search the web with Tavily and show the answer and the sources. `T` is `general`, `news`, or `finance`. `R` is `day`, `week`, `month`, or `year`. `D` is a comma-separated list of domains. |
| `lifeledger version` | Show the build version. |
| `lifeledger help` | Show the help text. |

## Project structure

```text
cmd/lifeledger/     # Binary entry point and commands
internal/actions/   # Actions: approval policy, executors, iCalendar, SMTP, audit
internal/agent/     # Agent loop, system prompt, and tools
internal/ingest/    # Email and PDF extraction into proposals
internal/money/     # Amount parsing and formatting in minor units
internal/config/    # Settings from environment variables and .env
internal/llm/       # Token Factory client: chat, embeddings, retry, fallback, spend limit
internal/memory/    # Memory engine: remember, recall, edit, forget, reindex
internal/store/     # SQLite storage, repositories, and SQL migrations
internal/tavily/    # Tavily Search API client with retry
internal/server/    # HTTP server and routes
internal/version/   # Build version, set at build time
docs/adr/           # Architecture decision records
```

## Troubleshooting

| Problem | Cause | Solution |
|---|---|---|
| `address already in use` | A different process uses port 8080. | Set `LIFELEDGER_ADDR=:8081` in `.env`, or stop the other process. |
| `go: go.mod requires go >= 1.24.1` | Your Go version is too old. | Install Go 1.24.1 or later. |
| `go-sqlite3 requires cgo to work` | The binary was built with `CGO_ENABLED=0`. | Build with `CGO_ENABLED=1` and a C compiler installed. |
| The first build or test run takes about one minute | Go compiles the SQLite C code one time. | Wait. Later builds use the build cache. |
| `cannot open database` at startup | The directory of `LIFELEDGER_DB_PATH` cannot be written. | Set `LIFELEDGER_DB_PATH` to a path that you can write to. |
| `/healthz` returns `503` | The database does not respond. | Read the server log for `database ping failed`. |
| `using keyword matching only` | `NEBIUS_API_KEY` is not set. Recall cannot use embeddings. | Set `NEBIUS_API_KEY`, then run `lifeledger memory reindex`. |
| `memory saved without embedding` in the log | The embedding call failed. The memory is saved. | Run `lifeledger memory reindex` when Token Factory is available. |
| Recall misses memories after a change of `LIFELEDGER_MODEL_EMBED` | The stored embeddings are from the old model. | Run `lifeledger memory reindex`. |
| The agent uses the wrong date for "today" or "tomorrow" | `LIFELEDGER_TIMEZONE` is not your time zone. | Set `LIFELEDGER_TIMEZONE`, for example `Asia/Kolkata`. |
| `I could not finish that` | The model used all 8 tool steps without an answer. | Send a shorter request with one task in it. |
| `the PDF may be a scanned image` | The PDF has no text layer. | Use a PDF with selectable text, or paste the text with `ingest add -`. |
| `the model did not return valid JSON` | The extraction reply was not JSON. | Run the command again. If it fails again, set `LIFELEDGER_LOG_LEVEL=debug` and read the log. |
| `already ingested as #N` | The same document was ingested before. | Use `ingest pending` to see its open proposals. |
| `email is not configured` | An email was approved, but `LIFELEDGER_SMTP_HOST` is empty. | Set the SMTP settings, then ask the agent to draft the email again. |
| `smtp auth: 535` | The SMTP user name or password is not correct. | Check the settings. For Gmail, make an app password. |
| `action #N was already approved or rejected` | Each action can be decided one time only. | Use `actions list` to see its status. |
| `tavily: TAVILY_API_KEY is not set` | `.env` has no Tavily key. | Set `TAVILY_API_KEY` in `.env`. Chat works without it, but has no web research. |
| `tavily: API returned 401` | The Tavily key is not correct. | Make a new key at app.tavily.com. |
| `tavily: API returned 432` or `433` | The Tavily plan or usage limit is reached. | Wait for the monthly reset, or change the plan. |
| `search with public terms only` | The agent put a phone number, account number, or email address in a search query. LifeLedger stopped the query. | No action is necessary. The agent tries again without the personal data. |
| `source_url must be a URL that web_search returned` | The agent tried to save a web fact with a source from a different conversation or no search. | Ask the agent to search again in the same conversation. |
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
- [ADR 0002](docs/adr/0002-sqlite-storage.md): SQLite storage and migrations.
- [ADR 0003](docs/adr/0003-memory-recall.md): memory recall with embeddings and keyword match.
- [ADR 0004](docs/adr/0004-agent-loop.md): agent loop with tool calls.
- [ADR 0005](docs/adr/0005-document-ingest.md): document ingest with proposals.
- [ADR 0006](docs/adr/0006-actions-and-approvals.md): actions, approval policy, and audit log.
- [ADR 0007](docs/adr/0007-web-research.md): web research with Tavily and cited memories.
- [Token Factory documentation](https://docs.tokenfactory.nebius.com/quickstart).
- [Tavily Search API](https://docs.tavily.com/documentation/api-reference/endpoint/search).

## License

[MIT](LICENSE)
