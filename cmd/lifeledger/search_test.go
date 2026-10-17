package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeTavily(t *testing.T, status int, body string) *[]map[string]any {
	t.Helper()
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.Header.Get("Authorization") != "Bearer tvly-test" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		requests = append(requests, req)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TAVILY_API_KEY", "tvly-test")
	t.Setenv("TAVILY_BASE_URL", srv.URL)
	return &requests
}

func TestSearchPrintsAnswerAndSources(t *testing.T) {
	requests := fakeTavily(t, http.StatusOK, `{
		"answer": "The deadline is 15 September 2026.",
		"results": [
			{"title": "Due dates", "url": "https://incometax.gov.in/due-dates", "published_date": "2026-07-31"},
			{"title": "Explainer", "url": "https://news.example/itr"}
		],
		"response_time": 0.5,
		"usage": {"credits": 1}
	}`)

	code, out, errOut := runCLI(t, "", "search", "-topic", "news", "-range", "month", "-n", "2", "-site", "incometax.gov.in, news.example", "ITR", "deadline")
	if code != 0 {
		t.Fatalf("exit %d, stderr = %s", code, errOut)
	}
	want := "The deadline is 15 September 2026.\n\n" +
		"[1] Due dates\n    https://incometax.gov.in/due-dates\n    published 2026-07-31\n" +
		"[2] Explainer\n    https://news.example/itr\n"
	if out != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", out, want)
	}
	if !strings.Contains(errOut, "2 results, 1 credits") {
		t.Fatalf("stderr = %q", errOut)
	}

	req := (*requests)[0]
	if req["query"] != "ITR deadline" || req["topic"] != "news" || req["time_range"] != "month" || req["max_results"] != float64(2) {
		t.Fatalf("request = %v", req)
	}
	if d := req["include_domains"].([]any); len(d) != 2 || d[1] != "news.example" {
		t.Fatalf("include_domains = %v", d)
	}
}

func TestSearchFailures(t *testing.T) {
	fakeTavily(t, http.StatusUnauthorized, `{"detail":{"error":"Unauthorized: missing or invalid API key."}}`)
	if code, _, errOut := runCLI(t, "", "search", "x"); code != 1 || !strings.Contains(errOut, "invalid API key") {
		t.Fatalf("unauthorized: exit %d, stderr = %s", code, errOut)
	}
	if code, _, errOut := runCLI(t, "", "search", "-topic", "sports", "x"); code != 1 || !strings.Contains(errOut, "topic must be one of") {
		t.Fatalf("bad topic: exit %d, stderr = %s", code, errOut)
	}
	if code, _, _ := runCLI(t, "", "search"); code != 2 {
		t.Fatalf("missing query: exit %d", code)
	}

	t.Setenv("TAVILY_API_KEY", "")
	if code, _, errOut := runCLI(t, "", "search", "x"); code != 1 || !strings.Contains(errOut, "TAVILY_API_KEY is not set") {
		t.Fatalf("no key: exit %d, stderr = %s", code, errOut)
	}
}
