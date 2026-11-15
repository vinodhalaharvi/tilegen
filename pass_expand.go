package main

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
	"go/token"
	"strings"
	"unicode"
)

// Expand is the first lowering: it removes sugar and knows nothing about
// the config. An entity is really three things - a struct, a storage
// interface, and a promise to implement that interface - so this pass
// splits it into exactly those, still as S-expressions.
//
//	(entity Order (field ID uuid.UUID) (store get save))
//	=>
//	(struct Order (field ID uuid.UUID))
//	(interface OrderStore (method Get ...) (method Save ...))
//	(impl OrderStore (for Order) (ops get save))
var Expand = &Pass{
	Name: "expand",
	Rules: []Rule{
		{Name: "entity", Pattern: sexp.Pat("(entity ?name ?items...)"), Then: expandEntity, Produces: "struct, store", Doc: "splits an entity into a struct, a store interface and an impl"},
	},
}

func expandEntity(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	name := b.Atom("name")
	st := sexp.L(sexp.Sym("struct"), sexp.Sym(name))
	var store *sexp.Node
	fields := map[string]string{} // field name -> type
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "store":
			store = it
		case "field":
			fields[it.Args()[0].Atom] = it.Args()[1].Atom
			st.List = append(st.List, it)
		default:
			st.List = append(st.List, it)
		}
	}
	if store == nil {
		return []*sexp.Node{st}, nil
	}

	iface := name + "Store"
	in := sexp.L(sexp.Sym("interface"), sexp.Sym(iface), sexp.L(sexp.Sym("doc"), sexp.Str(iface+" persists "+name+" values.")))
	impl := sexp.L(sexp.Sym("impl"), sexp.Sym(iface), sexp.L(sexp.Sym("for"), sexp.Sym(name)))
	ops := sexp.L(sexp.Sym("ops"))
	for _, op := range store.Args() {
		if op.Head() == "constraint" { // travels with the impl to the LLM
			impl.List = append(impl.List, op)
			continue
		}
		if op.IsList && len(op.List) == 1 && contains(knownRequirements(), op.Head()) {
			impl.List = append(impl.List, sexp.L(sexp.Sym("requires"), sexp.Sym(op.Head())))
			continue
		}
		meth, err := storeMethod(op, name, fields)
		if err != nil {
			return nil, err
		}
		in.List = append(in.List, meth)
		ops.List = append(ops.List, op)
	}
	impl.List = append(impl.List, ops)
	return []*sexp.Node{st, in, impl}, nil
}

// Store ops. The plain ones need no field; the -by ones take one:
//
//	get list save delete count
//	(list-by F) (get-by F) (count-by F) (exists-by F) (delete-by F)
//	(method NAME (params ...) (returns ...) (doc ...))  ; custom: always a hole
var (
	storeOps = []string{"get", "list", "save", "delete", "count"}
	fieldOps = []string{"list-by", "get-by", "count-by", "exists-by", "delete-by"}
)

// opKind splits an op into its kind and field: (list-by NoteID) -> list-by, NoteID.
func opKind(op *sexp.Node) (kind, field string) {
	if !op.IsList {
		return op.Atom, ""
	}
	if kind = op.Head(); kind != "method" && len(op.List) > 1 {
		field = op.List[1].Atom
	}
	return kind, field
}

// opMethodName is the Go method an op becomes.
func opMethodName(kind, field string) string {
	switch kind {
	case "get", "list", "save", "delete", "count":
		return strings.ToUpper(kind[:1]) + kind[1:]
	case "list-by":
		return "ListBy" + field
	case "get-by":
		return "GetBy" + field
	case "count-by":
		return "CountBy" + field
	case "exists-by":
		return "ExistsBy" + field
	case "delete-by":
		return "DeleteBy" + field
	}
	return ""
}

// fieldParam names the parameter for a field, keeping initialisms the
// Go way: NoteID -> noteID, Email -> email, HTTPServer -> httpServer.
func fieldParam(field string) string {
	p := lowerFirstWord(field)
	if token.IsKeyword(p) || p == "s" || p == "ctx" || p == "" {
		return p + "Value"
	}
	return p
}

// storeMethod is a table of small tiles, one per op. Each method records
// its op, so later passes can pick backend-specific hints and SQL.
func storeMethod(op *sexp.Node, entity string, fields map[string]string) (*sexp.Node, error) {
	kind, field := opKind(op)
	ptr, v, idType := "*"+entity, paramName(entity), fields["ID"]
	params := func(ps ...[2]string) *sexp.Node {
		n := sexp.L(sexp.Sym("params"))
		for _, p := range ps {
			n.List = append(n.List, sexp.L(sexp.Sym(p[0]), sexp.Sym(p[1])))
		}
		return n
	}
	returns := func(ts ...string) *sexp.Node {
		return sexp.L(append([]*sexp.Node{sexp.Sym("returns")}, sexp.Syms(ts...)...)...)
	}
	doc := func(f string, a ...any) *sexp.Node { return sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(f, a...))) }
	tag := sexp.L(sexp.Sym("op"), sexp.Sym(kind))
	if field != "" {
		tag.List = append(tag.List, sexp.Sym(field))
	}
	name := opMethodName(kind, field)
	byField := params([2]string{fieldParam(field), fields[field]})
	meth := func(d, ps, rs *sexp.Node) (*sexp.Node, error) {
		return sexp.L(sexp.Sym("method"), sexp.Sym(name), d, ps, rs, tag), nil
	}

	switch kind {
	case "get":
		return meth(doc("Get returns the %s with the given ID.", entity), params([2]string{"id", idType}), returns(ptr, "error"))
	case "list":
		return meth(doc("List returns all %s values ordered by ID.", entity), params(), returns("[]"+ptr, "error"))
	case "save":
		return meth(doc("Save inserts or replaces %s by ID.", v), params([2]string{v, ptr}), returns("error"))
	case "delete":
		return meth(doc("Delete removes the %s with the given ID.", entity), params([2]string{"id", idType}), returns("error"))
	case "count":
		return meth(doc("Count returns how many %s values exist.", entity), params(), returns("int64", "error"))
	case "list-by":
		return meth(doc("%s returns the %s values whose %s equals %s, ordered by ID.", name, entity, field, fieldParam(field)), byField, returns("[]"+ptr, "error"))
	case "get-by":
		return meth(doc("%s returns the %s with the lowest ID whose %s equals %s.", name, entity, field, fieldParam(field)), byField, returns(ptr, "error"))
	case "count-by":
		return meth(doc("%s returns how many %s values have %s equal to %s.", name, entity, field, fieldParam(field)), byField, returns("int64", "error"))
	case "exists-by":
		return meth(doc("%s reports whether any %s has %s equal to %s.", name, entity, field, fieldParam(field)), byField, returns("bool", "error"))
	case "delete-by":
		return meth(doc("%s removes every %s whose %s equals %s.", name, entity, field, fieldParam(field)), byField, returns("error"))
	case "method": // custom: carried through as written
		return sexp.L(append(append([]*sexp.Node{sexp.Sym("method")}, op.Args()...), sexp.L(sexp.Sym("op"), sexp.Sym("custom")))...), nil
	}
	return nil, fmt.Errorf("unknown store op %s", op.Flat())
}

// lowerFirstWord lower-cases a Go name's first word: NoteID -> noteID,
// ID -> id, HTTPServer -> httpServer.
func lowerFirstWord(s string) string {
	r := []rune(s)
	i := 0
	for i < len(r) && unicode.IsUpper(r[i]) {
		i++
	}
	switch {
	case i == 0:
		return s
	case i == 1 || i == len(r):
		// Email -> email; ID -> id
	default:
		i-- // HTTPServer: keep the S that starts the next word
	}
	for j := 0; j < i; j++ {
		r[j] = unicode.ToLower(r[j])
	}
	return string(r)
}
