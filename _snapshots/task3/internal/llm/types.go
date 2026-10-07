package llm

import "encoding/json"

type Tier string

const (
	Nano  Tier = "nano"
	Super Tier = "super"
	Ultra Tier = "ultra"
)

func ParseTier(s string) (Tier, bool) {
	switch t := Tier(s); t {
	case Nano, Super, Ultra:
		return t, true
	}
	return "", false
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

func System(content string) Message { return Message{Role: "system", Content: content} }
func User(content string) Message   { return Message{Role: "user", Content: content} }

func ToolResult(callID, content string) Message {
	return Message{Role: "tool", ToolCallID: callID, Content: content}
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function ToolSpec `json:"function"`
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func FunctionTool(name, description string, parameters json.RawMessage) Tool {
	return Tool{Type: "function", Function: ToolSpec{Name: name, Description: description, Parameters: parameters}}
}

type Request struct {
	Tier        Tier
	Messages    []Message
	Tools       []Tool
	Temperature *float64
	MaxTokens   int
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type Response struct {
	Model        string
	Message      Message
	FinishReason string
	Usage        Usage
	CostUSD      float64
}
