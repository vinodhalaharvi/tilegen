package main

import (
	"fmt"
	"strings"
)

// The sqlite backend: the same shape as postgres-sqlc, with a different
// dialect. tilegen writes the schema and queries, sqlc compiles them into
// database/sql code, and the LLM maps rows to domain types.
//
// It is durable, since the database is a file, but it does not serve
// another process, so a spec that needs that says so and this becomes
// illegal. The mapper costs more than postgres's: sqlite stores uuid and
// time as TEXT, so every such field is parsed by hand.
func init() {
	registerStoreBackend(&Backend{
		Name:     "sqlite",
		Tile:     "sqlite-sqlc",
		Doc:      "sqlite via sqlc: one file, no server; uuid and time are TEXT",
		Form:     "db-rows",
		Requires: []string{"sqlc"},
		Cost: Cost{
			{Dim: "llm-work", Value: 3, Source: "derived", Note: "same as postgres-sqlc: sqlc writes the queries"},
			{Dim: "maintenance", Value: 2},
			{Dim: "dependency", Value: 1, Source: "derived", Note: "a driver and sqlc, but no server to run"},
			{Dim: "runtime", Value: 1},
		},
		IllegalWhen: map[string]string{
			"cross-process": "a sqlite file serves one process; another would need its own connection to the same disk",
		},
		Topics:       []string{"sqlite", "sqlc"},
		Packages:     map[string]string{"db": "internal/db"},
		Generate:     "sqlc generate",
		GenerateDoc:  "generate internal/db from db/*.sql",
		Dialect:      SQLite,
		ErrorPackage: "", // database/sql errors are standard library
		ProjectForms: func(c *Ctx) []*Node { return []*Node{sqlcConfig(c)} },
		Implement: func(in StoreInput) (StoreParts, error) {
			sql, err := sqlFor(in.C, in.Entity, in.Struct, in.Ops)
			if err != nil {
				return StoreParts{}, err
			}
			// sqlite hands back TEXT for anything it has no type for, so
			// the mapper parses more than postgres's does. Say which.
			var parse []string
			for _, f := range in.Struct.FindAll("field") {
				typ := strings.TrimPrefix(f.List[2].Atom, "*")
				switch {
				case in.C.enumFor(f.List[2].Atom, in.Pkg.Name) != nil:
					parse = append(parse, fmt.Sprintf("%s with Parse%s", f.List[1].Atom, typ))
				case typ == "uuid.UUID":
					parse = append(parse, f.List[1].Atom+" with uuid.Parse")
				case typ == "time.Time":
					parse = append(parse, f.List[1].Atom+" with time.Parse(time.RFC3339, ...)")
				}
			}
			return StoreParts{
				Fields: []*Node{L(Sym("field"), Sym("q"), Sym("*db.Queries"))},
				Params: L(Sym("params"), L(Sym("q"), Sym("*db.Queries"))),
				Body:   fmt.Sprintf("return &%s{q: q}", in.Impl),
				Hint: func(meth *Node) string {
					h := postgresHint(meth, in.Entity)
					kind, _ := methodOp(meth)
					if len(parse) > 0 && (kind == "get" || kind == "get-by" || kind == "list" || kind == "list-by") {
						h += " sqlite stores these as TEXT, so convert " + strings.Join(parse, ", ") + "; never a bare cast."
					}
					if kind == "save" {
						h += " Write time values as RFC3339 and uuids as their canonical string form, so they read back the same."
					}
					return h
				},
				Extra:   sql,
				Context: []string{"db/query.sql", "internal/db/models.go", "internal/db/query.sql.go"},
			}, nil
		},
	})
}
