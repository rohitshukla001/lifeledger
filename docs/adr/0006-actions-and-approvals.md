# ADR 0006: Actions with an approval policy and an audit log

- Status: Accepted
- Date: 2026-10-16

## Context

The Personal AI track rewards an assistant that "takes real action on your behalf". Some actions change only the user's own ledger. Other actions leave the system, for example an email to a provider. A wrong email cannot be recalled. The user must always know what the agent did and why.

## Decision

1. Each change with an effect is an **action** record: `mark_paid`, `snooze`, `calendar_event`, or `send_email`. The agent, the user, and later the scheduled sweep create actions through one service (`internal/actions`).
2. The service validates the payload before it saves the action. It saves the normalized payload and a one-line summary.
3. The approval policy is in code:
   - `mark_paid`, `snooze`, and `calendar_event` change only local data. They run at once.
   - `send_email` leaves the system. It waits in the `pending` state until the user approves it.
   - An unknown kind needs approval.
4. An action moves `pending → approved → done | failed`, or `pending → rejected`. The move from `pending` uses a conditional SQL update, so a double approval cannot run an action twice.
5. Each step writes an audit entry with the actor (`agent`, `user`, or `policy`), the event, and the action ID.
6. `mark_paid` moves a recurring obligation to its next due date and clears a snooze. A one-off obligation becomes `done`. A month-end date is limited to the last day of the next month (31 January → 28 February).
7. `calendar_event` makes an iCalendar (RFC 5545) file: an all-day event on the due date, an `RRULE` for recurring items, and a reminder at 09:00 local time on the chosen day before.
8. `send_email` uses SMTP with STARTTLS (or TLS on port 465). The subject must be one line, so header injection is not possible. When SMTP is not configured, the approved action fails with a clear message.

## Consequences

- The agent cannot send anything outside the system without a user decision.
- The audit log is the history of all agent effects. It is append-only in the application.
- A new action kind needs an entry in the kind table with its approval rule, validation, and executor.
- After a month-end limit, a monthly date stays on the earlier day (28 February → 28 March). The user can correct it with `update_obligation`.
