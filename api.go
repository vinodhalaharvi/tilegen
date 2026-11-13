package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
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

	holes := Cost{{Dim: "llm-work", Value: 2, Source: "derived", Note: "only validation and error mapping are left open"}}
	RegisterOffer(&Offer{
		Tile:       "http-json",
		Engine:     noEngine,
		Capability: "api",
		Doc:        "net/http and JSON: a Handler and ServeMux over the package's stores",
		Cost: append(append(Cost{}, holes...), CostTerm{Dim: "maintenance", Value: 1},
			CostTerm{Dim: "dependency", Value: 0, Source: "derived", Note: "net/http only; Go 1.22 routes methods and wildcards"}),
		IllegalFor: map[string]string{
			"typed-clients": "there is no schema: every client writes its own requests and types by hand",
			"grpc-native":   "it is not gRPC",
			"streaming":     "this tile generates request and response handlers only; a stream of events needs one of the schema tiles",
		},
		Satisfies: map[string]string{
			"browser-clients": "fetch and JSON, with nothing in between",
		},
		Impl: &APITransport{Emit: emitHTTPJSON},
	})

	schema := Cost{{Dim: "maintenance", Value: 2, Source: "derived", Note: "a .proto and its generated code to keep in step, and field numbers that can never be reused"}}
	RegisterOffer(&Offer{
		Tile:       "connect",
		Engine:     noEngine,
		Capability: "api",
		Doc:        "a .proto and a Connect server: Connect, gRPC and gRPC-Web on net/http",
		Requires:   []string{"buf", "protoc-gen-go", "protoc-gen-connect-go"},
		Cost: append(append(append(Cost{}, holes...), schema...),
			CostTerm{Dim: "dependency", Value: 2, Source: "derived", Note: "connect and protobuf"}),
		IllegalFor: map[string]string{
			"grpc-native": "Connect speaks the gRPC protocol but is not grpc-go: its interceptors, reflection and health services do not plug in",
		},
		Satisfies: map[string]string{
			"typed-clients":   "clients are generated from the .proto, in any language buf has a plugin for",
			"streaming":       "a server-streaming RPC, over the Connect, gRPC and gRPC-Web protocols alike",
			"browser-clients": "the Connect protocol is plain HTTP with JSON or binary bodies, so a browser calls it with no proxy",
		},
		Impl: &APITransport{Emit: emitProtoAPI, Flavor: "connect", Plugin: "protoc-gen-connect-go",
			Imports: map[string]string{"connect": "connectrpc.com/connect"}},
	})

	RegisterOffer(&Offer{
		Tile:       "grpc",
		Engine:     noEngine,
		Capability: "api",
		Doc:        "a .proto and a grpc-go server",
		Requires:   []string{"buf", "protoc-gen-go", "protoc-gen-go-grpc"},
		Cost: append(append(append(Cost{}, holes...), schema...),
			CostTerm{Dim: "dependency", Value: 3, Source: "derived", Note: "grpc-go and protobuf: a large module tree"}),
		IllegalFor: map[string]string{
			"browser-clients": "a browser cannot speak gRPC; it needs gRPC-Web and a proxy in front of the server",
		},
		Satisfies: map[string]string{
			"typed-clients": "clients are generated from the .proto, in any language protoc has a plugin for",
			"streaming":     "a server-streaming RPC over HTTP/2",
			"grpc-native":   "it is grpc-go: interceptors, reflection, health and load balancing work as documented",
		},
		Impl: &APITransport{Emit: emitProtoAPI, Flavor: "grpc", Plugin: "protoc-gen-go-grpc",
			Imports: map[string]string{"codes": "google.golang.org/grpc/codes"}},
	})

	registerAlias("http-json", "http")

	RegisterPackageTile(&PackageTile{
		Form: "api",
		Pass: Select,
		Rule: Rule{Name: "api", Pattern: Pat("(api ?items...)"), Then: selectAPI,
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
	Emit    func(m *Munch, n *Node, ops []apiOp, tr *APITransport) ([]*Node, error)
}

// apiVerbs are the store operations an api can serve.
var apiVerbs = []string{"get", "list", "save", "delete"}

// apiOp is one operation an api serves: (get Order).
type apiOp struct{ Verb, Entity string }

func validateAPI(v *validator, n *Node, types map[string]bool) {
	for _, name := range []string{"Handler", "NewHandler", "APIServer", "NewAPIServer"} {
		if types[name] {
			v.bad(n, "(api ...) generates %s, but the package already declares it (one api or http form per package)", name)
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
			v.shape(it, "(doc ?text)")
		case contains(apiVerbs, it.Head()) || it.Head() == "watch":
			b := v.shape(it, "("+it.Head()+" ?entity)")
			if b == nil {
				continue
			}
			if key := it.Head() + " " + b.Atom("entity"); seen[key] {
				v.bad(it, "duplicate (%s)", key)
			} else {
				seen[key] = true
			}
		default:
			v.bad(it, "api contains (VERB Entity), where VERB is %s, (watch Event), (doc ...) and requirements like (typed-clients), got %s%s",
				strings.Join(apiVerbs, ", "), short(it),
				didYouMean(it.Head(), append(append([]string{"doc", "watch"}, apiVerbs...), capabilities["api"].Requirements...)))
		}
	}
	if len(seen) == 0 {
		v.bad(n, "(api ...) needs at least one operation, such as (get Order)")
	}
}

func selectAPI(m *Munch, b Bindings, n *Node) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	cov := c.Cover[pkg.Name+".API"]
	if cov == nil {
		return nil, fmt.Errorf("no tile was chosen for %s.API", pkg.Name)
	}
	tr, ok := cov.Offer.Impl.(*APITransport)
	if !ok {
		return nil, fmt.Errorf("the tile chosen for %s.API does not implement apis", pkg.Name)
	}
	var ops []apiOp
	for _, it := range n.Args() {
		if it.Head() == "watch" {
			if ev := it.List[1].Atom; pkg.Events[ev] == nil {
				return nil, fmt.Errorf("%s: (watch %s) streams an event, and package %s has no (event %s); add it to the package's (events ...)%s",
					it.Pos, ev, pkg.Name, ev, didYouMean(ev, sortedKeys(pkg.Events)))
			}
			ops = append(ops, apiOp{"watch", it.List[1].Atom})
			continue
		}
		if !contains(apiVerbs, it.Head()) {
			continue
		}
		op := apiOp{it.Head(), it.List[1].Atom}
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
func storeHas(iface *Node, verb string) bool {
	want := strings.ToUpper(verb[:1]) + verb[1:]
	for _, meth := range iface.FindAll("method") {
		if meth.List[1].Atom == want {
			return true
		}
	}
	return false
}

// emitHTTPJSON gives each operation the route a REST reader expects and
// hands the result to the http tile, so (api ...) and (http ...) cannot
// drift apart.
func emitHTTPJSON(m *Munch, n *Node, ops []apiOp, _ *APITransport) ([]*Node, error) {
	h := L(Sym("http"))
	h.Pos = n.Pos
	if doc := n.Find("doc"); doc != nil {
		h.List = append(h.List, doc)
	}
	for _, op := range ops {
		base := "/" + strings.ReplaceAll(snake(plural(op.Entity)), "_", "-")
		method, p := "GET", base
		switch op.Verb {
		case "get":
			p = base + "/{id}"
		case "save":
			method = "POST"
		case "delete":
			method, p = "DELETE", base+"/{id}"
		}
		h.List = append(h.List, L(Sym("route"), Sym(method), Str(p), L(Sym(op.Verb), Sym(op.Entity))))
	}
	return selectHTTP(m, Bindings{}, h)
}

// ---- the schema tiles: a .proto, and a server over it ----

// protoField is one field of an entity on the wire.
type protoField struct {
	Go     string // the domain field: CustomerEmail
	GoType string // its Go type, pointer stripped: time.Time
	Ptr    bool   // the domain field is a pointer: absent is meaningful
	Name   string // on the wire: customer_email
	Type   string // on the wire: string, google.protobuf.Timestamp, Status
	Kind   string // scalar, cast, uuid, time, enum
	Cast   string // for cast: the Go type protoc gives the field (int64 for an int)
	Num    int
}

// protoScalars maps a Go type to the proto type that carries it unchanged.
var protoScalars = map[string]string{
	"string": "string", "bool": "bool", "int32": "int32", "int64": "int64",
	"uint32": "uint32", "uint64": "uint64", "float32": "float", "float64": "double",
}

// protoCasts maps a Go type with no proto twin to the nearest one.
var protoCasts = map[string][2]string{ // Go type -> proto type, generated Go type
	"int": {"int64", "int64"}, "uint": {"uint64", "uint64"},
	"int16": {"int32", "int32"}, "int8": {"int32", "int32"},
	"uint16": {"uint32", "uint32"}, "uint8": {"uint32", "uint32"},
}

// protoMessage is an entity on the wire.
type protoMessage struct {
	Event    bool // an event the api streams, not an entity it stores
	Name     string
	Fields   []protoField
	Reserved []int
	IDType   string // the Go type of its ID field
}

// protoEnum is an enum on the wire. Value 0 is always UNSPECIFIED: proto3
// reads a missing enum as 0, and that must not be a real value.
type protoEnum struct {
	Name     string
	Values   []string // the spec's values
	Nums     map[string]int
	Reserved []int
}

func (e protoEnum) wire(val string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(val) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.ToUpper(snake(e.Name)) + "_" + b.String()
}

// protoGoName is the Go name protoc-gen-go gives a proto name:
// customer_email -> CustomerEmail, id -> Id. It follows the rule in
// google.golang.org/protobuf/internal/strs.GoCamelCase.
func protoGoName(s string) string {
	lower := func(c byte) bool { return 'a' <= c && c <= 'z' }
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' && i == 0:
			b = append(b, 'X')
		case c == '_' && i+1 < len(s) && lower(s[i+1]):
			// dropped: the next letter is capitalised instead
		case '0' <= c && c <= '9':
			b = append(b, c)
		default:
			if lower(c) {
				c -= 'a' - 'A'
			}
			b = append(b, c)
			for ; i+1 < len(s) && lower(s[i+1]); i++ {
				b = append(b, s[i+1])
			}
		}
	}
	return string(b)
}

// protoModel is everything the .proto and the server are written from.
type protoModel struct {
	Pkg      string // orders
	Module   string
	Service  string // OrdersService
	Alias    string // ordersv1, the generated package
	Messages []protoMessage
	Enums    []protoEnum
	Ops      []apiOp
	Time     bool // some field is a timestamp
}

func (pm *protoModel) message(name string) *protoMessage {
	for i := range pm.Messages {
		if pm.Messages[i].Name == name {
			return &pm.Messages[i]
		}
	}
	return nil
}

func (pm *protoModel) genImport() string { return pm.Module + "/gen/" + pm.Pkg + "/v1" }

// buildProtoModel reads the entities an api serves and gives every field
// and enum value its number. Numbers come from tilegen.lock where they are
// pinned, so reordering, adding or removing a field never renumbers the
// others; a removed field's number is reserved, never reused.
func buildProtoModel(c *Ctx, ops []apiOp) (*protoModel, error) {
	pkg := c.Pkg
	pm := &protoModel{Pkg: pkg.Name, Module: c.Module, Alias: pkg.Name + "v1", Ops: ops,
		Service: strings.ToUpper(pkg.Name[:1]) + pkg.Name[1:] + "Service"}
	enums := map[string]bool{}
	for _, op := range ops {
		if pm.message(op.Entity) != nil {
			continue
		}
		st := pkg.Structs[op.Entity]
		if op.Verb == "watch" {
			st = pkg.Events[op.Entity]
		}
		if st == nil {
			return nil, fmt.Errorf("the api serves %s, which is not an entity of package %s", op.Entity, pkg.Name)
		}
		msg := protoMessage{Name: op.Entity, Event: op.Verb == "watch"}
		var names []string
		for _, f := range st.FindAll("field") {
			pf := protoField{Go: f.List[1].Atom, GoType: f.List[2].Atom}
			pf.Name = snake(pf.Go)
			if strings.HasPrefix(pf.GoType, "*") {
				pf.Ptr, pf.GoType = true, pf.GoType[1:]
			}
			switch t := pf.GoType; {
			case protoScalars[t] != "":
				pf.Kind, pf.Type = "scalar", protoScalars[t]
			case protoCasts[t][0] != "":
				pf.Kind, pf.Type, pf.Cast = "cast", protoCasts[t][0], protoCasts[t][1]
			case t == "[]byte" && !pf.Ptr:
				pf.Kind, pf.Type = "scalar", "bytes"
			case t == "uuid.UUID":
				pf.Kind, pf.Type = "uuid", "string"
			case t == "time.Time":
				pf.Kind, pf.Type = "time", "google.protobuf.Timestamp"
				pm.Time = true
			case c.Enums[pkg.Name+"."+t] != nil:
				pf.Kind, pf.Type = "enum", t
				if !enums[t] {
					enums[t] = true
					pm.Enums = append(pm.Enums, protoEnum{Name: t, Values: c.Enums[pkg.Name+"."+t]})
				}
			default:
				return nil, fmt.Errorf("%s: %s.%s has type %s, which the %s tile cannot put on the wire; "+
					"it carries strings, numbers, bools, []byte, uuid.UUID, time.Time, this package's enums, and pointers to those",
					f.Pos, op.Entity, pf.Go, f.List[2].Atom, c.Cover[pkg.Name+".API"].Offer.Tile)
			}
			if pf.Go == "ID" {
				msg.IDType = pf.GoType
			}
			msg.Fields = append(msg.Fields, pf)
			names = append(names, pf.Go)
		}
		nums, reserved := c.pin(pkg.Name+"."+op.Entity, names)
		for i := range msg.Fields {
			msg.Fields[i].Num = nums[msg.Fields[i].Go]
		}
		sort.SliceStable(msg.Fields, func(i, j int) bool { return msg.Fields[i].Num < msg.Fields[j].Num })
		msg.Reserved = reserved
		if needsID(ops, op.Entity) && msg.IDType != "string" && msg.IDType != "uuid.UUID" && protoScalars[msg.IDType] == "" && protoCasts[msg.IDType][0] == "" {
			return nil, fmt.Errorf("the api gets or deletes %s by ID, but its ID has type %q, which cannot be put in a request", op.Entity, msg.IDType)
		}
		pm.Messages = append(pm.Messages, msg)
	}
	for i := range pm.Enums {
		if e := pm.Enums[i]; contains(e.Values, "unspecified") {
			return nil, fmt.Errorf("enum %s has a value named unspecified, which the .proto reserves for 0, the value a missing field reads as; rename it", e.Name)
		}
		pm.Enums[i].Nums, pm.Enums[i].Reserved = c.pin(pkg.Name+"."+pm.Enums[i].Name, pm.Enums[i].Values)
	}
	return pm, nil
}

func needsID(ops []apiOp, entity string) bool {
	for _, op := range ops {
		if op.Entity == entity && (op.Verb == "get" || op.Verb == "delete") {
			return true
		}
	}
	return false
}

// rpcName is the RPC an operation becomes: (list Order) -> ListOrders.
func rpcName(op apiOp) string {
	verb := strings.ToUpper(op.Verb[:1]) + op.Verb[1:]
	if op.Verb == "list" {
		return verb + plural(op.Entity)
	}
	return verb + op.Entity
}

// idProto is the proto type and Go type of an entity's ID in a request.
func idProto(goType string) string {
	switch {
	case protoScalars[goType] != "":
		return protoScalars[goType]
	case protoCasts[goType][0] != "":
		return protoCasts[goType][0]
	}
	return "string" // string and uuid.UUID
}

// protoText renders the .proto in the layout `buf format` produces, so a
// team that formats on save sees no diff.
func protoText(pm *protoModel) string {
	var b strings.Builder
	// The marker trails the first statement instead of leading it:
	// protoc-gen-go copies a .proto's leading comments into the Go it
	// writes, and that Go is not tilegen's.
	fmt.Fprintf(&b, "syntax = \"proto3\"; // %s\n\npackage %s.v1;\n\n", generatedMarker, pm.Pkg)
	if pm.Time {
		b.WriteString("import \"google/protobuf/timestamp.proto\";\n\n")
	}
	fmt.Fprintf(&b, "option go_package = \"%s;%s\";\n", pm.genImport(), pm.Alias)
	reserved := func(nums []int) {
		if len(nums) == 0 {
			return
		}
		var s []string
		for _, n := range nums {
			s = append(s, fmt.Sprint(n))
		}
		fmt.Fprintf(&b, "  reserved %s;\n", strings.Join(s, ", "))
	}
	for _, e := range pm.Enums {
		fmt.Fprintf(&b, "\nenum %s {\n", e.Name)
		reserved(e.Reserved)
		fmt.Fprintf(&b, "  %s = 0;\n", e.wire("unspecified"))
		vals := append([]string{}, e.Values...)
		sort.SliceStable(vals, func(i, j int) bool { return e.Nums[vals[i]] < e.Nums[vals[j]] })
		for _, v := range vals {
			fmt.Fprintf(&b, "  %s = %d;\n", e.wire(v), e.Nums[v])
		}
		b.WriteString("}\n")
	}
	for _, m := range pm.Messages {
		fmt.Fprintf(&b, "\nmessage %s {\n", m.Name)
		reserved(m.Reserved)
		for _, f := range m.Fields {
			opt := ""
			if f.Ptr && f.Kind != "time" { // a message field is already optional
				opt = "optional "
			}
			fmt.Fprintf(&b, "  %s%s %s = %d;\n", opt, f.Type, f.Name, f.Num)
		}
		b.WriteString("}\n")
	}
	for _, op := range pm.Ops {
		m, rpc, lower := pm.message(op.Entity), rpcName(op), snake(op.Entity)
		one := fmt.Sprintf("{\n  %s %s = 1;\n}\n", op.Entity, lower)
		id := fmt.Sprintf("{\n  %s id = 1;\n}\n", idProto(m.IDType))
		req, resp := "{}\n", "{}\n"
		switch op.Verb {
		case "get":
			req, resp = id, one
		case "list":
			resp = fmt.Sprintf("{\n  repeated %s %s = 1;\n}\n", op.Entity, snake(plural(op.Entity)))
		case "save":
			req, resp = one, one
		case "delete":
			req = id
		case "watch":
			resp = one
		}
		fmt.Fprintf(&b, "\nmessage %sRequest %s\nmessage %sResponse %s", rpc, req, rpc, resp)
	}
	fmt.Fprintf(&b, "\n// %s is the %s API.\nservice %s {\n", pm.Service, pm.Pkg, pm.Service)
	for _, op := range pm.Ops {
		rpc := rpcName(op)
		stream := ""
		if op.Verb == "watch" {
			stream = "stream "
		}
		fmt.Fprintf(&b, "  rpc %s(%sRequest) returns (%s%sResponse);\n", rpc, rpc, stream, rpc)
	}
	b.WriteString("}\n")
	return b.String()
}

// emitProtoAPI writes the .proto, asks for the buf plugins, and generates
// the server for either schema tile.
func emitProtoAPI(m *Munch, n *Node, ops []apiOp, tr *APITransport) ([]*Node, error) {
	c, pkg := m.C, m.C.Pkg
	if !c.Cfg.ContextFirst {
		return nil, fmt.Errorf("%s: the %s tile needs stores that take a context.Context, since every RPC carries one; remove (context-first false) from the config",
			n.Pos, c.Cover[pkg.Name+".API"].Offer.Tile)
	}
	pm, err := buildProtoModel(c, ops)
	if err != nil {
		return nil, err
	}
	protoFile := path.Join("proto", pm.Pkg, "v1", pm.Pkg+".proto")
	out := []*Node{
		L(Sym("text/file"), Str(protoFile), L(Sym("mode"), Sym("generated")), Str(protoText(pm))),
		L(Sym("buf/plugin"), Str("protoc-gen-go")),
		L(Sym("buf/plugin"), Str(tr.Plugin)),
	}

	gen, err := goFile(c, path.Join(pkg.Dir, "api_gen.go"), "generated", "", []*Node{L(Sym("go/raw"), Str(serverText(pm, tr, n.Text("doc"))))})
	if err != nil {
		return nil, err
	}
	imports := map[string]string{
		pm.Alias:      pm.genImport(),
		"timestamppb": "google.golang.org/protobuf/types/known/timestamppb",
		"uuid":        c.Requires["uuid"],
	}
	if tr.Flavor == "connect" {
		imports["connect"] = "connectrpc.com/connect"
		imports[pm.Alias+"connect"] = pm.genImport() + "/" + pm.Alias + "connect"
	} else {
		imports["grpc"] = "google.golang.org/grpc"
		imports["codes"] = "google.golang.org/grpc/codes"
		imports["status"] = "google.golang.org/grpc/status"
	}
	for _, q := range sortedKeys(imports) {
		if imports[q] != "" { // goimports drops the ones this server does not use
			gen.List = append(gen.List, L(Sym("import"), Sym(q), Str(imports[q])))
		}
	}
	out = append(out, gen)

	// The two judgment calls, per entity: what makes a value acceptable
	// (only where the api saves one), and which code a store error earns.
	codeType, codeDoc := "connect.Code", "connect.CodeNotFound for ErrNotFound, connect.CodeAlreadyExists for a conflict, connect.CodeInternal for anything else"
	if tr.Flavor == "grpc" {
		codeType, codeDoc = "codes.Code", "codes.NotFound for ErrNotFound, codes.AlreadyExists for a conflict, codes.Internal for anything else"
	}
	file := path.Join(pkg.Dir, "api.go")
	var methods []*Node
	intents := map[string]string{}
	for _, msg := range pm.Messages {
		e, lower := msg.Name, lowerFirstWord(msg.Name)
		if msg.Event {
			methods = append(methods, L(Sym("method"), Sym("allow"+e),
				L(Sym("params"), L(Sym("ctx"), Sym("context.Context")), L(Sym("e"), Sym(e))), L(Sym("returns"), Sym("bool"))))
			intents["allow"+e] = fmt.Sprintf("Report whether the caller behind ctx may be sent this %s. It is asked once per event per watcher, so it must be quick and must not block. Return true to send it to everyone.", e)
			continue
		}
		if serves(ops, "save", e) {
			methods = append(methods, L(Sym("method"), Sym("validate"+e), L(Sym("params"), L(Sym(lower), Sym("*"+e))), L(Sym("returns"), Sym("error"))))
			intents["validate"+e] = fmt.Sprintf("Report what makes this %s unacceptable, before it reaches the store. Return nil when it is fine; the error's text is sent to the client as an invalid-argument error.", e)
		}
		methods = append(methods, L(Sym("method"), Sym("codeFor"+e), L(Sym("params"), L(Sym("err"), Sym("error"))), L(Sym("returns"), Sym(codeType))))
		intents["codeFor"+e] = fmt.Sprintf("Map an error from %sStore to a status code: %s. The error's text is sent to the client, so return the internal code for anything not meant to be seen.", e, codeDoc)
	}
	context := []string{path.Join(pkg.Dir, pkg.Name+"_gen.go"), path.Join(pkg.Dir, "api_gen.go")}
	stubs, tasks := stubsAndTasksAs("s", pkg, "APIServer", "the API", file, methods,
		func(meth *Node) string { return intents[meth.List[1].Atom] }, context, nil)
	// Which errors mean a conflict is a question about the store's driver,
	// so name its error package and the prompt carries the real API.
	for _, t := range tasks {
		id := t.Text("id")
		i := strings.LastIndex(id, ".codeFor")
		if i < 0 {
			continue
		}
		if cov := c.Cover[pkg.Name+"."+id[i+len(".codeFor"):]+"Store"]; cov != nil {
			if be, ok := cov.Offer.Impl.(*Backend); ok && be.ErrorPackage != "" {
				t.List = append(t.List, L(Sym("api-package"), Str(be.ErrorPackage)))
			}
		}
	}
	impl, err := goFile(c, file, "keep", "", stubs)
	if err != nil {
		return nil, err
	}
	return append(append(out, impl), tasks...), nil
}

// storeField is the server's field for an entity's store: categories.
func storeField(entity string) string { return lowerFirstWord(plural(entity)) }

func serves(ops []apiOp, verb, entity string) bool {
	for _, op := range ops {
		if op.Verb == verb && op.Entity == entity {
			return true
		}
	}
	return false
}

// serverText writes the whole server: the type, one method per RPC, and
// the conversions between an entity and its wire form. All of it follows
// from the entities and the operations, so none of it is a hole.
func serverText(pm *protoModel, tr *APITransport, doc string) string {
	connect := tr.Flavor == "connect"
	pb := pm.Alias
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	// What an error becomes, in each flavor.
	invalid := func(err string) string {
		if connect {
			return "connect.NewError(connect.CodeInvalidArgument, " + err + ")"
		}
		return "status.Error(codes.InvalidArgument, " + err + ".Error())"
	}
	fromStore := func(e string) string {
		if connect {
			return "connect.NewError(s.codeFor" + e + "(err), err)"
		}
		return "status.Error(s.codeFor" + e + "(err), err.Error())"
	}

	if doc == "" {
		doc = fmt.Sprintf("APIServer serves the %s API over gRPC.", pm.Pkg)
		if connect {
			doc = fmt.Sprintf("APIServer serves the %s API over Connect, which also answers gRPC and\ngRPC-Web clients.", pm.Pkg)
		}
	}
	w("// %s\ntype APIServer struct {\n", strings.ReplaceAll(doc, "\n", "\n// "))
	if !connect {
		w("\t%s.Unimplemented%sServer\n", pb, pm.Service)
	}
	var params, inits []string
	watches := false
	for _, m := range pm.Messages {
		if m.Event {
			watches = true
			continue
		}
		field := storeField(m.Name)
		w("\t%s %sStore\n", field, m.Name)
		params = append(params, field+" "+m.Name+"Store")
		inits = append(inits, field+": "+field)
	}
	over := "the given stores"
	if watches {
		w("\tbus Bus\n")
		params, inits = append(params, "bus Bus"), append(inits, "bus: bus")
		over = "the given stores and bus"
		if len(params) == 1 {
			over = "the given bus"
		}
	}
	w("}\n\n")
	if watches {
		w("// watchBuffer is how many events a watcher may fall behind before it is\n// disconnected. Publishing never waits for a slow client.\nconst watchBuffer = 64\n\n")
	}
	w("// NewAPIServer returns an APIServer over %s.\nfunc NewAPIServer(%s) *APIServer {\n\treturn &APIServer{%s}\n}\n\n",
		over, strings.Join(params, ", "), strings.Join(inits, ", "))
	if connect {
		w("// Compile-time check that APIServer serves every RPC of %s.\nvar _ %sconnect.%sHandler = (*APIServer)(nil)\n\n", pm.Service, pb, pm.Service)
		w("// Routes returns a mux serving the API. Mount it on any net/http server;\n// serve it over h2c or TLS for gRPC clients to reach it.\n"+
			"func (s *APIServer) Routes() *http.ServeMux {\n\tmux := http.NewServeMux()\n\tmux.Handle(%sconnect.New%sHandler(s))\n\treturn mux\n}\n\n", pb, pm.Service)
	} else {
		w("// Compile-time check that APIServer serves every RPC of %s.\nvar _ %s.%sServer = (*APIServer)(nil)\n\n", pm.Service, pb, pm.Service)
		w("// Register adds the API to a gRPC server.\nfunc (s *APIServer) Register(srv grpc.ServiceRegistrar) {\n\t%s.Register%sServer(srv, s)\n}\n\n", pb, pm.Service)
	}

	for _, op := range pm.Ops {
		m, e, rpc := pm.message(op.Entity), op.Entity, rpcName(op)
		if op.Verb == "watch" {
			sig := fmt.Sprintf("req *%s.%sRequest, stream grpc.ServerStreamingServer[%s.%sResponse]) error {\n\tctx := stream.Context()", pb, rpc, pb, rpc)
			header, lagged := "stream.SendHeader(nil)", `status.Error(codes.ResourceExhausted, "watcher fell too far behind; reconnect")`
			if connect {
				sig = fmt.Sprintf("ctx context.Context, req *connect.Request[%s.%sRequest], stream *connect.ServerStream[%s.%sResponse]) error {", pb, rpc, pb, rpc)
				header, lagged = "stream.Send(nil)", `connect.NewError(connect.CodeResourceExhausted, errors.New("watcher fell too far behind; reconnect"))`
			}
			w("// %[1]s serves %[2]s.%[1]s: every %[3]s the bus delivers from now on,\n// until the client goes away.\nfunc (s *APIServer) %[1]s(%[4]s\n", rpc, pm.Service, e, sig)
			w("\tevents := make(chan %[1]s, watchBuffer)\n\tlagged := make(chan struct{})\n\tvar once sync.Once\n"+
				"\tstop := s.bus.On%[1]s(func(_ context.Context, e %[1]s) error {\n\t\tselect {\n\t\tcase events <- e:\n"+
				"\t\tdefault: // the client is not keeping up: drop it, not the publisher\n\t\t\tonce.Do(func() { close(lagged) })\n\t\t}\n\t\treturn nil\n\t})\n\tdefer stop()\n"+
				"\t// Answer now rather than with the first event: once the client's call\n\t// returns, it is subscribed and will miss nothing.\n"+
				"\tif err := %[2]s; err != nil {\n\t\treturn err\n\t}\n"+
				"\tfor {\n\t\tselect {\n\t\tcase <-ctx.Done():\n\t\t\treturn nil\n\t\tcase <-lagged:\n\t\t\treturn %[3]s\n\t\tcase e := <-events:\n"+
				"\t\t\tif !s.allow%[1]s(ctx, e) {\n\t\t\t\tcontinue\n\t\t\t}\n"+
				"\t\t\tif err := stream.Send(&%[4]s.%[5]sResponse{%[6]s: %[7]sToProto(&e)}); err != nil {\n\t\t\t\treturn err\n\t\t\t}\n\t\t}\n\t}\n}\n\n",
				e, header, lagged, pb, rpc, protoGoName(snake(e)), lowerFirstWord(e))
			continue
		}
		store, lower := "s."+storeField(e), lowerFirstWord(e)
		msg, sig := "req", fmt.Sprintf("req *%s.%sRequest) (*%s.%sResponse, error)", pb, rpc, pb, rpc)
		ret := func(lit string) string { return lit + ", nil" }
		if connect {
			msg = "req.Msg"
			sig = fmt.Sprintf("req *connect.Request[%s.%sRequest]) (*connect.Response[%s.%sResponse], error)", pb, rpc, pb, rpc)
			ret = func(lit string) string { return "connect.NewResponse(" + lit + "), nil" }
		}
		w("// %s serves %s.%s.\nfunc (s *APIServer) %s(ctx context.Context, %s {\n", rpc, pm.Service, rpc, rpc, sig)
		parseID := func() {
			switch t := m.IDType; {
			case t == "uuid.UUID":
				w("\tid, err := uuid.Parse(%s.GetId())\n\tif err != nil {\n\t\treturn nil, %s\n\t}\n", msg, invalid("err"))
			case protoCasts[t][0] != "":
				w("\tid := %s(%s.GetId())\n", t, msg)
			default:
				w("\tid := %s.GetId()\n", msg)
			}
		}
		field := protoGoName(snake(e))
		switch op.Verb {
		case "get":
			parseID()
			w("\tv, err := %s.Get(ctx, id)\n\tif err != nil {\n\t\treturn nil, %s\n\t}\n", store, fromStore(e))
			w("\treturn %s\n", ret(fmt.Sprintf("&%s.%sResponse{%s: %sToProto(v)}", pb, rpc, field, lower)))
		case "list":
			w("\tvs, err := %s.List(ctx)\n\tif err != nil {\n\t\treturn nil, %s\n\t}\n", store, fromStore(e))
			w("\tout := make([]*%s.%s, 0, len(vs))\n\tfor _, v := range vs {\n\t\tout = append(out, %sToProto(v))\n\t}\n", pb, e, lower)
			w("\treturn %s\n", ret(fmt.Sprintf("&%s.%sResponse{%s: out}", pb, rpc, protoGoName(snake(plural(e))))))
		case "save":
			w("\tv, err := %sFromProto(%s.Get%s())\n\tif err != nil {\n\t\treturn nil, %s\n\t}\n", lower, msg, field, invalid("err"))
			w("\tif err := s.validate%s(v); err != nil {\n\t\treturn nil, %s\n\t}\n", e, invalid("err"))
			w("\tif err := %s.Save(ctx, v); err != nil {\n\t\treturn nil, %s\n\t}\n", store, fromStore(e))
			w("\treturn %s\n", ret(fmt.Sprintf("&%s.%sResponse{%s: %sToProto(v)}", pb, rpc, field, lower)))
		case "delete":
			parseID()
			w("\tif err := %s.Delete(ctx, id); err != nil {\n\t\treturn nil, %s\n\t}\n", store, fromStore(e))
			w("\treturn %s\n", ret(fmt.Sprintf("&%s.%sResponse{}", pb, rpc)))
		}
		w("}\n\n")
	}

	for _, m := range pm.Messages {
		e, lower := m.Name, lowerFirstWord(m.Name)
		// Domain to wire: nothing here can fail.
		w("// %sToProto converts a %s to its wire form.\nfunc %sToProto(v *%s) *%s.%s {\n\tif v == nil {\n\t\treturn nil\n\t}\n\tp := &%s.%s{}\n", lower, e, lower, e, pb, e, pb, e)
		for _, f := range m.Fields {
			p, v := "p."+protoGoName(f.Name), "v."+f.Go
			switch {
			case f.Kind == "scalar" && !f.Ptr, f.Kind == "scalar" && f.Ptr:
				w("\t%s = %s\n", p, v)
			case f.Kind == "cast" && !f.Ptr:
				w("\t%s = %s(%s)\n", p, f.Cast, v)
			case f.Kind == "cast":
				w("\tif %s != nil {\n\t\tx := %s(*%s)\n\t\t%s = &x\n\t}\n", v, f.Cast, v, p)
			case f.Kind == "uuid" && !f.Ptr:
				w("\t%s = %s.String()\n", p, v)
			case f.Kind == "uuid":
				w("\tif %s != nil {\n\t\tx := %s.String()\n\t\t%s = &x\n\t}\n", v, v, p)
			case f.Kind == "time" && !f.Ptr:
				w("\t%s = timestamppb.New(%s)\n", p, v)
			case f.Kind == "time":
				w("\tif %s != nil {\n\t\t%s = timestamppb.New(*%s)\n\t}\n", v, p, v)
			case f.Kind == "enum" && !f.Ptr:
				w("\t%s = %sToProto(%s)\n", p, lowerFirstWord(f.GoType), v)
			case f.Kind == "enum":
				w("\tif %s != nil {\n\t\tx := %sToProto(*%s)\n\t\t%s = &x\n\t}\n", v, lowerFirstWord(f.GoType), v, p)
			}
		}
		w("\treturn p\n}\n\n")

		if m.Event {
			continue // an event only ever leaves
		}
		// Wire to domain: an ID that is not a UUID or an enum value this
		// program does not know is the client's mistake, and is reported.
		fallible := false
		for _, f := range m.Fields {
			fallible = fallible || !f.Ptr && (f.Kind == "uuid" || f.Kind == "enum") // a pointer's check declares its own
		}
		w("// %sFromProto converts a wire %s, reporting what cannot be one.\nfunc %sFromProto(p *%s.%s) (*%s, error) {\n\tif p == nil {\n\t\treturn nil, errors.New(\"%s is required\")\n\t}\n\tv := &%s{}\n",
			lower, e, lower, pb, e, e, snake(e), e)
		if fallible {
			w("\tvar err error\n")
		}
		for _, f := range m.Fields {
			g, v := protoGoName(f.Name), "v."+f.Go
			fail := fmt.Sprintf("\t\treturn nil, fmt.Errorf(\"%s: %%w\", err)\n", f.Name)
			switch {
			case f.Kind == "scalar" && !f.Ptr:
				w("\t%s = p.Get%s()\n", v, g)
			case f.Kind == "scalar":
				w("\t%s = p.%s\n", v, g)
			case f.Kind == "cast" && !f.Ptr:
				w("\t%s = %s(p.Get%s())\n", v, f.GoType, g)
			case f.Kind == "cast":
				w("\tif p.%s != nil {\n\t\tx := %s(*p.%s)\n\t\t%s = &x\n\t}\n", g, f.GoType, g, v)
			case f.Kind == "uuid" && !f.Ptr:
				w("\tif %s, err = uuid.Parse(p.Get%s()); err != nil {\n%s\t}\n", v, g, fail)
			case f.Kind == "uuid":
				w("\tif p.%s != nil {\n\t\tx, err := uuid.Parse(*p.%s)\n\t\tif err != nil {\n\t%s\t\t}\n\t\t%s = &x\n\t}\n", g, g, fail, v)
			case f.Kind == "time" && !f.Ptr:
				w("\tif p.%s != nil {\n\t\t%s = p.%s.AsTime()\n\t}\n", g, v, g)
			case f.Kind == "time":
				w("\tif p.%s != nil {\n\t\tx := p.%s.AsTime()\n\t\t%s = &x\n\t}\n", g, g, v)
			case f.Kind == "enum" && !f.Ptr:
				w("\tif %s, err = %sFromProto(p.Get%s()); err != nil {\n%s\t}\n", v, lowerFirstWord(f.GoType), g, fail)
			case f.Kind == "enum":
				w("\tif p.%s != nil {\n\t\tx, err := %sFromProto(*p.%s)\n\t\tif err != nil {\n\t%s\t\t}\n\t\t%s = &x\n\t}\n", g, lowerFirstWord(f.GoType), g, fail, v)
			}
		}
		w("\treturn v, nil\n}\n\n")
	}

	for _, e := range pm.Enums {
		lower := lowerFirstWord(e.Name)
		w("func %sToProto(v %s) %s.%s {\n\tswitch v {\n", lower, e.Name, pb, e.Name)
		for _, val := range e.Values {
			w("\tcase %s%s:\n\t\treturn %s.%s_%s\n", e.Name, goName(val), pb, e.Name, e.wire(val))
		}
		w("\t}\n\treturn %s.%s_%s\n}\n\n", pb, e.Name, e.wire("unspecified"))
		w("func %sFromProto(p %s.%s) (%s, error) {\n\tswitch p {\n", lower, pb, e.Name, e.Name)
		for _, val := range e.Values {
			w("\tcase %s.%s_%s:\n\t\treturn %s%s, nil\n", pb, e.Name, e.wire(val), e.Name, goName(val))
		}
		w("\t}\n\treturn \"\", fmt.Errorf(\"unknown %s %%v\", p)\n}\n\n", e.Name)
	}
	return strings.TrimRight(b.String(), "\n")
}

// bufFiles are buf.yaml and buf.gen.yaml for the plugins a project's api
// tiles asked for. One pair serves every package.
func bufFiles(plugins []string) (yaml, gen string) {
	yaml = "# " + generatedMarker + "\nversion: v2\nmodules:\n  - path: proto\n"
	var b strings.Builder
	b.WriteString("# " + generatedMarker + "\nversion: v2\nplugins:\n")
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
func watchRequirements(api *Node) []string {
	if api.Find("watch") != nil {
		return []string{"streaming"}
	}
	return nil
}
