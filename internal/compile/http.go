package compile

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
	goversion "go/version"
	"path"
	"sort"
	"strings"
)

// (package links
//   (http
//     (doc "The links API.")
//     (route GET    "/links"      (list Link))
//     (route GET    "/links/{id}" (get Link))
//     (route POST   "/links"      (save Link))
//     (route DELETE "/links/{id}" (delete Link))))
//
// generates a Handler over the package's stores and a Routes() method
// returning an *http.ServeMux. Go 1.22's router matches methods and {id}
// wildcards itself, so there is no dependency and no routing code to write.
//
// Most of a handler is known: decode, call the store, encode, choose a
// status. That is generated. Two things are judgment, so they are holes:
// validating a request body, and mapping a store error to a status code.
// One hole each per resource, not per route, because the answer is the
// Same across a resource's routes.

func init() {
	RegisterPackageTile(&PackageTile{
		Form: "http",
		Pass: Select,
		Rule: Rule{Name: "http", Pattern: sexp.Pat("(http ?items...)"), Then: selectHTTP,
			Produces: "go/file", Doc: "a Handler and ServeMux over the package's stores",
			Cost: Cost{{Dim: "llm-work", Value: 2, Source: "derived", Note: "only validation and error mapping are left open"},
				{Dim: "maintenance", Value: 1}, {Dim: "dependency", Value: 0, Source: "derived", Note: "net/http only; Go 1.22 routes methods and wildcards"}}},
		Validate: validateHTTP,
	})
}

// httpVerbs are the store operations a route can name, and what each one
// does with the request.
var httpVerbs = []string{"get", "list", "save", "delete"}

func validateHTTP(v *Validator, n *sexp.Node, types map[string]bool) {
	if v.goVersion != "" && goversion.Compare("go"+v.goVersion, "go1.22") < 0 {
		v.Bad(n, "(http ...) needs (go 1.22) or later, whose net/http routes methods and {wildcards}; the project has (go %s)", v.goVersion)
	}
	for _, name := range []string{"Handler", "NewHandler"} {
		if types[name] {
			v.Bad(n, "(http ...) generates %s, but the package already declares it (one http form per package)", name)
		}
		types[name] = true
	}
	seen := map[string]bool{}
	count := 0
	for _, it := range n.Args() {
		switch it.Head() {
		case "doc":
			v.Shape(it, "(doc ?text)")
		case "route":
			count++
			b := v.Shape(it, "(route ?method ?path ?op)")
			if b == nil {
				continue
			}
			method, p := b.Atom("method"), b.Atom("path")
			if method != strings.ToUpper(method) || !Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}, method) {
				v.Bad(it, "route method must be GET, POST, PUT, PATCH or DELETE, got %q%s", method,
					DidYouMean(strings.ToUpper(method), []string{"GET", "POST", "PUT", "PATCH", "DELETE"}))
			}
			if !strings.HasPrefix(p, "/") {
				v.Bad(it, "route path must start with /, got %q", p)
			}
			if key := method + " " + p; seen[key] {
				v.Bad(it, "duplicate route %s", key)
			} else {
				seen[key] = true
			}
			op := b.One("op")
			ob := sexp.Bindings{}
			if !sexp.Match(sexp.Pat("(?verb ?entity)"), op, ob) || !Contains(httpVerbs, ob.Atom("verb")) {
				v.Bad(op, "a route does (VERB Entity), where VERB is %s, got %s%s",
					strings.Join(httpVerbs, ", "), Short(op), DidYouMean(op.Head(), httpVerbs))
				continue
			}
			// A path wildcard is only meaningful where the store takes an ID.
			hasID := strings.Contains(p, "{id}")
			switch verb := ob.Atom("verb"); {
			case (verb == "get" || verb == "delete") && !hasID:
				v.Bad(it, "(%s %s) needs {id} in the path, since it acts on one %s", verb, ob.Atom("entity"), ob.Atom("entity"))
			case (verb == "list" || verb == "save") && hasID:
				v.Bad(it, "(%s %s) acts on the collection, so the path should not have {id}", verb, ob.Atom("entity"))
			}
		default:
			v.Bad(it, "http contains (route METHOD PATH (VERB Entity)) and (doc ...), got %s%s", Short(it), DidYouMean(it.Head(), []string{"route", "doc"}))
		}
	}
	if count == 0 {
		v.Bad(n, "(http ...) needs at least one (route ...)")
	}
}

type httpRoute struct {
	Method, Path, Verb, Entity string
	Node                       *sexp.Node
}

func selectHTTP(m *Munch, b sexp.Bindings, n *sexp.Node) ([]*sexp.Node, error) {
	c, pkg := m.C, m.C.Pkg
	var routes []httpRoute
	resources := map[string]bool{} // entities this handler serves
	for _, it := range n.FindAll("route") {
		op := it.List[3]
		r := httpRoute{Method: it.List[1].Atom, Path: it.List[2].Atom, Verb: op.Head(), Entity: op.List[1].Atom}
		if pkg.Ifaces[r.Entity+"Store"] == nil {
			return nil, fmt.Errorf("route %s %s serves %s, which has no store in this package; add (store ...) to the entity",
				r.Method, r.Path, r.Entity)
		}
		routes = append(routes, r)
		resources[r.Entity] = true
	}
	names := SortedKeys(resources)

	doc := n.Text("doc")
	if doc == "" {
		doc = fmt.Sprintf("Handler serves the %s HTTP API over its stores.", pkg.Name)
	}
	decl := sexp.L(sexp.Sym("go/struct"), sexp.Sym("Handler"), sexp.L(sexp.Sym("doc"), sexp.Str(doc)))
	params := sexp.L(sexp.Sym("params"))
	var inits []string
	for _, e := range names {
		field := LowerFirstWord(e) + "s"
		decl.List = append(decl.List, sexp.L(sexp.Sym("field"), sexp.Sym(field), sexp.Sym(e+"Store")))
		params.List = append(params.List, sexp.L(sexp.Sym(field), sexp.Sym(e+"Store")))
		inits = append(inits, field+": "+field)
	}
	ctor := sexp.L(sexp.Sym("go/func"), sexp.Sym("NewHandler"),
		sexp.L(sexp.Sym("doc"), sexp.Str("NewHandler returns a Handler over the given stores.")),
		params, sexp.L(sexp.Sym("returns"), sexp.Sym("*Handler")),
		sexp.L(sexp.Sym("body"), sexp.Str(fmt.Sprintf("return &Handler{%s}", strings.Join(inits, ", ")))))

	// Routes() registers every route; Go's ServeMux answers 404 and 405.
	var reg []string
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].Path+routes[i].Method < routes[j].Path+routes[j].Method })
	for _, r := range routes {
		reg = append(reg, fmt.Sprintf("\tmux.HandleFunc(%q, h.%s)", r.Method+" "+r.Path, handlerName(r)))
	}
	routesFn := sexp.L(sexp.Sym("go/func"), sexp.Sym("Routes"),
		sexp.L(sexp.Sym("doc"), sexp.Str("Routes returns a mux serving every route of this API.\nnet/http answers 404 for an unknown path and 405 for a known path\nwith the wrong method, so neither is handled here.")),
		sexp.L(sexp.Sym("recv"), sexp.Sym("h"), sexp.Sym("*Handler")), sexp.L(sexp.Sym("params")), sexp.L(sexp.Sym("returns"), sexp.Str("*http.ServeMux")),
		sexp.L(sexp.Sym("body"), sexp.Str("mux := http.NewServeMux()\n"+strings.Join(reg, "\n")+"\nreturn mux")))

	decls := []*sexp.Node{decl, ctor, routesFn}
	for _, r := range routes {
		fn, err := handlerFunc(c, r)
		if err != nil {
			return nil, err
		}
		decls = append(decls, fn)
	}
	decls = append(decls, sexp.L(sexp.Sym("go/raw"), sexp.Str(httpHelpers())))

	gen, err := GoFile(c, path.Join(pkg.Dir, "http_gen.go"), "generated", "", decls)
	if err != nil {
		return nil, err
	}

	// The two judgment calls, one pair per resource: what makes a request
	// body valid, and which status a store error deserves. One pair per
	// resource rather than per route, because the answer is the same
	// across a resource's routes.
	file := path.Join(pkg.Dir, "http.go")
	genFile := path.Join(pkg.Dir, pkg.Name+"_gen.go")
	var methods []*sexp.Node
	intents := map[string]string{}
	for _, e := range names {
		lower := LowerFirstWord(e)
		methods = append(methods,
			sexp.L(sexp.Sym("method"), sexp.Sym("validate"+e), sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym(lower), sexp.Sym("*"+e))), sexp.L(sexp.Sym("returns"), sexp.Sym("error"))),
			sexp.L(sexp.Sym("method"), sexp.Sym("statusFor"+e), sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("err"), sexp.Sym("error"))), sexp.L(sexp.Sym("returns"), sexp.Sym("int"))))
		intents["validate"+e] = fmt.Sprintf("Report what makes this %s unacceptable, before it reaches the store. Return nil when it is fine; the error's text is sent to the client with 400.", e)
		intents["statusFor"+e] = fmt.Sprintf("Map an error from %sStore to an HTTP status: ErrNotFound to 404, a conflict to 409, anything else to 500. Do not leak internal detail to the client.", e)
	}
	context := []string{genFile, path.Join(pkg.Dir, "http_gen.go")}
	stubs, tasks := stubsAndTasksAs("h", pkg, "Handler", "the HTTP API", file, methods,
		func(meth *sexp.Node) string { return intents[meth.List[1].Atom] }, context, nil)
	// A statusFor hole decides which errors mean 409, which is a question
	// about the store's driver, so name its error package: the prompt then
	// carries the real API instead of a remembered one.
	for _, t := range tasks {
		id := t.Text("id")
		if !strings.Contains(id, ".statusFor") {
			continue
		}
		// The backend that serves this resource, not every backend in the
		// project: a memory-backed store has no driver errors to map.
		e := strings.TrimPrefix(id[strings.LastIndex(id, ".statusFor"):], ".statusFor")
		cov := c.Cover[pkg.Name+"."+e+"Store"]
		if cov == nil {
			continue
		}
		if be, ok := cov.Offer.Impl.(*Backend); ok && be.ErrorPackage != "" {
			t.List = append(t.List, sexp.L(sexp.Sym("api-package"), sexp.Str(be.ErrorPackage)))
		}
	}

	implFile, err := GoFile(c, file, "keep", "", stubs)
	if err != nil {
		return nil, err
	}
	return append([]*sexp.Node{gen, implFile}, tasks...), nil
}

// handlerName is the method a route becomes: GET /links/{id} -> GetLink.
func handlerName(r httpRoute) string {
	verb := strings.ToUpper(r.Verb[:1]) + r.Verb[1:]
	if r.Verb == "list" {
		return verb + Plural(r.Entity)
	}
	return verb + r.Entity
}

// handlerFunc writes one route's handler. Everything here is known from
// the route and the store, so none of it is a hole.
func handlerFunc(c *Ctx, r httpRoute) (*sexp.Node, error) {
	e, recv := r.Entity, LowerFirstWord(r.Entity)+"s"
	ctx := ""
	if c.Cfg.ContextFirst {
		ctx = "r.Context(), "
	}
	idType := "string"
	var body strings.Builder
	if strings.Contains(r.Path, "{id}") {
		if st := c.Pkg.Structs[e]; st != nil {
			for _, f := range st.FindAll("field") {
				if f.List[1].Atom == "ID" {
					idType = f.List[2].Atom
				}
			}
		}
		body.WriteString(parseID(idType))
	}
	switch r.Verb {
	case "get":
		fmt.Fprintf(&body, "v, err := h.%s.Get(%sid)\nif err != nil {\n\thttpError(w, h.statusFor%s(err), err)\n\treturn\n}\nwriteJSON(w, http.StatusOK, v)", recv, ctx, e)
	case "list":
		fmt.Fprintf(&body, "vs, err := h.%s.List(%s)\nif err != nil {\n\thttpError(w, h.statusFor%s(err), err)\n\treturn\n}\nwriteJSON(w, http.StatusOK, vs)", recv, strings.TrimSuffix(ctx, ", "), e)
	case "save":
		fmt.Fprintf(&body, "var v %s\nif err := readJSON(r, &v); err != nil {\n\thttpError(w, http.StatusBadRequest, err)\n\treturn\n}\n"+
			"if err := h.validate%s(&v); err != nil {\n\thttpError(w, http.StatusBadRequest, err)\n\treturn\n}\n"+
			"if err := h.%s.Save(%s&v); err != nil {\n\thttpError(w, h.statusFor%s(err), err)\n\treturn\n}\nwriteJSON(w, http.StatusOK, &v)", e, e, recv, ctx, e)
	case "delete":
		fmt.Fprintf(&body, "if err := h.%s.Delete(%sid); err != nil {\n\thttpError(w, h.statusFor%s(err), err)\n\treturn\n}\nw.WriteHeader(http.StatusNoContent)", recv, ctx, e)
	}
	fn := sexp.L(sexp.Sym("go/func"), sexp.Sym(handlerName(r)),
		sexp.L(sexp.Sym("doc"), sexp.Str(fmt.Sprintf("%s serves %s %s.", handlerName(r), r.Method, r.Path))),
		sexp.L(sexp.Sym("recv"), sexp.Sym("h"), sexp.Sym("*Handler")),
		sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("w"), sexp.Str("http.ResponseWriter")), sexp.L(sexp.Sym("r"), sexp.Str("*http.Request"))),
		sexp.L(sexp.Sym("returns")),
		sexp.L(sexp.Sym("uses"), sexp.Sym(idType), sexp.Sym(r.Entity)), // types the body names, for imports
		sexp.L(sexp.Sym("body"), sexp.Str(body.String())))
	return fn, nil
}

// parseID turns the {id} wildcard into a typed value, or a 400.
func parseID(typ string) string {
	switch typ {
	case "string":
		return "id := r.PathValue(\"id\")\n"
	case "uuid.UUID":
		return "id, err := uuid.Parse(r.PathValue(\"id\"))\nif err != nil {\n\thttpError(w, http.StatusBadRequest, err)\n\treturn\n}\n"
	case "int64", "int":
		return fmt.Sprintf("id, err := strconv.Parse%s(r.PathValue(\"id\"), 10, 64)\nif err != nil {\n\thttpError(w, http.StatusBadRequest, err)\n\treturn\n}\n", "Int")
	}
	return fmt.Sprintf("// TODO: parse %s from r.PathValue(\"id\")\nvar id %s\n", typ, typ)
}

// httpHelpers is the small, fixed code every handler uses.
func httpHelpers() string {
	return `// writeJSON sends v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status is already written, so the client sees a truncated
		// body; there is nothing better to do than stop.
		return
	}
}

// readJSON decodes a request body, rejecting unknown fields so a typo in
// a client's payload is an error rather than a silently ignored field.
func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// httpError sends an error as JSON. The message is the error's text, so
// statusFor... should return 500 for anything it does not recognise.
func httpError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}`
}
