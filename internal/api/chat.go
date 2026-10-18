package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rohitshukla001/lifeledger/internal/agent"
	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

const (
	maxMessageRunes = 8000
	chatTimeout     = 5 * time.Minute
	keepAliveEvery  = 15 * time.Second
)

type stepJSON struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result,omitempty"`
	Failed    bool   `json:"failed,omitempty"`
}

type chatResponse struct {
	Conversation conversationJSON `json:"conversation"`
	Reply        string           `json:"reply"`
	Steps        []stepJSON       `json:"steps"`
	Usage        llm.Usage        `json:"usage"`
	CostUSD      float64          `json:"cost_usd"`
}

func stepView(s agent.ToolStep) stepJSON {
	return stepJSON{Name: s.Name, Arguments: s.Arguments, Result: s.Result, Failed: s.Failed}
}

func (a *API) chat(w http.ResponseWriter, r *http.Request) {
	if a.agent == nil {
		a.fail(w, r, fmt.Errorf("%w: chat needs NEBIUS_API_KEY", errUnavailable))
		return
	}
	var in struct {
		ConversationID int64  `json:"conversation_id"`
		Message        string `json:"message"`
	}
	if err := decode(w, r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	in.Message = strings.TrimSpace(in.Message)
	if in.Message == "" {
		a.fail(w, r, ledger.Invalidf("message is required"))
		return
	}
	if utf8.RuneCountInString(in.Message) > maxMessageRunes {
		a.fail(w, r, ledger.Invalidf("message is longer than %d characters", maxMessageRunes))
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), chatTimeout)
	defer cancel()

	var conv store.Conversation
	var err error
	created := in.ConversationID == 0
	if created {
		conv, err = a.agent.StartConversation(ctx, in.Message)
	} else {
		conv, err = a.store.GetConversation(ctx, in.ConversationID)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	unlock, ok := a.lock(conv.ID)
	if !ok {
		writeError(w, http.StatusConflict, "conversation_busy", "this conversation is answering another message")
		return
	}
	defer unlock()

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		a.streamChat(ctx, w, r, conv, created, in.Message)
		return
	}
	res, err := a.agent.Reply(ctx, conv.ID, in.Message)
	if err != nil {
		a.discard(conv, created)
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, chatView(conv, res))
}

func chatView(conv store.Conversation, res *agent.Result) chatResponse {
	return chatResponse{
		Conversation: conversationView(conv),
		Reply:        res.Reply,
		Steps:        mapSlice(res.Steps, stepView),
		Usage:        res.Usage,
		CostUSD:      res.CostUSD,
	}
}

func (a *API) streamChat(ctx context.Context, w http.ResponseWriter, r *http.Request, conv store.Conversation, created bool, message string) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sse := &sseWriter{w: w, rc: rc}
	sse.send("conversation", conversationView(conv))
	stop := sse.keepAlive(keepAliveEvery)
	res, err := a.agent.ReplyStream(ctx, conv.ID, message, func(e agent.Event) {
		sse.send(string(e.Kind), stepView(e.Step))
	})
	stop()

	if err != nil {
		a.discard(conv, created)
		status, code, msg := classify(err)
		if status == http.StatusInternalServerError {
			a.log.Error("chat stream failed", "conversation_id", conv.ID, "err", err)
		}
		var body errorBody
		body.Error.Code, body.Error.Message = code, msg
		sse.send("error", body)
		return
	}
	sse.send("reply", chatView(conv, res))
}

func (a *API) discard(conv store.Conversation, created bool) {
	if !created {
		return
	}
	if err := a.store.DeleteConversation(context.Background(), conv.ID); err != nil {
		a.log.Warn("cannot delete the empty conversation", "conversation_id", conv.ID, "err", err)
	}
}

type sseWriter struct {
	mu     sync.Mutex
	w      http.ResponseWriter
	rc     *http.ResponseController
	closed bool
}

func (s *sseWriter) send(event string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte(`{}`)
	}
	s.write(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
}

func (s *sseWriter) write(frame string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if _, err := s.w.Write([]byte(frame)); err != nil {
		s.closed = true
		return
	}
	if err := s.rc.Flush(); err != nil {
		s.closed = true
	}
}

func (s *sseWriter) keepAlive(every time.Duration) func() {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				s.write(": ping\n\n")
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

func (a *API) listConversations(w http.ResponseWriter, r *http.Request) {
	n, err := limit(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	found, err := a.store.ListConversations(r.Context(), n)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list(mapSlice(found, conversationView)))
}

func (a *API) getConversation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	conv, err := a.store.GetConversation(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	msgs, err := a.store.Messages(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		conversationJSON
		Messages []messageJSON `json:"messages"`
	}{conversationView(conv), mapSlice(msgs, messageView)})
}

func (a *API) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	unlock, ok := a.lock(id)
	if !ok {
		writeError(w, http.StatusConflict, "conversation_busy", "this conversation is answering a message")
		return
	}
	defer unlock()
	if err := a.store.DeleteConversation(r.Context(), id); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
