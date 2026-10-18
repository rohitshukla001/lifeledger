# LifeLedger HTTP API

This document gives the endpoints of the LifeLedger HTTP API. For the design decisions, see [ADR 0009](adr/0009-http-api.md).

## Basics

- Base path: `/api/v1`. The health endpoint `GET /healthz` is outside the API and needs no token.
- Authentication: send `Authorization: Bearer <LIFELEDGER_API_TOKEN>` with each request.
- Request bodies are JSON objects of 1 MB or less. An unknown field causes an error.
- Dates are `YYYY-MM-DD`. Timestamps are RFC 3339 in UTC. Amounts are decimal strings, for example `"2340.50"`.
- A list response has the form `{"items": [...]}`. The `limit` query parameter (1 to 500, default 50) sets the maximum number of items.

## Errors

An error response has this form:

```json
{"error": {"code": "invalid_request", "message": "unknown category \"tax\""}}
```

| Status | Code | Cause |
|---|---|---|
| 400 | `invalid_request` | A value in the request is not correct. The message tells which value. |
| 401 | `unauthorized` | The bearer token is missing or not correct. |
| 404 | `not_found` | The item or the endpoint does not exist. |
| 405 | `method_not_allowed` | The endpoint does not accept this method. The `Allow` header gives the permitted methods. |
| 409 | `already_decided` | The action or proposal was approved, accepted, or rejected before. |
| 409 | `conversation_busy` | The conversation answers a different message now. |
| 413 | `too_large` | The request body is too large. |
| 502 | `model_error`, `search_error` | Token Factory or Tavily returned an error. |
| 503 | `not_configured` | The feature needs a key that is not set. |
| 503 | `budget_exceeded`, `out_of_credits` | The daily spend limit is reached, or the Token Factory account has no credit. |
| 500 | `internal` | An unexpected error occurred. The server log has the details. |

## Endpoints

| Method and path | Purpose |
|---|---|
| `GET /overview` | Counts of overdue items, items due today and in the next 7 days, snoozed items, items without a due date, pending actions, and pending proposals. Also the next 5 dated items. |
| `GET /obligations` | List obligations, soonest due first. Query: `status` (`active` (default), `open`, `snoozed`, `done`, `cancelled`, `all`), `category`, `due_within_days`, `limit`. |
| `POST /obligations` | Add an obligation. Body: `title` (required), `category`, `amount`, `currency`, `due_on`, `recurrence`, `status`, `snoozed_until`, `notes`. |
| `GET /obligations/{id}` | Get one obligation. |
| `PATCH /obligations/{id}` | Change the fields in the body. An empty string clears `amount`, `due_on`, or `snoozed_until`. |
| `DELETE /obligations/{id}` | Delete an obligation and its reminders. |
| `GET /actions` | List actions, newest first. Query: `status`, `limit`. |
| `POST /actions` | Request an action. Body: `kind` (`mark_paid`, `snooze`, `calendar_event`, `send_email`), `obligation_id`, `payload`. Actions that change only local data run at once. `send_email` waits for approval. |
| `GET /actions/{id}` | Get one action. |
| `POST /actions/{id}/approve` | Approve and run a pending action. |
| `POST /actions/{id}/reject` | Reject a pending action. |
| `GET /actions/{id}/calendar.ics` | Download the iCalendar file of a finished `calendar_event` action. |
| `GET /audit` | The audit log, newest first. |
| `GET /memories` | List memories, newest first. |
| `POST /memories` | Save a memory. Body: `content`, `kind` (`fact`, `preference`, or `note`). Status 200 tells that the memory was already known. |
| `GET /memories/recall?q=` | Find memories by meaning and keywords. Each item has a `score`. |
| `PATCH /memories/{id}` | Change the text of a memory. Body: `content`. |
| `DELETE /memories/{id}` | Delete a memory permanently. |
| `POST /chat` | Send a message to the agent. Body: `message`, and `conversation_id` to continue a conversation. See [Chat](#chat). |
| `GET /conversations` | List conversations, newest first. |
| `GET /conversations/{id}` | Get a conversation with all its messages, tool calls, and tool results. |
| `DELETE /conversations/{id}` | Delete a conversation. |
| `POST /ingest` | Extract obligations from a document as proposals. Send `multipart/form-data` with a `file` field (a PDF of 10 MB or less, or UTF-8 text) and an optional `name` field, or JSON with `name` and `text`. Status 200 tells that the same document was ingested before. |
| `GET /proposals` | List proposals. Query: `status` (`pending` (default), `accepted`, `rejected`, `all`). |
| `POST /proposals/{id}/accept` | Turn a proposal into an obligation. The response is the new obligation. |
| `POST /proposals/{id}/reject` | Discard a proposal. |
| `GET /notifications` | Reminders and weekly reviews, newest first, with their delivery status. |
| `POST /sweep` | Run one sweep now. Body (optional): `{"review": true}` also writes a weekly review now. |
| `GET /search?q=` | Search the web with Tavily. Query: `q`, `topic`, `time_range`, `max_results` (1 to 20), `domains` (comma-separated). |

## Chat

`POST /api/v1/chat` gives one JSON response by default:

```json
{
  "conversation": {"id": 1, "title": "My electricity bill is due on the 21st", "created_at": "...", "updated_at": "..."},
  "reply": "Saved the electricity bill, due 21 October.",
  "steps": [{"name": "add_obligation", "arguments": "{...}", "result": "{...}"}],
  "usage": {"prompt_tokens": 1830, "completion_tokens": 64},
  "cost_usd": 0.00061
}
```

To get the progress while the agent works, send `Accept: text/event-stream`. The response is a stream of server-sent events:

| Event | Data |
|---|---|
| `conversation` | The conversation. It is always the first event. |
| `tool_call` | `name` and `arguments` of a tool call, before the tool runs. |
| `tool_result` | `name`, `arguments`, `result`, and `failed` (true when the tool returned an error). |
| `reply` | The full JSON response, as above. It is the last event. |
| `error` | An error body, as in [Errors](#errors). It is the last event. |

Lines that start with `:` are keep-alive comments. The server sends one each 15 seconds.

Example:

```sh
curl -N -H "Authorization: Bearer $LIFELEDGER_API_TOKEN" -H "Accept: text/event-stream" \
  -d '{"message": "What is due this week?"}' localhost:8080/api/v1/chat
```

The browser `EventSource` API cannot send a POST request or an `Authorization` header. Use `fetch` and read the response body as a stream.
