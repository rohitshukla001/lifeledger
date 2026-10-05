package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "lifeledger ") {
		t.Fatalf("stdout = %q, want prefix 'lifeledger '", out.String())
	}
}

func TestRunHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(errOut.String(), "serve") {
		t.Fatalf("help text does not list commands: %q", errOut.String())
	}
}

func TestRunNoCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-env-file", "", "fly"}, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), `unknown command "fly"`) {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestRunBadConfig(t *testing.T) {
	t.Setenv("LIFELEDGER_ENV", "staging")
	var out, errOut bytes.Buffer
	if code := run([]string{"-env-file", "", "serve"}, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "config error") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}
