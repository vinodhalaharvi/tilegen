package main

import (
	"fmt"
	"strings"
)

// We never write database access code. For storage=postgres the impl tile
// emits a schema and named queries, and sqlc - an existing, battle-tested
// compiler from SQL to Go - generates the typed data layer. The LLM only
// writes the thin mapping between sqlc rows and domain types.

var sqlTypes = map[string]string{
	"string": "TEXT", "bool": "BOOLEAN",
	"int": "BIGINT", "int64": "BIGINT", "int32": "INTEGER", "int16": "SMALLINT",
	"float64": "DOUBLE PRECISION", "float32": "REAL",
	"[]byte": "BYTEA", "time.Time": "TIMESTAMPTZ", "uuid.UUID": "UUID",
	"json.RawMessage": "JSONB",
}

func sqlFor(c *Ctx, entity string, st *Node, ops *Node) ([]*Node, error) {
	table := plural(snake(entity))
	var cols, names []string
	for _, f := range st.FindAll("field") {
		name, typ := f.List[1].Atom, f.List[2].Atom
		col := snake(name)
		nullable := strings.HasPrefix(typ, "*")
		sqlt, ok := sqlTypes[strings.TrimPrefix(typ, "*")]
		if !ok {
			c.warn(f.Pos, "no SQL type for %s; using JSONB for column %s", typ, col)
			sqlt = "JSONB"
		}
		switch {
		case name == "ID":
			sqlt += " PRIMARY KEY"
		case !nullable:
			sqlt += " NOT NULL"
		}
		cols = append(cols, fmt.Sprintf("  %s %s", col, sqlt))
		names = append(names, col)
	}
	out := []*Node{L(Sym("sql/table"), Str(table),
		Str(fmt.Sprintf("CREATE TABLE %s (\n%s\n);", table, strings.Join(cols, ",\n"))))}

	for _, op := range ops.Args() {
		var name, body string
		switch op.Atom {
		case "get":
			name, body = "Get"+entity, fmt.Sprintf(":one\nSELECT * FROM %s WHERE id = $1;", table)
		case "list":
			name, body = "List"+plural(entity), fmt.Sprintf(":many\nSELECT * FROM %s ORDER BY id;", table)
		case "save":
			ph := make([]string, len(names))
			var set []string
			for i, n := range names {
				ph[i] = fmt.Sprintf("$%d", i+1)
				if n != "id" {
					set = append(set, fmt.Sprintf("%s = EXCLUDED.%s", n, n))
				}
			}
			conflict := "DO NOTHING"
			if len(set) > 0 {
				conflict = "DO UPDATE SET " + strings.Join(set, ", ")
			}
			name = "Save" + entity
			body = fmt.Sprintf(":exec\nINSERT INTO %s (%s)\nVALUES (%s)\nON CONFLICT (id) %s;",
				table, strings.Join(names, ", "), strings.Join(ph, ", "), conflict)
		case "delete":
			name, body = "Delete"+entity, fmt.Sprintf(":exec\nDELETE FROM %s WHERE id = $1;", table)
		}
		out = append(out, L(Sym("sql/query"), Str(name), Str("-- name: "+name+" "+body)))
	}
	return out, nil
}

// sqlcConfig describes sqlc.yaml. Overrides make sqlc emit the same Go
// types the domain uses, which shrinks the mapping the LLM has to write.
func sqlcConfig(c *Ctx) *Node {
	n := L(Sym("sqlc/config"),
		L(Sym("schema"), Str("db/schema.sql")),
		L(Sym("queries"), Str("db/query.sql")),
		L(Sym("out"), Str("internal/db")),
		L(Sym("override"), Str("timestamptz"), Str("time.Time")))
	for _, imp := range c.Requires {
		if imp == "github.com/google/uuid" {
			n.List = append(n.List, L(Sym("override"), Str("uuid"), Str("github.com/google/uuid.UUID")))
		}
	}
	return n
}
