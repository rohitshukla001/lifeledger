package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

func runMemory(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"-env-file", "", "memory"}, args...), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestMemoryCommandsEndToEnd(t *testing.T) {
	fakeTokenFactory(t, nil)
	t.Setenv("LIFELEDGER_DB_PATH", filepath.Join(t.TempDir(), "ll.db"))

	if code, out, errOut := runMemory(t, "add", "Car insurance renews in March"); code != 0 || out != "#1 [fact] Car insurance renews in March  (source: cli)\n" {
		t.Fatalf("add: %d %q %q", code, out, errOut)
	}
	if code, out, _ := runMemory(t, "add", "-kind", "preference", "Electricity bills go to the HDFC card"); code != 0 || !strings.HasPrefix(out, "#2 [preference]") {
		t.Fatalf("add preference: %d %q", code, out)
	}
	if code, out, _ := runMemory(t, "add", "car insurance renews in march!"); code != 0 || !strings.HasPrefix(out, "already remembered: #1") {
		t.Fatalf("duplicate add: %d %q", code, out)
	}

	code, out, _ := runMemory(t, "recall", "-n", "1", "vehicle cover")
	if code != 0 || !strings.Contains(out, "#1 [fact] Car insurance") || strings.Count(out, "\n") != 1 {
		t.Fatalf("recall: %d %q", code, out)
	}

	if code, out, _ := runMemory(t, "edit", "#2", "Electricity bills go to the ICICI card"); code != 0 || !strings.Contains(out, "ICICI") {
		t.Fatalf("edit: %d %q", code, out)
	}
	if code, out, _ := runMemory(t, "forget", "1"); code != 0 || out != "forgot #1\n" {
		t.Fatalf("forget: %d %q", code, out)
	}
	if code, out, _ := runMemory(t, "list"); code != 0 || strings.Count(out, "\n") != 1 || !strings.Contains(out, "ICICI") {
		t.Fatalf("list: %d %q", code, out)
	}
	if code, out, _ := runMemory(t, "reindex"); code != 0 || out != "embedded 0 memories\n" {
		t.Fatalf("reindex: %d %q", code, out)
	}
}

func TestMemoryWorksWithoutAPIKey(t *testing.T) {
	fakeTokenFactory(t, nil)
	t.Setenv("NEBIUS_API_KEY", "")
	t.Setenv("LIFELEDGER_DB_PATH", filepath.Join(t.TempDir(), "ll.db"))

	if code, _, errOut := runMemory(t, "add", "Electricity provider is BESCOM"); code != 0 || !strings.Contains(errOut, "keyword matching only") {
		t.Fatalf("add without key: %d %q", code, errOut)
	}
	if code, out, _ := runMemory(t, "recall", "electricity provider"); code != 0 || !strings.Contains(out, "BESCOM") {
		t.Fatalf("keyword recall: %d %q", code, out)
	}
	if code, _, errOut := runMemory(t, "reindex"); code != 1 || !strings.Contains(errOut, "no embedding model") {
		t.Fatalf("reindex without key: %d %q", code, errOut)
	}
}

func TestMemoryRejectsBadInput(t *testing.T) {
	fakeTokenFactory(t, nil)
	t.Setenv("LIFELEDGER_DB_PATH", filepath.Join(t.TempDir(), "ll.db"))

	cases := []struct {
		args []string
		code int
		msg  string
	}{
		{nil, 2, "usage"},
		{[]string{"shout"}, 2, "unknown memory command"},
		{[]string{"forget", "abc"}, 2, "invalid id"},
		{[]string{"forget", "42"}, 1, "no memory with that id"},
		{[]string{"edit", "1"}, 2, "usage"},
		{[]string{"add", "-kind", "rumour", "x"}, 1, "store: add memory"},
		{[]string{"add"}, 1, "content is empty"},
	}
	for _, c := range cases {
		code, _, errOut := runMemory(t, c.args...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: code %d, stderr %q; want %d and %q", c.args, code, errOut, c.code, c.msg)
		}
	}
}

func TestMemoryListShowsWebSource(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ll.db")
	t.Setenv("NEBIUS_API_KEY", "")
	t.Setenv("LIFELEDGER_DB_PATH", dbPath)
	db, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	m := store.Memory{Kind: store.MemoryFact, Content: "ITR deadline is 15 September 2026", Source: "web", SourceRef: "https://incometax.gov.in/due-dates"}
	if err := db.AddMemory(context.Background(), &m); err != nil {
		t.Fatal(err)
	}
	db.Close()

	want := "#1 [fact] ITR deadline is 15 September 2026  (source: web https://incometax.gov.in/due-dates)\n"
	if code, out, errOut := runMemory(t, "list"); code != 0 || out != want {
		t.Fatalf("list: %d %q %q", code, out, errOut)
	}
}
