# ADR 0009: HTTP API, bearer token, and chat streaming

- Status: Accepted
- Date: 2026-10-18

## Context

The Angular UI (Task 11) and the deployed service (Task 12) need all features over HTTP. LifeLedger holds personal data, so the API must not be open on the internet. An agent turn can take from 2 seconds to more than 1 minute, because each tool step is a model call. The user must see progress during that time.

## Decision

1. The API is in `internal/api` under `/api/v1`. It uses the standard library `http.ServeMux` with method and path patterns. Each handler calls the same services as the CLI: `store`, `memory`, `actions`, `ingest`, `agent`, and `sweep`.
2. Authentication is one static bearer token, `LIFELEDGER_API_TOKEN`. The comparison takes constant time. The token is necessary in `prod` and must have 24 or more characters. In `dev`, an empty token turns the check off, and the server logs a warning. LifeLedger has one user, so it has no user accounts or sessions.
3. A token is never accepted in a URL. URLs go into logs and browser history.
4. Validation errors have one type, `ledger.ErrInvalid`. The `ledger`, `actions`, `ingest`, `memory`, and `agent` packages wrap their input errors in it, and the API maps it to HTTP 400 with the original message. The other errors map to fixed codes: not found 404, already decided 409, missing key or budget 503, upstream errors 502, and all others 500 with no details.
5. The obligation validation, view, and list filter moved from the agent to `internal/ledger`. The agent tools and the API use the same rules.
6. JSON bodies are 1 MB or less, and an unknown field is an error. A PDF upload is 10 MB or less. A document that starts with `%PDF-` goes to the PDF reader. Other documents must be UTF-8 text.
7. `POST /api/v1/chat` returns JSON by default. With `Accept: text/event-stream`, it streams server-sent events: `conversation`, `tool_call`, `tool_result`, and then `reply` or `error`. A comment line each 15 seconds keeps proxies from closing an idle connection. The handler removes the write deadline for the stream.
8. The stream shows progress at the step level, not at the token level. Token streaming needs the model's tool-call fragments to be joined, and the tier fallback cannot change model after the first token. The step events give the user the important progress: which tool runs, and its result.
9. The agent turn uses a context that does not stop when the client disconnects, with a limit of 5 minutes. A turn can change data (for example, `mark_paid`). If it stopped halfway, the conversation would not show a change that was made. The turn finishes and is saved, and the client can read it later.
10. One conversation answers one message at a time. A second message to a busy conversation gets HTTP 409. If the first message of a new conversation fails, the API deletes the empty conversation.
11. Unknown paths and wrong methods get JSON errors (404 and 405 with an `Allow` header), not the plain text of the standard library.

## Consequences

- The UI can show each tool call while the agent works, and it can read old conversations with their tool steps.
- A browser client must use `fetch` with a stream reader, because `EventSource` cannot send a POST body or an `Authorization` header.
- Anyone who has the token has full access. Keep it secret, and change it if it leaks.
- The busy check is in process memory. It works for one server process, which is the deployment model of LifeLedger.
