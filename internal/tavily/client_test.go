package tavily

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Options{APIKey: "tvly-test", BaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	return c, &waits
}

func TestSearchSendsRequestAndParsesResponse(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tvly-test" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{
			"query": "ITR filing deadline 2026", "topic": "news", "search_depth": "basic", "max_results": float64(3),
			"include_answer": true, "include_usage": true, "time_range": "month",
		}
		for k, v := range want {
			if body[k] != v {
				t.Errorf("%s = %v, want %v", k, body[k], v)
			}
		}
		if d := body["include_domains"].([]any); len(d) != 1 || d[0] != "incometax.gov.in" {
			t.Errorf("include_domains = %v", body["include_domains"])
		}
		w.Write([]byte(`{
			"query": "ITR filing deadline 2026",
			"answer": "The deadline is 15 September 2026.",
			"results": [{"title": "Due dates", "url": "https://incometax.gov.in/due-dates", "content": "ITR due 15 Sep", "score": 0.91, "published_date": "2026-09-01"}],
			"response_time": 0.84,
			"usage": {"credits": 1}
		}`))
	})

	res, err := c.Search(context.Background(), Query{
		Text: "  ITR filing\n deadline 2026 ", Topic: "news", TimeRange: "month", MaxResults: 3,
		IncludeDomains: []string{"incometax.gov.in"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "The deadline is 15 September 2026." || len(res.Results) != 1 || res.Usage.Credits != 1 {
		t.Fatalf("res = %+v", res)
	}
	if r := res.Results[0]; r.URL != "https://incometax.gov.in/due-dates" || r.Score != 0.91 || r.PublishedDate != "2026-09-01" {
		t.Fatalf("result = %+v", r)
	}
}

func TestSearchDefaultsOmitOptionalFields(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["topic"] != "general" || body["max_results"] != float64(5) {
			t.Errorf("defaults = %v", body)
		}
		if _, ok := body["time_range"]; ok {
			t.Error("time_range must be omitted when empty")
		}
		if _, ok := body["include_domains"]; ok {
			t.Error("include_domains must be omitted when empty")
		}
		w.Write([]byte(`{"query":"q","results":[]}`))
	})
	if _, err := c.Search(context.Background(), Query{Text: "q"}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchValidatesQuery(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	cases := map[string]Query{
		"query is empty":            {Text: "  \n "},
		"longer than 400":           {Text: strings.Repeat("a", 401)},
		"topic must be one of":      {Text: "q", Topic: "sports"},
		"time range must be one of": {Text: "q", TimeRange: "decade"},
		"between 1 and 20":          {Text: "q", MaxResults: 21},
	}
	for want, q := range cases {
		if _, err := c.Search(context.Background(), q); !errors.Is(err, ErrInvalidQuery) || !strings.Contains(err.Error(), want) {
			t.Errorf("%+v: err = %v, want %q", q, err, want)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("an invalid query must not reach the API")
	}
}

func TestSearchRetriesRateLimitAndServerErrors(t *testing.T) {
	var calls atomic.Int32
	c, waits := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"detail":{"error":"slow down"}}`))
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.Write([]byte(`{"query":"q","results":[{"title":"t","url":"https://example.com"}]}`))
		}
	})
	res, err := c.Search(context.Background(), Query{Text: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(res.Results) != 1 {
		t.Fatalf("calls = %d, res = %+v", calls.Load(), res)
	}
	if len(*waits) != 2 || (*waits)[0] != 2*time.Second {
		t.Fatalf("waits = %v, the first must follow Retry-After", *waits)
	}
}

func TestSearchErrorsAreClassified(t *testing.T) {
	cases := []struct {
		status int
		body   string
		target error
		msg    string
	}{
		{http.StatusUnauthorized, `{"detail":{"error":"Unauthorized: missing or invalid API key."}}`, ErrUnauthorized, "missing or invalid API key"},
		{432, `{"detail":{"error":"This request exceeds your plan's set usage limit."}}`, ErrQuotaExceeded, "usage limit"},
		{433, `{"detail":"pay-as-you-go limit"}`, ErrQuotaExceeded, "pay-as-you-go limit"},
		{http.StatusBadRequest, `not json`, nil, "not json"},
	}
	for _, tc := range cases {
		var calls atomic.Int32
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		})
		_, err := c.Search(context.Background(), Query{Text: "q"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != tc.status || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%d: err = %v", tc.status, err)
		}
		if tc.target != nil && !errors.Is(err, tc.target) {
			t.Errorf("%d: err = %v, want %v", tc.status, err, tc.target)
		}
		if calls.Load() != 1 {
			t.Errorf("%d: calls = %d, client errors must not be retried", tc.status, calls.Load())
		}
	}
}

func TestSearchGivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := c.Search(context.Background(), Query{Text: "q"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable || calls.Load() != 3 {
		t.Fatalf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestSearchStopsWhenContextIsCancelled(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ctx, cancel := context.WithCancel(context.Background())
	c.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := c.Search(ctx, Query{Text: "q"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewValidatesOptions(t *testing.T) {
	if _, err := New(Options{}); err == nil || !strings.Contains(err.Error(), "TAVILY_API_KEY") {
		t.Fatalf("err = %v", err)
	}
	if _, err := New(Options{APIKey: "k", BaseURL: "api.tavily.com"}); err == nil {
		t.Fatal("a base URL without a scheme must fail")
	}
	c, err := New(Options{APIKey: "k"})
	if err != nil || c.endpoint != "https://api.tavily.com/search" {
		t.Fatalf("endpoint = %q, err = %v", c.endpoint, err)
	}
}
