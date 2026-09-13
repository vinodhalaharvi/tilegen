package main

import (
	"fmt"
	"path"
	"strings"
)

// (package api
//   (auth
//     (doc "Who a request to this API is from.")
//     (revocable)
//     (third-party-idp)))
//
// generates, in <package>/auth_gen.go and with no holes:
//
//   - Principal, who a request turned out to be;
//   - Authenticator, the interface every scheme satisfies;
//   - ErrUnauthenticated and ErrForbidden, the two answers a scheme gives
//     when it will not produce a principal;
//   - WithPrincipal and PrincipalFrom, the context pair;
//   - Protect, middleware that authenticates before a handler runs, so a
//     handler behind it can assume PrincipalFrom succeeds.
//
// The scheme chosen for the package's auth need supplies the
// implementation: a struct, a constructor, and one hole, Authenticate.
// Reading a credential and deciding whether to trust it is the whole
// judgment call, and it is the only thing left open.

func init() {
	RegisterCapability(&Capability{
		Name:         "auth",
		Doc:          "decides who a request is from",
		Requirements: []string{"revocable", "third-party-idp", "offline-verify", "expiring"},
	})

	RegisterPackageTile(&PackageTile{
		Form: "auth",
		Pass: Select,
		Rule: Rule{Name: "auth", Pattern: Pat("(auth ?items...)"), Then: selectAuth,
			Produces: "auth", Doc: "a Principal, an Authenticator interface, and middleware that fills the context",
			Cost: Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 0}, {Dim: "dependency", Value: 0}}},
		Validate: validateAuth,
	})
}

func validateAuth(v *validator, n *Node, types map[string]bool) {
	for _, name := range []string{"Principal", "Authenticator"} {
		if types[name] {
			v.bad(n, "(auth ...) generates %s, but the package already declares it (one auth form per package)", name)
		}
		types[name] = true
	}
	for _, it := range n.Args() {
		if it.IsList && len(it.List) == 1 {
			validateRequirements("auth", []string{it.Head()}, it, v)
			continue
		}
		switch it.Head() {
		case "doc":
			v.shape(it, "(doc ?text)")
		default:
			v.bad(it, "auth contains (doc ...) and requirements like (revocable), got %s%s",
				short(it), didYouMean(it.Head(), append([]string{"doc"}, capabilities["auth"].Requirements...)))
		}
	}
}

// AuthScheme is what an auth offer contributes: the implementation type's
// name and the code that declares it. The Principal type, the interface
// and the middleware are the same whichever scheme wins, so the tile
// makes those.
type AuthScheme struct {
	Suffix string    // the implementation type: JWTAuthenticator
	Topics []string  // GitHub topics a project using it gets
	Import [2]string // qualifier and import path its code needs, if any
	Emit   func(in AuthInput) ([]*Node, error)

	// Hint is the intent for Authenticate. Empty means the scheme emits
	// complete code and leaves no hole.
	Hint func(in AuthInput) string
}

// AuthInput is what a scheme needs to declare its implementation.
type AuthInput struct {
	C       *Ctx
	Pkg     *PkgScope
	Impl    string // the type name to declare
	CtxType string // "context.Context, " or ""
	CtxArg  string // "ctx, " or ""
}

// selectAuth makes the parts every scheme shares, then asks the chosen
// scheme for the implementation.
func selectAuth(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	cov := c.Cover[pkg.Name+".Auth"]
	if cov == nil {
		return nil, fmt.Errorf("no tile was chosen for %s.Auth", pkg.Name)
	}
	scheme, ok := cov.Offer.Impl.(*AuthScheme)
	if !ok {
		return nil, fmt.Errorf("the tile chosen for %s.Auth does not implement authentication", pkg.Name)
	}
	ctxType, ctxArg := "", ""
	if c.Cfg.ContextFirst {
		ctxType, ctxArg = "context.Context, ", "ctx, "
	}
	in := AuthInput{C: c, Pkg: pkg, Impl: scheme.Suffix, CtxType: ctxType, CtxArg: ctxArg}

	doc := n.Text("doc")
	if doc == "" {
		doc = fmt.Sprintf("Principal is who a request to package %s turned out to be.", pkg.Name)
	}
	principal := L(Sym("go/struct"), Sym("Principal"), L(Sym("doc"), Str(doc)),
		L(Sym("field"), Sym("Subject"), Sym("string"), L(Sym("doc"), Str("Subject identifies the caller: a user id, a service name, whatever the scheme says."))),
		L(Sym("field"), Sym("Scopes"), Str("[]string"), L(Sym("doc"), Str("Scopes are what the caller is allowed to do, as the scheme reported them."))))

	params := L(Sym("params"))
	if c.Cfg.ContextFirst {
		params.List = append(params.List, L(Sym("ctx"), Sym("context.Context")))
	}
	params.List = append(params.List, L(Sym("r"), Sym("*http.Request")))
	iface := L(Sym("go/interface"), Sym("Authenticator"),
		L(Sym("doc"), Str("Authenticator decides who a request is from.\nIt reports ErrUnauthenticated when it cannot say, which is not the same\nas an error: a failure to reach a key server is the scheme's own error.")),
		L(Sym("method"), Sym("Authenticate"),
			L(Sym("doc"), Str("Authenticate returns the principal r is from, or an error.\nIt never returns a principal alongside an error.")),
			params, L(Sym("returns"), Sym("*Principal"), Sym("error"))))

	// Inside Protect the only context is the request's, so the call there
	// is r.Context() even though the interface is context-first.
	callArg := ""
	if ctxArg != "" {
		callArg = "r.Context(), "
	}
	decls := []*Node{principal, iface, L(Sym("go/raw"), Str(authHelpers(callArg)))}

	impl, err := scheme.Emit(in)
	if err != nil {
		return nil, err
	}
	assert := L(Sym("go/assert"), Sym("Authenticator"), Sym(scheme.Suffix))

	// A scheme that emits complete code goes in the generated file with
	// everything else. One that leaves Authenticate to the LLM gets a
	// scaffolded file of its own, kept and reconciled like a store's.
	if scheme.Hint == nil {
		gen, err := goFile(c, path.Join(pkg.Dir, "auth_gen.go"), "generated", "", append(append(decls, impl...), assert))
		if err != nil {
			return nil, err
		}
		return []*Node{gen}, nil
	}
	gen, err := goFile(c, path.Join(pkg.Dir, "auth_gen.go"), "generated", "", append(decls, assert))
	if err != nil {
		return nil, err
	}
	file := path.Join(pkg.Dir, snake(scheme.Suffix)+".go")
	context := []string{path.Join(pkg.Dir, pkg.Name+"_gen.go"), path.Join(pkg.Dir, "auth_gen.go")}
	stubs, tasks := stubsAndTasks(pkg, scheme.Suffix, "Authenticator", file, iface.FindAll("method"),
		func(meth *Node) string { return scheme.Hint(in) }, context,
		[]string{"Return ErrUnauthenticated when the request carries no usable credential, and never a principal alongside an error."})
	implFile, err := goFile(c, file, "keep", "", append(impl, stubs...))
	if err != nil {
		return nil, err
	}
	return append([]*Node{gen, implFile}, tasks...), nil
}

// authHelpers is the fixed code around any scheme: the two sentinel
// errors, the context pair, and the middleware. It uses http.Error rather
// than the http tile's JSON helpers, so a package can have auth without
// having an (http ...) form.
func authHelpers(ctxArg string) string {
	return strings.NewReplacer("CTXARG", ctxArg).Replace(`// ErrUnauthenticated means the request carried no usable credential, or
// one this Authenticator will not accept. Protect answers 401.
var ErrUnauthenticated = errors.New("unauthenticated")

// ErrForbidden means the request is from someone, but not someone allowed
// to make it. Protect answers 403.
var ErrForbidden = errors.New("forbidden")

// principalKey is unexported and a distinct type, so nothing outside this
// package can set or collide with the value Protect stores.
type principalKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal Protect put in ctx, if any.
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok
}

// Protect authenticates every request before next sees it. A request that
// fails never reaches next, so a handler behind Protect may assume
// PrincipalFrom succeeds.
//
// The three answers are deliberately distinct: an unusable credential is
// 401, a usable credential without the right is 403, and anything else is
// the Authenticator's own failure, which is 500 and not the caller's
// fault. No error text reaches the client.
func Protect(a Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.Authenticate(CTXARGr)
		switch {
		case errors.Is(err, ErrUnauthenticated):
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		case errors.Is(err, ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		case err != nil:
			http.Error(w, "authentication failed", http.StatusInternalServerError)
			return
		case p == nil:
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}`)
}
