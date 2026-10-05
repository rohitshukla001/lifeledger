// Package config loads LifeLedger settings from environment variables.
//
// For local development, Load also reads a .env file if one exists. A value
// that is already set in the process environment always wins over the file.
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

// Environment names.
const (
	EnvDev  = "dev"
	EnvProd = "prod"
)

// Config holds all runtime settings.
type Config struct {
	// Addr is the TCP address for the HTTP server, for example ":8080".
	Addr string
	// Env is "dev" or "prod". It controls the log format.
	Env string
	// LogLevel is "debug", "info", "warn", or "error".
	LogLevel string
}

// Load reads the optional .env file at envFile, then builds a Config from
// the process environment. Pass an empty envFile to skip the file.
func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := loadDotEnv(envFile); err != nil {
			return Config{}, err
		}
	}

	cfg := Config{
		Addr:     getenv("LIFELEDGER_ADDR", ":8080"),
		Env:      strings.ToLower(getenv("LIFELEDGER_ENV", EnvDev)),
		LogLevel: strings.ToLower(getenv("LIFELEDGER_LOG_LEVEL", "info")),
	}

	// Nebius Serverless and most container hosts set PORT.
	if port := os.Getenv("PORT"); port != "" && os.Getenv("LIFELEDGER_ADDR") == "" {
		cfg.Addr = ":" + port
	}

	return cfg, cfg.Validate()
}

// Validate returns an error for each setting that has a value that is not
// permitted.
func (c Config) Validate() error {
	var errs []error
	if c.Addr == "" {
		errs = append(errs, errors.New("LIFELEDGER_ADDR must not be empty"))
	}
	if c.Env != EnvDev && c.Env != EnvProd {
		errs = append(errs, fmt.Errorf("LIFELEDGER_ENV must be %q or %q, got %q", EnvDev, EnvProd, c.Env))
	}
	if _, err := c.SlogLevel(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// SlogLevel converts LogLevel to a slog.Level.
func (c Config) SlogLevel() (slog.Level, error) {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("LIFELEDGER_LOG_LEVEL must be debug, info, warn, or error, got %q", c.LogLevel)
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// loadDotEnv sets variables from a KEY=VALUE file. It does not replace a
// variable that is already set. A missing file is not an error.
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

// parseDotEnvLine parses one line. It returns ok=false for blank lines and
// comments. It accepts an optional "export " prefix and removes one pair of
// matching single or double quotes around the value.
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
	// Remove an inline comment from an unquoted value.
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return key, value, true, nil
}
