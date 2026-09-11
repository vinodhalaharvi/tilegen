package main

import (
	"fmt"
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
		{Name: "entity", Pattern: Pat("(entity ?name ?items...)"), Then: expandEntity},
	},
}

func expandEntity(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	name := b.Atom("name")
	st := L(Sym("struct"), Sym(name))
	var store *Node
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
		return []*Node{st}, nil
	}

	iface := name + "Store"
	in := L(Sym("interface"), Sym(iface), L(Sym("doc"), Str(iface+" persists "+name+" values.")))
	impl := L(Sym("impl"), Sym(iface), L(Sym("for"), Sym(name)))
	ops := L(Sym("ops"))
	for _, op := range store.Args() {
		if op.Head() == "constraint" { // travels with the impl to the LLM
			impl.List = append(impl.List, op)
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
	return []*Node{st, in, impl}, nil
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
func opKind(op *Node) (kind, field string) {
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
func storeMethod(op *Node, entity string, fields map[string]string) (*Node, error) {
	kind, field := opKind(op)
	ptr, v, idType := "*"+entity, paramName(entity), fields["ID"]
	params := func(ps ...[2]string) *Node {
		n := L(Sym("params"))
		for _, p := range ps {
			n.List = append(n.List, L(Sym(p[0]), Sym(p[1])))
		}
		return n
	}
	returns := func(ts ...string) *Node { return L(append([]*Node{Sym("returns")}, Syms(ts...)...)...) }
	doc := func(f string, a ...any) *Node { return L(Sym("doc"), Str(fmt.Sprintf(f, a...))) }
	tag := L(Sym("op"), Sym(kind))
	if field != "" {
		tag.List = append(tag.List, Sym(field))
	}
	name := opMethodName(kind, field)
	byField := params([2]string{fieldParam(field), fields[field]})
	meth := func(d, ps, rs *Node) (*Node, error) { return L(Sym("method"), Sym(name), d, ps, rs, tag), nil }

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
		return L(append(append([]*Node{Sym("method")}, op.Args()...), L(Sym("op"), Sym("custom")))...), nil
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
