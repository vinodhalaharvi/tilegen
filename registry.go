package main

import (
	"fmt"
	"sort"
	"strings"
)

// The tile registry. Every tile declares what it covers, what capability
// it produces, what it needs, and what it costs. Pass rules carry this in
// their Rule; storage backends register a Backend. `tilegen tiles` lists
// the whole registry, as a table or as S-expressions.
//
// Costs are declared, not yet used: selection is still maximal munch plus
// the config. Choosing the cheapest legal tile is the next roadmap step.

// Cost is a tile's declared cost along named dimensions, in a fixed order.
type Cost []CostTerm

type CostTerm struct {
	Dim   string // llm-work | maintenance | dependency | runtime | uncertainty
	Value int
}

// Short is the compact form for tables: llm 3, maint 2, dep 3.
func (c Cost) Short() string {
	abbr := map[string]string{"llm-work": "llm", "maintenance": "maint", "dependency": "dep", "runtime": "run", "uncertainty": "unc"}
	parts := make([]string, len(c))
	for i, t := range c {
		d := abbr[t.Dim]
		if d == "" {
			d = t.Dim
		}
		parts[i] = fmt.Sprintf("%s %d", d, t.Value)
	}
	return strings.Join(parts, ", ")
}

func (c Cost) String() string {
	parts := make([]string, len(c))
	for i, t := range c {
		parts[i] = fmt.Sprintf("%s %d", t.Dim, t.Value)
	}
	return strings.Join(parts, ", ")
}

// Backend is a registered storage tile: it implements an entity's store
// interface for one kind of storage, chosen by (config (storage NAME)).
type Backend struct {
	Name         string   // the config value: (storage NAME)
	Tile         string   // its name in the registry, e.g. postgres-sqlc
	Doc          string   // one line for `tilegen tiles`
	Requires     []string // external tools
	Cost         Cost
	Topics       []string             // GitHub topics a project using it gets
	Packages     map[string]string    // project folders its code lives in, by package name (reserved)
	Generate     string               // command to run after tilegen and before building, if any
	GenerateDoc  string               // what it does, for the starter README
	IllegalWhen  map[string]string    // need -> why this backend cannot serve it, e.g. durable
	Imports      map[string]string    // package qualifiers its code uses -> import paths
	ProjectForms func(c *Ctx) []*Node // project-level target forms, once per project using it
	Form         string               // what its store code yields: "" (domain values) or e.g. db-rows

	// Implement scaffolds one store implementation.
	Implement func(StoreInput) (StoreParts, error)
}

// StoreInput is what a backend gets to implement one store.
type StoreInput struct {
	C      *Ctx
	Pkg    *PkgScope
	Entity string // Order
	Struct *Node  // the entity's (struct ...)
	Iface  string // OrderStore
	Impl   string // PostgresOrderStore
	IDType string
	Ops    *Node // (ops get list ...)
}

// StoreParts is what a backend contributes to the implementation file.
type StoreParts struct {
	Fields  []*Node // (field name Type) of the implementation struct
	Params  *Node   // the constructor's (params ...)
	Body    string  // the constructor's body
	Hint    func(meth *Node) string
	Extra   []*Node  // more target forms, e.g. (sql/query ...)
	Context []string // more context files for the LLM
}

var backends = map[string]*Backend{}

// RegisterBackend adds a storage tile to the registry. Backends call it
// from init, like database/sql drivers.
func RegisterBackend(b *Backend) {
	if _, dup := backends[b.Name]; dup {
		panic("tilegen: backend registered twice: " + b.Name)
	}
	backends[b.Name] = b
}

func lookupBackend(name string) *Backend { return backends[name] }

// form is the form a backend's store code yields; domain unless declared.
func (b *Backend) form() string {
	if b.Form == "" {
		return domainForm
	}
	return b.Form
}

func backendNames() []string {
	names := make([]string, 0, len(backends))
	for n := range backends {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TileInfo is one row of the registry, whatever kind of tile it is.
type TileInfo struct {
	IllegalWhen               map[string]string
	Form                      string    // for backends: the form their code yields
	Converts                  [2]string // for chains: from, to
	CostRule                  string    // for chains: how the cost is computed
	Name, Pass, Produces, Doc string
	Covers                    []*Node // the pattern(s) it matches
	Requires                  []string
	Cost                      Cost
}

// Registry lists every tile: the pass rules in order, then the backends.
func Registry() []TileInfo {
	var out []TileInfo
	for _, p := range Pipeline {
		for _, r := range p.Rules {
			out = append(out, TileInfo{Name: r.Name, Pass: p.Name, Covers: []*Node{r.Pattern},
				Produces: r.Produces, Doc: r.Doc, Cost: r.Cost})
		}
	}
	for _, name := range backendNames() {
		b := backends[name]
		out = append(out, TileInfo{Name: b.Tile, Pass: "select", Covers: []*Node{Pat("(impl ?iface ?parts...)"), L(Sym("backend"), Sym(b.Name))},
			Produces: "store", Doc: b.Doc, Requires: b.Requires, Cost: b.Cost, IllegalWhen: b.IllegalWhen, Form: b.form()})
	}
	for _, ch := range chains {
		out = append(out, TileInfo{Name: ch.Name, Pass: "select", Covers: []*Node{L(Sym("convert"), Sym(ch.From), Sym(ch.To))},
			Produces: ch.To, Doc: ch.Doc, Converts: [2]string{ch.From, ch.To}, CostRule: ch.CostRule})
	}
	return out
}

// tileSexp renders a registry row as an S-expression: the shape tiles will
// have once they can be written as data.
func tileSexp(t TileInfo) *Node {
	n := L(Sym("tile"), Sym(t.Name), L(Sym("pass"), Sym(t.Pass)))
	if t.Doc != "" {
		n.List = append(n.List, L(Sym("doc"), Str(t.Doc)))
	}
	n.List = append(n.List, L(append([]*Node{Sym("covers")}, t.Covers...)...))
	if t.Produces != "" {
		prod := L(Sym("produces"))
		for _, p := range strings.Split(t.Produces, ", ") {
			prod.List = append(prod.List, Sym(p))
		}
		n.List = append(n.List, prod)
	}
	if t.Form != "" && t.Form != domainForm {
		n.List = append(n.List, L(Sym("form"), Sym(t.Form)))
	}
	if t.Converts[0] != "" {
		n.List = append(n.List, L(Sym("converts"), Sym(t.Converts[0]), Sym(t.Converts[1])))
	}
	if t.CostRule != "" {
		n.List = append(n.List, L(Sym("cost-rule"), Str(t.CostRule)))
	}
	for _, need := range sortedKeys(t.IllegalWhen) {
		n.List = append(n.List, L(Sym("illegal-when"), L(Sym("store"), Sym(need)), Str(t.IllegalWhen[need])))
	}
	if len(t.Requires) > 0 {
		req := L(Sym("requires"))
		for _, r := range t.Requires {
			req.List = append(req.List, L(Sym("tool"), Sym(r)))
		}
		n.List = append(n.List, req)
	}
	if len(t.Cost) > 0 {
		cost := L(Sym("cost"))
		for _, c := range t.Cost {
			cost.List = append(cost.List, L(Sym(c.Dim), Sym(fmt.Sprint(c.Value))))
		}
		n.List = append(n.List, cost)
	}
	return n
}

// PackageTile is a tile for a new package-level spec form, registered from
// its own file: its rule joins a pass (just before the catch-all), and it
// validates its own form. New forms need no change to the core.
type PackageTile struct {
	Form     string // the head it covers, e.g. events
	Pass     *Pass
	Rule     Rule
	Validate func(v *validator, n *Node, types map[string]bool)
}

var packageTiles = map[string]*PackageTile{}

// RegisterPackageTile adds a tile for a package-level form.
func RegisterPackageTile(t *PackageTile) {
	if _, dup := packageTiles[t.Form]; dup {
		panic("tilegen: package form registered twice: " + t.Form)
	}
	packageTiles[t.Form] = t
	rules := t.Pass.Rules
	if n := len(rules); n > 0 && rules[n-1].Pattern.Atom == "_" { // keep the catch-all last
		t.Pass.Rules = append(append(rules[:n-1:n-1], t.Rule), rules[n-1])
	} else {
		t.Pass.Rules = append(rules, t.Rule)
	}
}

// knownPackageForms is every package-level form a tile covers, for
// did-you-mean suggestions.
func knownPackageForms() []string {
	out := append([]string{}, packageForms...)
	for f := range packageTiles {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
