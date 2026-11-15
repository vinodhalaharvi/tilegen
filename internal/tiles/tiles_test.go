package tiles

import (
	"reflect"
	"sort"
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
		"store":     {"badger", "bolt", "memory", "mongo", "postgres-pgx", "postgres-sqlc", "sqlite-sqlc"},
		"event-bus": {"kafka", "local-bus", "nats-core", "nats-jetstream", "rabbitmq", "redis-streams"},
		"auth":      {"api-key", "jwt-local", "oidc", "session-store"},
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
