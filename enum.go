package main

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
	"go/token"
	"strings"
	"unicode"
)

// (enum Status
//   (doc "Status is where an order is in its lifecycle.")
//   pending paid shipped cancelled)
//
// becomes a string type, typed constants in declaration order, a
// StatusValues list, Valid(), and ParseStatus(). No holes: an enum is pure
// structure. With postgres, an enum field is a TEXT column with a CHECK
// constraint, so the database enforces the same values.

func (v *validator) enum(n *sexp.Node, types map[string]bool) {
	b := v.shape(n, "(enum ?name ?items...)")
	if b == nil {
		return
	}
	name := b.One("name")
	v.declName(name, types)
	values := map[string]bool{}
	count := 0
	for _, it := range b.Rest("items") {
		if it.IsList {
			if it.Head() != "doc" {
				v.bad(it, "enum items are values and (doc ...), got %s%s", short(it), didYouMean(it.Head(), []string{"doc"}))
			} else {
				v.shape(it, "(doc ?text)")
			}
			continue
		}
		count++
		val := it.Atom
		switch {
		case val == "" || strings.ContainsAny(val, "'\"\\`"):
			v.bad(it, "enum value %q must be non-empty and free of quotes and backslashes", val)
			continue
		case values[val]:
			v.bad(it, "duplicate enum value %q", val)
			continue
		}
		values[val] = true
		if name != nil && !name.IsList {
			c := name.Atom + goName(val)
			if !token.IsIdentifier(c) {
				v.bad(it, "enum value %q does not make a Go constant name (got %s)", val, c)
			} else if types[c] {
				v.bad(it, "constant %s (from %q) collides with another name in the package", c, val)
			}
			types[c] = true
		}
	}
	if count == 0 {
		v.bad(n, "enum needs at least one value")
	}
}

// goName turns an enum value into the tail of a Go constant name:
// paid -> Paid, in-transit -> InTransit, on_hold -> OnHold.
func goName(val string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(val, func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '.' }) {
		r := []rune(part)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}

// enumValues returns an enum's values in declaration order.
func enumValues(n *sexp.Node) []string {
	var out []string
	for _, it := range n.Args()[1:] {
		if !it.IsList {
			out = append(out, it.Atom)
		}
	}
	return out
}

func selectEnum(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	name := b.Atom("name")
	vals := enumValues(n)
	doc := n.Text("doc")
	if doc == "" {
		doc = fmt.Sprintf("%s is one of: %s.", name, strings.Join(vals, ", "))
	}
	consts := sexp.L(sexp.Sym("go/consts"))
	var names []string
	for _, v := range vals {
		c := name + goName(v)
		names = append(names, c)
		consts.List = append(consts.List, sexp.L(sexp.Sym("const"), sexp.Sym(c), sexp.Sym(name), sexp.Str(v)))
	}
	list := strings.Join(names, ", ")
	return []*sexp.Node{
		sexp.L(sexp.Sym("go/type"), sexp.Sym(name), sexp.Sym("string"), sexp.L(sexp.Sym("doc"), sexp.Str(doc))),
		consts,
		sexp.L(sexp.Sym("go/var"), sexp.Sym(name+"Values"), sexp.Str(fmt.Sprintf("[]%s{%s}", name, list)),
			sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("%sValues lists every %s, in declaration order.", name, name)))),
		sexp.L(sexp.Sym("go/func"), sexp.Sym("Valid"), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("Valid reports whether s is a known %s.", name))),
			sexp.L(sexp.Sym("recv"), sexp.Sym("s"), sexp.Sym(name)), sexp.L(sexp.Sym("params")), sexp.L(sexp.Sym("returns"), sexp.Sym("bool")),
			sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("switch s {\ncase %s:\nreturn true\n}\nreturn false", list)))),
		sexp.L(sexp.Sym("go/func"), sexp.Sym("Parse"+name), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("Parse%s returns the %s spelled s, or an error.", name, name))),
			sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("s"), sexp.Sym("string"))), sexp.L(sexp.Sym("returns"), sexp.Sym(name), sexp.Sym("error")),
			sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("if v := %s(s); v.Valid() {\nreturn v, nil\n}\nreturn \"\", fmt.Errorf(\"unknown %s %%q\", s)", name, name)))),
	}, nil
}

// enumFor finds the enum a field type refers to, in this package (Status)
// or another project package (orders.Status). Pointers are nullable enums.
func (c *Ctx) enumFor(typ, pkg string) []string {
	typ = strings.TrimPrefix(typ, "*")
	if !strings.Contains(typ, ".") {
		typ = pkg + "." + typ
	}
	return c.Enums[typ]
}
