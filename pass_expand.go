package main

import "fmt"

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
	var idType string
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "store":
			store = it
		case "field":
			if it.Args()[0].Atom == "ID" {
				idType = it.Args()[1].Atom
			}
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
		if op.IsList { // (constraint "...") travels with the impl to the LLM
			impl.List = append(impl.List, op)
			continue
		}
		meth, err := storeMethod(op.Atom, name, idType)
		if err != nil {
			return nil, err
		}
		in.List = append(in.List, meth)
		ops.List = append(ops.List, op)
	}
	impl.List = append(impl.List, ops)
	return []*Node{st, in, impl}, nil
}

// storeMethod is a table of four tiny tiles, one per CRUD verb.
func storeMethod(op, entity, idType string) (*Node, error) {
	ptr, v := "*"+entity, paramName(entity)
	params := func(ps ...[2]string) *Node {
		n := L(Sym("params"))
		for _, p := range ps {
			n.List = append(n.List, L(Sym(p[0]), Sym(p[1])))
		}
		return n
	}
	returns := func(ts ...string) *Node { return L(append([]*Node{Sym("returns")}, Syms(ts...)...)...) }
	doc := func(s string) *Node { return L(Sym("doc"), Str(s)) }

	switch op {
	case "get":
		return L(Sym("method"), Sym("Get"),
			doc(fmt.Sprintf("Get returns the %s with the given ID.", entity)),
			params([2]string{"id", idType}), returns(ptr, "error")), nil
	case "list":
		return L(Sym("method"), Sym("List"),
			doc(fmt.Sprintf("List returns all %s values ordered by ID.", entity)),
			params(), returns("[]"+ptr, "error")), nil
	case "save":
		return L(Sym("method"), Sym("Save"),
			doc(fmt.Sprintf("Save inserts or replaces %s by ID.", v)),
			params([2]string{v, ptr}), returns("error")), nil
	case "delete":
		return L(Sym("method"), Sym("Delete"),
			doc(fmt.Sprintf("Delete removes the %s with the given ID.", entity)),
			params([2]string{"id", idType}), returns("error")), nil
	}
	return nil, fmt.Errorf("unknown store op %q", op)
}
