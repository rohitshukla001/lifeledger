# ADR 0011: Container image and Nebius Serverless deployment

- Status: Accepted, amended 2026-10-25
- Date: 2026-10-21

## Context

The hackathon needs a public demo URL that stays live until judging ends. The rules accept a deployment on Nebius AI Cloud Serverless. LifeLedger is one Go binary with a built web app and a SQLite database. It calls Token Factory, Tavily, and Telegram over HTTPS. It does not need a GPU.

## Decision

1. One multi-stage `Dockerfile` builds the image:
   1. `node:24-bookworm-slim` builds the web app. This stage runs on the build platform, because its output does not depend on the CPU.
   2. `golang:1.24-bookworm` builds the binary with CGO, which the SQLite driver needs.
   3. `gcr.io/distroless/cc-debian12:nonroot` runs the binary. It has glibc, CA certificates, and no shell. The process runs as user 65532. The builder and the runtime both use Debian 12, so the glibc versions agree. Time zone data is in the binary (`time/tzdata`).
2. The image sets `LIFELEDGER_ENV=prod`. Thus the server does not start without `LIFELEDGER_API_TOKEN`, and the logs are JSON.
3. The `ci` GitHub Actions workflow runs the Go checks and the web checks on each push and pull request. On a push to `main` or to a `v*` tag, it builds a `linux/amd64` image and pushes it to GitHub Container Registry. Thus a developer on an Apple Silicon Mac does not have to build an `amd64` image locally.
4. LifeLedger runs as one Nebius Serverless AI **endpoint** on `cpu-d3` with the `2vcpu-8gb` preset and a 20 GiB disk. The endpoint serves the web app, the API, and the sweep.
5. The endpoint has no Nebius token auth (`--auth`). Nebius token auth asks for a bearer token on each request, which a browser cannot send when it loads a page. LifeLedger checks its own token on each API request (ADR 0009). The static web app has no data in it.
6. We do not use a Serverless **job** for the sweep. The sweep already runs in the endpoint process every 15 minutes. A job runs on a different VM, so it would need the SQLite file on a shared network filesystem with two writers. SQLite does not support that safely.
7. The database is on the container disk. A stop or a restart of the endpoint removes it. For the demo, `LIFELEDGER_SEED_DEMO=true` adds the demo data again when the database is empty. The demo data uses dates relative to the current day, so it always shows overdue, due-soon, and later items.
8. `scripts/smoke.sh` checks a deployed URL: health, web app, security headers, token check, the main API lists, and (with `--chat`) one real agent reply.

## Consequences

- One `nebius ai endpoint create` command deploys the demo. An image push and `nebius ai endpoint restart` update it with the same URL.
- Personal data on the demo endpoint does not survive a restart. A deployment for daily use needs persistent storage. A Nebius shared filesystem mounted at `/data` is a candidate, but we did not test SQLite locking on it.
- The endpoint runs all the time, so it costs compute hours until it is stopped. The daily model budget limits the Token Factory cost.
- The distroless image has no shell. Use `nebius ai endpoint logs` to debug.

## Amendment (2026-10-25): demo hosting on Render

### Context

Nebius AI Cloud asks for a prepaid balance before it creates resources. The free trial program stopped in July 2026. Without a balance, `nebius ai endpoint create` fails with `PermissionDenied`, and the console suspends AI Cloud resources. Token Factory works without that balance and uses the hackathon credits.

The rules accept a project that makes a runtime call to the Token Factory inference API. A deployment on Nebius AI Cloud is one option, not a requirement. The rules require a live demo URL until judging ends.

### Decision

1. The live demo runs on the Render free plan, as a web service from the image `ghcr.io/rohitshukla001/lifeledger:latest`.
2. All model calls go to Nebius Token Factory with NVIDIA Nemotron 3 models. This does not change.
3. `PORT=8080` sends Render traffic to the server port. The health check path is `/healthz`.
4. `LIFELEDGER_DAILY_BUDGET_USD=1` keeps the Token Factory cost inside the hackathon credits until judging ends.
5. The Nebius Serverless runbook stays in the repository for users who have an AI Cloud balance.

### Consequences

- The demo costs nothing to host.
- After 15 minutes with no requests, Render stops the app. The next request waits approximately one minute. The sweep does not run while the app is stopped.
- Each start removes the data, and the demo data comes back. A judge's changes do not stay after the app stops.
- Render does not deploy a new image automatically. After CI publishes an image, select **Manual Deploy > Deploy latest reference**.

