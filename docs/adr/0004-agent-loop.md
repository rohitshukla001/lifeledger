# ADR 0004: Agent loop with tool calls

- Status: Accepted
- Date: 2026-10-09

## Context

LifeLedger must answer questions about the user's obligations, record new ones, and remember facts across conversations. The answers must come from stored data, not from the model's guesses. The track judges reward an assistant that "remembers context across sessions and takes real action".

## Decision

1. LifeLedger has its own agent loop in Go (`internal/agent`). It does not use an agent framework.
2. Each turn uses Nemotron 3 Super with OpenAI-style tool calling. The tools are `remember`, `recall`, `add_obligation`, `list_obligations`, and `update_obligation`.
3. The loop runs up to 8 model calls for each turn. If the model still asks for tools, LifeLedger makes one more call with no tools, so the turn always ends with a text answer.
4. A tool error goes back to the model as `{"error": "..."}`. The model can then correct its arguments or ask the user a question.
5. The system prompt is built for each turn. It contains the local date, the time zone, the default currency, the rules, and the memories that `recall` finds for the user's message. This is how a new conversation knows facts from earlier conversations.
6. The conversation history sent to the model is limited to the last 40 messages. The cut always starts at a user message, so a tool call is never separated from its result.
7. LifeLedger saves the messages of a turn only after the turn completes. If the model call fails, nothing is saved and the user can send the message again.
8. Tool arguments are validated in Go: dates must be `YYYY-MM-DD`, amounts are converted to minor units, and categories, statuses, and recurrences must be in the permitted lists.

## Consequences

- Each answer about an obligation comes from the database through a tool call.
- A turn can make more than one model call. Cost and token usage are reported for each turn.
- Tool side effects stay when a later model call in the same turn fails. The agent can see them on the next turn with `list_obligations`.
- Actions with an external effect (email, calendar) are not in this loop yet. They need an approval step (Task 7).
