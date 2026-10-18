package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("LIFELEDGER_ADDR", "")
	t.Setenv("LIFELEDGER_ENV", "")
	t.Setenv("LIFELEDGER_LOG_LEVEL", "")
	t.Setenv("PORT", "")
	t.Setenv("LIFELEDGER_DB_PATH", "")
	t.Setenv("LIFELEDGER_TIMEZONE", "")
	t.Setenv("LIFELEDGER_CURRENCY", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":8080" || cfg.Env != EnvDev || cfg.LogLevel != slog.LevelInfo || cfg.DBPath != "data/lifeledger.db" ||
		cfg.Location != time.UTC || cfg.Currency != "USD" {
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

func TestLoadTimezoneAndCurrency(t *testing.T) {
	t.Setenv("LIFELEDGER_TIMEZONE", "Asia/Kolkata")
	t.Setenv("LIFELEDGER_CURRENCY", "inr")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Location.String() != "Asia/Kolkata" || cfg.Currency != "INR" {
		t.Fatalf("location = %v, currency = %q", cfg.Location, cfg.Currency)
	}

	for key, bad := range map[string]string{"LIFELEDGER_TIMEZONE": "Mars/Olympus", "LIFELEDGER_CURRENCY": "RUPEE"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, bad)
			if _, err := Load(""); err == nil {
				t.Fatalf("%s=%s: want error", key, bad)
			}
		})
	}
}

func TestLoadSMTP(t *testing.T) {
	t.Setenv("LIFELEDGER_SMTP_HOST", "")
	t.Setenv("LIFELEDGER_SMTP_PORT", "")
	cfg, err := Load("")
	if err != nil || cfg.SMTPHost != "" || cfg.SMTPPort != 587 {
		t.Fatalf("defaults: %+v, %v", cfg, err)
	}

	t.Setenv("LIFELEDGER_SMTP_HOST", "smtp.example.com")
	t.Setenv("LIFELEDGER_SMTP_FROM", "")
	if _, err := Load(""); err == nil {
		t.Fatal("a host without a sender must fail")
	}
	t.Setenv("LIFELEDGER_SMTP_FROM", "me@example.com")
	t.Setenv("LIFELEDGER_SMTP_PORT", "smtp")
	if _, err := Load(""); err == nil {
		t.Fatal("a port that is not a number must fail")
	}
	t.Setenv("LIFELEDGER_SMTP_PORT", "465")
	if cfg, err := Load(""); err != nil || cfg.SMTPPort != 465 || cfg.SMTPFrom != "me@example.com" {
		t.Fatalf("custom: %+v, %v", cfg, err)
	}
}

func TestLoadTavily(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("TAVILY_BASE_URL", "")
	cfg, err := Load("")
	if err != nil || cfg.TavilyAPIKey != "" || cfg.TavilyBaseURL != "https://api.tavily.com" {
		t.Fatalf("defaults: %+v, %v", cfg, err)
	}

	t.Setenv("TAVILY_API_KEY", "tvly-abc")
	t.Setenv("TAVILY_BASE_URL", "http://localhost:9000")
	if cfg, err := Load(""); err != nil || cfg.TavilyAPIKey != "tvly-abc" || cfg.TavilyBaseURL != "http://localhost:9000" {
		t.Fatalf("custom: %+v, %v", cfg, err)
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
	t.Setenv("LIFELEDGER_API_TOKEN", strings.Repeat("t", 32))

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

func TestLoadSweepDefaults(t *testing.T) {
	for _, k := range []string{"LIFELEDGER_SWEEP_INTERVAL", "LIFELEDGER_REMIND_HOUR", "LIFELEDGER_REVIEW_DAY", "LIFELEDGER_REVIEW_HOUR", "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"} {
		t.Setenv(k, "")
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SweepInterval != 15*time.Minute || cfg.RemindHour != 9 || !cfg.ReviewEnabled || cfg.ReviewDay != time.Sunday || cfg.ReviewHour != 18 {
		t.Fatalf("defaults: %+v", cfg)
	}

	t.Setenv("LIFELEDGER_SWEEP_INTERVAL", "0")
	t.Setenv("LIFELEDGER_REMIND_HOUR", "8")
	t.Setenv("LIFELEDGER_REVIEW_DAY", "Monday")
	t.Setenv("LIFELEDGER_REVIEW_HOUR", "7")
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:ABC")
	t.Setenv("TELEGRAM_CHAT_ID", "42")
	cfg, err = Load("")
	if err != nil || cfg.SweepInterval != 0 || cfg.RemindHour != 8 || cfg.ReviewDay != time.Monday || cfg.ReviewHour != 7 || cfg.TelegramChatID != "42" {
		t.Fatalf("custom: %+v, %v", cfg, err)
	}

	t.Setenv("LIFELEDGER_REVIEW_DAY", "off")
	if cfg, err = Load(""); err != nil || cfg.ReviewEnabled {
		t.Fatalf("off: %+v, %v", cfg, err)
	}
}

func TestLoadRejectsBadSweepSettings(t *testing.T) {
	cases := []struct{ key, value, want string }{
		{"LIFELEDGER_SWEEP_INTERVAL", "30s", "LIFELEDGER_SWEEP_INTERVAL"},
		{"LIFELEDGER_REMIND_HOUR", "24", "LIFELEDGER_REMIND_HOUR"},
		{"LIFELEDGER_REVIEW_HOUR", "evening", "LIFELEDGER_REVIEW_HOUR"},
		{"LIFELEDGER_REVIEW_DAY", "someday", "LIFELEDGER_REVIEW_DAY"},
		{"TELEGRAM_CHAT_ID", "42", "must be set together"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv("TELEGRAM_BOT_TOKEN", "")
			t.Setenv(tc.key, tc.value)
			if _, err := Load(""); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s=%s: err = %v", tc.key, tc.value, err)
			}
		})
	}
}

func TestLoadAPIToken(t *testing.T) {
	t.Setenv("LIFELEDGER_ENV", "dev")
	t.Setenv("LIFELEDGER_API_TOKEN", "")
	if cfg, err := Load(""); err != nil || cfg.APIToken != "" {
		t.Fatalf("dev without a token: %+v, %v", cfg, err)
	}

	t.Setenv("LIFELEDGER_ENV", "prod")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "LIFELEDGER_API_TOKEN is required") {
		t.Fatalf("prod without a token: %v", err)
	}
	t.Setenv("LIFELEDGER_API_TOKEN", "short")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "at least 24 characters") {
		t.Fatalf("short token: %v", err)
	}
	t.Setenv("LIFELEDGER_API_TOKEN", strings.Repeat("k", 24))
	if cfg, err := Load(""); err != nil || len(cfg.APIToken) != 24 {
		t.Fatalf("valid token: %+v, %v", cfg, err)
	}
}
