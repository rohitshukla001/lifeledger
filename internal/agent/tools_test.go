package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rohitshukla001/lifeledger/internal/llm"
	"github.com/rohitshukla001/lifeledger/internal/store"
)

func runTool(t *testing.T, f *fixture, name, args string) (map[string]any, error) {
	t.Helper()
	out, err := f.agent.tools.run(context.Background(), f.conv.ID, llm.FunctionCall{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("tool %s returned invalid JSON: %s", name, out)
	}
	return m, nil
}

func TestToolSchemasAreValid(t *testing.T) {
	f := newFixture(t)
	for _, d := range f.agent.tools.definitions() {
		var schema map[string]any
		if err := json.Unmarshal(d.Function.Parameters, &schema); err != nil || schema["type"] != "object" {
			t.Errorf("%s: bad schema: %v", d.Function.Name, err)
		}
	}
}

func TestAddObligationValidation(t *testing.T) {
	f := newFixture(t)
	bad := map[string]string{
		`{}`:                                  "title is required",
		`{"title":"x","category":"tax"}`:      "unknown category",
		`{"title":"x","recurrence":"daily"}`:  "unknown recurrence",
		`{"title":"x","amount":"12.345"}`:     "more than 2 decimal places",
		`{"title":"x","amount":"-5"}`:         "not a positive number",
		`{"title":"x","currency":"RUPEES"}`:   "3-letter",
		`{"title":"x","due_on":"21/10/2026"}`: "YYYY-MM-DD",
		`{"title":"x","amount":{"value":1}}`:  "not a positive number",
		`{"title":5}`:                         "invalid arguments",
	}
	for args, want := range bad {
		if _, err := runTool(t, f, "add_obligation", args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", args, err, want)
		}
	}
}

func TestAddObligationAcceptsNumericAmountAndIgnoresStatus(t *testing.T) {
	f := newFixture(t)
	out, err := runTool(t, f, "add_obligation", `{"title":"Netflix","category":"subscription","amount":649,"currency":"inr","status":"done"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["amount"] != "649.00" || out["currency"] != "INR" || out["status"] != "open" || out["due_on"] != nil {
		t.Fatalf("out = %v", out)
	}
}

func TestListObligationsFilters(t *testing.T) {
	f := newFixture(t)
	for _, args := range []string{
		`{"title":"Rent","category":"bill","due_on":"2026-10-12"}`,
		`{"title":"Insurance","category":"renewal","due_on":"2026-12-01"}`,
		`{"title":"Old bill","category":"bill","due_on":"2026-09-01"}`,
		`{"title":"Warranty card","category":"warranty"}`,
	} {
		if _, err := runTool(t, f, "add_obligation", args); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runTool(t, f, "update_obligation", `{"id":3,"status":"done"}`); err != nil {
		t.Fatal(err)
	}

	titles := func(args string) string {
		t.Helper()
		out, err := runTool(t, f, "list_obligations", args)
		if err != nil {
			t.Fatal(err)
		}
		if out["today"] != "2026-10-10" {
			t.Fatalf("today = %v", out["today"])
		}
		var names []string
		for _, o := range out["obligations"].([]any) {
			names = append(names, o.(map[string]any)["title"].(string))
		}
		return strings.Join(names, ",")
	}

	cases := map[string]string{
		`{}`:                         "Rent,Insurance,Warranty card",
		`{"status":"all"}`:           "Old bill,Rent,Insurance,Warranty card",
		`{"status":"done"}`:          "Old bill",
		`{"due_within_days":7}`:      "Rent",
		`{"category":"renewal"}`:     "Insurance",
		`{"status":"all","limit":1}`: "Old bill",
	}
	for args, want := range cases {
		if got := titles(args); got != want {
			t.Errorf("%s: got %s, want %s", args, got, want)
		}
	}
	if _, err := runTool(t, f, "list_obligations", `{"status":"lost"}`); err == nil {
		t.Error("unknown status must fail")
	}
}

func TestUpdateObligationChangesOnlyGivenFields(t *testing.T) {
	f := newFixture(t)
	if _, err := runTool(t, f, "add_obligation", `{"title":"Broadband","category":"bill","amount":"999","due_on":"2026-10-15","notes":"Airtel"}`); err != nil {
		t.Fatal(err)
	}

	out, err := runTool(t, f, "update_obligation", `{"id":1,"status":"snoozed","snoozed_until":"2026-10-13"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != "snoozed" || out["snoozed_until"] != "2026-10-13" || out["amount"] != "999.00" || out["notes"] != "Airtel" || out["title"] != "Broadband" {
		t.Fatalf("out = %v", out)
	}

	out, err = runTool(t, f, "update_obligation", `{"id":1,"due_on":"","amount":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if out["due_on"] != nil || out["amount"] != nil {
		t.Fatalf("empty strings must clear the fields: %v", out)
	}
	o, _ := f.store.GetObligation(context.Background(), 1)
	if o.DueOn != nil || o.AmountMinor != nil || o.Status != store.StatusSnoozed {
		t.Fatalf("stored = %+v", o)
	}
}

func TestRememberAndRecallTools(t *testing.T) {
	f := newFixture(t)
	out, err := runTool(t, f, "remember", `{"content":"Prefers WhatsApp reminders","kind":"preference"}`)
	if err != nil || out["already_known"] != false {
		t.Fatalf("remember: %v, %v", out, err)
	}
	out, _ = runTool(t, f, "remember", `{"content":"prefers whatsapp reminders"}`)
	if out["already_known"] != true {
		t.Fatalf("duplicate must be reported as already known: %v", out)
	}
	if _, err := runTool(t, f, "remember", `{"content":"x","kind":"secret"}`); err == nil {
		t.Fatal("unknown kind must fail")
	}

	m, _ := f.store.GetMemory(context.Background(), 1)
	if m.Source != "chat" || m.SourceRef != "conversation:1" {
		t.Fatalf("memory source = %+v", m)
	}

	out, err = runTool(t, f, "recall", `{"query":"how do I like reminders"}`)
	if err != nil {
		t.Fatal(err)
	}
	if hits := out["memories"].([]any); len(hits) != 1 {
		t.Fatalf("recall = %v", out)
	}
}

func TestParseAmount(t *testing.T) {
	ok := map[string]int64{"0": 0, "12": 1200, "12.5": 1250, "1,234.56": 123456, ".75": 75, " 2 340 ": 234000, "99.": 9900}
	for in, want := range ok {
		if got, err := parseAmount(in); err != nil || got != want {
			t.Errorf("parseAmount(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-1", "1.234", "1e5", "92233720368547758.08"} {
		if _, err := parseAmount(in); err == nil {
			t.Errorf("parseAmount(%q): want error", in)
		}
	}
	if got := formatAmount(123405); got != "1234.05" {
		t.Errorf("formatAmount = %q", got)
	}
}
