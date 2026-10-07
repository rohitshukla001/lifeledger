# ADR 0001: Nemotron model tiers, fallback, and spend control

- Status: Accepted
- Date: 2026-10-05

## Context

LifeLedger uses NVIDIA Nemotron models on Nebius Token Factory. Token Factory has three Nemotron 3 sizes. They have different cost and quality:

| Tier | Model | Input, USD per 1M tokens | Output, USD per 1M tokens |
|---|---|---|---|
| Nano | `nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B` | 0.06 | 0.24 |
| Super | `nvidia/nemotron-3-super-120b-a12b` | 0.30 | 0.90 |
| Ultra | `nvidia/Nemotron-3-Ultra-550b-a55b` | 1.00 | 3.00 |

The service must stay available during judging (1 to 15 December 2026). Token Factory sends HTTP 402 when the account has no credit. Token Factory has no spend limit on the account.

## Decision

1. Each call selects a tier, not a model ID. Configuration maps each tier to a model ID.
2. The default tier is Super. Extraction and short text use Nano. Planning and long reviews use Ultra.
3. The client retries HTTP 429, HTTP 5xx, and network errors up to 3 times. It uses exponential backoff with jitter. It obeys `Retry-After`.
4. After the retries fail, the client tries the next lower tier: Ultra to Super, then Super to Nano.
5. The client does not retry or fall back on other 4xx errors, HTTP 402, or a cancelled context.
6. The client keeps a daily spend limit in USD. The limit resets at 00:00 UTC. The client calculates cost from token usage and the price table. An unknown model ID gets the Ultra price.

## Consequences

- One configuration change replaces a model. No code change is necessary.
- A short Ultra outage gives Super answers, not errors.
- The spend limit stops calls before the account runs out of credit. The limit is kept in memory, so a restart sets it to zero.
- The price table is in code. When Token Factory changes its prices, the table must be updated.
