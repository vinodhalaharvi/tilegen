package main

import (
	"fmt"
	"go/token"
	"os"
	"strings"
	"unicode"
)

// Config picks between equally valid concretizations. The spec says
// *what*; the config says *which house style*. Same spec + different
// config = different, equally correct Go.
type Config struct {
	JSONTags     string // snake | camel | none
	ContextFirst bool   // prepend ctx context.Context to interface methods
	Storage      string // memory | postgres (postgres emits sqlc inputs)
	Layout       string // flat | internal
}

func DefaultConfig() Config {
	return Config{JSONTags: "snake", ContextFirst: true, Storage: "memory", Layout: "flat"}
}

var configChoices = map[string][]string{
	"json-tags":     {"snake", "camel", "none"},
	"context-first": {"yes", "no"},
	"storage":       {"memory", "postgres"},
	"layout":        {"flat", "internal"},
}

// LoadConfig reads (config (key value)...). A missing path means defaults.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	forms, err := Parse(path, string(src))
	if err != nil {
		return cfg, err
	}
	b := Bindings{}
	if len(forms) != 1 || !Match(Pat("(config ?opts...)"), forms[0], b) {
		return cfg, fmt.Errorf("%s: expected a single (config ...) form", path)
	}
	for _, o := range b.Rest("opts") {
		ob := Bindings{}
		if !Match(Pat("(?key ?val)"), o, ob) || ob.One("val").IsList {
			return cfg, fmt.Errorf("%s: expected (key value), got %s", o.Pos, o.Flat())
		}
		key, val := ob.Atom("key"), ob.Atom("val")
		choices, ok := configChoices[key]
		if !ok {
			return cfg, fmt.Errorf("%s: unknown config key %q", o.Pos, key)
		}
		if !contains(choices, val) {
			return cfg, fmt.Errorf("%s: %s must be one of %s, got %q", o.Pos, key, strings.Join(choices, "|"), val)
		}
		switch key {
		case "json-tags":
			cfg.JSONTags = val
		case "context-first":
			cfg.ContextFirst = val == "yes"
		case "storage":
			cfg.Storage = val
		case "layout":
			cfg.Layout = val
		}
	}
	return cfg, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// snake converts Go names to snake_case, keeping initialisms together:
// ID -> id, PlacedAt -> placed_at, UserID -> user_id, HTTPServer -> http_server.
func snake(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 {
			prevLower := unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1])
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if prevLower || (unicode.IsUpper(r[i-1]) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

// camel converts Go names to lowerCamel: ID -> id, PlacedAt -> placedAt,
// HTTPServer -> httpServer.
func camel(s string) string {
	parts := strings.Split(snake(s), "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// paramName picks a parameter name for a value of type T: Order -> order.
// Keywords and builtins fall back to v (an entity called Type or Map).
func paramName(typ string) string {
	n := camel(typ)
	if token.IsKeyword(n) || n == "" {
		return "v"
	}
	return n
}

// plural makes a table name: order -> orders, category -> categories.
func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	}
	return s + "s"
}

// exported reports whether a Go identifier is exported.
func exported(s string) bool {
	return s != "" && unicode.IsUpper([]rune(s)[0])
}
