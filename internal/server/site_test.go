package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testSite(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"index.html":                "<!doctype html><title>LifeLedger</title>",
		"main-ABCD1234.js":          "console.log(1)",
		"favicon.svg":               "<svg/>",
		"media/font-WXYZ9876.woff2": "font",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h, err := Site(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSiteServesTheAppShell(t *testing.T) {
	h := testSite(t)
	for _, path := range []string{"/", "/index.html", "/ledger", "/chat/12", "/../../etc/passwd"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>LifeLedger</title>") {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") ||
			rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: headers %v", path, rec.Header())
		}
	}
}

func TestSiteAssets(t *testing.T) {
	h := testSite(t)
	cases := []struct {
		path, cache string
		status      int
	}{
		{"/main-ABCD1234.js", "public, max-age=31536000, immutable", http.StatusOK},
		{"/media/font-WXYZ9876.woff2", "public, max-age=31536000, immutable", http.StatusOK},
		{"/favicon.svg", "no-cache", http.StatusOK},
		{"/missing-ABCD1234.js", "", http.StatusNotFound},
		{"/media", "no-cache", http.StatusOK},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.status || rec.Header().Get("Cache-Control") != tc.cache {
			t.Errorf("%s: %d %q, want %d %q", tc.path, rec.Code, rec.Header().Get("Cache-Control"), tc.status, tc.cache)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: %d %v", rec.Code, rec.Header())
	}
}

func TestSiteNeedsIndex(t *testing.T) {
	if _, err := Site(t.TempDir()); err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("err = %v", err)
	}
}

func TestServerRoutesSiteAPIAndHealth(t *testing.T) {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("pong")) })
	h := New(Options{Logger: testLogger(), DB: fakeDB{}, API: api, Site: testSite(t)}).Handler()

	for path, want := range map[string]string{
		"/api/v1/ping": "pong",
		"/healthz":     `"status":"ok"`,
		"/today":       "<title>LifeLedger</title>",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Body.String())
		}
	}
}
