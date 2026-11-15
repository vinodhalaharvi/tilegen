package tiles

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

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
	return strings.ToUpper(compile.Snake(e.Name)) + "_" + b.String()
}

// ProtoGoName is the Go name protoc-gen-go gives a proto name:
// customer_email -> CustomerEmail, id -> Id. It follows the rule in
// google.golang.org/protobuf/internal/strs.GoCamelCase.
func ProtoGoName(s string) string {
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
	Ops      []compile.APIOp
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
func buildProtoModel(c *compile.Ctx, ops []compile.APIOp) (*protoModel, error) {
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
			pf.Name = compile.Snake(pf.Go)
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
		nums, reserved := c.Pin(pkg.Name+"."+op.Entity, names)
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
		if e := pm.Enums[i]; compile.Contains(e.Values, "unspecified") {
			return nil, fmt.Errorf("enum %s has a value named unspecified, which the .proto reserves for 0, the value a missing field reads as; rename it", e.Name)
		}
		pm.Enums[i].Nums, pm.Enums[i].Reserved = c.Pin(pkg.Name+"."+pm.Enums[i].Name, pm.Enums[i].Values)
	}
	return pm, nil
}

func needsID(ops []compile.APIOp, entity string) bool {
	for _, op := range ops {
		if op.Entity == entity && (op.Verb == "get" || op.Verb == "delete") {
			return true
		}
	}
	return false
}

// rpcName is the RPC an operation becomes: (list Order) -> ListOrders.
func rpcName(op compile.APIOp) string {
	verb := strings.ToUpper(op.Verb[:1]) + op.Verb[1:]
	if op.Verb == "list" {
		return verb + compile.Plural(op.Entity)
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
	fmt.Fprintf(&b, "syntax = \"proto3\"; // %s\n\npackage %s.v1;\n\n", compile.GeneratedMarker, pm.Pkg)
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
		m, rpc, lower := pm.message(op.Entity), rpcName(op), compile.Snake(op.Entity)
		one := fmt.Sprintf("{\n  %s %s = 1;\n}\n", op.Entity, lower)
		id := fmt.Sprintf("{\n  %s id = 1;\n}\n", idProto(m.IDType))
		req, resp := "{}\n", "{}\n"
		switch op.Verb {
		case "get":
			req, resp = id, one
		case "list":
			resp = fmt.Sprintf("{\n  repeated %s %s = 1;\n}\n", op.Entity, compile.Snake(compile.Plural(op.Entity)))
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
func emitProtoAPI(m *compile.Munch, n *sexp.Node, ops []compile.APIOp, tr *compile.APITransport) ([]*sexp.Node, error) {
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
	out := []*sexp.Node{
		sexp.L(sexp.Sym("text/file"), sexp.Str(protoFile), sexp.L(sexp.Sym("mode"), sexp.Sym("generated")), sexp.Str(protoText(pm))),
		sexp.L(sexp.Sym("buf/plugin"), sexp.Str("protoc-gen-go")),
		sexp.L(sexp.Sym("buf/plugin"), sexp.Str(tr.Plugin)),
	}

	gen, err := compile.GoFile(c, path.Join(pkg.Dir, "api_gen.go"), "generated", "", []*sexp.Node{sexp.L(sexp.Sym("go/raw"), sexp.Str(serverText(pm, tr, n.Text("doc"))))})
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
	for _, q := range compile.SortedKeys(imports) {
		if imports[q] != "" { // goimports drops the ones this server does not use
			gen.List = append(gen.List, sexp.L(sexp.Sym("import"), sexp.Sym(q), sexp.Str(imports[q])))
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
	var methods []*sexp.Node
	intents := map[string]string{}
	for _, msg := range pm.Messages {
		e, lower := msg.Name, compile.LowerFirstWord(msg.Name)
		if msg.Event {
			methods = append(methods, sexp.L(sexp.Sym("method"), sexp.Sym("allow"+e),
				sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("ctx"), sexp.Sym("context.Context")), sexp.L(sexp.Sym("e"), sexp.Sym(e))), sexp.L(sexp.Sym("returns"), sexp.Sym("bool"))))
			intents["allow"+e] = fmt.Sprintf("Report whether the caller behind ctx may be sent this %s. It is asked once per event per watcher, so it must be quick and must not block. Return true to send it to everyone.", e)
			continue
		}
		if serves(ops, "save", e) {
			methods = append(methods, sexp.L(sexp.Sym("method"), sexp.Sym("validate"+e), sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym(lower), sexp.Sym("*"+e))), sexp.L(sexp.Sym("returns"), sexp.Sym("error"))))
			intents["validate"+e] = fmt.Sprintf("Report what makes this %s unacceptable, before it reaches the store. Return nil when it is fine; the error's text is sent to the client as an invalid-argument error.", e)
		}
		methods = append(methods, sexp.L(sexp.Sym("method"), sexp.Sym("codeFor"+e), sexp.L(sexp.Sym("params"), sexp.L(sexp.Sym("err"), sexp.Sym("error"))), sexp.L(sexp.Sym("returns"), sexp.Sym(codeType))))
		intents["codeFor"+e] = fmt.Sprintf("Map an error from %sStore to a status code: %s. The error's text is sent to the client, so return the internal code for anything not meant to be seen.", e, codeDoc)
	}
	context := []string{path.Join(pkg.Dir, pkg.Name+"_gen.go"), path.Join(pkg.Dir, "api_gen.go")}
	stubs, tasks := compile.StubsAndTasksAs("s", pkg, "APIServer", "the API", file, methods,
		func(meth *sexp.Node) string { return intents[meth.List[1].Atom] }, context, nil)
	// Which errors mean a conflict is a question about the store's driver,
	// so name its error package and the prompt carries the real API.
	for _, t := range tasks {
		id := t.Text("id")
		i := strings.LastIndex(id, ".codeFor")
		if i < 0 {
			continue
		}
		if cov := c.Cover[pkg.Name+"."+id[i+len(".codeFor"):]+"Store"]; cov != nil {
			if be, ok := cov.Offer.Impl.(*compile.Backend); ok && be.ErrorPackage != "" {
				t.List = append(t.List, sexp.L(sexp.Sym("api-package"), sexp.Str(be.ErrorPackage)))
			}
		}
	}
	impl, err := compile.GoFile(c, file, "keep", "", stubs)
	if err != nil {
		return nil, err
	}
	return append(append(out, impl), tasks...), nil
}

// storeField is the server's field for an entity's store: categories.
func storeField(entity string) string { return compile.LowerFirstWord(compile.Plural(entity)) }

func serves(ops []compile.APIOp, verb, entity string) bool {
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
func serverText(pm *protoModel, tr *compile.APITransport, doc string) string {
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
				e, header, lagged, pb, rpc, ProtoGoName(compile.Snake(e)), compile.LowerFirstWord(e))
			continue
		}
		store, lower := "s."+storeField(e), compile.LowerFirstWord(e)
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
		field := ProtoGoName(compile.Snake(e))
		switch op.Verb {
		case "get":
			parseID()
			w("\tv, err := %s.Get(ctx, id)\n\tif err != nil {\n\t\treturn nil, %s\n\t}\n", store, fromStore(e))
			w("\treturn %s\n", ret(fmt.Sprintf("&%s.%sResponse{%s: %sToProto(v)}", pb, rpc, field, lower)))
		case "list":
			w("\tvs, err := %s.List(ctx)\n\tif err != nil {\n\t\treturn nil, %s\n\t}\n", store, fromStore(e))
			w("\tout := make([]*%s.%s, 0, len(vs))\n\tfor _, v := range vs {\n\t\tout = append(out, %sToProto(v))\n\t}\n", pb, e, lower)
			w("\treturn %s\n", ret(fmt.Sprintf("&%s.%sResponse{%s: out}", pb, rpc, ProtoGoName(compile.Snake(compile.Plural(e))))))
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
		e, lower := m.Name, compile.LowerFirstWord(m.Name)
		// Domain to wire: nothing here can fail.
		w("// %sToProto converts a %s to its wire form.\nfunc %sToProto(v *%s) *%s.%s {\n\tif v == nil {\n\t\treturn nil\n\t}\n\tp := &%s.%s{}\n", lower, e, lower, e, pb, e, pb, e)
		for _, f := range m.Fields {
			p, v := "p."+ProtoGoName(f.Name), "v."+f.Go
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
				w("\t%s = %sToProto(%s)\n", p, compile.LowerFirstWord(f.GoType), v)
			case f.Kind == "enum":
				w("\tif %s != nil {\n\t\tx := %sToProto(*%s)\n\t\t%s = &x\n\t}\n", v, compile.LowerFirstWord(f.GoType), v, p)
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
			lower, e, lower, pb, e, e, compile.Snake(e), e)
		if fallible {
			w("\tvar err error\n")
		}
		for _, f := range m.Fields {
			g, v := ProtoGoName(f.Name), "v."+f.Go
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
				w("\tif %s, err = %sFromProto(p.Get%s()); err != nil {\n%s\t}\n", v, compile.LowerFirstWord(f.GoType), g, fail)
			case f.Kind == "enum":
				w("\tif p.%s != nil {\n\t\tx, err := %sFromProto(*p.%s)\n\t\tif err != nil {\n\t%s\t\t}\n\t\t%s = &x\n\t}\n", g, compile.LowerFirstWord(f.GoType), g, fail, v)
			}
		}
		w("\treturn v, nil\n}\n\n")
	}

	for _, e := range pm.Enums {
		lower := compile.LowerFirstWord(e.Name)
		w("func %sToProto(v %s) %s.%s {\n\tswitch v {\n", lower, e.Name, pb, e.Name)
		for _, val := range e.Values {
			w("\tcase %s%s:\n\t\treturn %s.%s_%s\n", e.Name, compile.GoName(val), pb, e.Name, e.wire(val))
		}
		w("\t}\n\treturn %s.%s_%s\n}\n\n", pb, e.Name, e.wire("unspecified"))
		w("func %sFromProto(p %s.%s) (%s, error) {\n\tswitch p {\n", lower, pb, e.Name, e.Name)
		for _, val := range e.Values {
			w("\tcase %s.%s_%s:\n\t\treturn %s%s, nil\n", pb, e.Name, e.wire(val), e.Name, compile.GoName(val))
		}
		w("\t}\n\treturn \"\", fmt.Errorf(\"unknown %s %%v\", p)\n}\n\n", e.Name)
	}
	return strings.TrimRight(b.String(), "\n")
}
