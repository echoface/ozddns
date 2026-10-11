// Package config loads the optional start-up configuration file (config.yaml).
//
// It deliberately holds only *deployment* knobs — things that decide how the
// process runs, not how it behaves at runtime. Anything a user can change from
// the panel (providers, IP sources, tasks, timezone, password, ...) lives in the
// database instead, so the two never fight over the same value.
//
// The file is a small, flat YAML subset (also valid JSON). Only top-level
// "key: value" scalar lines are understood; nested maps, lists and anchors are
// not. Unknown keys are ignored so an older binary never fails on a newer file.
//
// Example:
//
//	# config.yaml — how the process binds and where it stores data
//	listen: 0.0.0.0:9876
//	db: /data/ozddns.db
//	logLevel: info
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// File is the parsed configuration file. The zero value means "nothing set".
type File struct {
	// Listen is the address the daemon binds for its API and panel.
	Listen string `json:"listen"`
	// DB is the SQLite database path.
	DB string `json:"db"`
	// LogLevel is one of debug | info | warn | error.
	LogLevel string `json:"logLevel"`
}

// DefaultTemplate is the annotated file written on first run, so a fresh
// install hands you an editable config instead of an empty directory.
const DefaultTemplate = `# OzDDNS deployment configuration.
#
# Read once at start-up. Each key is overridden by, in order:
#   1. the matching command-line flag     (--listen / --db / --log-level)
#   2. the environment variable           (OZDDNS_LISTEN / OZDDNS_DB / OZDDNS_LOG_LEVEL)
#   3. the value below
#
# These are deployment knobs only. Providers, tasks, IP sources and the rest of
# the runtime configuration live in the database and are edited from the panel.
listen: 127.0.0.1:9876
db: ./ozddns.db
logLevel: info
`

// Default returns the built-in defaults.
func Default() File {
	return File{Listen: "127.0.0.1:9876", DB: "./ozddns.db", LogLevel: "info"}
}

// WriteDefault creates path with DefaultTemplate when it does not exist yet,
// creating parent directories as needed. It reports whether the file was
// created (false when it already existed).
func WriteDefault(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
	}
	if err := os.WriteFile(path, []byte(DefaultTemplate), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// Load reads path. A path that does not exist is not an error: it returns
// (nil, nil) so callers can treat "no config file" as a normal state. Any other
// failure (unreadable file, malformed content) is returned so a broken config
// fails fast instead of being silently ignored.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	f, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return f, nil
}

// Parse decodes the flat YAML/JSON subset described in the package comment.
func Parse(data []byte) (*File, error) {
	var f File
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		return &f, nil
	}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") {
			continue
		}
		// Tolerate a trailing comment that is clearly separated from the value.
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("line %d: expected \"key: value\", got %q", i+1, line)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = unquote(strings.TrimSpace(value))
		switch key {
		case "listen":
			f.Listen = value
		case "db", "database":
			f.DB = value
		case "loglevel", "log_level", "log-level":
			f.LogLevel = value
		default:
			// Unknown keys are ignored for forward compatibility.
		}
	}
	return &f, nil
}

// unquote strips one layer of matching single or double quotes.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
