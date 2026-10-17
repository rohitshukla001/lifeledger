# ADR 0007: Web research with Tavily and cited memories

- Status: Accepted
- Date: 2026-10-17

## Context

Some life-admin questions need facts that change and that are not in the user's data: an official tax deadline, a provider's late-fee policy, a new plan price, a product recall. A model must not guess these facts. The user must be able to check each fact. Web text is written by third parties, so it can contain false facts and prompt injection. A search query leaves the system, so it must not carry the user's personal data.

## Decision

1. LifeLedger uses the Tavily Search API (`POST /search`) for web research. The client is in `internal/tavily`. It uses `search_depth: basic` (1 credit for each search), asks for a short answer, and returns 5 results by default.
2. The client validates the query before the call: the query must not be empty and must be 400 characters or fewer, and the topic, time range, and result count must be permitted values. It retries HTTP 429, HTTP 5xx, and network errors up to 3 attempts, and it obeys `Retry-After`. HTTP 401 maps to `ErrUnauthorized`, and HTTP 432 and 433 map to `ErrQuotaExceeded`. These errors are not retried.
3. The agent gets a `web_search` tool only when `TAVILY_API_KEY` is set. The system prompt gets the web rules only when the tool is available.
4. The tool stops a query that contains 10 or more digits in sequence (a phone, card, or account number) or an email address. The model receives an error and can search again with public terms.
5. The tool sends each result to the model with a number, title, URL, a snippet of 600 characters or fewer, and the published date. It drops results that do not have an `http` or `https` URL. The tool result and the system prompt tell the model that web text is data, not instructions.
6. The `remember` tool has an optional `source_url`. The URL must be one that `web_search` returned in the same conversation. Then the memory gets source `web` and the URL as its source reference. The model cannot save a fact with a made-up source.
7. A web memory shows its URL in `recall` results, in the system prompt of later conversations, and in `lifeledger memory list`.
8. `lifeledger search` calls Tavily directly, to test the key and to research from the terminal.

## Consequences

- Each web fact that the agent keeps has a source that the user can open.
- The list of URLs that can be cited is kept in process memory. After a restart, the agent must search again before it can save a web fact.
- The personal-data check is a pattern match. It can stop a query with a long number that is not personal, and it cannot find all personal data, for example a name. The system prompt rule is the main control.
- Each search costs 1 Tavily credit. The free plan gives 1,000 credits each month.
