# ADR 0002: SQLite storage with embedded migrations

- Status: Accepted
- Date: 2026-10-07

## Context

LifeLedger keeps the data of one person: obligations, memories, and conversations. The data volume is small, a few thousand rows. The service runs as one container on Nebius Serverless. The data must stay on the user's own instance.

## Decision

1. LifeLedger uses SQLite in one file. The default path is `data/lifeledger.db`.
2. The driver is `github.com/mattn/go-sqlite3`. It is the most used Go SQLite driver, and it has no Go dependencies. It needs CGO and a C compiler at build time.
3. The connection uses WAL journal mode, foreign keys, a 5 second busy timeout, and immediate transactions. Immediate transactions prevent lock upgrade failures when two writers start at the same time.
4. SQL migration files are embedded in the binary. `store.Open` applies new migrations at startup, each in its own transaction. The `schema_migrations` table records each applied version.
5. CHECK constraints in the schema enforce the permitted values for category, status, recurrence, currency, and dates.
6. Calendar dates (`due_on`, `snoozed_until`) are stored as `YYYY-MM-DD` text, with no time zone. Timestamps are stored as Unix milliseconds.
7. Money is stored as an integer in minor units with an ISO 4217 currency code.
8. "Forget" is a hard delete. LifeLedger does not keep deleted personal data.

## Consequences

- No database server is necessary. A backup is a copy of one file.
- The build needs a C compiler: Xcode Command Line Tools on macOS, and `gcc` in the Docker build stage. The runtime image needs glibc.
- The first build compiles SQLite and takes about one minute. Later builds use the Go build cache.
- One SQLite file supports one instance of the service. More instances need a different database.
- A pure-Go driver (`modernc.org/sqlite`) can replace the driver with a change to the driver name and the DSN only. The SQL does not change.
