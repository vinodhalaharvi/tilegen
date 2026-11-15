package main

import (
	"fmt"
	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/symbols"
	"github.com/vinodhalaharvi/tilegen/internal/tiles"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// tilegen import -tiles [DIR]
//
// reads DIR/go.mod and asks one question of each direct requirement: does a
// tile already cover it? The ones that do are the parts of this project
// tilegen can already compile. The ones that do not are the tiles this
// project would need, and it writes a tile-spec skeleton for each.
//
// It reads go.mod and nothing else, unless -symbols asks for more: no
// scanning of implementations, no guessing what a dependency is for. A direct
// requirement is a deliberate statement by the team that they use this
// thing, which is enough to say whether tilegen knows about it.
//
// What it fills in is what go.mod states: the module path, the version,
// the qualifier. What it cannot know is left as a comment, because a tile
// whose (illegal-when ...) and (doc ...) were guessed would be worse than
// no tile: those fields are the whole explanation a user reads.

// tileCoverage is one direct requirement and the tile that covers it, if
// any.
type tileCoverage struct {
	Module  string   // as it appears in go.mod
	Version string   // as it appears in go.mod
	Tiles   []string // every tile that speaks it; empty when none does
}

// coveredModules maps an import path to the tile that speaks it, taken
// from what the tiles themselves declare rather than from a list kept by
// hand: a store backend's Imports, and a bus transport's or auth
// scheme's Import.
func coveredModules() map[string][]string {
	out := map[string][]string{}
	add := func(path, tile string) {
		if path == "" || compile.Contains(out[path], tile) {
			return
		}
		out[path] = append(out[path], tile)
	}
	for _, b := range compile.Backends {
		for _, p := range b.Imports {
			add(p, b.Tile)
		}
	}
	for _, capability := range compile.CapabilityNames() {
		for _, o := range compile.OffersOf(capability) {
			switch impl := o.Impl.(type) {
			case *compile.BusTransport:
				add(impl.Import[1], o.Tile)
			case *compile.AuthScheme:
				add(impl.Import[1], o.Tile)
			case *compile.Transport:
				add(impl.Import[1], o.Tile)
			case *compile.SecretSource:
				add(impl.Import[1], o.Tile)
			case *compile.APITransport:
				for _, p := range impl.Imports {
					add(p, o.Tile)
				}
			}
		}
	}
	// Two tiles may speak the same library: postgres-sqlc and postgres-pgx
	// both import pgx. Sorting keeps the report the same on every run.
	for p := range out {
		sort.Strings(out[p])
	}
	return out
}

// tileFor finds the tile covering a module. A tile declares the package it
// imports, which may be below the module root: go.mongodb.org/mongo-driver
// is required, and go.mongodb.org/mongo-driver/mongo is imported. Matching
// on the module being a prefix of the import path handles that without a
// table of special cases.
func tilesFor(module string, covered map[string][]string) []string {
	if t, ok := covered[module]; ok {
		return t
	}
	var out []string
	for p, names := range covered {
		if strings.HasPrefix(p, module+"/") {
			for _, t := range names {
				if !compile.Contains(out, t) {
					out = append(out, t)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// scanModule reads go.mod and reports every direct requirement, covered or
// not, sorted so the output is stable.
func scanModule(dir string) (modPath string, cov []tileCoverage, err error) {
	modPath, _, requires, err := readGoMod(dir)
	if err != nil {
		return "", nil, err
	}
	covered := coveredModules()
	for _, m := range compile.SortedKeys(requires) {
		cov = append(cov, tileCoverage{Module: m, Version: requires[m], Tiles: tilesFor(m, covered)})
	}
	sort.SliceStable(cov, func(i, j int) bool {
		if (len(cov[i].Tiles) == 0) != (len(cov[j].Tiles) == 0) {
			return len(cov[i].Tiles) == 0 // what is missing comes first
		}
		return cov[i].Module < cov[j].Module
	})
	return modPath, cov, nil
}

// importTiles is `tilegen import -tiles`.
func importTiles(dir, outDir string, force, withSymbols bool, stdout io.Writer) error {
	modPath, cov, err := scanModule(dir)
	if err != nil {
		return err
	}
	if len(cov) == 0 {
		fmt.Fprintf(stdout, "%s requires nothing directly; there is nothing to cover.\n", modPath)
		return nil
	}

	var missing []tileCoverage
	fmt.Fprintf(stdout, "%s\n\n", modPath)
	for _, c := range cov {
		if len(c.Tiles) == 0 {
			missing = append(missing, c)
			fmt.Fprintf(stdout, "  not covered  %-46s %s\n", c.Module, c.Version)
			continue
		}
		fmt.Fprintf(stdout, "  %-12s %-46s %s\n", strings.Join(c.Tiles, ", "), c.Module, c.Version)
	}

	if len(missing) == 0 {
		fmt.Fprintf(stdout, "\nEvery direct requirement is covered by a tile.\n")
		return nil
	}

	full := filepath.Join(dir, outDir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\n%d requirement(s) have no tile. Skeletons:\n\n", len(missing))
	for _, c := range missing {
		name := tileNameFor(c.Module)
		file := filepath.Join(full, name+".sexp")
		if _, err := os.Stat(file); err == nil && !force {
			fmt.Fprintf(stdout, "  kept   %s (exists; -force overwrites)\n", path.Join(outDir, name+".sexp"))
			continue
		}
		text := tileSpecSkeleton(name, c)
		if withSymbols {
			text += symbolsComment(c)
		}
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "  wrote  %s\n", path.Join(outDir, name+".sexp"))
	}
	fmt.Fprintf(stdout, "\nEach skeleton needs four things only you know: what kind of tile it is,\n"+
		"one line of doc, what it cannot do, and what it costs. They are marked\n"+
		"TODO and the validator will not let a tile through without them.\n"+
		"Then: tilegen -out . %s/NAME.sexp\n", outDir)
	return nil
}

// skeletonSymbolBudget is how much of a module's symbols a skeleton carries, in characters.
const skeletonSymbolBudget = 8000

// symbolsComment is what the module exports, as a comment to write the
// TODOs from: facts from the type checker, beside the judgment only a
// person has.
func symbolsComment(c tileCoverage) string {
	if symbols.Read == nil {
		return "\n; The module's symbols cannot be read in this build.\n"
	}
	m, err := symbols.Read(c.Module, c.Version)
	if err != nil {
		return "\n; The module's symbols could not be read: " + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"
	}
	var b strings.Builder
	b.WriteString("\n; What the module exports. `tilegen symbols " + c.Module + "@" + m.Version + "` prints all of it.\n;\n")
	for _, line := range strings.Split(strings.TrimRight(m.Text(skeletonSymbolBudget), "\n"), "\n") {
		b.WriteString(strings.TrimRight("; "+line, " ") + "\n")
	}
	return b.String()
}

// writeWrapped writes a comma-separated list as comment lines no wider
// than a page.
func writeWrapped(b *strings.Builder, first, rest string, items []string) {
	line := first
	for i, it := range items {
		if i < len(items)-1 {
			it += ","
		} else {
			it += "."
		}
		if len(line)+len(it) > 76 && line != first && line != rest {
			b.WriteString(strings.TrimRight(line, " ") + "\n")
			line = rest
		}
		line += it + " "
	}
	b.WriteString(strings.TrimRight(line, " ") + "\n")
}

// kindComparison is the range of llm-work among the tiles tilegen already
// Has of a kind, with the tile at each end: something to set a number
// against.
func kindComparison(kind string) string {
	costs := map[string]compile.Cost{}
	for _, row := range compile.Registry() {
		costs[row.Name] = row.Cost
	}
	lo, hi, loTile, hiTile := 0, 0, "", ""
	for _, o := range compile.OffersOf(tiles.TileKindCapability[kind]) {
		for _, term := range costs[o.Tile] {
			if term.Dim != "llm-work" {
				continue
			}
			if loTile == "" || term.Value < lo || (term.Value == lo && o.Tile < loTile) {
				lo, loTile = term.Value, o.Tile
			}
			if hiTile == "" || term.Value > hi || (term.Value == hi && o.Tile < hiTile) {
				hi, hiTile = term.Value, o.Tile
			}
		}
	}
	if loTile == "" {
		return ""
	}
	return fmt.Sprintf("llm-work runs from %d (%s) to %d (%s)", lo, loTile, hi, hiTile)
}

// tileNameFor turns a module path into a tile name: the last path element,
// with a major-version suffix dropped, lower-cased, and anything outside
// the registry's alphabet replaced.
func tileNameFor(module string) string {
	parts := strings.Split(module, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && len(last) > 1 && last[0] == 'v' && last[1] >= '0' && last[1] <= '9' {
		last = parts[len(parts)-2]
	}
	last = strings.ToLower(last)
	var b strings.Builder
	for _, r := range last {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" || !tiles.ValidTileName(name) {
		return "tile"
	}
	return name
}

// tileSpecSkeleton writes what go.mod states and marks the rest TODO. The
// judgment fields are left as comments rather than plausible defaults: a
// guessed (illegal-when ...) reads exactly like a real one in explain, and
// that is the one place a wrong answer would not look wrong.
func tileSpecSkeleton(name string, c tileCoverage) string {
	qual := strings.ReplaceAll(name, "-", "")
	var b strings.Builder

	fmt.Fprintf(&b, "; Skeleton from go.mod: %s %s.\n", c.Module, c.Version)
	b.WriteString("; The import, version and qualifier are what go.mod states. Everything\n")
	b.WriteString("; marked TODO is judgment, and tilegen will not guess it: a wrong\n")
	b.WriteString("; (illegal-when ...) reads exactly like a right one in explain.\n;\n")
	b.WriteString(";   tilegen -out . SPEC     generate the tile, with two holes\n")
	b.WriteString(";   tilegen fill -out . SPEC\n\n")

	b.WriteString("(project tilegen-tiles\n")
	b.WriteString("  (module github.com/vinodhalaharvi/tilegen)\n")
	b.WriteString("  (go 1.26)\n")
	fmt.Fprintf(&b, "  (require (%s %s %s))\n\n", qual, c.Module, c.Version)
	b.WriteString("  (package tiles\n    (dir \"internal/tiles\")\n\n")

	fmt.Fprintf(&b, "    (tile-spec %s\n", name)
	fmt.Fprintf(&b, "      ; TODO: which kind of tile is this? One of: %s.\n", strings.Join(tiles.TileKinds, ", "))
	b.WriteString("      ; If it is none of them, this module is not a tile: delete this file.\n")
	b.WriteString("      (kind TODO)\n\n")
	b.WriteString("      ; TODO: the one line `tilegen tiles` prints.\n")
	b.WriteString("      (doc \"TODO\")\n\n")
	fmt.Fprintf(&b, "      (import %s %s)\n", qual, importPathFor(c.Module))
	fmt.Fprintf(&b, "      (error-package %s)\n", qual)
	fmt.Fprintf(&b, "      (topics %s)\n\n", name)

	b.WriteString("      ; TODO: which requirements make this the wrong answer? One clause\n")
	b.WriteString("      ; per case, or none if it can meet them all. The reason is what a\n")
	b.WriteString("      ; user reads when this tile is ruled out, so write it for a person.\n")
	writeWrapped(&b, "      ; Requirements: ", "      ;   ", compile.KnownRequirements())
	b.WriteString("      ; (illegal-when (REQUIREMENT) \"...\")\n\n")

	b.WriteString("      ; TODO: what it costs, each a whole number. For comparison:\n")
	for _, kind := range tiles.TileKinds {
		if cmp := kindComparison(kind); cmp != "" {
			fmt.Fprintf(&b, "      ;   %s: %s\n", kind, cmp)
		}
	}
	b.WriteString("      (cost\n")
	b.WriteString("        (llm-work TODO (source user \"TODO: how much must be written by hand?\"))\n")
	b.WriteString("        (maintenance TODO (source user \"TODO: what goes wrong over months?\"))\n")
	b.WriteString("        (dependency TODO (source user \"TODO: what does it pull in, and what must be installed?\"))\n")
	b.WriteString("        (runtime TODO (source user \"TODO: what has to be running?\"))))))\n")
	return b.String()
}

// importPathFor guesses nothing: a module path is a valid import path for
// its root package, and the skeleton says so. If the package the tile
// Needs is below the root, the person writing the tile changes this line.
func importPathFor(module string) string { return module }
