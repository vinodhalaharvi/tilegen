package main

import (
	"fmt"
	"strings"
)

// The pgx backend: postgres without a code generator. tilegen writes the
// schema; the LLM writes each query and the row scanning by hand against a
// pgxpool.Pool, with the query tilegen would have given sqlc as a hint.
// More LLM work and more to maintain than sqlc, but no generate step.
func init() {
	registerStoreBackend(&Backend{
		Name: "pgx",
		Tile: "postgres-pgx",
		Doc:  "postgres via pgx: tilegen writes the schema, the LLM writes the SQL",
		IllegalWhen: map[string]string{
			"no-broker": "a postgres to run, with backups, connections and upgrades",
		},
		Cost: Cost{
			{Dim: "llm-work", Value: 7, Source: "derived", Note: "the LLM writes every query and scan by hand"},
			{Dim: "maintenance", Value: 5, Source: "derived", Note: "hand-written SQL drifts from the schema silently"},
			{Dim: "dependency", Value: 1, Source: "derived", Note: "pgx only, and no build-time tool"},
			{Dim: "runtime", Value: 1},
			{Dim: "operations", Value: 5, Source: "derived", Note: "a server to run, with backups, connections and upgrades"},
		},
		Topics:       []string{"postgres", "pgx"},
		ErrorPackage: "github.com/jackc/pgx/v5/pgconn", // PgError and its SQLSTATE codes
		Imports:      map[string]string{"pgxpool": "github.com/jackc/pgx/v5/pgxpool"},
		Implement: func(in StoreInput) (StoreParts, error) {
			sql, err := sqlFor(in.C, in.Entity, in.Struct, in.Ops)
			if err != nil {
				return StoreParts{}, err
			}
			var extra []*Node
			suggest := map[string]string{} // query name -> the SQL sqlc would have compiled
			for _, n := range sql {
				if n.Head() == "sql/table" {
					extra = append(extra, n) // the schema, for migrations
					continue
				}
				if _, text, ok := strings.Cut(n.List[2].Atom, "\n"); ok {
					suggest[n.List[1].Atom] = strings.Join(strings.Fields(text), " ")
				}
			}
			return StoreParts{
				Fields: []*Node{L(Sym("field"), Sym("pool"), Sym("*pgxpool.Pool"))},
				Params: L(Sym("params"), L(Sym("pool"), Sym("*pgxpool.Pool"))),
				Body:   fmt.Sprintf("return &%s{pool: pool}", in.Impl),
				Hint: func(meth *Node) string {
					kind, f := methodOp(meth)
					h := "Write the SQL and run it with s.pool (pgx v5), against the table in db/schema.sql."
					if q := suggest[queryName(kind, in.Entity, f)]; q != "" {
						h += " A query that fits: " + q
					}
					if kind == "get" || kind == "get-by" {
						h += " Map pgx.ErrNoRows to ErrNotFound."
					}
					if d := customHint(meth); kind == "custom" && d != "" {
						h = d + " " + h
					}
					return h
				},
				Extra:   extra,
				Context: []string{"db/schema.sql"},
			}, nil
		},
	})
}
