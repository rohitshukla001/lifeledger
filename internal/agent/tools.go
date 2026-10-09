package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rohitshukla001/lifeledger/internal/llm"
)

type Tool struct {
	Name        string
	Description string
	Parameters  string
	Run         func(ctx context.Context, call Call) (any, error)
}

type Call struct {
	ConversationID int64
	Args           json.RawMessage
}

type registry struct {
	order []string
	tools map[string]Tool
}

func newRegistry(tools ...Tool) *registry {
	r := &registry{tools: map[string]Tool{}}
	for _, t := range tools {
		if _, dup := r.tools[t.Name]; dup {
			panic("agent: duplicate tool " + t.Name)
		}
		if !json.Valid([]byte(t.Parameters)) {
			panic("agent: invalid JSON schema for tool " + t.Name)
		}
		r.order = append(r.order, t.Name)
		r.tools[t.Name] = t
	}
	return r
}

func (r *registry) definitions() []llm.Tool {
	defs := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		defs = append(defs, llm.FunctionTool(t.Name, t.Description, json.RawMessage(t.Parameters)))
	}
	return defs
}

func (r *registry) run(ctx context.Context, conversationID int64, fc llm.FunctionCall) (string, error) {
	t, ok := r.tools[fc.Name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", fc.Name)
	}
	args := json.RawMessage(fc.Arguments)
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if !json.Valid(args) {
		return "", fmt.Errorf("arguments for %s are not valid JSON", fc.Name)
	}

	out, err := t.Run(ctx, Call{ConversationID: conversationID, Args: args})
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode %s result: %w", fc.Name, err)
	}
	return string(b), nil
}

func decodeArgs(call Call, v any) error {
	if err := json.Unmarshal(call.Args, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}
