package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// loadEnvFile parses a .env-style file: KEY=VALUE lines, optional "export "
// prefix, # comments, and optional single or double quotes around the value.
func loadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := make(map[string]string)
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, n)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		env[key] = value
	}
	return env, sc.Err()
}

// envLookup prefers non-empty process environment values, falling back to
// file values, so a key set in the shell or MCP client config overrides the
// file but an empty placeholder (KEY=) does not hide it.
func envLookup(file map[string]string) func(string) string {
	return func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return file[key]
	}
}
