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
)

const (
	EnvDev  = "dev"
	EnvProd = "prod"
)

type Config struct {
	Addr     string
	Env      string
	LogLevel slog.Level
	DBPath   string

	NebiusAPIKey   string
	NebiusBaseURL  string
	ModelNano      string
	ModelSuper     string
	ModelUltra     string
	ModelEmbed     string
	DailyBudgetUSD float64
}

func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return Config{}, err
		}
	}

	cfg := Config{
		Addr:          getenv("LIFELEDGER_ADDR", ":8080"),
		Env:           strings.ToLower(getenv("LIFELEDGER_ENV", EnvDev)),
		DBPath:        getenv("LIFELEDGER_DB_PATH", "data/lifeledger.db"),
		NebiusAPIKey:  os.Getenv("NEBIUS_API_KEY"),
		NebiusBaseURL: getenv("NEBIUS_BASE_URL", "https://api.tokenfactory.nebius.com/v1/"),
		ModelNano:     getenv("LIFELEDGER_MODEL_NANO", "nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B"),
		ModelSuper:    getenv("LIFELEDGER_MODEL_SUPER", "nvidia/nemotron-3-super-120b-a12b"),
		ModelUltra:    getenv("LIFELEDGER_MODEL_ULTRA", "nvidia/Nemotron-3-Ultra-550b-a55b"),
		ModelEmbed:    getenv("LIFELEDGER_MODEL_EMBED", "Qwen/Qwen3-Embedding-8B"),
	}

	if port := os.Getenv("PORT"); port != "" && os.Getenv("LIFELEDGER_ADDR") == "" {
		cfg.Addr = ":" + port
	}

	var errs []error
	if cfg.Env != EnvDev && cfg.Env != EnvProd {
		errs = append(errs, fmt.Errorf("LIFELEDGER_ENV must be %q or %q, got %q", EnvDev, EnvProd, cfg.Env))
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LIFELEDGER_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LIFELEDGER_LOG_LEVEL: %w", err))
	}
	budget, err := strconv.ParseFloat(getenv("LIFELEDGER_DAILY_BUDGET_USD", "2"), 64)
	if err != nil || budget < 0 {
		errs = append(errs, fmt.Errorf("LIFELEDGER_DAILY_BUDGET_USD must be a number >= 0, got %q", os.Getenv("LIFELEDGER_DAILY_BUDGET_USD")))
	}
	cfg.DailyBudgetUSD = budget
	return cfg, errors.Join(errs...)
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
