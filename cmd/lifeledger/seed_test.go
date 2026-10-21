package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedCommand(t *testing.T) {
	t.Setenv("NEBIUS_API_KEY", "")
	t.Setenv("LIFELEDGER_DB_PATH", filepath.Join(t.TempDir(), "seed.db"))

	code, out, errOut := runCLI(t, "", "seed")
	if code != 0 || !strings.HasPrefix(out, "seeded 9 obligations, 4 memories, 1 actions, 2 proposals into ") {
		t.Fatalf("seed: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := runCLI(t, "", "seed"); code != 1 || !strings.Contains(errOut, "already has data") {
		t.Fatalf("second seed: %d %q", code, errOut)
	}
	if code, out, _ := runCLI(t, "", "actions"); code != 0 || !strings.Contains(out, "Request to waive the late fee") {
		t.Fatalf("pending actions: %d %q", code, out)
	}
}
