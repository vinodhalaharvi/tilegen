package tiles

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
)

func offered(capability string) []string {
	var out []string
	for _, o := range compile.OffersOf(capability) {
		out = append(out, o.Tile)
	}
	sort.Strings(out)
	return out
}

// Importing this package is what puts the built-in tiles in the registry.
func TestTheBuiltInTilesAreRegistered(t *testing.T) {
	if err := compile.CheckRegistry(); err != nil {
		t.Fatalf("the registry is inconsistent: %v", err)
	}
	for capability, want := range map[string][]string{
		"store":     {"badger", "bolt", "dynamodb", "etcd", "leveldb", "memory", "mongo", "pebble", "postgres-pgx", "postgres-sqlc", "redis-kv", "sqlite-sqlc"},
		"event-bus": {"kafka", "local-bus", "nats-core", "nats-jetstream", "rabbitmq", "redis-streams"},
		"auth":      {"api-key", "jwt-local", "oidc", "session-store"},
		"api":       {"connect", "grpc", "http-json"},
		"secrets":   {"env-secrets", "file-secrets", "vault"},
		"transport": {"kafka", "nats-core"},
	} {
		if got := offered(capability); !reflect.DeepEqual(got, want) {
			t.Errorf("%s is offered by %v, want %v", capability, got, want)
		}
	}
}

// Every storage tile says what it is, what it costs and what rules it
// out, in words a person will read in explain.
func TestEveryStoreBackendIsComplete(t *testing.T) {
	if len(compile.Backends) < 7 {
		t.Fatalf("%d store backends registered", len(compile.Backends))
	}
	known := compile.KnownRequirements()
	for name, b := range compile.Backends {
		if b.Name != name || b.Tile == "" || !ValidTileName(b.Tile) {
			t.Errorf("%s: name %q, tile %q", name, b.Name, b.Tile)
		}
		if strings.TrimSpace(b.Doc) == "" || strings.HasPrefix(b.Doc, "TODO") {
			t.Errorf("%s has no doc", name)
		}
		if b.Implement == nil {
			t.Errorf("%s cannot implement a store", name)
		}
		dims := map[string]bool{}
		for _, term := range b.Cost {
			if dims[term.Dim] {
				t.Errorf("%s prices %s twice", name, term.Dim)
			}
			dims[term.Dim] = true
			if term.Value < 0 {
				t.Errorf("%s: %s costs %d", name, term.Dim, term.Value)
			}
		}
		if !dims["llm-work"] || !dims["maintenance"] {
			t.Errorf("%s does not say what it costs to write and to keep: %v", name, b.Cost)
		}
		for _, claims := range []map[string]string{b.IllegalWhen, b.Satisfies} {
			for req, reason := range claims {
				if !compile.Contains(known, req) {
					t.Errorf("%s speaks of (%s), which is not a requirement anyone can state", name, req)
				}
				if strings.TrimSpace(reason) == "" {
					t.Errorf("%s gives no reason for (%s)", name, req)
				}
			}
		}
		for req := range b.IllegalWhen {
			if _, both := b.Satisfies[req]; both {
				t.Errorf("%s both meets and cannot meet (%s)", name, req)
			}
		}
	}
}

// Each kind of tile a tile-spec can describe offers a capability that exists.
func TestTileKinds(t *testing.T) {
	if len(TileKinds) == 0 {
		t.Fatal("no kinds")
	}
	for _, kind := range TileKinds {
		capability := TileKindCapability[kind]
		if capability == "" || compile.Capabilities[capability] == nil {
			t.Errorf("kind %s offers %q, which is not a capability", kind, capability)
		}
		if len(offered(capability)) == 0 {
			t.Errorf("kind %s: nothing offers %s yet", kind, capability)
		}
	}
	for kind := range TileKindCapability {
		if !compile.Contains(TileKinds, kind) {
			t.Errorf("%s has a capability and is not a kind", kind)
		}
	}
}

func TestValidTileName(t *testing.T) {
	for _, ok := range []string{"memory", "postgres-sqlc", "s3", "a", "nats-jetstream"} {
		if !ValidTileName(ok) {
			t.Errorf("%q should be a tile name", ok)
		}
	}
	for _, bad := range []string{"", "Memory", "3d", "-x", "x_y", "x y", "x.y", "é"} {
		if ValidTileName(bad) {
			t.Errorf("%q should not be a tile name", bad)
		}
	}
}

// filePrefix is how a tile's file is named for the capability it offers.
var filePrefix = map[string]string{"store": "store", "event-bus": "bus", "auth": "auth", "api": "api", "secrets": "secrets"}

// Every tile is one file, named for what it offers and what it is, and no
// file holds two. A tile that offers two capabilities registers both in
// its one file.
func TestOneFilePerTile(t *testing.T) {
	fset := token.NewFileSet()
	pkgFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{} // tile -> file
	for _, file := range pkgFiles {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var here []string
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "RegisterOffer" && sel.Sel.Name != "RegisterStoreBackend") {
				return true
			}
			unary, ok := call.Args[0].(*ast.UnaryExpr)
			if !ok {
				t.Errorf("%s: a registration that is not a literal cannot be checked", file)
				return true
			}
			lit, ok := unary.X.(*ast.CompositeLit)
			if !ok {
				t.Errorf("%s: a registration that is not a literal cannot be checked", file)
				return true
			}
			fields := map[string]string{}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if b, ok := kv.Value.(*ast.BasicLit); ok && b.Kind == token.STRING {
					fields[kv.Key.(*ast.Ident).Name], _ = strconv.Unquote(b.Value)
				}
			}
			tile := fields["Tile"]
			if tile == "" {
				tile = fields["Name"]
			}
			capability := fields["Capability"]
			if sel.Sel.Name == "RegisterStoreBackend" {
				capability = "store"
			}
			if !compile.Contains(here, tile) {
				here = append(here, tile)
			}
			if prev, elsewhere := found[tile]; elsewhere && prev != file {
				t.Errorf("%s is registered in %s and in %s", tile, prev, file)
			}
			if _, seen := found[tile]; !seen {
				// The file is named for the first capability the tile registers.
				want := filePrefix[capability] + "_" + strings.ReplaceAll(tile, "-", "_") + ".go"
				if filePrefix[capability] == "" {
					t.Errorf("%s registers %s for %q, which has no file prefix", file, tile, capability)
				} else if file != want {
					t.Errorf("%s is in %s; a tile's file is named for it: %s", tile, file, want)
				}
			}
			found[tile] = file
			return true
		})
		if len(here) > 1 {
			t.Errorf("%s holds %d tiles (%s); one file holds one", file, len(here), strings.Join(here, ", "))
		}
	}
	// And the other way: every tile this package is known to offer was found in a file.
	for capability := range filePrefix {
		for _, tile := range offered(capability) {
			if found[tile] == "" {
				t.Errorf("%s (%s) is registered and no file in this package holds it", tile, capability)
			}
		}
	}
	if len(found) != 28 {
		t.Errorf("%d tiles found in files", len(found))
	}
}
