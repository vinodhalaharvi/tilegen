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
		kind, f := opKind(op)
		col := snake(f)
		var body string
		switch kind {
		case "get":
			body = fmt.Sprintf(":one\nSELECT * FROM %s WHERE id = $1;", table)
		case "list":
			body = fmt.Sprintf(":many\nSELECT * FROM %s ORDER BY id;", table)
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
			body = fmt.Sprintf(":exec\nINSERT INTO %s (%s)\nVALUES (%s)\nON CONFLICT (id) %s;",
				table, strings.Join(names, ", "), strings.Join(ph, ", "), conflict)
		case "delete":
			body = fmt.Sprintf(":exec\nDELETE FROM %s WHERE id = $1;", table)
		case "count":
			body = fmt.Sprintf(":one\nSELECT count(*) FROM %s;", table)
		case "list-by":
			body = fmt.Sprintf(":many\nSELECT * FROM %s WHERE %s = $1 ORDER BY id;", table, col)
		case "get-by":
			body = fmt.Sprintf(":one\nSELECT * FROM %s WHERE %s = $1 ORDER BY id LIMIT 1;", table, col)
		case "count-by":
			body = fmt.Sprintf(":one\nSELECT count(*) FROM %s WHERE %s = $1;", table, col)
		case "exists-by":
			body = fmt.Sprintf(":one\nSELECT EXISTS (SELECT 1 FROM %s WHERE %s = $1);", table, col)
		case "delete-by":
			body = fmt.Sprintf(":exec\nDELETE FROM %s WHERE %s = $1;", table, col)
		default:
			continue // custom methods: no SQL
		}
		name := queryName(kind, entity, f)
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

// queryName names the sqlc query for an op; sqlc turns it into the method
// the LLM calls: (list-by NoteID) on Share -> ListSharesByNoteID.
func queryName(kind, entity, field string) string {
	switch kind {
	case "get":
		return "Get" + entity
	case "list":
		return "List" + plural(entity)
	case "save":
		return "Save" + entity
	case "delete":
		return "Delete" + entity
	case "count":
		return "Count" + plural(entity)
	case "list-by":
		return "List" + plural(entity) + "By" + field
	case "get-by":
		return "Get" + entity + "By" + field
	case "count-by":
		return "Count" + plural(entity) + "By" + field
	case "exists-by":
		return "Exists" + entity + "By" + field
	case "delete-by":
		return "Delete" + plural(entity) + "By" + field
	}
	return ""
}
