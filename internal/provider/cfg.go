package provider

import (
	"fmt"
	"strconv"
	"strings"
)

// Config is a decoded provider config block with typed accessors. Values survive
// a JSON round-trip through SQLite, so numbers may arrive as float64.
type Config map[string]any

// String returns a string field (empty when missing).
func (c Config) String(key string) string {
	v, ok := c[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

// Has reports whether a non-empty value is present.
func (c Config) Has(key string) bool { return c.String(key) != "" }

// Bool returns a boolean field with a default.
func (c Config) Bool(key string, def bool) bool {
	v, ok := c[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(t)
		if err == nil {
			return b
		}
	}
	return def
}

// Int returns an integer field with a default.
func (c Config) Int(key string, def int) int {
	v, ok := c[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return n
		}
	}
	return def
}

// Require returns an error listing the first missing required key.
func (c Config) Require(keys ...string) error {
	for _, k := range keys {
		if !c.Has(k) {
			return fmt.Errorf("missing required config field %q", k)
		}
	}
	return nil
}
