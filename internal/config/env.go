package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// LoadDotEnv parses a dotenv file and sets its values in the process
// environment. It deliberately never invokes a shell. Missing files are
// treated as success, matching the application's historical behavior.
func LoadDotEnv(path string) error {
	values, err := readDotEnv(path)
	if err != nil {
		return err
	}
	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

func readDotEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(line[len("export "):])
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}

		// Strip comments only outside quoted values.
		if len(value) > 0 && value[0] != '"' && value[0] != '\'' {
			if index := strings.Index(value, " #"); index >= 0 {
				value = strings.TrimSpace(value[:index])
			}
		}
		if len(value) >= 2 {
			switch value[0] {
			case '"':
				if value[len(value)-1] == '"' {
					if unquoted, unquoteErr := strconv.Unquote(value); unquoteErr == nil {
						value = unquoted
					}
				}
			case '\'':
				if value[len(value)-1] == '\'' {
					value = value[1 : len(value)-1]
				}
			}
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func envValue(lookup LookupEnv, dotenv map[string]string, name string) (string, bool) {
	if value, ok := dotenv[name]; ok {
		return value, true
	}
	return lookup(name)
}

func getEnv(lookup LookupEnv, dotenv map[string]string, name, fallback string) string {
	if value, ok := envValue(lookup, dotenv, name); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvBool(lookup LookupEnv, dotenv map[string]string, name string, fallback bool) (bool, error) {
	value, ok := envValue(lookup, dotenv, name)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return parsed, nil
}

func parseDurationEnv(lookup LookupEnv, dotenv map[string]string, name string, fallback time.Duration) (time.Duration, error) {
	value, ok := envValue(lookup, dotenv, name)
	if !ok || value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, errors.New("invalid " + name)
	}
	return parsed, nil
}

// ParseDurationEnv preserves the small legacy helper used by package-main
// tests while keeping parsing rules in this package.
func ParseDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	return parseDurationEnv(os.LookupEnv, nil, name, fallback)
}

// GetEnv returns a non-empty process environment value or fallback.
func GetEnv(name, fallback string) string {
	return getEnv(os.LookupEnv, nil, name, fallback)
}

// GetEnvBool parses a process environment boolean or returns fallback.
func GetEnvBool(name string, fallback bool) (bool, error) {
	return getEnvBool(os.LookupEnv, nil, name, fallback)
}
