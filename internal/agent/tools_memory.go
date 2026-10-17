package agent

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

const webSource = "web"

var memoryKinds = []store.MemoryKind{store.MemoryFact, store.MemoryPreference, store.MemoryNote}

func (a *Agent) rememberTool() Tool {
	return Tool{
		Name:        "remember",
		Description: "Save a lasting fact, preference, or note about the user so that it is available in future conversations.",
		Parameters: `{
			"type": "object",
			"properties": {
				"content": {"type": "string", "description": "One self-contained sentence, for example: Car insurance is with Acko."},
				"kind": {"type": "string", "enum": ["fact", "preference", "note"]},
				"source_url": {"type": "string", "description": "URL of the web_search result that supports this fact, if it came from the web."}
			},
			"required": ["content"]
		}`,
		Run: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Content   string           `json:"content"`
				Kind      store.MemoryKind `json:"kind"`
				SourceURL string           `json:"source_url"`
			}
			if err := decodeArgs(call, &in); err != nil {
				return nil, err
			}
			if in.Kind == "" {
				in.Kind = store.MemoryFact
			}
			if !slices.Contains(memoryKinds, in.Kind) {
				return nil, fmt.Errorf("kind must be fact, preference, or note")
			}
			mem := store.Memory{
				Kind:      in.Kind,
				Content:   in.Content,
				Source:    "chat",
				SourceRef: fmt.Sprintf("conversation:%d", call.ConversationID),
			}
			if in.SourceURL = strings.TrimSpace(in.SourceURL); in.SourceURL != "" {
				if !a.cited.has(call.ConversationID, in.SourceURL) {
					return nil, fmt.Errorf("source_url must be a URL that web_search returned in this conversation")
				}
				mem.Source, mem.SourceRef = webSource, in.SourceURL
			}
			m, created, err := a.memory.Remember(ctx, mem)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"id": m.ID, "content": m.Content, "kind": m.Kind, "already_known": !created}
			if m.Source == webSource {
				out["source_url"] = m.SourceRef
			}
			return out, nil
		},
	}
}

func (a *Agent) recallTool() Tool {
	return Tool{
		Name:        "recall",
		Description: "Search the user's saved memories by meaning and keywords.",
		Parameters: `{
			"type": "object",
			"properties": {
				"query": {"type": "string"},
				"limit": {"type": "integer", "minimum": 1, "maximum": 20}
			},
			"required": ["query"]
		}`,
		Run: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := decodeArgs(call, &in); err != nil {
				return nil, err
			}
			if in.Limit <= 0 {
				in.Limit = 5
			}
			hits, err := a.memory.Recall(ctx, in.Query, min(in.Limit, 20))
			if err != nil {
				return nil, err
			}
			type item struct {
				ID        int64            `json:"id"`
				Kind      store.MemoryKind `json:"kind"`
				Content   string           `json:"content"`
				Score     float64          `json:"score"`
				SourceURL string           `json:"source_url,omitempty"`
			}
			out := make([]item, 0, len(hits))
			for _, h := range hits {
				it := item{ID: h.Memory.ID, Kind: h.Memory.Kind, Content: h.Memory.Content, Score: math.Round(h.Score*100) / 100}
				if h.Memory.Source == webSource {
					it.SourceURL = h.Memory.SourceRef
				}
				out = append(out, it)
			}
			return map[string]any{"memories": out}, nil
		},
	}
}
