package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/agent"
	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/ledger"
	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/store"
	"github.com/rohitshukla001/lifeledger/internal/sweep"
	"github.com/rohitshukla001/lifeledger/internal/tavily"
)

const (
	maxJSONBytes = 1 << 20
	defaultLimit = 50
	maxLimit     = 500
)

var errUnavailable = errors.New("feature is not configured")

type Options struct {
	Store    *store.Store
	Agent    *agent.Agent
	Memory   *memory.Engine
	Actions  *actions.Service
	Ingest   *ingest.Service
	Sweeper  *sweep.Sweeper
	Search   agent.Searcher
	Token    string
	Location *time.Location
	Currency string
	Logger   *slog.Logger
	Now      func() time.Time
}

type API struct {
	store    *store.Store
	agent    *agent.Agent
	memory   *memory.Engine
	actions  *actions.Service
	ingest   *ingest.Service
	sweeper  *sweep.Sweeper
	search   agent.Searcher
	token    []byte
	loc      *time.Location
	currency string
	log      *slog.Logger
	now      func() time.Time
	busy     sync.Map
	handler  http.Handler
}

func New(o Options) *API {
	a := &API{
		store:    o.Store,
		agent:    o.Agent,
		memory:   o.Memory,
		actions:  o.Actions,
		ingest:   o.Ingest,
		sweeper:  o.Sweeper,
		search:   o.Search,
		token:    []byte(o.Token),
		loc:      o.Location,
		currency: o.Currency,
		log:      o.Logger,
		now:      o.Now,
	}
	if a.loc == nil {
		a.loc = time.UTC
	}
	if a.log == nil {
		a.log = slog.New(slog.DiscardHandler)
	}
	if a.now == nil {
		a.now = time.Now
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/overview", a.overview)

	mux.HandleFunc("GET /api/v1/obligations", a.listObligations)
	mux.HandleFunc("POST /api/v1/obligations", a.createObligation)
	mux.HandleFunc("GET /api/v1/obligations/{id}", a.getObligation)
	mux.HandleFunc("PATCH /api/v1/obligations/{id}", a.updateObligation)
	mux.HandleFunc("DELETE /api/v1/obligations/{id}", a.deleteObligation)

	mux.HandleFunc("GET /api/v1/actions", a.listActions)
	mux.HandleFunc("POST /api/v1/actions", a.requestAction)
	mux.HandleFunc("GET /api/v1/actions/{id}", a.getAction)
	mux.HandleFunc("POST /api/v1/actions/{id}/approve", a.approveAction)
	mux.HandleFunc("POST /api/v1/actions/{id}/reject", a.rejectAction)
	mux.HandleFunc("GET /api/v1/actions/{id}/calendar.ics", a.actionCalendar)
	mux.HandleFunc("GET /api/v1/audit", a.listAudit)

	mux.HandleFunc("GET /api/v1/memories", a.listMemories)
	mux.HandleFunc("POST /api/v1/memories", a.createMemory)
	mux.HandleFunc("GET /api/v1/memories/recall", a.recallMemories)
	mux.HandleFunc("PATCH /api/v1/memories/{id}", a.updateMemory)
	mux.HandleFunc("DELETE /api/v1/memories/{id}", a.deleteMemory)

	mux.HandleFunc("POST /api/v1/chat", a.chat)
	mux.HandleFunc("GET /api/v1/conversations", a.listConversations)
	mux.HandleFunc("GET /api/v1/conversations/{id}", a.getConversation)
	mux.HandleFunc("DELETE /api/v1/conversations/{id}", a.deleteConversation)

	mux.HandleFunc("POST /api/v1/ingest", a.ingestDocument)
	mux.HandleFunc("GET /api/v1/proposals", a.listProposals)
	mux.HandleFunc("POST /api/v1/proposals/{id}/accept", a.acceptProposal)
	mux.HandleFunc("POST /api/v1/proposals/{id}/reject", a.rejectProposal)

	mux.HandleFunc("GET /api/v1/notifications", a.listNotifications)
	mux.HandleFunc("POST /api/v1/sweep", a.runSweep)
	mux.HandleFunc("GET /api/v1/search", a.webSearch)

	a.handler = a.authenticate(unmatchedAsJSON(mux))
	return a
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(a.token) > 0 {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), a.token) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="lifeledger"`)
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type statusOnly struct {
	header http.Header
	status int
}

func (s *statusOnly) Header() http.Header         { return s.header }
func (s *statusOnly) Write(b []byte) (int, error) { return len(b), nil }
func (s *statusOnly) WriteHeader(status int)      { s.status = status }

func unmatchedAsJSON(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		probe := &statusOnly{header: http.Header{}, status: http.StatusNotFound}
		h.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			w.Header().Set("Allow", probe.header.Get("Allow"))
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" is not allowed here")
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
}

func (a *API) today() time.Time {
	return ledger.Today(a.now(), a.loc)
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code, body.Error.Message = code, message
	writeJSON(w, status, body)
}

func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := classify(err)
	if status == http.StatusInternalServerError {
		a.log.Error("api request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	if r.Context().Err() != nil {
		return
	}
	writeError(w, status, code, msg)
}

func classify(err error) (int, string, string) {
	var apiErr *llm.APIError
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("request body is larger than %d bytes", tooLarge.Limit)
	case errors.Is(err, ledger.ErrInvalid), errors.Is(err, tavily.ErrInvalidQuery):
		return http.StatusBadRequest, "invalid_request", err.Error()
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found", "not found"
	case errors.Is(err, store.ErrAlreadyDecided):
		return http.StatusConflict, "already_decided", "this item was already approved or rejected"
	case errors.Is(err, errUnavailable), errors.Is(err, ingest.ErrNoModel):
		return http.StatusServiceUnavailable, "not_configured", err.Error()
	case errors.Is(err, llm.ErrBudgetExceeded):
		return http.StatusServiceUnavailable, "budget_exceeded", err.Error()
	case errors.Is(err, llm.ErrOutOfCredits):
		return http.StatusServiceUnavailable, "out_of_credits", err.Error()
	case errors.As(err, &apiErr):
		return http.StatusBadGateway, "model_error", err.Error()
	case errors.Is(err, tavily.ErrQuotaExceeded), errors.Is(err, tavily.ErrUnauthorized):
		return http.StatusBadGateway, "search_error", err.Error()
	}
	return http.StatusInternalServerError, "internal", "internal error"
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		return ledger.Invalidf("invalid JSON body: %v", err)
	}
	if dec.More() {
		return ledger.Invalidf("invalid JSON body: more than one value")
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, ledger.Invalidf("invalid id %q", r.PathValue("id"))
	}
	return id, nil
}

func queryInt(r *http.Request, key string, fallback, lo, hi int) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return 0, ledger.Invalidf("%s must be a whole number from %d to %d", key, lo, hi)
	}
	return n, nil
}

func limit(r *http.Request) (int, error) {
	return queryInt(r, "limit", defaultLimit, 1, maxLimit)
}

type items[T any] struct {
	Items []T `json:"items"`
}

func list[T any](v []T) items[T] {
	if v == nil {
		v = []T{}
	}
	return items[T]{Items: v}
}

func (a *API) lock(conversationID int64) (func(), bool) {
	mu, _ := a.busy.LoadOrStore(conversationID, &sync.Mutex{})
	m := mu.(*sync.Mutex)
	if !m.TryLock() {
		return nil, false
	}
	return m.Unlock, true
}
