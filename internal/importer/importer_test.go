package importer

import (
	"strings"
	"testing"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/tiles"
)

const appMod = `module example.com/app

go 1.22

require (
	github.com/jackc/pgx/v5 v5.7.2
	github.com/nats-io/nats.go v1.55.0
	example.com/unknown/thing v0.3.0
	golang.org/x/text v0.21.0 // indirect
)
`

const otherMod = `module example.com/other

go 1.22

require github.com/jackc/pgx/v5 v5.6.0
`

func modules(ms []ModuleJSON) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Module
	}
	return out
}

// A go.mod says which libraries a team already uses. What tilegen makes
// of that comes from the tiles themselves, not from a list kept here.
func TestScanGoMods(t *testing.T) {
	got := ScanGoMods([]string{appMod, otherMod})
	if len(got.Errors) != 0 {
		t.Fatalf("errors: %v", got.Errors)
	}
	if strings.Join(got.Modules, " ") != "example.com/app example.com/other" {
		t.Errorf("the modules read: %v", got.Modules)
	}
	if strings.Join(modules(got.Known), " ") != "github.com/jackc/pgx/v5 github.com/nats-io/nats.go" {
		t.Errorf("known: %v", modules(got.Known))
	}
	if strings.Join(modules(got.Unknown), " ") != "example.com/unknown/thing" {
		t.Errorf("an indirect requirement is not something the team chose; unknown: %v", modules(got.Unknown))
	}
	for _, m := range got.Known {
		if len(m.Tiles) == 0 {
			t.Errorf("%s is known and no tile is named for it", m.Module)
		}
		for _, tile := range m.Tiles {
			if !tiles.ValidTileName(tile) {
				t.Errorf("%s names %q, which is not a tile", m.Module, tile)
			}
		}
		if m.Module == "github.com/jackc/pgx/v5" && m.In != 2 {
			t.Errorf("pgx is required by both files; counted in %d", m.In)
		}
	}
	running := map[string]RunningJSON{}
	for _, r := range got.Running {
		running[r.Engine] = r
	}
	if r := running["postgres"]; r.In != 2 || len(r.Tiles) == 0 || len(r.Modules) == 0 {
		t.Errorf("postgres is in use in both: %+v", r)
	}
	if _, ok := running["nats"]; !ok {
		t.Errorf("nats is in use; running: %+v", got.Running)
	}
	if got.Running[0].Engine != "postgres" {
		t.Errorf("the engine most in use comes first: %+v", got.Running)
	}
	for _, e := range got.Engines {
		if e == "" || e == compile.NoEngine {
			t.Errorf("%q is not an engine anyone runs", e)
		}
	}

	// What is not a go.mod is said so, by its place in the list, and the rest is still read.
	mixed := ScanGoMods([]string{"this is { not a go.mod", appMod, ""})
	if len(mixed.Errors) != 2 || !strings.HasPrefix(mixed.Errors[0], "file 1 is not a go.mod: ") || !strings.HasPrefix(mixed.Errors[1], "file 3 is not a go.mod: ") {
		t.Errorf("errors: %v", mixed.Errors)
	}
	if len(mixed.Known) != 2 {
		t.Errorf("the good file was not read beside the bad ones: %v", modules(mixed.Known))
	}
	if empty := ScanGoMods(nil); len(empty.Known)+len(empty.Unknown)+len(empty.Running)+len(empty.Errors) != 0 || len(empty.Engines) == 0 {
		t.Errorf("nothing to read: %+v", empty)
	}
}

func TestTileNameFor(t *testing.T) {
	for module, want := range map[string]string{
		"github.com/dgraph-io/badger/v4": "badger",
		"go.mongodb.org/mongo-driver/v2": "mongo-driver",
		"github.com/nats-io/nats.go":     "nats-go",
		"github.com/google/uuid":         "uuid",
		"go.etcd.io/bbolt":               "bbolt",
		"github.com/Shopify/sarama":      "sarama",
		"example.com/x/v":                "v",
		"example.com/x/vault":            "vault",
		"example.com/9lives":             "tile", // a tile name starts with a letter
		"example.com/_":                  "tile",
		"v2":                             "v2",
	} {
		got := tileNameFor(module)
		if got != want {
			t.Errorf("tileNameFor(%q) = %q, want %q", module, got, want)
		}
		if !tiles.ValidTileName(got) {
			t.Errorf("tileNameFor(%q) = %q is not a tile name", module, got)
		}
	}
}

func TestTrimFence(t *testing.T) {
	tile := "(tile kv (offers store))"
	for name, in := range map[string]string{
		"bare":           tile,
		"padded":         "\n  " + tile + "\n",
		"fenced":         "```\n" + tile + "\n```",
		"fenced as lisp": "```lisp\n" + tile + "\n```\n",
		"an open fence":  "```\n" + tile,
	} {
		if got := trimFence(in); got != tile {
			t.Errorf("%s: %q", name, got)
		}
	}
	if trimFence("") != "" || trimFence("```") != "" {
		t.Error("nothing in, nothing out")
	}
}

// A built-in tile, written the way a draft is written, reads back as itself.
func TestDraftFormOfABuiltInTile(t *testing.T) {
	for _, capability := range []string{"store", "event-bus"} {
		for _, o := range compile.OffersOf(capability) {
			form := DraftForm(o)
			if !strings.HasPrefix(form, "(tile "+o.Tile+"\n  (offers "+capability+")") {
				t.Errorf("%s: the form opens %q", o.Tile, strings.SplitN(form, "\n", 3)[0])
			}
			back, err := compile.ParseDrafts(strings.Replace(form, "(tile "+o.Tile, "(tile "+o.Tile+"-copy", 1))
			if err != nil {
				t.Errorf("%s: its own form does not parse as a draft: %v\n%s", o.Tile, err, form)
				continue
			}
			if len(back) != 1 || back[0].Capability != capability || back[0].Doc != o.Doc {
				t.Errorf("%s: read back as %+v", o.Tile, back[0])
			}
		}
	}
}

func TestModelCanBeChosen(t *testing.T) {
	t.Setenv("TILEGEN_MODEL", "")
	if Model() != DefaultModel {
		t.Errorf("the default model: %q", Model())
	}
	t.Setenv("TILEGEN_MODEL", "  some-other-model ")
	if Model() != "some-other-model" {
		t.Errorf("TILEGEN_MODEL: %q", Model())
	}
}
