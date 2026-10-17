# ADR 0008: Sweep, reminders, weekly review, and Telegram delivery

- Status: Accepted
- Date: 2026-10-17

## Context

The Personal AI track asks for an "always-on" assistant. A ledger that waits for the user to ask is not enough. LifeLedger must tell the user about a due date at the correct time, without duplicate messages, and it must continue when a model or a delivery channel is not available. The same logic must run in the long-running server and in a scheduled job.

## Decision

1. One sweep (`internal/sweep`) does all scheduled work. `lifeledger serve` runs it at startup and then each `LIFELEDGER_SWEEP_INTERVAL` (default 15 minutes). `lifeledger sweep` runs it one time, for a cron job or a Nebius Serverless Job. A failure in one step does not stop the other steps.
2. Each sweep does these steps in this sequence:
   1. It opens each snoozed item whose snooze date is today or earlier, and it writes an audit entry with the actor `sweep`.
   2. At or after `LIFELEDGER_REMIND_HOUR` local time, it makes reminders for open items that are due in 7 days or less, or that are overdue.
   3. It writes the weekly review when its time slot has come.
   4. It sends the notifications that were not delivered.
3. A reminder has a stage: `7d` (4 to 7 days), `3d` (2 or 3 days), `1d`, `today`, or `overdue`. The key `reminder:<item>:<due date>:<stage>` is unique in the `notifications` table. Thus each stage is sent one time. A new due date (for example, after `mark_paid` on a monthly bill) starts the stages again. When a sweep misses a stage, the item gets only its current stage.
4. Reminder text comes from a template, not from a model. It is fast and free, and it cannot contain a wrong date or amount.
5. The weekly review slot is a local weekday and hour (default Sunday 18:00). The key is `review:<slot date>`. If the server was down at the slot, the next sweep in 72 hours writes the review. After 72 hours, LifeLedger skips that week.
6. The review sends a digest (overdue items, items due in 14 days, snoozed items, approvals that wait, and actions done in the last 7 days) as JSON to Nemotron 3 Ultra. Ultra writes a summary of 180 words or fewer, at temperature 0.3. The message contains the summary and then the full digest, so the user can check each fact. If the model call fails, the message contains only the digest.
7. Telegram is the optional delivery channel (`internal/notify`). The message is plain text with link previews off. Reminders that wait at the same time go in one message. Each review is a separate message.
8. A failed delivery increments `attempts` and records the error. A later sweep tries again until 5 attempts. A sweep does not send a notification that is more than 24 hours old. Thus a channel that you add later does not receive old reminders.
9. The bot token is part of the Telegram URL. Errors from the HTTP client contain the URL. The client removes the URL from transport errors, so the token does not get into logs or into the database.

## Consequences

- Without Telegram, notifications stay in the database. The `notifications` command and the HTTP API (Task 10) show them.
- If `serve` and `lifeledger sweep` run at the same time, the unique keys stop duplicate notifications. A notification can be delivered two times in that case. Run only one of them in production.
- A reminder stage that the user missed (for example, during a long outage) is not sent later. The current stage and the weekly review cover the item.
- Each weekly review costs one Ultra call. The daily spend limit applies to it.
