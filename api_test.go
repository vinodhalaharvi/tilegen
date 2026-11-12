package main

import (
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// apiSpec is an order service with an (api ...) block; reqs are the
// requirements written in it, fields extra fields of Order.
func apiSpec(reqs, fields string) string {
	return `(project orders (module example.com/orders) (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package orders
    (enum Status pending paid shipped)
    (entity Order
      (field ID uuid.UUID)
      (field CustomerEmail string)
      (field TotalCents int64)
      (field Status Status)
      (field Note *string)
      (field Quantity int)
      (field PlacedAt time.Time)
      (field ShippedAt *time.Time)
      ` + fields + `
      (store get list save delete))
    (api (get Order) (list Order) (save Order) (delete Order) ` + reqs + `)))`
}

// genAPI generates spec into out and returns the tile its api got.
func genAPI(t *testing.T, out, spec string, reselect bool) string {
	t.Helper()
	sp, _ := writeSpec(t, t.TempDir(), spec, "")
	if err := run(Options{Spec: sp, Out: out, Reselect: reselect}, io.Discard); err != nil {
		t.Fatal(err)
	}
	lock := read(t, out, "tilegen.lock")
	i := strings.Index(lock, "(tile orders.API ")
	if i < 0 {
		t.Fatalf("lock has no api entry:\n%s", lock)
	}
	rest := lock[i+len("(tile orders.API "):]
	return rest[:strings.Index(rest, ")")]
}

func TestAPIIsChosenByWhatClientsNeed(t *testing.T) {
	for reqs, want := range map[string]string{
		"":                                  "http-json", // nothing asked: the cheapest
		"(browser-clients)":                 "http-json",
		"(typed-clients)":                   "connect", // cheaper than grpc, and both are legal
		"(typed-clients) (browser-clients)": "connect",
		"(grpc-native)":                     "grpc",
		"(typed-clients) (grpc-native)":     "grpc",
	} {
		if got := genAPI(t, t.TempDir(), apiSpec(reqs, ""), false); got != want {
			t.Errorf("api %s: chose %s, want %s", reqs, got, want)
		}
	}
	// A browser cannot speak gRPC, so nothing offers both.
	sp, _ := writeSpec(t, t.TempDir(), apiSpec("(grpc-native) (browser-clients)", ""), "")
	err := run(Options{Spec: sp, Out: t.TempDir()}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "a browser cannot speak gRPC") {
		t.Errorf("grpc-native with browser-clients should be refused with the reason, got: %v", err)
	}
}

func TestAPIWithoutASchemaIsTheHTTPTile(t *testing.T) {
	// (api ...) on http-json and the routes written out by hand are the
	// same server, byte for byte.
	a, b := t.TempDir(), t.TempDir()
	genAPI(t, a, apiSpec("", ""), false)
	byHand := strings.Replace(apiSpec("", ""), "(api (get Order) (list Order) (save Order) (delete Order) )",
		`(http (route GET "/orders/{id}" (get Order)) (route GET "/orders" (list Order))
		       (route POST "/orders" (save Order)) (route DELETE "/orders/{id}" (delete Order)))`, 1)
	sp, _ := writeSpec(t, t.TempDir(), byHand, "")
	if err := run(Options{Spec: sp, Out: b}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"orders/http_gen.go", "orders/http.go"} {
		if read(t, a, f) != read(t, b, f) {
			t.Errorf("%s differs between (api ...) and (http ...)", f)
		}
	}
	for _, f := range []string{"buf.yaml", "buf.gen.yaml", "proto/orders/v1/orders.proto"} {
		if _, err := os.Stat(filepath.Join(a, f)); err == nil {
			t.Errorf("http-json should not write %s", f)
		}
	}
}

func TestAPISchemaTiles(t *testing.T) {
	for tile, c := range map[string]struct {
		reqs   string
		plugin string
		server []string
		hole   string
	}{
		"connect": {"(typed-clients)", "protoc-gen-connect-go", []string{
			`"connectrpc.com/connect"`, `"example.com/orders/gen/orders/v1/ordersv1connect"`,
			"var _ ordersv1connect.OrdersServiceHandler = (*APIServer)(nil)",
			"func (s *APIServer) Routes() *http.ServeMux {",
			"func (s *APIServer) GetOrder(ctx context.Context, req *connect.Request[ordersv1.GetOrderRequest]) (*connect.Response[ordersv1.GetOrderResponse], error) {",
			"return nil, connect.NewError(s.codeForOrder(err), err)",
		}, "func (s *APIServer) codeForOrder(err error) connect.Code {"},
		"grpc": {"(grpc-native)", "protoc-gen-go-grpc", []string{
			`"google.golang.org/grpc/codes"`, "ordersv1.UnimplementedOrdersServiceServer",
			"var _ ordersv1.OrdersServiceServer = (*APIServer)(nil)",
			"func (s *APIServer) Register(srv grpc.ServiceRegistrar) {",
			"func (s *APIServer) GetOrder(ctx context.Context, req *ordersv1.GetOrderRequest) (*ordersv1.GetOrderResponse, error) {",
			"return nil, status.Error(s.codeForOrder(err), err.Error())",
		}, "func (s *APIServer) codeForOrder(err error) codes.Code {"},
	} {
		out := t.TempDir()
		if got := genAPI(t, out, apiSpec(c.reqs, ""), false); got != tile {
			t.Fatalf("chose %s, want %s", got, tile)
		}
		proto := read(t, out, "proto/orders/v1/orders.proto")
		for _, want := range []string{
			// The marker trails the first statement: a leading comment
			// would be copied into the Go that buf writes.
			"syntax = \"proto3\"; // Code generated by tilegen. DO NOT EDIT.\n\npackage orders.v1;\n",
			`option go_package = "example.com/orders/gen/orders/v1;ordersv1";`,
			"enum Status {\n  STATUS_UNSPECIFIED = 0;\n  STATUS_PENDING = 1;\n  STATUS_PAID = 2;\n  STATUS_SHIPPED = 3;\n}",
			"  string id = 1;\n  string customer_email = 2;\n  int64 total_cents = 3;\n  Status status = 4;\n  optional string note = 5;\n  int64 quantity = 6;\n" +
				"  google.protobuf.Timestamp placed_at = 7;\n  google.protobuf.Timestamp shipped_at = 8;\n}",
			"message ListOrdersRequest {}", "  repeated Order orders = 1;",
			"  rpc DeleteOrder(DeleteOrderRequest) returns (DeleteOrderResponse);",
		} {
			if !strings.Contains(proto, want) {
				t.Errorf("%s: .proto missing %q:\n%s", tile, want, proto)
			}
		}
		if gen := read(t, out, "buf.gen.yaml"); !strings.Contains(gen, "local: protoc-gen-go\n") || !strings.Contains(gen, "local: "+c.plugin+"\n") {
			t.Errorf("%s: buf.gen.yaml should run protoc-gen-go and %s:\n%s", tile, c.plugin, gen)
		}
		server := read(t, out, "orders/api_gen.go")
		for _, want := range append(c.server,
			"p.Id = v.ID.String()", "p.Quantity = int64(v.Quantity)", "p.Note = v.Note",
			"if v.ID, err = uuid.Parse(p.GetId()); err != nil {", "v.Quantity = int(p.GetQuantity())",
			"case ordersv1.Status_STATUS_PAID:\n\t\treturn StatusPaid, nil",
			"if err := s.validateOrder(v); err != nil {") {
			if !strings.Contains(server, want) {
				t.Errorf("%s: server missing %q", tile, want)
			}
		}
		holes := read(t, out, "orders/api.go")
		if !strings.Contains(holes, c.hole) || !strings.Contains(holes, "func (s *APIServer) validateOrder(order *Order) error {") {
			t.Errorf("%s: holes file is wrong:\n%s", tile, holes)
		}
		for _, f := range []string{"orders/api_gen.go", "orders/api.go"} {
			if _, err := parser.ParseFile(token.NewFileSet(), f, read(t, out, f), 0); err != nil {
				t.Errorf("%s: %s does not parse: %v", tile, f, err)
			}
		}
		tasks := read(t, out, "tilegen.tasks.json")
		if !strings.Contains(tasks, "orders.APIServer.codeForOrder") || !strings.Contains(tasks, "orders.APIServer.validateOrder") {
			t.Errorf("%s: the two judgment calls should be tasks", tile)
		}
		// A second run changes nothing, and buf agrees with the layout.
		before := read(t, out, "proto/orders/v1/orders.proto") + read(t, out, "tilegen.lock") + server
		genAPI(t, out, apiSpec(c.reqs, ""), false)
		if before != read(t, out, "proto/orders/v1/orders.proto")+read(t, out, "tilegen.lock")+read(t, out, "orders/api_gen.go") {
			t.Errorf("%s: generating twice is not stable", tile)
		}
		if buf, err := exec.LookPath("buf"); err == nil {
			for _, args := range [][]string{{"lint"}, {"format", "-d", "--exit-code"}} {
				cmd := exec.Command(buf, args...)
				cmd.Dir = out
				if o, err := cmd.CombinedOutput(); err != nil {
					t.Errorf("%s: buf %s: %v\n%s", tile, strings.Join(args, " "), err, o)
				}
			}
		}
	}
}

func TestAPIOnlyServesWhatWasAsked(t *testing.T) {
	out := t.TempDir()
	spec := strings.Replace(apiSpec("(typed-clients)", ""), "(api (get Order) (list Order) (save Order) (delete Order)", "(api (get Order)", 1)
	genAPI(t, out, spec, false)
	proto := read(t, out, "proto/orders/v1/orders.proto")
	if strings.Contains(proto, "SaveOrder") || strings.Contains(proto, "ListOrders") || !strings.Contains(proto, "rpc GetOrder(") {
		t.Errorf("only GetOrder was asked for:\n%s", proto)
	}
	// Nothing is saved, so nothing needs validating.
	if strings.Contains(read(t, out, "orders/api.go"), "validateOrder") {
		t.Error("validateOrder is a hole only where the api saves an Order")
	}
}

func TestProtoNumbersArePinned(t *testing.T) {
	out := t.TempDir()
	base := apiSpec("(typed-clients)", "")
	genAPI(t, out, base, false)

	// Remove TotalCents (3), put a new field first, drop an enum value and
	// reorder the rest. Every survivor keeps its number; what was removed
	// is reserved; what is new takes numbers nobody ever had.
	changed := strings.Replace(base, "(field TotalCents int64)", "", 1)
	changed = strings.Replace(changed, "(field ID uuid.UUID)", "(field Currency string) (field ID uuid.UUID)", 1)
	changed = strings.Replace(changed, "(enum Status pending paid shipped)", "(enum Status shipped refunded pending)", 1)
	genAPI(t, out, changed, true) // -reselect chooses tiles again, never numbers
	proto := read(t, out, "proto/orders/v1/orders.proto")
	for _, want := range []string{
		"enum Status {\n  reserved 2;\n  STATUS_UNSPECIFIED = 0;\n  STATUS_PENDING = 1;\n  STATUS_SHIPPED = 3;\n  STATUS_REFUNDED = 4;\n}",
		"message Order {\n  reserved 3;\n  string id = 1;\n  string customer_email = 2;\n  Status status = 4;",
		"  string currency = 9;\n}",
	} {
		if !strings.Contains(proto, want) {
			t.Errorf(".proto missing %q:\n%s", want, proto)
		}
	}
	if lock := read(t, out, "tilegen.lock"); !strings.Contains(lock, "(number Currency 9)\n    (reserved 3))") {
		t.Errorf("the lock should carry the numbers:\n%s", lock)
	}

	// Bring the field back: it is a new field, and 3 stays retired.
	genAPI(t, out, base, false)
	proto = read(t, out, "proto/orders/v1/orders.proto")
	for _, want := range []string{"  reserved 3, 9;\n", "  int64 total_cents = 10;\n", "  reserved 2, 4;\n", "  STATUS_PAID = 5;\n"} {
		if !strings.Contains(proto, want) {
			t.Errorf(".proto missing %q:\n%s", want, proto)
		}
	}
}

func TestLockRefusesBadProtoPins(t *testing.T) {
	for lock, want := range map[string]string{
		`(lock (proto orders.Order (number ID 1) (number Email 1)))`: "number 1 is given to both ID and Email",
		`(lock (proto orders.Order (number ID 1) (reserved 1)))`:     "number 1 is given to both ID and reserved",
		`(lock (proto orders.Order (number ID 0)))`:                  "0 is not a field number",
		`(lock (proto orders.Order (number ID one)))`:                "expected a number, got one",
		`(lock (proto orders.Order (field ID 1)))`:                   "expected (number FIELD N) or (reserved N...)",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, lockFile), []byte(lock), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadLock(dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", lock, want, err)
		}
	}
}

func TestAPIErrors(t *testing.T) {
	gen := func(spec string) error {
		sp, _ := writeSpec(t, t.TempDir(), spec, "")
		return run(Options{Spec: sp, Out: t.TempDir()}, io.Discard)
	}
	swap := func(reqs, old, new string) string { return strings.Replace(apiSpec(reqs, ""), old, new, 1) }
	for name, c := range map[string]struct{ spec, want string }{
		"unknown verb":      {swap("", "(get Order)", "(fetch Order)"), "api contains (VERB Entity)"},
		"misspelt need":     {swap("", "(get Order)", "(get Order) (typed-client)"), "typed-clients"},
		"no operations":     {swap("", "(get Order) (list Order) (save Order) (delete Order)", ""), "needs at least one operation"},
		"twice":             {swap("", "(get Order)", "(get Order) (get Order)"), "duplicate (get Order)"},
		"no such entity":    {swap("", "(get Order)", "(get Invoice)"), "Invoice, which has no store in this package"},
		"store cannot":      {swap("(typed-clients)", "(store get list save delete)", "(store get list save)"), "needs OrderStore to delete; add delete to its (store ...)"},
		"with http":         {swap("", "(api ", `(http (route GET "/orders" (list Order))) (api `), "one api or http form per package"},
		"unsupported type":  {apiSpec("(typed-clients)", "(field Tags []string)"), "Order.Tags has type []string, which the connect tile cannot put on the wire"},
		"unspecified value": {swap("(typed-clients)", "(enum Status pending", "(enum Status unspecified pending"), "has a value named unspecified"},
	} {
		if err := gen(c.spec); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error containing %q, got: %v", name, c.want, err)
		}
	}
	// The same field is fine where nothing has to put it on a wire with a schema.
	if err := gen(apiSpec("", "(field Tags []string)")); err != nil {
		t.Errorf("http-json carries any JSON-able type: %v", err)
	}
}

func TestSwitchingAPITileSweepsTheSchema(t *testing.T) {
	out := t.TempDir()
	genAPI(t, out, apiSpec("(typed-clients)", ""), false)
	if got := genAPI(t, out, apiSpec("(browser-clients)", ""), true); got != "http-json" {
		t.Fatalf("chose %s", got)
	}
	for _, f := range []string{"proto/orders/v1/orders.proto", "orders/api_gen.go", "buf.yaml", "buf.gen.yaml"} {
		if _, err := os.Stat(filepath.Join(out, f)); err == nil {
			t.Errorf("%s should be swept once nothing produces it", f)
		}
	}
	if lock := read(t, out, "tilegen.lock"); strings.Contains(lock, "(proto ") {
		t.Errorf("no schema, so no numbers to pin:\n%s", lock)
	}
}

func TestProtoGoName(t *testing.T) {
	// What protoc-gen-go names the field, so the server can refer to it.
	for in, want := range map[string]string{
		"id": "Id", "customer_email": "CustomerEmail", "total_cents": "TotalCents", "url2": "Url2",
		"address_line_1": "AddressLine_1", "html5_url": "Html5Url", "orders": "Orders", "_x": "XX",
	} {
		if got := protoGoName(in); got != want {
			t.Errorf("protoGoName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigChoosesTheAPITile(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	sp, cp := writeSpec(t, dir, apiSpec("(typed-clients)", ""), "(config (api grpc))")
	if err := run(Options{Spec: sp, Config: cp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, out, "tilegen.lock"), "(tile orders.API grpc)") {
		t.Error("the config should choose grpc over the cheaper connect")
	}
	_, cp = writeSpec(t, dir, apiSpec("(typed-clients)", ""), "(config (api http))")
	if err := run(Options{Spec: sp, Config: cp, Out: t.TempDir()}, io.Discard); err == nil || !strings.Contains(err.Error(), "there is no schema") {
		t.Errorf("a config cannot choose an illegal tile: %v", err)
	}
}

func TestAPIServerForSeveralEntities(t *testing.T) {
	out := t.TempDir()
	spec := `(project shop (module example.com/shop) (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0))
  (package catalog
    (enum Kind physical digital)
    (entity Category (field ID int64) (field Name string) (field Kind *Kind) (field OwnerID *uuid.UUID)
      (store get save))
    (entity Product (field ID string) (field Name string) (store list))
    (api (get Category) (save Category) (list Product) (grpc-native))))`
	sp, _ := writeSpec(t, t.TempDir(), spec, "")
	if err := run(Options{Spec: sp, Out: out}, io.Discard); err != nil {
		t.Fatal(err)
	}
	server := read(t, out, "catalog/api_gen.go")
	for _, want := range []string{
		"categories CategoryStore", "products   ProductStore", // named as a reader would
		"func NewAPIServer(categories CategoryStore, products ProductStore) *APIServer {",
		"id := req.GetId()\n\tv, err := s.categories.Get(ctx, id)", // an int64 ID needs no parsing
		"x, err := kindFromProto(*p.Kind)",
	} {
		if !strings.Contains(server, want) {
			t.Errorf("server missing %q:\n%s", want, server)
		}
	}
	// Only optional fields can fail to convert here, and each checks its
	// own error, so a shared err would be declared and never used.
	if strings.Contains(server, "var err error") {
		t.Error("categoryFromProto declares an err it never uses, which does not compile")
	}
	if !strings.Contains(read(t, out, "proto/catalog/v1/catalog.proto"), "message GetCategoryRequest {\n  int64 id = 1;\n}") {
		t.Error("the request should carry the ID in its own type")
	}
}
