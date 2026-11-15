package tiles

import (
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "http-json",
		Engine:     compile.NoEngine,
		Capability: "api",
		Doc:        "net/http and JSON: a Handler and ServeMux over the package's stores",
		Cost: append(append(compile.Cost{}, apiHoles...), compile.CostTerm{Dim: "maintenance", Value: 1},
			compile.CostTerm{Dim: "dependency", Value: 0, Source: "derived", Note: "net/http only; Go 1.22 routes methods and wildcards"}),
		IllegalFor: map[string]string{
			"typed-clients": "there is no schema: every client writes its own requests and types by hand",
			"grpc-native":   "it is not gRPC",
			"streaming":     "this tile generates request and response handlers only; a stream of events needs one of the schema tiles",
		},
		Satisfies: map[string]string{
			"browser-clients": "fetch and JSON, with nothing in between",
		},
		Impl: &compile.APITransport{Emit: emitHTTPJSON},
	})
	compile.RegisterAlias("http-json", "http")
}

// emitHTTPJSON gives each operation the route a REST reader expects and
// hands the result to the http tile, so (api ...) and (http ...) cannot
// drift apart.
func emitHTTPJSON(m *compile.Munch, n *sexp.Node, ops []compile.APIOp, _ *compile.APITransport) ([]*sexp.Node, error) {
	h := sexp.L(sexp.Sym("http"))
	h.Pos = n.Pos
	if doc := n.Find("doc"); doc != nil {
		h.List = append(h.List, doc)
	}
	for _, op := range ops {
		base := "/" + strings.ReplaceAll(compile.Snake(compile.Plural(op.Entity)), "_", "-")
		method, p := "GET", base
		switch op.Verb {
		case "get":
			p = base + "/{id}"
		case "save":
			method = "POST"
		case "delete":
			method, p = "DELETE", base+"/{id}"
		}
		h.List = append(h.List, sexp.L(sexp.Sym("route"), sexp.Sym(method), sexp.Str(p), sexp.L(sexp.Sym(op.Verb), sexp.Sym(op.Entity))))
	}
	return compile.SelectHTTP(m, sexp.Bindings{}, h)
}
