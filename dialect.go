package main

import (
	"fmt"
	"strings"
)

// A Dialect is what a SQL backend needs beyond the shared query shapes:
// how it spells types, how it numbers placeholders, and how it upserts.
// The query bodies themselves are the same everywhere, so adding a
// database is a Dialect, not a second SQL generator.
type Dialect struct {
	Name        string            // sqlc's engine: postgresql, sqlite
	SQLPackage  string            // sqlc's sql_package: pgx/v5, database/sql
	Types       map[string]string // Go type -> column type
	Fallback    string            // the column type for anything unmapped
	Placeholder func(i int) string
	Upsert      func(table string, cols, set []string, ph []string) string

	// Enum renders a check constraint, which not every database spells
	// the same way.
	Enum func(col string, values []string) string

	// Overrides are the sqlc type mappings this database needs: postgres
	// hands back its own timestamptz and uuid types, while sqlite stores
	// both as TEXT and needs nothing.
	Overrides func(c *Ctx) []*Node
}

// Postgres is the dialect tilegen has always written.
var Postgres = &Dialect{
	Name:       "postgresql",
	SQLPackage: "pgx/v5",
	Types: map[string]string{
		"string": "TEXT", "bool": "BOOLEAN",
		"int": "BIGINT", "int64": "BIGINT", "int32": "INTEGER", "int16": "SMALLINT",
		"float64": "DOUBLE PRECISION", "float32": "REAL",
		"[]byte": "BYTEA", "time.Time": "TIMESTAMPTZ", "uuid.UUID": "UUID",
		"json.RawMessage": "JSONB",
	},
	Fallback:    "JSONB",
	Placeholder: func(i int) string { return fmt.Sprintf("$%d", i+1) },
	Upsert: func(table string, cols, set, ph []string) string {
		conflict := "DO NOTHING"
		if len(set) > 0 {
			conflict = "DO UPDATE SET " + strings.Join(set, ", ")
		}
		return fmt.Sprintf("INSERT INTO %s (%s)\nVALUES (%s)\nON CONFLICT (id) %s;",
			table, strings.Join(cols, ", "), strings.Join(ph, ", "), conflict)
	},
	Enum: checkConstraint,
	Overrides: func(c *Ctx) []*Node {
		out := []*Node{L(Sym("override"), Str("timestamptz"), Str("time.Time"))}
		for _, imp := range c.Requires {
			if imp == "github.com/google/uuid" {
				out = append(out, L(Sym("override"), Str("uuid"), Str("github.com/google/uuid.UUID")))
			}
		}
		return out
	},
}

// SQLite stores everything in a handful of storage classes, so uuid and
// time are TEXT and sqlc maps them through overrides. Its upsert is the
// same ON CONFLICT form, and its placeholders are positional.
var SQLite = &Dialect{
	Name:       "sqlite",
	SQLPackage: "database/sql",
	Types: map[string]string{
		"string": "TEXT", "bool": "BOOLEAN",
		"int": "INTEGER", "int64": "INTEGER", "int32": "INTEGER", "int16": "INTEGER",
		"float64": "REAL", "float32": "REAL",
		"[]byte": "BLOB", "time.Time": "TEXT", "uuid.UUID": "TEXT",
		"json.RawMessage": "TEXT",
	},
	Fallback:    "TEXT",
	Placeholder: func(i int) string { return fmt.Sprintf("?%d", i+1) },
	Upsert: func(table string, cols, set, ph []string) string {
		conflict := "DO NOTHING"
		if len(set) > 0 {
			conflict = "DO UPDATE SET " + strings.Join(set, ", ")
		}
		return fmt.Sprintf("INSERT INTO %s (%s)\nVALUES (%s)\nON CONFLICT (id) %s;",
			table, strings.Join(cols, ", "), strings.Join(ph, ", "), conflict)
	},
	Enum: checkConstraint,
	// Everything is TEXT, so sqlc's defaults are right: a uuid or a time
	// arrives as a string and the mapper parses it, which is exactly the
	// work the row-mapper chain already prices.
	Overrides: func(*Ctx) []*Node { return nil },
}

// checkConstraint is the SQL standard's way, which both dialects accept.
func checkConstraint(col string, values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + v + "'"
	}
	return fmt.Sprintf("TEXT CHECK (%s IN (%s))", col, strings.Join(quoted, ", "))
}

// dialectOf is the dialect of the backend a project uses for SQL, or
// Postgres when none does.
func dialectOf(c *Ctx) *Dialect {
	for _, be := range usedBackends(c) {
		if be.Dialect != nil {
			return be.Dialect
		}
	}
	return Postgres
}
