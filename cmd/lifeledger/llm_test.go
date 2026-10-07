package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeTokenFactory(t *testing.T, catalog []string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			var data []map[string]string
			for _, id := range catalog {
				data = append(data, map[string]string{"id": id})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		case "/v1/chat/completions":
			var req struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "pong from " + req.Model}}},
				"usage":   map[string]int{"prompt_tokens": 12, "completion_tokens": 3},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("NEBIUS_API_KEY", "test-key")
	t.Setenv("NEBIUS_BASE_URL", srv.URL+"/v1/")
	t.Setenv("LIFELEDGER_MODEL_NANO", "n")
	t.Setenv("LIFELEDGER_MODEL_SUPER", "s")
	t.Setenv("LIFELEDGER_MODEL_ULTRA", "u")
}

func TestAskPrintsReply(t *testing.T) {
	fakeTokenFactory(t, nil)
	var out, errOut bytes.Buffer
	if code := run([]string{"-env-file", "", "ask", "-tier", "nano", "ping"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "pong from n" {
		t.Fatalf("stdout = %q", got)
	}
	if !strings.Contains(errOut.String(), "tokens=12/3") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestAskRejectsBadInput(t *testing.T) {
	fakeTokenFactory(t, nil)
	for _, args := range [][]string{{"ask"}, {"ask", "-tier", "mega", "hi"}} {
		var out, errOut bytes.Buffer
		if code := run(append([]string{"-env-file", ""}, args...), &out, &errOut); code != 2 {
			t.Errorf("%v: exit code = %d, want 2", args, code)
		}
	}
}

func TestAskNeedsAPIKey(t *testing.T) {
	fakeTokenFactory(t, nil)
	t.Setenv("NEBIUS_API_KEY", "")
	var out, errOut bytes.Buffer
	if code := run([]string{"-env-file", "", "ask", "hi"}, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "NEBIUS_API_KEY") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestModelsMarksConfiguredTiers(t *testing.T) {
	fakeTokenFactory(t, []string{"n", "other", "s", "u"})
	var out, errOut bytes.Buffer
	if code := run([]string{"-env-file", "", "models"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	want := "* n (nano)\n  other\n* s (super)\n* u (ultra)\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

func TestModelsFailsWhenConfiguredModelIsMissing(t *testing.T) {
	fakeTokenFactory(t, []string{"n", "s"})
	var out, errOut bytes.Buffer
	if code := run([]string{"-env-file", "", "models"}, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), `ultra model "u"`) {
		t.Fatalf("stderr = %q", errOut.String())
	}
}
