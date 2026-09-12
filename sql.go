package main

import (
	"fmt"
	"strings"
)

// We never write database access code. For storage=postgres the impl tile
// emits a schema and named queries, and sqlc - an existing, battle-tested
// compiler from SQL to Go - generates the typed data layer. The LLM only
// writes the thin mapping between sqlc rows and domain types.

// sqlTypes is the postgres mapping, kept as the default for code that
// asks about a type without a project in hand (the chain cost model).
var sqlTypes = Postgres.Types

func sqlFor(c *Ctx, entity string, st *Node, ops *Node) ([]*Node, error) {
	d := dialectOf(c)
	table := plural(snake(entity))
	var cols, names []string
	for _, f := range st.FindAll("field") {
		name, typ := f.List[1].Atom, f.List[2].Atom
		col := snake(name)
		nullable := strings.HasPrefix(typ, "*")
		sqlt, ok := d.Types[strings.TrimPrefix(typ, "*")]
		if vals := c.enumFor(typ, c.Pkg.Name); vals != nil {
			sqlt, ok = d.Enum(col, vals), true
		}
		if !ok {
			c.warn(f.Pos, "no SQL type for %s; using %s for column %s", typ, d.Fallback, col)
			sqlt = d.Fallback
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
			body = fmt.Sprintf(":one\nSELECT * FROM %s WHERE id = %s;", table, d.Placeholder(0))
		case "list":
			body = fmt.Sprintf(":many\nSELECT * FROM %s ORDER BY id;", table)
		case "save":
			ph := make([]string, len(names))
			var set []string
			for i, n := range names {
				ph[i] = d.Placeholder(i)
				if n != "id" {
					set = append(set, fmt.Sprintf("%s = EXCLUDED.%s", n, n))
				}
			}
			body = ":exec\n" + d.Upsert(table, names, set, ph)
		case "delete":
			body = fmt.Sprintf(":exec\nDELETE FROM %s WHERE id = %s;", table, d.Placeholder(0))
		case "count":
			body = fmt.Sprintf(":one\nSELECT count(*) FROM %s;", table)
		case "list-by":
			body = fmt.Sprintf(":many\nSELECT * FROM %s WHERE %s = %s ORDER BY id;", table, col, d.Placeholder(0))
		case "get-by":
			body = fmt.Sprintf(":one\nSELECT * FROM %s WHERE %s = %s ORDER BY id LIMIT 1;", table, col, d.Placeholder(0))
		case "count-by":
			body = fmt.Sprintf(":one\nSELECT count(*) FROM %s WHERE %s = %s;", table, col, d.Placeholder(0))
		case "exists-by":
			body = fmt.Sprintf(":one\nSELECT EXISTS (SELECT 1 FROM %s WHERE %s = %s);", table, col, d.Placeholder(0))
		case "delete-by":
			body = fmt.Sprintf(":exec\nDELETE FROM %s WHERE %s = %s;", table, col, d.Placeholder(0))
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
	d := dialectOf(c)
	n := L(Sym("sqlc/config"),
		L(Sym("engine"), Str(d.Name)),
		L(Sym("sql-package"), Str(d.SQLPackage)),
		L(Sym("schema"), Str("db/schema.sql")),
		L(Sym("queries"), Str("db/query.sql")),
		L(Sym("out"), Str("internal/db")))
	for _, o := range d.Overrides(c) {
		n.List = append(n.List, o)
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
