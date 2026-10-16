package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

type actionView struct {
	ActionID int64  `json:"action_id"`
	Status   string `json:"status"`
	Summary  string `json:"summary"`
	Result   string `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
	Note     string `json:"note,omitempty"`
}

func (a *Agent) actionTool(name, description, parameters, kind string, payload func(raw json.RawMessage) (*int64, json.RawMessage, error)) Tool {
	return Tool{
		Name:        name,
		Description: description,
		Parameters:  parameters,
		Run: func(ctx context.Context, call Call) (any, error) {
			obligationID, body, err := payload(call.Args)
			if err != nil {
				return nil, err
			}
			act, err := a.actions.Request(ctx, actions.Request{
				Kind:         kind,
				ObligationID: obligationID,
				Payload:      body,
				RequestedBy:  actions.ActorAgent,
				SourceRef:    fmt.Sprintf("conversation:%d", call.ConversationID),
			})
			if err != nil {
				return nil, err
			}

			v := actionView{ActionID: act.ID, Status: string(act.Status), Summary: act.Summary, Result: act.Result, Error: act.Error}
			switch {
			case act.Status == store.ActionPending:
				v.Note = fmt.Sprintf("Nothing is sent yet. Tell the user that action #%d waits for their approval.", act.ID)
			case kind == actions.KindCalendarEvent && act.Status == store.ActionDone:
				v.Result = ""
				v.Note = fmt.Sprintf("The calendar file is ready. The user can download it from action #%d.", act.ID)
			}
			return v, nil
		},
	}
}

func obligationPayload(raw json.RawMessage) (*int64, json.RawMessage, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if in.ID == 0 {
		return nil, nil, fmt.Errorf("id is required")
	}
	return &in.ID, raw, nil
}

func emailPayload(raw json.RawMessage) (*int64, json.RawMessage, error) {
	var in struct {
		ObligationID *int64 `json:"obligation_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if in.ObligationID != nil && *in.ObligationID == 0 {
		in.ObligationID = nil
	}
	return in.ObligationID, raw, nil
}

func (a *Agent) actionTools() []Tool {
	return []Tool{
		a.actionTool("mark_paid",
			"Record that the user paid or finished an obligation. A recurring obligation moves to its next due date; a one-off one becomes done.",
			`{"type": "object", "properties": {"id": {"type": "integer"}}, "required": ["id"]}`,
			actions.KindMarkPaid, obligationPayload),
		a.actionTool("snooze_obligation",
			"Stop reminders for an obligation until a later date.",
			`{"type": "object", "properties": {"id": {"type": "integer"}, "until": {"type": "string", "description": "YYYY-MM-DD, after today"}}, "required": ["id", "until"]}`,
			actions.KindSnooze, obligationPayload),
		a.actionTool("add_calendar_reminder",
			"Make a calendar file (.ics) with an all-day event on the due date and a reminder before it.",
			`{"type": "object", "properties": {"id": {"type": "integer"}, "remind_days_before": {"type": "integer", "minimum": 0, "maximum": 30}}, "required": ["id"]}`,
			actions.KindCalendarEvent, obligationPayload),
		a.actionTool("draft_email",
			"Draft an email for the user, for example to a provider. It is sent only after the user approves it.",
			`{"type": "object", "properties": {
				"to": {"type": "string", "description": "Recipient email address"},
				"subject": {"type": "string"},
				"body": {"type": "string", "description": "Plain text, signed with the user's name if known"},
				"obligation_id": {"type": "integer"}
			}, "required": ["to", "subject", "body"]}`,
			actions.KindSendEmail, emailPayload),
	}
}
