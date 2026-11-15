package tiles

import (
	"fmt"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// busDecl is the shape every remote transport has: a struct holding a
// client and a name, and a constructor taking both. The methods are holes,
// so this is all the tile has to build.
func busDecl(in compile.BusInput, what string, fields []*sexp.Node, params *sexp.Node, body, note string) ([]*sexp.Node, error) {
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym(in.Impl), sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf(
		"%s is a Bus backed by %s. %s", in.Impl, what, note))))
	decl.List = append(decl.List, fields...)
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("New"+in.Impl),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("New%s returns a %s over the given client.", in.Impl, in.Impl))),
		params,
		sexp.L(sexp.Sym("returns"), sexp.Sym("*"+in.Impl)),
		sexp.L(sexp.Sym("body"), sexp.Str(body)))
	return []*sexp.Node{decl, ctor}, nil
}
