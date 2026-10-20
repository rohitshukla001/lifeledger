package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func TestHealthz(t *testing.T) {
	srv := New(Options{Addr: ":0", Logger: testLogger(), DB: fakeDB{}})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || body.Database != "ok" || body.Version == "" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestHealthzReportsDatabaseFailure(t *testing.T) {
	srv := New(Options{Addr: ":0", Logger: testLogger(), DB: fakeDB{err: errors.New("disk I/O error")}})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "unavailable" || body.Database != "unavailable" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestHealthzRejectsPost(t *testing.T) {
	srv := New(Options{Addr: ":0", Logger: testLogger(), DB: fakeDB{}})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	srv := New(Options{Addr: ":0", Logger: testLogger(), DB: fakeDB{}})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestServeAndGracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{Addr: ln.Addr().String(), Logger: testLogger(), DB: fakeDB{}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down in 5s")
	}
}

func TestAPIIsMountedAndPanicsAreRecovered(t *testing.T) {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("pong")) })
	api.HandleFunc("GET /api/v1/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	h := New(Options{Addr: ":0", Logger: testLogger(), DB: fakeDB{}, API: api}).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Fatalf("ping: %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Fatalf("boom: %d %q", rec.Code, rec.Body.String())
	}
}
