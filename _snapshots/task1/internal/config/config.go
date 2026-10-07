package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
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
}

func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return Config{}, err
		}
	}

	cfg := Config{
		Addr: getenv("LIFELEDGER_ADDR", ":8080"),
		Env:  strings.ToLower(getenv("LIFELEDGER_ENV", EnvDev)),
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
