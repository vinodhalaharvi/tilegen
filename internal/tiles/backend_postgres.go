package tiles

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
	"strings"
)

// The postgres backend: tilegen writes the schema and named queries, sqlc
// compiles them into typed Go in internal/db, and the LLM maps rows to
// domain types.
func init() {
	compile.RegisterStoreBackend(&compile.Backend{
		Name:     "postgres",
		Tile:     "postgres-sqlc",
		Engine:   "postgres",
		Form:     "db-rows", // sqlc's own row types: a row-mapper converts them
		Doc:      "postgres via sqlc: tilegen writes SQL, sqlc writes the Go, the LLM maps rows",
		Requires: []string{"sqlc"},
		IllegalWhen: map[string]string{
			"no-broker": "a postgres to run, with backups, connections and upgrades",
		},
		Satisfies: map[string]string{
			"durable":         "a committed transaction is on disk before COMMIT returns",
			"cross-process":   "a server other processes connect to over the network",
			"lookup-by-field": "a WHERE clause, and an index when you add one",
			"outbox":          "the entity's row and the event's row are written in one transaction",
			"inbox":           "a handler's writes and the record that it ran are committed in one transaction",
		},
		Cost: compile.Cost{
			{Dim: "llm-work", Value: 3, Source: "derived", Note: "the LLM writes only row-to-domain mapping; sqlc writes the queries"},
			{Dim: "maintenance", Value: 2},
			{Dim: "dependency", Value: 3, Source: "derived", Note: "pgx, plus sqlc as a build-time tool"},
			{Dim: "runtime", Value: 1},
			{Dim: "operations", Value: 5, Source: "derived", Note: "a server to run, plus sqlc in the build"},
		},
		Topics:       []string{"postgres", "sqlc"},
		ErrorPackage: "github.com/jackc/pgx/v5/pgconn", // PgError and its SQLSTATE codes
		Packages:     map[string]string{"db": "internal/db"},
		Generate:     "sqlc generate",
		GenerateDoc:  "generate internal/db from db/*.sql",
		ProjectForms: func(c *compile.Ctx) []*sexp.Node { return []*sexp.Node{compile.SQLCConfig(c)} },
		Implement: func(in compile.StoreInput) (compile.StoreParts, error) {
			sql, err := compile.SQLFor(in.C, in.Entity, in.Struct, in.Ops)
			if err != nil {
				return compile.StoreParts{}, err
			}
			var parse []string // enum fields arrive from sqlc as strings
			for _, f := range in.Struct.FindAll("field") {
				if in.C.EnumFor(f.List[2].Atom, in.Pkg.Name) != nil {
					typ := strings.TrimPrefix(f.List[2].Atom, "*")
					if q, name, ok := strings.Cut(typ, "."); ok {
						typ = q + ".Parse" + name
					} else {
						typ = "Parse" + typ
					}
					parse = append(parse, fmt.Sprintf("%s with %s", f.List[1].Atom, typ))
				}
			}
			return compile.StoreParts{
				Fields: []*sexp.Node{sexp.L(sexp.Sym("field"), sexp.Sym("q"), sexp.Sym("*db.Queries"))},
				Params: sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("q"), sexp.Sym("*db.Queries"))),
				Body:   fmt.Sprintf("return &%s{q: q}", in.Impl),
				Hint: func(meth *sexp.Node) string {
					h := compile.PostgresHint(meth, in.Entity)
					if kind, _ := compile.MethodOp(meth); len(parse) > 0 && (kind == "get" || kind == "get-by" || kind == "list" || kind == "list-by") {
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
