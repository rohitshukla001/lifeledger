package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("LIFELEDGER_ADDR", "")
	t.Setenv("LIFELEDGER_ENV", "")
	t.Setenv("LIFELEDGER_LOG_LEVEL", "")
	t.Setenv("PORT", "")
	t.Setenv("LIFELEDGER_DB_PATH", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":8080" || cfg.Env != EnvDev || cfg.LogLevel != slog.LevelInfo || cfg.DBPath != "data/lifeledger.db" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadPortFallback(t *testing.T) {
	t.Setenv("LIFELEDGER_ADDR", "")
	t.Setenv("PORT", "9090")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":9090" {
		t.Fatalf("Addr = %q, want :9090", cfg.Addr)
	}
}

func TestLoadAddrBeatsPort(t *testing.T) {
	t.Setenv("LIFELEDGER_ADDR", "127.0.0.1:7000")
	t.Setenv("PORT", "9090")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:7000" {
		t.Fatalf("Addr = %q, want 127.0.0.1:7000", cfg.Addr)
	}
}

func TestLoadModelDefaultsAndBudget(t *testing.T) {
	t.Setenv("LIFELEDGER_MODEL_SUPER", "")
	t.Setenv("LIFELEDGER_MODEL_EMBED", "")
	t.Setenv("LIFELEDGER_DAILY_BUDGET_USD", "0.5")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ModelSuper != "nvidia/nemotron-3-super-120b-a12b" || cfg.ModelEmbed != "Qwen/Qwen3-Embedding-8B" || cfg.DailyBudgetUSD != 0.5 {
		t.Fatalf("unexpected llm config: %+v", cfg)
	}
}

func TestLoadRejectsBadBudget(t *testing.T) {
	for _, v := range []string{"-1", "lots"} {
		t.Setenv("LIFELEDGER_DAILY_BUDGET_USD", v)
		if _, err := Load(""); err == nil {
			t.Errorf("budget %q: want error", v)
		}
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	t.Setenv("LIFELEDGER_ENV", "staging")
	t.Setenv("LIFELEDGER_LOG_LEVEL", "loud")

	if _, err := Load(""); err == nil {
		t.Fatal("Load: want error for bad env and log level, got nil")
	}
}

func TestDotEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# comment
export LIFELEDGER_ENV=prod
LIFELEDGER_LOG_LEVEL="debug"
LL_TEST_INLINE=value # trailing comment
LL_TEST_SINGLE='a # b'
LL_TEST_KEEP=from-file
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"LIFELEDGER_ENV", "LIFELEDGER_LOG_LEVEL", "LL_TEST_INLINE", "LL_TEST_SINGLE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("LL_TEST_KEEP", "from-process")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != EnvProd || cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("file values not applied: %+v", cfg)
	}
	if got := os.Getenv("LL_TEST_INLINE"); got != "value" {
		t.Errorf("LL_TEST_INLINE = %q, want value", got)
	}
	if got := os.Getenv("LL_TEST_SINGLE"); got != "a # b" {
		t.Errorf("LL_TEST_SINGLE = %q, want 'a # b'", got)
	}
	if got := os.Getenv("LL_TEST_KEEP"); got != "from-process" {
		t.Errorf("LL_TEST_KEEP = %q, process value must win", got)
	}
}

func TestDotEnvMissingFileIsOK(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatalf("Load with missing file: %v", err)
	}
}

func TestDotEnvBadLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("NOEQUALS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load: want error for a line with no '=', got nil")
	}
}
