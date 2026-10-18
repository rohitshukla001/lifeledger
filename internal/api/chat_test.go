package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/llm"
)

type sseEvent struct {
	name string
	data map[string]any
}

func readEvents(t *testing.T, resp *http.Response) []sseEvent {
	t.Helper()
	var events []sseEvent
	var name string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
				t.Fatalf("event %s has bad data: %s", name, line)
			}
			events = append(events, sseEvent{name, data})
		}
	}
	return events
}

func (f *fixture) stream(t *testing.T, body string) (*http.Response, []sseEvent) {
	t.Helper()
	srv := httptest.NewServer(f.handler)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/chat", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp, readEvents(t, resp)
}

func TestChatJSON(t *testing.T) {
	f := newFixture(t)
	f.llm.push(
		callTool("add_obligation", `{"title":"Electricity bill","category":"bill","amount":"2340.50","due_on":"2026-10-21"}`),
		say("Saved the electricity bill, due 21 October."),
	)

	out := f.do(http.MethodPost, "/api/v1/chat", map[string]any{"message": "My electricity bill of 2340.50 is due on the 21st"}, http.StatusOK)
	conv := out["conversation"].(map[string]any)
	steps := out["steps"].([]any)
	if conv["id"] != float64(1) || conv["title"] != "My electricity bill of 2340.50 is due on the 21st" ||
		out["reply"] != "Saved the electricity bill, due 21 October." || len(steps) != 1 || out["cost_usd"] != 0.003 {
		t.Fatalf("chat = %v", out)
	}
	if step := steps[0].(map[string]any); step["name"] != "add_obligation" || step["failed"] != nil || !strings.Contains(step["result"].(string), `"amount":"2340.50"`) {
		t.Fatalf("step = %v", step)
	}
	if usage := out["usage"].(map[string]any); usage["prompt_tokens"] != float64(100) {
		t.Fatalf("usage = %v", usage)
	}

	f.llm.push(say("It is due on 21 October."))
	next := f.do(http.MethodPost, "/api/v1/chat", map[string]any{"conversation_id": 1, "message": "When is it due?"}, http.StatusOK)
	if next["conversation"].(map[string]any)["id"] != float64(1) || len(next["steps"].([]any)) != 0 {
		t.Fatalf("follow-up = %v", next)
	}

	if convs := f.items("/api/v1/conversations"); len(convs) != 1 {
		t.Fatalf("conversations = %v", convs)
	}
	detail := f.do(http.MethodGet, "/api/v1/conversations/1", nil, http.StatusOK)
	var roles []string
	for _, m := range detail["messages"].([]any) {
		roles = append(roles, m.(map[string]any)["role"].(string))
	}
	if strings.Join(roles, ",") != "user,assistant,tool,assistant,user,assistant" {
		t.Fatalf("roles = %v", roles)
	}
	if call := detail["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any); call["name"] != "add_obligation" {
		t.Fatalf("tool call = %v", call)
	}

	f.do(http.MethodDelete, "/api/v1/conversations/1", nil, http.StatusNoContent)
	f.wantError(http.MethodGet, "/api/v1/conversations/1", nil, http.StatusNotFound, "not_found", "not found")
}

func TestChatErrors(t *testing.T) {
	f := newFixture(t)
	f.wantError(http.MethodPost, "/api/v1/chat", map[string]any{"message": "  "}, http.StatusBadRequest, "invalid_request", "message is required")
	f.wantError(http.MethodPost, "/api/v1/chat", map[string]any{"message": strings.Repeat("a", maxMessageRunes+1)}, http.StatusBadRequest, "invalid_request", "longer than")
	f.wantError(http.MethodPost, "/api/v1/chat", map[string]any{"conversation_id": 7, "message": "hi"}, http.StatusNotFound, "not_found", "not found")

	f.llm.push(fail(llm.ErrBudgetExceeded))
	f.wantError(http.MethodPost, "/api/v1/chat", map[string]any{"message": "hi"}, http.StatusServiceUnavailable, "budget_exceeded", "daily spend limit")
	if convs := f.items("/api/v1/conversations"); len(convs) != 0 {
		t.Fatalf("a failed first message must not leave an empty conversation: %v", convs)
	}

	f.llm.push(say("hello"))
	out := f.do(http.MethodPost, "/api/v1/chat", map[string]any{"message": "hi"}, http.StatusOK)
	id := int64(out["conversation"].(map[string]any)["id"].(float64))
	unlock, _ := f.api.lock(id)
	f.wantError(http.MethodPost, "/api/v1/chat", map[string]any{"conversation_id": id, "message": "again"}, http.StatusConflict, "conversation_busy", "another message")
	f.wantError(http.MethodDelete, fmt.Sprintf("/api/v1/conversations/%d", id), nil, http.StatusConflict, "conversation_busy", "answering")
	unlock()

	f.opts.Agent = nil
	f.rebuild()
	f.wantError(http.MethodPost, "/api/v1/chat", map[string]any{"message": "hi"}, http.StatusServiceUnavailable, "not_configured", "NEBIUS_API_KEY")
}

func TestChatStream(t *testing.T) {
	f := newFixture(t)
	f.llm.push(
		callTool("list_obligations", `{}`),
		say("Nothing is due."),
	)
	resp, events := f.stream(t, `{"message":"What is due this week?"}`)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("response = %d %v", resp.StatusCode, resp.Header)
	}
	var names []string
	for _, e := range events {
		names = append(names, e.name)
	}
	if strings.Join(names, ",") != "conversation,tool_call,tool_result,reply" {
		t.Fatalf("events = %v", names)
	}
	if events[0].data["id"] != float64(1) || events[1].data["name"] != "list_obligations" || events[1].data["result"] != nil ||
		!strings.Contains(events[2].data["result"].(string), `"obligations":[]`) || events[3].data["reply"] != "Nothing is due." {
		t.Fatalf("event data = %+v", events)
	}
}

func TestChatStreamReportsErrors(t *testing.T) {
	f := newFixture(t)
	f.llm.push(fail(&llm.APIError{Status: 503, Message: "overloaded"}))
	resp, events := f.stream(t, `{"message":"hi"}`)
	if resp.StatusCode != http.StatusOK || len(events) != 2 || events[0].name != "conversation" || events[1].name != "error" {
		t.Fatalf("events = %+v", events)
	}
	if e := events[1].data["error"].(map[string]any); e["code"] != "model_error" || !strings.Contains(e["message"].(string), "overloaded") {
		t.Fatalf("error = %v", e)
	}
	if convs := f.items("/api/v1/conversations"); len(convs) != 0 {
		t.Fatalf("conversations = %v", convs)
	}

	rec := f.request(http.MethodPost, "/api/v1/chat", `{"message":""}`, "Accept", "text/event-stream")
	if code, _ := errorCode(t, rec); rec.Code != http.StatusBadRequest || code != "invalid_request" {
		t.Fatalf("validation errors must come before the stream: %d %s", rec.Code, code)
	}
}

func TestSSEKeepAlive(t *testing.T) {
	rec := httptest.NewRecorder()
	sse := &sseWriter{w: rec, rc: http.NewResponseController(rec)}
	stop := sse.keepAlive(2 * time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	stop()
	sse.send("reply", map[string]string{"reply": "ok"})
	body := rec.Body.String()
	if !strings.Contains(body, ": ping\n\n") || !strings.HasSuffix(body, "event: reply\ndata: {\"reply\":\"ok\"}\n\n") {
		t.Fatalf("body = %q", body)
	}
}
