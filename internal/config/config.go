package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDev  = "dev"
	EnvProd = "prod"

	minTokenLen = 24
)

type Config struct {
	Addr     string
	APIToken string
	Env      string
	LogLevel slog.Level
	DBPath   string
	Location *time.Location
	Currency string

	NebiusAPIKey   string
	NebiusBaseURL  string
	ModelNano      string
	ModelSuper     string
	ModelUltra     string
	ModelEmbed     string
	DailyBudgetUSD float64

	TavilyAPIKey  string
	TavilyBaseURL string

	SweepInterval time.Duration
	RemindHour    int
	ReviewEnabled bool
	ReviewDay     time.Weekday
	ReviewHour    int

	TelegramBotToken string
	TelegramChatID   string

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
}

func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return Config{}, err
		}
	}

	cfg := Config{
		Addr:          getenv("LIFELEDGER_ADDR", ":8080"),
		APIToken:      os.Getenv("LIFELEDGER_API_TOKEN"),
		Env:           strings.ToLower(getenv("LIFELEDGER_ENV", EnvDev)),
		DBPath:        getenv("LIFELEDGER_DB_PATH", "data/lifeledger.db"),
		Currency:      strings.ToUpper(getenv("LIFELEDGER_CURRENCY", "USD")),
		NebiusAPIKey:  os.Getenv("NEBIUS_API_KEY"),
		NebiusBaseURL: getenv("NEBIUS_BASE_URL", "https://api.tokenfactory.nebius.com/v1/"),
		ModelNano:     getenv("LIFELEDGER_MODEL_NANO", "nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B"),
		ModelSuper:    getenv("LIFELEDGER_MODEL_SUPER", "nvidia/nemotron-3-super-120b-a12b"),
		ModelUltra:    getenv("LIFELEDGER_MODEL_ULTRA", "nvidia/Nemotron-3-Ultra-550b-a55b"),
		ModelEmbed:    getenv("LIFELEDGER_MODEL_EMBED", "Qwen/Qwen3-Embedding-8B"),
		TavilyAPIKey:  os.Getenv("TAVILY_API_KEY"),
		TavilyBaseURL: getenv("TAVILY_BASE_URL", "https://api.tavily.com"),

		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		SMTPHost:         os.Getenv("LIFELEDGER_SMTP_HOST"),
		SMTPUsername:     os.Getenv("LIFELEDGER_SMTP_USERNAME"),
		SMTPPassword:     os.Getenv("LIFELEDGER_SMTP_PASSWORD"),
		SMTPFrom:         os.Getenv("LIFELEDGER_SMTP_FROM"),
	}

	if port := os.Getenv("PORT"); port != "" && os.Getenv("LIFELEDGER_ADDR") == "" {
		cfg.Addr = ":" + port
	}

	var errs []error
	if cfg.Env != EnvDev && cfg.Env != EnvProd {
		errs = append(errs, fmt.Errorf("LIFELEDGER_ENV must be %q or %q, got %q", EnvDev, EnvProd, cfg.Env))
	}
	switch {
	case cfg.APIToken == "" && cfg.Env == EnvProd:
		errs = append(errs, errors.New("LIFELEDGER_API_TOKEN is required when LIFELEDGER_ENV is prod"))
	case cfg.APIToken != "" && len(cfg.APIToken) < minTokenLen:
		errs = append(errs, fmt.Errorf("LIFELEDGER_API_TOKEN must have at least %d characters", minTokenLen))
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LIFELEDGER_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LIFELEDGER_LOG_LEVEL: %w", err))
	}
	loc, err := time.LoadLocation(getenv("LIFELEDGER_TIMEZONE", "UTC"))
	if err != nil {
		errs = append(errs, fmt.Errorf("LIFELEDGER_TIMEZONE: %w", err))
	}
	cfg.Location = loc
	if len(cfg.Currency) != 3 {
		errs = append(errs, fmt.Errorf("LIFELEDGER_CURRENCY must be a 3-letter ISO 4217 code, got %q", cfg.Currency))
	}
	budget, err := strconv.ParseFloat(getenv("LIFELEDGER_DAILY_BUDGET_USD", "2"), 64)
	if err != nil || budget < 0 {
		errs = append(errs, fmt.Errorf("LIFELEDGER_DAILY_BUDGET_USD must be a number >= 0, got %q", os.Getenv("LIFELEDGER_DAILY_BUDGET_USD")))
	}
	cfg.DailyBudgetUSD = budget

	port, err := strconv.Atoi(getenv("LIFELEDGER_SMTP_PORT", "587"))
	if err != nil || port <= 0 || port > 65535 {
		errs = append(errs, fmt.Errorf("LIFELEDGER_SMTP_PORT must be a TCP port, got %q", os.Getenv("LIFELEDGER_SMTP_PORT")))
	}
	cfg.SMTPPort = port
	if cfg.SMTPHost != "" && cfg.SMTPFrom == "" {
		errs = append(errs, errors.New("LIFELEDGER_SMTP_FROM is required when LIFELEDGER_SMTP_HOST is set"))
	}

	interval, err := time.ParseDuration(getenv("LIFELEDGER_SWEEP_INTERVAL", "15m"))
	if err != nil || interval < 0 || (interval > 0 && interval < time.Minute) {
		errs = append(errs, fmt.Errorf("LIFELEDGER_SWEEP_INTERVAL must be 0 or a duration of 1m or more, got %q", os.Getenv("LIFELEDGER_SWEEP_INTERVAL")))
	}
	cfg.SweepInterval = interval
	if cfg.RemindHour, err = hour("LIFELEDGER_REMIND_HOUR", 9); err != nil {
		errs = append(errs, err)
	}
	if cfg.ReviewHour, err = hour("LIFELEDGER_REVIEW_HOUR", 18); err != nil {
		errs = append(errs, err)
	}
	day := strings.ToLower(getenv("LIFELEDGER_REVIEW_DAY", "sunday"))
	if day != "off" {
		cfg.ReviewEnabled = true
		if cfg.ReviewDay, err = weekday(day); err != nil {
			errs = append(errs, err)
		}
	}
	if (cfg.TelegramBotToken == "") != (cfg.TelegramChatID == "") {
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID must be set together"))
	}
	return cfg, errors.Join(errs...)
}

func hour(key string, fallback int) (int, error) {
	raw := getenv(key, strconv.Itoa(fallback))
	h, err := strconv.Atoi(raw)
	if err != nil || h < 0 || h > 23 {
		return fallback, fmt.Errorf("%s must be an hour from 0 to 23, got %q", key, raw)
	}
	return h, nil
}

func weekday(name string) (time.Weekday, error) {
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), name) {
			return d, nil
		}
	}
	return time.Sunday, fmt.Errorf("LIFELEDGER_REVIEW_DAY must be a weekday name or \"off\", got %q", name)
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, ok, err := parseDotEnvLine(scanner.Text())
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("%s:%d: set %s: %w", path, lineNo, key, err)
		}
	}
	return scanner.Err()
}

func parseDotEnvLine(line string) (key, value string, ok bool, err error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}
	line = strings.TrimPrefix(line, "export ")

	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false, fmt.Errorf("line has no '=': %q", line)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, errors.New("key is empty")
	}
	value = strings.TrimSpace(value)
	if n := len(value); n >= 2 {
		if (value[0] == '"' && value[n-1] == '"') || (value[0] == '\'' && value[n-1] == '\'') {
			return key, value[1 : n-1], true, nil
		}
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return key, value, true, nil
}
