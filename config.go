package main

import (
	"errors"
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
	Storage      string // auto (cheapest legal backend per store) | a registered backend
	Layout       string // flat | internal
}

func DefaultConfig() Config {
	return Config{JSONTags: "snake", ContextFirst: true, Storage: "auto", Layout: "flat"}
}

var configChoices = map[string][]string{
	"json-tags":     {"snake", "camel", "none"},
	"context-first": {"yes", "no"},
	"storage":       nil, // the registered backends; see choicesFor
	"layout":        {"flat", "internal"},
}

// LoadConfig reads a file holding a single (config ...) form.
// An empty path means defaults.
func LoadConfig(path string) (Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig(), err
	}
	forms, err := Parse(path, string(src))
	if err != nil {
		return DefaultConfig(), err
	}
	if len(forms) != 1 {
		return DefaultConfig(), fmt.Errorf("%s: expected a single (config ...) form", path)
	}
	return ParseConfig(forms[0])
}

// ParseConfig reads (config (key value)...), from a config file or from a
// (config ...) form inside a spec directory.
func ParseConfig(form *Node) (Config, error) {
	cfg := DefaultConfig()
	b := Bindings{}
	if !Match(Pat("(config ?opts...)"), form, b) {
		return cfg, fmt.Errorf("%s: expected (config ...), got %s", form.Pos, short(form))
	}
	var errs []error // report every problem at once, like the spec validator
	for _, o := range b.Rest("opts") {
		ob := Bindings{}
		if !Match(Pat("(?key ?val)"), o, ob) || ob.One("val").IsList {
			errs = append(errs, fmt.Errorf("%s: expected (key value), got %s", o.Pos, o.Flat()))
			continue
		}
		key, val := ob.Atom("key"), ob.Atom("val")
		choices, ok := choicesFor(key)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: unknown config key %q%s", o.Pos, key, didYouMean(key, sortedKeys(configChoices))))
			continue
		}
		if !contains(choices, val) {
			errs = append(errs, fmt.Errorf("%s: %s must be one of %s, got %q%s", o.Pos, key, strings.Join(choices, "|"), val, didYouMean(val, choices)))
			continue
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
	return cfg, errors.Join(errs...)
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

// choicesFor returns a config key's allowed values. Storage comes from the
// backend registry, so a new backend needs no change here.
func choicesFor(key string) ([]string, bool) {
	choices, ok := configChoices[key]
	if key == "storage" {
		choices = append([]string{"auto"}, backendNames()...)
	}
	return choices, ok
}
