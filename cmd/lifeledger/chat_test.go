package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

func runChat(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runWithInput(append([]string{"-env-file", "", "chat"}, args...), strings.NewReader(input), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestChatSessionAndContinue(t *testing.T) {
	fakeTokenFactory(t, nil)
	dbPath := filepath.Join(t.TempDir(), "ll.db")
	t.Setenv("LIFELEDGER_DB_PATH", dbPath)

	code, out, errOut := runChat(t, "hello\n\n/new\nsecond topic\n/quit\nnever sent\n")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut)
	}
	if strings.Count(out, "ledger> pong from s") != 2 || !strings.Contains(out, "Started a new conversation.") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "[conversation #1,") || !strings.Contains(errOut, "[conversation #2,") {
		t.Fatalf("stderr = %q", errOut)
	}

	code, out, _ = runChat(t, "follow up", "-c", "1")
	if code != 0 || !strings.Contains(out, "Continuing conversation #1: hello") {
		t.Fatalf("continue: %d %q", code, out)
	}

	db, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	msgs, _ := db.Messages(context.Background(), 1)
	if len(msgs) != 4 || msgs[2].Content != "follow up" {
		t.Fatalf("conversation 1 = %+v", msgs)
	}
	convs, _ := db.ListConversations(context.Background(), 0)
	if len(convs) != 2 {
		t.Fatalf("conversations = %d, want 2", len(convs))
	}
}

func TestChatErrors(t *testing.T) {
	fakeTokenFactory(t, nil)
	t.Setenv("LIFELEDGER_DB_PATH", filepath.Join(t.TempDir(), "ll.db"))

	if code, _, errOut := runChat(t, "", "-c", "9"); code != 1 || !strings.Contains(errOut, "no conversation with id 9") {
		t.Fatalf("unknown conversation: %d %q", code, errOut)
	}

	t.Setenv("NEBIUS_API_KEY", "")
	if code, _, errOut := runChat(t, "hi\n"); code != 1 || !strings.Contains(errOut, "NEBIUS_API_KEY") {
		t.Fatalf("missing key: %d %q", code, errOut)
	}
}
