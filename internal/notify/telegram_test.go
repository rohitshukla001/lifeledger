package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTelegramSend(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/bot123:ABC/sendMessage" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	}))
	defer srv.Close()

	tg, err := NewTelegram(TelegramOptions{Token: "123:ABC", ChatID: "42", BaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Send(context.Background(), "  Rent is due tomorrow.\n"); err != nil {
		t.Fatal(err)
	}
	if got["chat_id"] != "42" || got["text"] != "Rent is due tomorrow." {
		t.Fatalf("body = %v", got)
	}
	if lp := got["link_preview_options"].(map[string]any); lp["is_disabled"] != true {
		t.Fatalf("link previews must be off: %v", got)
	}

	if err := tg.Send(context.Background(), strings.Repeat("é", 5000)); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(got["text"].(string)); n != MaxMessageRunes {
		t.Fatalf("long text has %d runes, want %d", n, MaxMessageRunes)
	}
	if err := tg.Send(context.Background(), " \n"); err == nil {
		t.Fatal("an empty message must fail")
	}
}

func TestTelegramErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, "telegram: 400 Bad Request: chat not found"},
		{http.StatusTooManyRequests, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 5","parameters":{"retry_after":5}}`, "telegram: 429 Too Many Requests"},
		{http.StatusBadGateway, `<html>bad gateway</html>`, "HTTP 502 with a body that is not JSON"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		}))
		tg, _ := NewTelegram(TelegramOptions{Token: "123:SECRET", ChatID: "42", BaseURL: srv.URL})
		err := tg.Send(context.Background(), "hi")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d: err = %v, want %q", tc.status, err, tc.want)
		}
		srv.Close()
	}
}

func TestTelegramTransportErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	tg, _ := NewTelegram(TelegramOptions{Token: "123:SECRET", ChatID: "42", BaseURL: srv.URL})
	err := tg.Send(context.Background(), "hi")
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.HasPrefix(err.Error(), "telegram: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewTelegramValidates(t *testing.T) {
	for _, o := range []TelegramOptions{{Token: "t"}, {ChatID: "1"}, {Token: "123/../x", ChatID: "1"}} {
		if _, err := NewTelegram(o); err == nil {
			t.Errorf("%+v: want error", o)
		}
	}
	tg, err := NewTelegram(TelegramOptions{Token: "123:ABC", ChatID: "@me"})
	if err != nil || tg.endpoint != "https://api.telegram.org/bot123:ABC/sendMessage" {
		t.Fatalf("endpoint = %q, err = %v", tg.endpoint, err)
	}
}
