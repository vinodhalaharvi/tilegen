//go:build !js

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/vinodhalaharvi/tilegen/internal/mcpserver"
	"os"
	"path/filepath"
	"testing"
)

// golden compares got with a file under testdata, or rewrites the file
// when the tests run with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to write it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s has changed. If that is meant, run go test -update and read the diff.\n got %d bytes, want %d", path, len(got), len(want))
	}
}

// The catalogue, in the order tilegen prints it. Where tiles are
// registered from must not change it.
func TestGoldenCatalogue(t *testing.T) {
	// The listing says whether each tool a tile needs is installed. With
	// no PATH none is, on every machine.
	t.Setenv("PATH", "")
	var text bytes.Buffer
	if err := tilesCmd(nil, &text); err != nil {
		t.Fatal(err)
	}
	golden(t, "catalogue.txt", text.Bytes())
	var asJSON bytes.Buffer
	if err := tilesCmd([]string{"-json"}, &asJSON); err != nil {
		t.Fatal(err)
	}
	golden(t, "catalogue.json", asJSON.Bytes())
}

// Every example's decisions, with the reasons: which tile each need got,
// what else could have, and what was ruled out.
func TestGoldenDecisions(t *testing.T) {
	for _, c := range mcpCases(t) {
		got, err := mcpserver.Explain()(context.Background(), mcpserver.ExplainIn{SpecIn: mcpserver.SpecIn{Spec: c.spec, Config: c.config, Policy: c.policy}})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		golden(t, filepath.Join("decisions", c.name+".json"), append(b, '\n'))
	}
}
