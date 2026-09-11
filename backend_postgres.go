package main

import (
	"fmt"
	"strings"
)

// The postgres backend: tilegen writes the schema and named queries, sqlc
// compiles them into typed Go in internal/db, and the LLM maps rows to
// domain types.
func init() {
	RegisterBackend(&Backend{
		Name:         "postgres",
		Tile:         "postgres-sqlc",
		Form:         "db-rows", // sqlc's own row types: a row-mapper converts them
		Doc:          "postgres via sqlc: tilegen writes SQL, sqlc writes the Go, the LLM maps rows",
		Requires:     []string{"sqlc"},
		Cost:         Cost{{"llm-work", 3}, {"maintenance", 2}, {"dependency", 3}, {"runtime", 1}},
		Topics:       []string{"postgres", "sqlc"},
		Packages:     map[string]string{"db": "internal/db"},
		Generate:     "sqlc generate",
		GenerateDoc:  "generate internal/db from db/*.sql",
		ProjectForms: func(c *Ctx) []*Node { return []*Node{sqlcConfig(c)} },
		Implement: func(in StoreInput) (StoreParts, error) {
			sql, err := sqlFor(in.C, in.Entity, in.Struct, in.Ops)
			if err != nil {
				return StoreParts{}, err
			}
			var parse []string // enum fields arrive from sqlc as strings
			for _, f := range in.Struct.FindAll("field") {
				if in.C.enumFor(f.List[2].Atom, in.Pkg.Name) != nil {
					typ := strings.TrimPrefix(f.List[2].Atom, "*")
					if q, name, ok := strings.Cut(typ, "."); ok {
						typ = q + ".Parse" + name
					} else {
						typ = "Parse" + typ
					}
					parse = append(parse, fmt.Sprintf("%s with %s", f.List[1].Atom, typ))
				}
			}
			return StoreParts{
				Fields: []*Node{L(Sym("field"), Sym("q"), Sym("*db.Queries"))},
				Params: L(Sym("params"), L(Sym("q"), Sym("*db.Queries"))),
				Body:   fmt.Sprintf("return &%s{q: q}", in.Impl),
				Hint: func(meth *Node) string {
					h := postgresHint(meth, in.Entity)
					if kind, _ := methodOp(meth); len(parse) > 0 && (kind == "get" || kind == "get-by" || kind == "list" || kind == "list-by") {
						h += " Convert " + strings.Join(parse, ", ") + ", never with a bare cast."
					}
					return h
				},
				Extra:   sql,
				Context: []string{"db/query.sql", "internal/db/models.go", "internal/db/query.sql.go"},
			}, nil
		},
	})
}
