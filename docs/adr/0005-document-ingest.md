# ADR 0005: Document ingest with proposals

- Status: Accepted
- Date: 2026-10-13

## Context

Most life-admin data arrives as emails and PDF bills. The user must not type each due date and amount by hand. A document can contain wrong or hostile text, for example an instruction to the model inside an email. An extraction error that goes directly into the ledger gives wrong reminders.

## Decision

1. LifeLedger reads plain text (email, pasted text) and PDF files with a text layer. PDF text comes from `github.com/ledongthuc/pdf`, a pure-Go library with no dependencies.
2. Nemotron 3 Nano extracts the obligations. The request uses temperature 0 and a strict JSON Schema (`response_format: json_schema`). If Token Factory rejects the schema with HTTP 400, LifeLedger sends the request again with the JSON format in the prompt only.
3. The extraction model gets no tools. The prompt tells it that the document is data and that it must ignore instructions inside the document.
4. Go validates each extracted item. An unknown category becomes `other`. An unknown recurrence becomes `none`. An amount, currency, or date that does not parse is removed. Confidence is limited to the range 0 to 1. Items with no title and repeated items are removed.
5. Each item becomes a **proposal**, not an obligation. The user accepts or rejects each proposal. Accept creates the obligation and marks the proposal in one transaction.
6. A proposal gets `duplicate_of` when an open obligation has the same normalized title, or the same due date and amount.
7. The SHA-256 hash of the normalized text identifies a document. The same document is not sent to the model again; LifeLedger shows the earlier proposals.
8. Text longer than 40,000 characters is truncated. PDF files larger than 10 MB are rejected.

## Consequences

- Nothing from a document enters the ledger without a user decision.
- An instruction hidden in a document cannot call tools or change data. It can only change the proposals, and the user sees each proposal with a quote from the document as evidence.
- Scanned PDFs without a text layer are rejected. OCR is not in scope.
- A document with obligations after the first 40,000 characters gives incomplete proposals. The command shows a note when this occurs.
