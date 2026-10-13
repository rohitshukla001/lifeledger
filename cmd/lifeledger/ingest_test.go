package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runIngest(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runWithInput(append([]string{"-env-file", "", "ingest"}, args...), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestIngestCommandsEndToEnd(t *testing.T) {
	fakeTokenFactory(t, nil)
	dir := t.TempDir()
	t.Setenv("LIFELEDGER_DB_PATH", filepath.Join(dir, "ll.db"))
	email := filepath.Join(dir, "airtel.eml")
	if err := os.WriteFile(email, []byte("Your Airtel bill of Rs 999 is due. Pay by 15 Nov."), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runIngest(t, "", "add", email)
	if code != 0 || !strings.Contains(out, "Airtel invoice") ||
		!strings.Contains(out, "#1 [bill] Airtel broadband — INR 999.00, due 2026-11-15, monthly  (confidence 90%, pending)") ||
		!strings.Contains(out, `"Pay by 15 Nov"`) {
		t.Fatalf("add: %d\n%s\n%s", code, out, errOut)
	}

	if code, out, _ := runIngest(t, "Your Airtel bill of Rs 999 is due. Pay by 15 Nov.", "add", "-name", "pasted", "-"); code != 0 || !strings.Contains(out, "already ingested as #1") {
		t.Fatalf("same text from stdin must be reused: %d %q", code, out)
	}

	if code, out, _ := runIngest(t, "", "pending"); code != 0 || !strings.Contains(out, "#1 [bill] Airtel broadband") {
		t.Fatalf("pending: %d %q", code, out)
	}
	if code, out, _ := runIngest(t, "", "accept", "1"); code != 0 || out != "accepted #1 as obligation #1: Airtel broadband\n" {
		t.Fatalf("accept: %d %q", code, out)
	}
	if code, _, errOut := runIngest(t, "", "reject", "1", "7"); code != 1 || !strings.Contains(errOut, "#1: store: proposal was already accepted or rejected") || !strings.Contains(errOut, "no proposal #7") {
		t.Fatalf("reject: %d %q", code, errOut)
	}
	if code, out, _ := runIngest(t, "", "pending"); code != 0 || out != "no pending proposals\n" {
		t.Fatalf("pending after accept: %d %q", code, out)
	}
}

func TestIngestRejectsBadInput(t *testing.T) {
	fakeTokenFactory(t, nil)
	t.Setenv("LIFELEDGER_DB_PATH", filepath.Join(t.TempDir(), "ll.db"))

	cases := []struct {
		args []string
		code int
		msg  string
	}{
		{nil, 2, "usage"},
		{[]string{"shred"}, 2, "unknown ingest command"},
		{[]string{"add"}, 2, "usage"},
		{[]string{"add", "/no/such/file"}, 1, "no such file"},
		{[]string{"accept"}, 2, "usage"},
		{[]string{"accept", "x"}, 2, "invalid id"},
	}
	for _, c := range cases {
		code, _, errOut := runIngest(t, "", c.args...)
		if code != c.code || !strings.Contains(errOut, c.msg) {
			t.Errorf("%v: code %d, stderr %q; want %d and %q", c.args, code, errOut, c.code, c.msg)
		}
	}

	t.Setenv("NEBIUS_API_KEY", "")
	if code, _, errOut := runIngest(t, "x", "add", "-"); code != 1 || !strings.Contains(errOut, "NEBIUS_API_KEY") {
		t.Fatalf("add without key: %d %q", code, errOut)
	}
}
