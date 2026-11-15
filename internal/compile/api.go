package compile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// (package orders
//   (api
//     (doc "The orders API.")
//     (get Order) (list Order) (save Order) (delete Order)
//     (typed-clients)))
//
// says what the package serves and what its clients need, and leaves how to
// the solver. Three tiles offer it:
//
//   - http-json: net/http and JSON, the same Handler (http ...) generates;
//   - connect:   a .proto and a Connect server, which also answers gRPC and
//                gRPC-Web clients;
//   - grpc:      the same .proto and a grpc-go server.
//
// (http ...) is for a package that writes its routes by hand; (api ...) is
// for one that wants them chosen.
//
// The two schema tiles write the .proto from the entities, as the sqlc
// tiles write the SQL, and buf turns it into Go. The server is generated
// whole, conversions included: turning an Order into its wire form is
// known from its fields. Two things are judgment and stay holes, as they
// do for HTTP: what makes a value acceptable, and which code an error
// from the store deserves.

func init() {
	RegisterCapability(&Capability{
		Name:         "api",
		Doc:          "serves a package's entities to clients in other programs",
		Requirements: []string{"typed-clients", "browser-clients", "grpc-native", "streaming"},
	})

	RegisterPackageTile(&PackageTile{
		Form: "api",
		Pass: Select,
		Rule: Rule{Name: "api", Pattern: sexp.Pat("(api ?items...)"), Then: selectAPI,
			Produces: "api", Doc: "a server for the package's entities, over whichever transport its clients need",
			Cost: Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 0}, {Dim: "dependency", Value: 0}}},
		Validate: validateAPI,
	})
}

// APITransport is what an api offer contributes: the code that emits it.
type APITransport struct {
	Flavor  string            // "connect" or "grpc" for the schema tiles
	Plugin  string            // the buf plugin that generates its service code
	Imports map[string]string // qualifier -> import path its hole signatures name
	Emit    func(m *Munch, n *sexp.Node, ops []APIOp, tr *APITransport) ([]*sexp.Node, error)
}

// apiVerbs are the store operations an api can serve.
var apiVerbs = []string{"get", "list", "save", "delete"}

// APIOp is one operation an api serves: (get Order).
type APIOp struct{ Verb, Entity string }

func validateAPI(v *Validator, n *sexp.Node, types map[string]bool) {
	for _, name := range []string{"Handler", "NewHandler", "APIServer", "NewAPIServer"} {
		if types[name] {
			v.Bad(n, "(api ...) generates %s, but the package already declares it (one api or http form per package)", name)
		}
		types[name] = true
	}
	seen := map[string]bool{}
	for _, it := range n.Args() {
		if it.IsList && len(it.List) == 1 {
			validateRequirements("api", []string{it.Head()}, it, v)
			continue
		}
		switch {
		case it.Head() == "doc":
			v.Shape(it, "(doc ?text)")
		case Contains(apiVerbs, it.Head()) || it.Head() == "watch":
			b := v.Shape(it, "("+it.Head()+" ?entity)")
			if b == nil {
				continue
			}
			if key := it.Head() + " " + b.Atom("entity"); seen[key] {
				v.Bad(it, "duplicate (%s)", key)
			} else {
				seen[key] = true
			}
		default:
			v.Bad(it, "api contains (VERB Entity), where VERB is %s, (watch Event), (doc ...) and requirements like (typed-clients), got %s%s",
				strings.Join(apiVerbs, ", "), Short(it),
				DidYouMean(it.Head(), append(append([]string{"doc", "watch"}, apiVerbs...), Capabilities["api"].Requirements...)))
		}
	}
	if len(seen) == 0 {
		v.Bad(n, "(api ...) needs at least one operation, such as (get Order)")
	}
}

func selectAPI(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	c, pkg := m.C, m.C.Pkg
	cov := c.Cover[pkg.Name+".API"]
	if cov == nil {
		return nil, fmt.Errorf("no tile was chosen for %s.API", pkg.Name)
	}
	tr, ok := cov.Offer.Impl.(*APITransport)
	if !ok {
		return nil, fmt.Errorf("the tile chosen for %s.API does not implement apis", pkg.Name)
	}
	var ops []APIOp
	for _, it := range n.Args() {
		if it.Head() == "watch" {
			if ev := it.List[1].Atom; pkg.Events[ev] == nil {
				return nil, fmt.Errorf("%s: (watch %s) streams an event, and package %s has no (event %s); add it to the package's (events ...)%s",
					it.Pos, ev, pkg.Name, ev, DidYouMean(ev, SortedKeys(pkg.Events)))
			}
			ops = append(ops, APIOp{"watch", it.List[1].Atom})
			continue
		}
		if !Contains(apiVerbs, it.Head()) {
			continue
		}
		op := APIOp{it.Head(), it.List[1].Atom}
		if pkg.Ifaces[op.Entity+"Store"] == nil {
			return nil, fmt.Errorf("%s: (%s %s) serves %s, which has no store in this package; add (store ...) to the entity",
				it.Pos, op.Verb, op.Entity, op.Entity)
		}
		if !storeHas(pkg.Ifaces[op.Entity+"Store"], op.Verb) {
			return nil, fmt.Errorf("%s: (%s %s) needs %sStore to %s; add %s to its (store ...)",
				it.Pos, op.Verb, op.Entity, op.Entity, op.Verb, op.Verb)
		}
		ops = append(ops, op)
	}
	return tr.Emit(m, n, ops, tr)
}

// storeHas reports whether a store interface has the method a verb calls.
func storeHas(iface *sexp.Node, verb string) bool {
	want := strings.ToUpper(verb[:1]) + verb[1:]
	for _, meth := range iface.FindAll("method") {
		if meth.List[1].Atom == want {
			return true
		}
	}
	return false
}

// ---- the schema tiles: a .proto, and a server over it ----

// bufFiles are buf.yaml and buf.gen.yaml for the plugins a project's api
// tiles asked for. One pair serves every package.
func bufFiles(plugins []string) (yaml, gen string) {
	yaml = "# " + GeneratedMarker + "\nversion: v2\nmodules:\n  - path: proto\n"
	var b strings.Builder
	b.WriteString("# " + GeneratedMarker + "\nversion: v2\nplugins:\n")
	seen := map[string]bool{}
	sort.Strings(plugins)
	for _, p := range plugins {
		if seen[p] {
			continue
		}
		seen[p] = true
		fmt.Fprintf(&b, "  - local: %s\n    out: gen\n    opt: paths=source_relative\n", p)
	}
	return yaml, b.String()
}

// watchRequirements is the requirement a (watch Event) puts on its api:
// only a tile that can hold a stream open can serve one.
func watchRequirements(api *sexp.Node) []string {
	if api.Find("watch") != nil {
		return []string{"streaming"}
	}
	return nil
}
