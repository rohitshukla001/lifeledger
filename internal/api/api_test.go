package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/agent"
	"github.com/rohitshukla001/lifeledger/internal/ingest"
	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/memory"
	"github.com/rohitshukla001/lifeledger/internal/store"
	"github.com/rohitshukla001/lifeledger/internal/sweep"
	"github.com/rohitshukla001/lifeledger/internal/tavily"
)

const testToken = "test-token-0123456789abcdef"

var kolkata = mustLoad("Asia/Kolkata")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

type scriptedLLM struct {
	mu     sync.Mutex
	script []func(llm.Request) (*llm.Response, error)
}

func (s *scriptedLLM) Chat(_ context.Context, req llm.Request) (*llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.script) == 0 {
		return nil, fmt.Errorf("scriptedLLM: no response left for %s", req.Tier)
	}
	next := s.script[0]
	s.script = s.script[1:]
	return next(req)
}

func (s *scriptedLLM) push(steps ...func(llm.Request) (*llm.Response, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.script = append(s.script, steps...)
}

func say(text string) func(llm.Request) (*llm.Response, error) {
	return func(llm.Request) (*llm.Response, error) {
		return &llm.Response{Message: llm.Message{Role: "assistant", Content: text}, Usage: llm.Usage{PromptTokens: 50, CompletionTokens: 5}, CostUSD: 0.001}, nil
	}
}

func callTool(name, args string) func(llm.Request) (*llm.Response, error) {
	return func(llm.Request) (*llm.Response, error) {
		return &llm.Response{Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call_" + name, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: args},
		}}}, Usage: llm.Usage{PromptTokens: 50, CompletionTokens: 10}, CostUSD: 0.002}, nil
	}
}

func fail(err error) func(llm.Request) (*llm.Response, error) {
	return func(llm.Request) (*llm.Response, error) { return nil, err }
}

type mailer struct {
	mu   sync.Mutex
	sent []string
}

func (m *mailer) Send(_ context.Context, to, subject, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, to+": "+subject)
	return nil
}

type fakeSearch struct{}

func (fakeSearch) Search(_ context.Context, q tavily.Query) (*tavily.Response, error) {
	if strings.TrimSpace(q.Text) == "" {
		return nil, fmt.Errorf("%w: query is empty", tavily.ErrInvalidQuery)
	}
	return &tavily.Response{Query: q.Text, Answer: "15 September 2026", Results: []tavily.Result{{Title: "Due dates", URL: "https://incometax.gov.in/due-dates"}}}, nil
}

type fixture struct {
	t       *testing.T
	store   *store.Store
	llm     *scriptedLLM
	extract *scriptedLLM
	mail    *mailer
	opts    Options
	handler http.Handler
	api     *API
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := func() time.Time { return time.Date(2026, 10, 18, 6, 30, 0, 0, time.UTC) }
	s.SetClock(now)

	f := &fixture{t: t, store: s, llm: &scriptedLLM{}, extract: &scriptedLLM{}, mail: &mailer{}}
	mem := memory.New(s, nil, nil)
	acts := actions.New(actions.Options{Store: s, Mailer: f.mail, Location: kolkata, Now: now})
	f.opts = Options{
		Store:    s,
		Memory:   mem,
		Actions:  acts,
		Agent:    agent.New(agent.Options{LLM: f.llm, Store: s, Memory: mem, Actions: acts, Location: kolkata, Currency: "INR", Now: now}),
		Ingest:   ingest.New(ingest.Options{LLM: f.extract, Store: s, Location: kolkata, Currency: "INR", Now: now}),
		Sweeper:  sweep.New(sweep.Options{Store: s, Location: kolkata, Currency: "INR", Now: now}),
		Search:   fakeSearch{},
		Token:    testToken,
		Location: kolkata,
		Currency: "INR",
		Now:      now,
	}
	f.rebuild()
	return f
}

func (f *fixture) rebuild() {
	f.api = New(f.opts)
	f.handler = f.api
}

func (f *fixture) request(method, path string, body any, header ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	case []byte:
		r = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			f.t.Fatal(err)
		}
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+testToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) do(method, path string, body any, wantStatus int) map[string]any {
	f.t.Helper()
	rec := f.request(method, path, body)
	if rec.Code != wantStatus {
		f.t.Fatalf("%s %s: status %d, want %d, body %s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		f.t.Fatalf("%s %s: body is not a JSON object: %s", method, path, rec.Body.String())
	}
	return out
}

func (f *fixture) items(path string) []map[string]any {
	f.t.Helper()
	out := f.do(http.MethodGet, path, nil, http.StatusOK)
	var list []map[string]any
	for _, it := range out["items"].([]any) {
		list = append(list, it.(map[string]any))
	}
	return list
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %s", rec.Body.String())
	}
	return body.Error.Code, body.Error.Message
}

func (f *fixture) wantError(method, path string, body any, status int, code, msgPart string) {
	f.t.Helper()
	rec := f.request(method, path, body)
	got, msg := errorCode(f.t, rec)
	if rec.Code != status || got != code || !strings.Contains(msg, msgPart) {
		f.t.Errorf("%s %s: %d %s %q, want %d %s containing %q", method, path, rec.Code, got, msg, status, code, msgPart)
	}
}

func TestAuthentication(t *testing.T) {
	f := newFixture(t)
	for _, auth := range []string{"", "Bearer wrong-token", "Basic " + testToken, testToken} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if code, _ := errorCode(t, rec); rec.Code != http.StatusUnauthorized || code != "unauthorized" || rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("Authorization %q: status %d, code %s", auth, rec.Code, code)
		}
	}
	f.do(http.MethodGet, "/api/v1/overview", nil, http.StatusOK)

	f.opts.Token = ""
	f.rebuild()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("without a configured token the API must be open: %d", rec.Code)
	}
}

func TestRoutingErrors(t *testing.T) {
	f := newFixture(t)
	f.wantError(http.MethodGet, "/api/v1/nope", nil, http.StatusNotFound, "not_found", "no such endpoint")
	f.wantError(http.MethodGet, "/api/v1/obligations/abc", nil, http.StatusBadRequest, "invalid_request", `invalid id "abc"`)
	f.wantError(http.MethodGet, "/api/v1/obligations/0", nil, http.StatusBadRequest, "invalid_request", "invalid id")
	f.wantError(http.MethodGet, "/api/v1/obligations?limit=0", nil, http.StatusBadRequest, "invalid_request", "limit must be")
	rec := f.request(http.MethodPut, "/api/v1/obligations", "{}")
	if code, _ := errorCode(t, rec); rec.Code != http.StatusMethodNotAllowed || code != "method_not_allowed" || !strings.Contains(rec.Header().Get("Allow"), "POST") {
		t.Fatalf("PUT: %d %s %v", rec.Code, code, rec.Header())
	}
}

func TestObligationsCRUD(t *testing.T) {
	f := newFixture(t)

	created := f.do(http.MethodPost, "/api/v1/obligations", map[string]any{
		"title": "Electricity bill", "category": "bill", "amount": "2,340.50", "due_on": "2026-10-21", "recurrence": "monthly",
	}, http.StatusCreated)
	if created["id"] != float64(1) || created["amount"] != "2340.50" || created["currency"] != "INR" || created["days_until_due"] != float64(3) ||
		created["status"] != "open" || created["source"] != "user" || created["created_at"] == nil {
		t.Fatalf("created = %v", created)
	}
	f.do(http.MethodPost, "/api/v1/obligations", map[string]any{"title": "Passport", "category": "document", "due_on": "2027-02-01"}, http.StatusCreated)
	f.do(http.MethodPost, "/api/v1/obligations", map[string]any{"title": "Old bill", "due_on": "2026-09-01", "status": "done"}, http.StatusCreated)

	f.wantError(http.MethodPost, "/api/v1/obligations", map[string]any{"title": "x", "category": "tax"}, http.StatusBadRequest, "invalid_request", "unknown category")
	f.wantError(http.MethodPost, "/api/v1/obligations", map[string]any{"title": "x", "colour": "red"}, http.StatusBadRequest, "invalid_request", `unknown field "colour"`)
	f.wantError(http.MethodPost, "/api/v1/obligations", `{"title":"x"} {"title":"y"}`, http.StatusBadRequest, "invalid_request", "more than one value")
	f.wantError(http.MethodPost, "/api/v1/obligations", `{"title":`, http.StatusBadRequest, "invalid_request", "invalid JSON body")
	f.wantError(http.MethodPost, "/api/v1/obligations", `{"notes":"`+strings.Repeat("a", maxJSONBytes)+`"}`, http.StatusRequestEntityTooLarge, "too_large", "larger than")

	titles := func(query string) string {
		var out []string
		for _, it := range f.items("/api/v1/obligations" + query) {
			out = append(out, it["title"].(string))
		}
		return strings.Join(out, ",")
	}
	cases := map[string]string{
		"":                     "Electricity bill,Passport",
		"?status=all":          "Old bill,Electricity bill,Passport",
		"?status=done":         "Old bill",
		"?due_within_days=7":   "Electricity bill",
		"?category=document":   "Passport",
		"?status=all&limit=1":  "Old bill",
		"?due_within_days=365": "Electricity bill,Passport",
	}
	for q, want := range cases {
		if got := titles(q); got != want {
			t.Errorf("%s: %s, want %s", q, got, want)
		}
	}
	if out := f.do(http.MethodGet, "/api/v1/obligations", nil, http.StatusOK); out["today"] != "2026-10-18" {
		t.Errorf("today = %v", out["today"])
	}
	f.wantError(http.MethodGet, "/api/v1/obligations?status=lost", nil, http.StatusBadRequest, "invalid_request", "unknown status")
	f.wantError(http.MethodGet, "/api/v1/obligations?due_within_days=-1", nil, http.StatusBadRequest, "invalid_request", "due_within_days")

	got := f.do(http.MethodPatch, "/api/v1/obligations/1", map[string]any{"amount": "2500", "notes": "BESCOM"}, http.StatusOK)
	if got["amount"] != "2500.00" || got["notes"] != "BESCOM" || got["due_on"] != "2026-10-21" || got["title"] != "Electricity bill" {
		t.Fatalf("patched = %v", got)
	}
	f.wantError(http.MethodPatch, "/api/v1/obligations/1", map[string]any{"status": "snoozed"}, http.StatusBadRequest, "invalid_request", "snoozed_until is required")
	got = f.do(http.MethodPatch, "/api/v1/obligations/1", map[string]any{"status": "snoozed", "snoozed_until": "2026-10-20"}, http.StatusOK)
	if got["status"] != "snoozed" || got["snoozed_until"] != "2026-10-20" {
		t.Fatalf("snoozed = %v", got)
	}

	f.do(http.MethodGet, "/api/v1/obligations/2", nil, http.StatusOK)
	f.do(http.MethodDelete, "/api/v1/obligations/2", nil, http.StatusNoContent)
	f.wantError(http.MethodGet, "/api/v1/obligations/2", nil, http.StatusNotFound, "not_found", "not found")
	f.wantError(http.MethodDelete, "/api/v1/obligations/2", nil, http.StatusNotFound, "not_found", "not found")
	f.wantError(http.MethodPatch, "/api/v1/obligations/99", map[string]any{"notes": "x"}, http.StatusNotFound, "not_found", "not found")
}

func TestActionsFlow(t *testing.T) {
	f := newFixture(t)
	f.do(http.MethodPost, "/api/v1/obligations", map[string]any{"title": "Airtel broadband", "category": "bill", "amount": "999", "due_on": "2026-10-20", "recurrence": "monthly"}, http.StatusCreated)

	paid := f.do(http.MethodPost, "/api/v1/actions", map[string]any{"kind": "mark_paid", "obligation_id": 1}, http.StatusCreated)
	if paid["status"] != "done" || paid["result"] != "next due 2026-11-20" || paid["needs_approval"] != false || paid["requested_by"] != "user" {
		t.Fatalf("mark_paid = %v", paid)
	}

	cal := f.do(http.MethodPost, "/api/v1/actions", map[string]any{"kind": "calendar_event", "obligation_id": 1, "payload": map[string]int{"remind_days_before": 2}}, http.StatusCreated)
	if cal["status"] != "done" || cal["result"] != nil || cal["calendar_url"] != "/api/v1/actions/2/calendar.ics" {
		t.Fatalf("calendar = %v", cal)
	}
	rec := f.request(http.MethodGet, "/api/v1/actions/2/calendar.ics", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/calendar") ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "lifeledger-2.ics") || !strings.HasPrefix(rec.Body.String(), "BEGIN:VCALENDAR") {
		t.Fatalf("ics: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
	f.wantError(http.MethodGet, "/api/v1/actions/1/calendar.ics", nil, http.StatusBadRequest, "invalid_request", "not a finished calendar action")

	email := f.do(http.MethodPost, "/api/v1/actions", map[string]any{"kind": "send_email", "payload": map[string]string{"to": "billing@airtel.example", "subject": "Late fee", "body": "Please waive it."}}, http.StatusCreated)
	if email["status"] != "pending" || email["needs_approval"] != true {
		t.Fatalf("email = %v", email)
	}
	second := f.do(http.MethodPost, "/api/v1/actions", map[string]any{"kind": "send_email", "payload": map[string]string{"to": "a@b.example", "subject": "Hi", "body": "x"}}, http.StatusCreated)

	if pending := f.items("/api/v1/actions?status=pending"); len(pending) != 2 {
		t.Fatalf("pending = %v", pending)
	}
	approved := f.do(http.MethodPost, "/api/v1/actions/3/approve", nil, http.StatusOK)
	if approved["status"] != "done" || len(f.mail.sent) != 1 || f.mail.sent[0] != "billing@airtel.example: Late fee" {
		t.Fatalf("approved = %v, sent = %v", approved, f.mail.sent)
	}
	f.wantError(http.MethodPost, "/api/v1/actions/3/approve", nil, http.StatusConflict, "already_decided", "already approved or rejected")
	rejected := f.do(http.MethodPost, fmt.Sprintf("/api/v1/actions/%.0f/reject", second["id"]), nil, http.StatusOK)
	if rejected["status"] != "rejected" {
		t.Fatalf("rejected = %v", rejected)
	}
	f.wantError(http.MethodPost, "/api/v1/actions/99/approve", nil, http.StatusNotFound, "not_found", "not found")

	f.wantError(http.MethodPost, "/api/v1/actions", map[string]any{"kind": "pay_bill"}, http.StatusBadRequest, "invalid_request", `unknown action "pay_bill"`)
	f.wantError(http.MethodPost, "/api/v1/actions", map[string]any{"kind": "mark_paid", "obligation_id": 42}, http.StatusBadRequest, "invalid_request", "no obligation with id 42")
	f.wantError(http.MethodPost, "/api/v1/actions", map[string]any{"kind": "snooze", "obligation_id": 1, "payload": map[string]string{"until": "2026-10-01"}}, http.StatusBadRequest, "invalid_request", "after today")
	f.wantError(http.MethodGet, "/api/v1/actions?status=lost", nil, http.StatusBadRequest, "invalid_request", "unknown status")

	if got := f.do(http.MethodGet, "/api/v1/actions/1", nil, http.StatusOK); got["kind"] != "mark_paid" {
		t.Fatalf("get = %v", got)
	}
	audit := f.items("/api/v1/audit?limit=100")
	if len(audit) < 8 || audit[0]["actor"] == nil {
		t.Fatalf("audit = %v", audit)
	}
}

func TestMemories(t *testing.T) {
	f := newFixture(t)
	m := f.do(http.MethodPost, "/api/v1/memories", map[string]any{"content": "Car insurance is with Acko", "kind": "fact"}, http.StatusCreated)
	if m["id"] != float64(1) || m["source"] != "user" {
		t.Fatalf("memory = %v", m)
	}
	if again := f.do(http.MethodPost, "/api/v1/memories", map[string]any{"content": "car insurance is with acko"}, http.StatusOK); again["id"] != float64(1) {
		t.Fatalf("duplicate = %v", again)
	}
	f.do(http.MethodPost, "/api/v1/memories", map[string]any{"content": "Prefers reminders in the evening", "kind": "preference"}, http.StatusCreated)
	f.wantError(http.MethodPost, "/api/v1/memories", map[string]any{"content": "x", "kind": "secret"}, http.StatusBadRequest, "invalid_request", "kind must be")
	f.wantError(http.MethodPost, "/api/v1/memories", map[string]any{"content": "  "}, http.StatusBadRequest, "invalid_request", "content is empty")

	if all := f.items("/api/v1/memories"); len(all) != 2 {
		t.Fatalf("list = %v", all)
	}
	hits := f.items("/api/v1/memories/recall?q=who+insures+my+car&limit=3")
	if len(hits) != 1 || hits[0]["content"] != "Car insurance is with Acko" || hits[0]["score"] == nil {
		t.Fatalf("recall = %v", hits)
	}
	f.wantError(http.MethodGet, "/api/v1/memories/recall", nil, http.StatusBadRequest, "invalid_request", "q is required")

	edited := f.do(http.MethodPatch, "/api/v1/memories/1", map[string]any{"content": "Car insurance is with Digit"}, http.StatusOK)
	if edited["content"] != "Car insurance is with Digit" {
		t.Fatalf("edited = %v", edited)
	}
	f.wantError(http.MethodPatch, "/api/v1/memories/9", map[string]any{"content": "x"}, http.StatusNotFound, "not_found", "not found")
	f.do(http.MethodDelete, "/api/v1/memories/1", nil, http.StatusNoContent)
	f.wantError(http.MethodDelete, "/api/v1/memories/1", nil, http.StatusNotFound, "not_found", "not found")
}

func TestOverviewNotificationsAndSweep(t *testing.T) {
	f := newFixture(t)
	for _, o := range []map[string]any{
		{"title": "Rent", "due_on": "2026-10-15"},
		{"title": "Water", "due_on": "2026-10-18"},
		{"title": "Gym", "due_on": "2026-10-22"},
		{"title": "Passport", "due_on": "2027-02-01"},
		{"title": "Find PAN card"},
		{"title": "Netflix", "status": "snoozed", "snoozed_until": "2026-11-01"},
	} {
		f.do(http.MethodPost, "/api/v1/obligations", o, http.StatusCreated)
	}
	f.do(http.MethodPost, "/api/v1/actions", map[string]any{"kind": "send_email", "payload": map[string]string{"to": "a@b.example", "subject": "Hi", "body": "x"}}, http.StatusCreated)

	ov := f.do(http.MethodGet, "/api/v1/overview", nil, http.StatusOK)
	want := map[string]float64{"overdue": 1, "due_today": 1, "due_next_7_days": 1, "snoozed": 1, "without_due_date": 1, "pending_actions": 1, "pending_proposals": 0}
	for k, v := range want {
		if ov[k] != v {
			t.Errorf("%s = %v, want %v", k, ov[k], v)
		}
	}
	if up := ov["upcoming"].([]any); len(up) != 4 || up[0].(map[string]any)["title"] != "Rent" {
		t.Errorf("upcoming = %v", up)
	}

	rep := f.do(http.MethodPost, "/api/v1/sweep", nil, http.StatusOK)
	if rep["reminders"] != float64(3) || rep["review"] != nil {
		t.Fatalf("sweep = %v", rep)
	}
	rep = f.do(http.MethodPost, "/api/v1/sweep", map[string]bool{"review": true}, http.StatusOK)
	review := rep["review"].(map[string]any)
	if rep["reminders"] != float64(0) || review["kind"] != "review" || !strings.Contains(review["body"].(string), "Rent") {
		t.Fatalf("sweep with review = %v", rep)
	}
	if notes := f.items("/api/v1/notifications?limit=10"); len(notes) != 4 || notes[0]["kind"] != "review" {
		t.Fatalf("notifications = %v", notes)
	}

	f.opts.Sweeper = nil
	f.rebuild()
	f.wantError(http.MethodPost, "/api/v1/sweep", nil, http.StatusServiceUnavailable, "not_configured", "sweep is off")
}

func TestWebSearch(t *testing.T) {
	f := newFixture(t)
	out := f.do(http.MethodGet, "/api/v1/search?q=ITR+deadline&topic=news&max_results=3&domains=incometax.gov.in", nil, http.StatusOK)
	if out["answer"] != "15 September 2026" || len(out["results"].([]any)) != 1 {
		t.Fatalf("search = %v", out)
	}
	f.wantError(http.MethodGet, "/api/v1/search", nil, http.StatusBadRequest, "invalid_request", "query is empty")
	f.wantError(http.MethodGet, "/api/v1/search?q=x&max_results=50", nil, http.StatusBadRequest, "invalid_request", "max_results")

	f.opts.Search = nil
	f.rebuild()
	f.wantError(http.MethodGet, "/api/v1/search?q=x", nil, http.StatusServiceUnavailable, "not_configured", "TAVILY_API_KEY")
}

func TestClassifyModelErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{llm.ErrBudgetExceeded, http.StatusServiceUnavailable, "budget_exceeded"},
		{&llm.APIError{Status: 402, Message: "no credit"}, http.StatusServiceUnavailable, "out_of_credits"},
		{&llm.APIError{Status: 500, Message: "boom"}, http.StatusBadGateway, "model_error"},
		{tavily.ErrQuotaExceeded, http.StatusBadGateway, "search_error"},
		{ingest.ErrNoModel, http.StatusServiceUnavailable, "not_configured"},
		{store.ErrAlreadyDecided, http.StatusConflict, "already_decided"},
		{fmt.Errorf("disk full"), http.StatusInternalServerError, "internal"},
	}
	for _, tc := range cases {
		status, code, msg := classify(tc.err)
		if status != tc.status || code != tc.code {
			t.Errorf("%v: %d %s, want %d %s", tc.err, status, code, tc.status, tc.code)
		}
		if code == "internal" && msg != "internal error" {
			t.Errorf("internal errors must not leak details: %q", msg)
		}
	}
}
