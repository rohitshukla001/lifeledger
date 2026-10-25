# Deploy LifeLedger on Nebius Serverless

This runbook deploys LifeLedger as a Nebius Serverless AI endpoint on a CPU-only VM. The endpoint serves the web app, the API, and the sweep from one container. For the design decisions, see [ADR 0011](adr/0011-deployment.md).

## Before you start

You need:

- A Nebius AI Cloud project with the `editor` role, and quota for one Compute VM and one VPC allocation.
- A prepaid balance in Nebius AI Cloud. Without it, `nebius ai endpoint create` fails with `PermissionDenied`. Token Factory credits do not pay for AI Cloud compute.
- The Nebius CLI. Install it with `curl -sSL https://artifacts.nebius.cloud/cli/install.sh | bash`, then run `exec -l $SHELL` and `nebius version`.
- A CLI profile for the project: `nebius profile create`. Follow the prompts and select the project.
- `jq`, for example `brew install jq` on macOS.
- A Token Factory API key (`NEBIUS_API_KEY`). Optional: a Tavily key and a Telegram bot.

## 1. Get the container image

Use one of these two options.

### Option A: GitHub Container Registry (recommended)

The `ci` workflow builds a `linux/amd64` image on each push to `main` and pushes it to `ghcr.io/rohitshukla001/lifeledger`.

1. Push your changes to `main` and wait until the `ci` workflow is green.
2. On GitHub, open **Packages > lifeledger > Package settings** and set the visibility to **Public**. Nebius can then pull the image without credentials.
3. Use this image name in step 3: `ghcr.io/rohitshukla001/lifeledger:latest`.

### Option B: Nebius Container Registry

Use this option if you do not want a public image. The build runs on your computer, so it needs Docker with `linux/amd64` support.

```sh
export REGION_ID=<region_ID>
export REGISTRY_PATH=$(nebius registry create --name lifeledger --format json | jq -r ".metadata.id" | cut -d- -f 2)
nebius registry configure-helper

make docker IMAGE=cr.$REGION_ID.nebius.cloud/$REGISTRY_PATH/lifeledger:latest
docker push cr.$REGION_ID.nebius.cloud/$REGISTRY_PATH/lifeledger:latest
```

An image in Container Registry of the same project needs no credentials in step 3.

## 2. Prepare the settings

Make a new API token for the deployment. Do not use the token of your local `.env`.

```sh
export LIFELEDGER_API_TOKEN=$(openssl rand -hex 32)
export NEBIUS_API_KEY=<your Token Factory key>
export TAVILY_API_KEY=<your Tavily key, or leave it empty>
export SUBNET_ID=$(nebius vpc subnet list --format json | jq -r '.items[0].metadata.id')
echo "$SUBNET_ID"
```

If the project has more than one subnet, find the correct ID with `nebius vpc subnet list`.

Keep `LIFELEDGER_API_TOKEN` in your password manager. You need it to open the web app.

## 3. Create the endpoint

```sh
nebius ai endpoint create \
  --name lifeledger \
  --image ghcr.io/rohitshukla001/lifeledger:latest \
  --container-port 8080 \
  --platform cpu-d3 \
  --preset 2vcpu-8gb \
  --disk-size 20Gi \
  --subnet-id "$SUBNET_ID" \
  --env "LIFELEDGER_API_TOKEN=$LIFELEDGER_API_TOKEN,NEBIUS_API_KEY=$NEBIUS_API_KEY,TAVILY_API_KEY=$TAVILY_API_KEY,LIFELEDGER_TIMEZONE=Asia/Kolkata,LIFELEDGER_CURRENCY=INR,LIFELEDGER_SEED_DEMO=true,LIFELEDGER_DAILY_BUDGET_USD=3"
```

Notes:

- Do not add `--auth token`. With that flag, Nebius asks for its own token before each request, and a browser cannot open the web app. LifeLedger checks `LIFELEDGER_API_TOKEN` on each API request.
- `--public` is not necessary. The endpoint gets a managed HTTPS URL without it.
- To keep the keys out of the endpoint settings, store them in a Nebius secret and use `--env-secret KEY=<secret selector>` instead of `--env` for those keys.
- `LIFELEDGER_SEED_DEMO=true` adds the demo data when the database is empty. Remove it for personal use.
- To get reminders on Telegram, add `TELEGRAM_BOT_TOKEN=...,TELEGRAM_CHAT_ID=...` to `--env`.

The endpoint starts in approximately five minutes. Monitor it:

```sh
export ENDPOINT_ID=$(nebius ai endpoint get-by-name --name lifeledger --format jsonpath='{.metadata.id}')
nebius ai endpoint get "$ENDPOINT_ID" --format json | jq -r '.status.state'
nebius ai endpoint logs "$ENDPOINT_ID"
```

Wait until the state is `RUNNING` and the log shows `http server listening`.

## 4. Check the deployment

```sh
export ENDPOINT_URL=$(nebius ai endpoint get "$ENDPOINT_ID" --format json \
  | jq -r '.status.public_endpoints[] | select(startswith("https://"))' | head -1)
echo "$ENDPOINT_URL"

scripts/smoke.sh "$ENDPOINT_URL" --chat
```

The script checks the health endpoint, the web app, the security headers, the token check, and the main API lists. With `--chat`, it also sends one message to Nemotron 3 Super. This costs less than one cent. All lines must show `ok`.

Then open the URL in a browser and enter the API token.

## Update the deployment

You cannot change the settings of an endpoint after you create it.

- **New image with the same tag:** push the image, then run `nebius ai endpoint restart --id "$ENDPOINT_ID"`. The endpoint pulls the image again and keeps its URL.
- **New settings:** delete the endpoint and create it again. The new endpoint gets a new URL.

## Data and cost

- The database is on the container disk. A stop or a restart removes it. With `LIFELEDGER_SEED_DEMO=true`, the demo data comes back at the next start.
- `cpu-d3` with `2vcpu-8gb` costs approximately USD 0.066 for each hour (2 × 0.015 + 8 × 0.0045, from the Nebius Compute price list of October 2026), or approximately USD 48 for each month. The 20 GiB disk costs approximately USD 1.40 for each month. Check the current prices before you deploy.
- Token Factory calls are charged separately. `LIFELEDGER_DAILY_BUDGET_USD` stops model calls when the day's spend reaches the limit.
- To stop the compute charge, run `nebius ai endpoint stop --id "$ENDPOINT_ID"`. The URL stays the same, but the data is removed. To delete the endpoint, run `nebius ai endpoint delete "$ENDPOINT_ID"`.

## Troubleshooting

| Problem | Cause | Solution |
|---|---|---|
| `nebius ai endpoint create` fails with `PermissionDenied` | AI Cloud has no prepaid balance, or the console shows that AI Cloud resources are suspended. | Top up the balance in **Billing**, or use a different container host (see the README). |
| The state is `ERROR` with `NotEnoughResources` | No `cpu-d3` capacity in the region. | Delete the endpoint and create it again later, or use a different region. |
| The state stays `IMAGE_PULLING`, then fails | Nebius cannot pull the image. | Make the GHCR package public, or use Container Registry in the same project. |
| `exec format error` in the logs | The image is not `linux/amd64`. | Build with `make docker`, which sets `--platform linux/amd64`, or use the CI image. |
| `config error: LIFELEDGER_API_TOKEN is required` in the logs | The token is missing in `--env`. | Create the endpoint again with `LIFELEDGER_API_TOKEN` in `--env`. |
| The web app shows `The server did not accept this token` | The token is not the one in the endpoint settings. | Enter the token from step 2. |
| `model features are off` in the logs | `NEBIUS_API_KEY` is missing. Chat and import return `503`. | Create the endpoint again with the key. |
| `scripts/smoke.sh` fails on `agent reply` with HTTP 503 | The model key is missing, or the daily budget is used. | Read the error message in the script output and the endpoint logs. |
