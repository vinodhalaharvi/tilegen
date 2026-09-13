package main

import (
	"fmt"
	"strings"
)

// The mongo backend: a collection per entity, documents rather than rows.
// Its store hands back bson documents, not domain values, so something has
// to convert them - the same shape as the sqlc backends, and priced the
// same way, by a chain rule rather than by a flat number.
func init() {
	registerStoreBackend(&Backend{
		Name: "mongo",
		Tile: "mongo",
		Doc:  "mongo: a collection per entity, documents rather than rows",
		Cost: Cost{
			{Dim: "llm-work", Value: 5, Source: "derived", Note: "filters and updates written by hand, but no schema to keep in step"},
			{Dim: "maintenance", Value: 3},
			{Dim: "dependency", Value: 3, Source: "derived", Note: "the driver, and a server to run"},
			{Dim: "runtime", Value: 2},
		},
		Form:         "bson-docs",
		Imports:      map[string]string{"mongo": "go.mongodb.org/mongo-driver/mongo"},
		Topics:       []string{"mongodb", "nosql"},
		ErrorPackage: "mongo",
		Implement: func(in StoreInput) (StoreParts, error) {
			return StoreParts{
				Fields: []*Node{
					L(Sym("field"), Sym("coll"), Sym("*mongo.Collection")),
				},
				Params: L(Sym("params"), L(Sym("db"), Sym("*mongo.Database"))),
				Body: fmt.Sprintf("return &%s{coll: db.Collection(%q)}",
					in.Impl, collectionName(in.Entity)),
				Hint: mongoHint,
			}, nil
		},
	})

	// bson documents are not domain values: every read comes back as a
	// document that has to be decoded into the entity, and the work is the
	// entity's shape, not a constant. Same reasoning as the row mapper.
	RegisterChain(&Chain{
		Name: "doc-mapper",
		From: "bson-docs",
		To:   domainForm,
		Doc:  "decodes mongo documents into domain types, by hand",
		CostRule: "1 per entity, +1 per enum field (a document stores it as a string), " +
			"+1 per pointer field (a missing key and a null are not the same thing)",
		Cost: func(e EntityShape) (Cost, string) {
			units := 1
			var enums, ptrs int
			for _, f := range e.Fields {
				if f.Enum {
					enums++
				}
				if strings.HasPrefix(f.Type, "*") {
					ptrs++
				}
			}
			units += enums + ptrs
			parts := []string{"1 entity"}
			if enums > 0 {
				parts = append(parts, fmt.Sprintf("%d enum field%s", enums, sIf(enums)))
			}
			if ptrs > 0 {
				parts = append(parts, fmt.Sprintf("%d pointer field%s", ptrs, sIf(ptrs)))
			}
			return Cost{{Dim: "llm-work", Value: units}, {Dim: "maintenance", Value: (units + 1) / 2}},
				strings.Join(parts, ", ")
		},
	})
}

// sIf pluralises a count in a cost breakdown.
func sIf(n int) string { return map[bool]string{true: "s"}[n > 1] }

// collectionName is the collection an entity's documents live in.
func collectionName(entity string) string { return lowerFirstWord(entity) + "s" }

// mongoHint says what each method has to do in the driver's terms.
func mongoHint(meth *Node) string {
	kind, f := methodOp(meth)
	p := firstParam(meth)
	switch kind {
	case "get":
		return "FindOne with a filter on _id, then Decode into the value. Return ErrNotFound when the driver reports no documents."
	case "list":
		return "Find with an empty filter, sorted by _id, then decode the cursor into a slice."
	case "save":
		return "ReplaceOne with a filter on _id and the Upsert option, so a new value is inserted and an existing one replaced."
	case "delete":
		return "DeleteOne with a filter on _id. Deleting a missing document is not an error."
	case "count":
		return "CountDocuments with an empty filter."
	case "list-by":
		return fmt.Sprintf("Find with a filter matching %s against %s, sorted by _id, then decode the cursor.", f, p)
	case "get-by":
		return fmt.Sprintf("FindOne with a filter matching %s against %s, sorted by _id. Return ErrNotFound when there is no document.", f, p)
	case "count-by":
		return fmt.Sprintf("CountDocuments with a filter matching %s against %s.", f, p)
	case "exists-by":
		return fmt.Sprintf("CountDocuments with a filter matching %s against %s and a limit of 1.", f, p)
	case "delete-by":
		return fmt.Sprintf("DeleteMany with a filter matching %s against %s.", f, p)
	}
	return ""
}
