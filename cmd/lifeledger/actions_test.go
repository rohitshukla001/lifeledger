package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/actions"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runWithInput(append([]string{"-env-file", ""}, args...), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func seedActions(t *testing.T, dbPath string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	due := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	o := &store.Obligation{Title: "Airtel broadband", DueOn: &due, Recurrence: store.RecurMonthly}
	if err := db.CreateObligation(ctx, o); err != nil {
		t.Fatal(err)
	}
	svc := actions.New(actions.Options{Store: db})
	email := json.RawMessage(`{"to":"billing@airtel.example","subject":"Late fee","body":"Please waive it."}`)
	for _, r := range []actions.Request{
		{Kind: actions.KindSendEmail, Payload: email, RequestedBy: actions.ActorAgent},
		{Kind: actions.KindSendEmail, Payload: email, RequestedBy: actions.ActorAgent},
		{Kind: actions.KindCalendarEvent, ObligationID: &o.ID, RequestedBy: actions.ActorAgent},
	} {
		if _, err := svc.Request(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestActionsCommands(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ll.db")
	t.Setenv("LIFELEDGER_DB_PATH", dbPath)
	t.Setenv("LIFELEDGER_SMTP_HOST", "")

	if code, out, _ := runCLI(t, "", "actions"); code != 0 || out != "no actions wait for approval\n" {
		t.Fatalf("empty pending: %d %q", code, out)
	}
	seedActions(t, dbPath)

	code, out, _ := runCLI(t, "", "actions")
	if code != 0 || strings.Count(out, "[pending]") != 2 || !strings.Contains(out, `#1 [pending] Email billing@airtel.example: "Late fee" — agent`) {
		t.Fatalf("pending: %d %q", code, out)
	}

	code, out, errOut := runCLI(t, "", "actions", "approve", "1")
	if code != 1 || !strings.Contains(out, "#1 [failed]") || !strings.Contains(out, "email is not configured") {
		t.Fatalf("approve without SMTP: %d %q %q", code, out, errOut)
	}
	if code, out, _ := runCLI(t, "", "actions", "reject", "2"); code != 0 || out != "rejected action #2\n" {
		t.Fatalf("reject: %d %q", code, out)
	}
	if code, _, errOut := runCLI(t, "", "actions", "approve", "2", "9"); code != 1 ||
		!strings.Contains(errOut, "action #2 was already approved or rejected") || !strings.Contains(errOut, "no action #9") {
		t.Fatalf("approve decided and missing: %d %q", code, errOut)
	}

	ics := filepath.Join(dir, "reminder.ics")
	if code, out, _ := runCLI(t, "", "actions", "ics", "-o", ics, "3"); code != 0 || out != "wrote "+ics+"\n" {
		t.Fatalf("ics: %d %q", code, out)
	}
	if b, _ := os.ReadFile(ics); !strings.Contains(string(b), "DTSTART;VALUE=DATE:20261115") {
		t.Fatalf("ics file = %q", b)
	}
	if code, _, errOut := runCLI(t, "", "actions", "ics", "1"); code != 1 || !strings.Contains(errOut, "not a finished calendar action") {
		t.Fatalf("ics of email: %d %q", code, errOut)
	}

	if code, out, _ := runCLI(t, "", "actions", "show", "3"); code != 0 || !strings.Contains(out, "#3 [done]") || !strings.Contains(out, `payload: {"remind_days_before":1}`) || strings.Contains(out, "BEGIN:VCALENDAR") {
		t.Fatalf("show: %d %q", code, out)
	}
	if code, out, _ := runCLI(t, "", "actions", "list", "-n", "2"); code != 0 || strings.Count(out, "\n") != 2 || !strings.HasPrefix(out, "#3") {
		t.Fatalf("list: %d %q", code, out)
	}

	code, out, _ = runCLI(t, "", "audit")
	for _, want := range []string{"agent   action.requested  action:1", "user    action.approved   action:1", "policy  action.failed     action:1", "user    action.rejected   action:2", "policy  action.done       action:3"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit misses %q:\n%s", want, out)
		}
	}
	if code != 0 {
		t.Fatalf("audit exit = %d", code)
	}

	for _, args := range [][]string{{"actions", "launch"}, {"actions", "approve"}, {"actions", "show"}, {"actions", "show", "x"}} {
		if code, _, _ := runCLI(t, "", args...); code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, code)
		}
	}
}

func TestChatApprovesActions(t *testing.T) {
	fakeTokenFactory(t, nil)
	dbPath := filepath.Join(t.TempDir(), "ll.db")
	t.Setenv("LIFELEDGER_DB_PATH", dbPath)
	t.Setenv("LIFELEDGER_SMTP_HOST", "")
	seedActions(t, dbPath)

	code, out, errOut := runChat(t, "/reject 2\n/approve 1\n/approve x\n")
	if code != 0 || !strings.Contains(out, "rejected action #2") || !strings.Contains(out, "#1 [failed]") || !strings.Contains(errOut, `invalid id "x"`) {
		t.Fatalf("chat: %d %q %q", code, out, errOut)
	}
}
