package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rohitshukla001/lifeledger/internal/store"
)

func TestSweepAndNotificationsCommands(t *testing.T) {
	fakeTokenFactory(t, nil)
	dbPath := filepath.Join(t.TempDir(), "ll.db")
	t.Setenv("LIFELEDGER_DB_PATH", dbPath)
	t.Setenv("LIFELEDGER_TIMEZONE", "UTC")
	t.Setenv("LIFELEDGER_REMIND_HOUR", "0")
	t.Setenv("LIFELEDGER_REVIEW_DAY", "off")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")

	db, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	today, _ := time.Parse(time.DateOnly, time.Now().UTC().Format(time.DateOnly))
	minor := int64(99900)
	if err := db.CreateObligation(context.Background(), &store.Obligation{Title: "Airtel broadband", AmountMinor: &minor, Currency: "INR", DueOn: &today}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if code, out, _ := runCLI(t, "", "notifications"); code != 0 || out != "no notifications yet\n" {
		t.Fatalf("empty list: %d %q", code, out)
	}

	code, out, errOut := runCLI(t, "", "sweep", "-review")
	if code != 0 {
		t.Fatalf("sweep: exit %d, stderr = %s", code, errOut)
	}
	if !strings.Contains(out, "LifeLedger weekly review") || !strings.Contains(out, "pong from u") ||
		!strings.Contains(out, "Airtel broadband, INR 999.00, due") || !strings.HasSuffix(out, "woke 0 snoozed, 1 new reminders, scheduled review not due, delivered 0, failed 0\n") {
		t.Fatalf("sweep output =\n%s", out)
	}

	code, out, _ = runCLI(t, "", "notifications", "list", "-n", "5")
	if code != 0 || !strings.Contains(out, "#2 ") || !strings.Contains(out, "Due today: Airtel broadband  [not sent]") ||
		!strings.Contains(out, "    pong from u") {
		t.Fatalf("list =\n%s", out)
	}

	if code, out, _ := runCLI(t, "", "sweep"); code != 0 || !strings.Contains(out, "0 new reminders") {
		t.Fatalf("second sweep: %d %q", code, out)
	}
}

func TestNotificationsTestNeedsTelegram(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	if code, _, errOut := runCLI(t, "", "notifications", "test"); code != 1 || !strings.Contains(errOut, "TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID") {
		t.Fatalf("exit %d, stderr = %s", code, errOut)
	}
	if code, _, errOut := runCLI(t, "", "notifications", "send"); code != 2 || !strings.Contains(errOut, "usage: lifeledger notifications") {
		t.Fatalf("exit %d, stderr = %s", code, errOut)
	}
}
