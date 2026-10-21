# ADR 0010: Angular web app

- Status: Accepted
- Date: 2026-10-19

## Context

Judges score the design and the product experience. A terminal client does not show the main features: live tool calls, approvals, reminders, and an editable memory. The user wants Angular. The app must work with the HTTP API (ADR 0009) and must not weaken the privacy of the data.

## Decision

1. The app is in `web/`. It uses Angular 22 with standalone components, signals, the built-in control flow, and no zone.js. It has no UI component library. The styles are plain CSS with design tokens for light and dark mode.
2. The fonts (Fraunces, IBM Plex Sans, IBM Plex Mono) come from npm packages and are part of the build. The app sends no request to a third-party host.
3. The Go server serves the built app from `LIFELEDGER_WEB_DIR` at `/`, with a fallback to `index.html` for app routes. One origin serves the app and the API, so the app needs no CORS. If the directory has no build, the server runs without the app.
4. The server sends a strict Content Security Policy: scripts and fonts only from the same origin, no frames, and no `object`. Styles permit `'unsafe-inline'`, because Angular adds component styles at run time. Critical CSS inlining is off, because it uses an inline event handler that the policy blocks.
5. File names with a content hash get a cache time of one year. `index.html` and other files get `no-cache`, so a new build takes effect at once.
6. The user enters the API token one time. The app checks it with `GET /api/v1/overview` before it saves it in `localStorage`. An HTTP interceptor adds the token to `/api/` requests only. A `401` ends the session and opens the unlock page. **Lock** removes the token. If the server has no token (dev mode), the app opens without the unlock step.
7. Chat uses `fetch` with a stream reader for the server-sent events, because `EventSource` cannot send a POST body or an `Authorization` header. A small parser handles frames that arrive in parts. `/chat` and `/chat/:id` use one route (a URL matcher), so the page keeps the live stream when the first reply gives the conversation ID.
8. The app never renders HTML from the model. Replies are text. The app makes links only from `http` and `https` URLs, and opens them with `rel="noopener noreferrer"`.
9. Each page has a clear job: Today (what needs attention), Chat, Ledger (all items, edit in a side panel), Approvals (emails and import proposals), Import (PDF or pasted text), Memory (read, edit, forget, recall), and Activity (notifications and audit log).
10. Unit tests use Vitest with jsdom: the SSE parser, formatting, the transcript builder, the interceptor and guard, the API service, and the main pages with a fake API.

## Consequences

- `make web` must run before the binary can serve the app. Task 12 builds the app in the Docker image.
- The token in `localStorage` can be read by a script that runs in the app. The strict CSP and the absence of model-rendered HTML lower this risk. A cookie session would remove it, but would add login state to a one-user service.
- Developers need Node.js 22.22.3 or later for the web app. The Go service does not need Node.js.
