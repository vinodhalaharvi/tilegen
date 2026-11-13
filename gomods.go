package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"golang.org/x/mod/modfile"
)

// What a team already runs is written down in its go.mod files: a direct
// requirement on pgx says there is a Postgres somewhere, one on nats.go
// that there is a NATS. Reading a handful of them answers "what do we
// operate already?", which is what (running ...) in a policy wants to know.
//
// Only what go.mod states is used. A requirement a tile speaks is a tile
// the team already uses; one no tile speaks is listed as unknown, with no
// guess at what it is for.

// GoModsJSON is what several go.mod files say between them.
type GoModsJSON struct {
	Engines []string      `json:"engines"`           // every engine a tile can run on
	Modules []string      `json:"modules"`           // the module each file declares
	Running []RunningJSON `json:"running,omitempty"` // engines in use, most used first
	Known   []ModuleJSON  `json:"known,omitempty"`   // requirements a tile speaks
	Unknown []ModuleJSON  `json:"unknown,omitempty"` // direct requirements no tile speaks
	Errors  []string      `json:"errors,omitempty"`  // files that are not a go.mod
}

// RunningJSON is one engine and the evidence for it.
type RunningJSON struct {
	Engine  string   `json:"engine"`
	Tiles   []string `json:"tiles"`   // the tiles that run on it
	Modules []string `json:"modules"` // the requirements that show it is in use
	In      int      `json:"in"`      // how many of the files require one of them
}

// ModuleJSON is one direct requirement.
type ModuleJSON struct {
	Module  string   `json:"module"`
	Version string   `json:"version"`
	Tiles   []string `json:"tiles,omitempty"`
	In      int      `json:"in"`
}

// scanGoMods reads the text of several go.mod files.
func scanGoMods(texts []string) GoModsJSON {
	out := GoModsJSON{Engines: engines(), Modules: []string{}}
	covered := coveredModules()
	engineOfTile := map[string]string{}
	for _, capability := range capabilityNames() {
		for _, o := range offersOf(capability) {
			if operated(o) { // badger in a go.mod is a library in use, not a server being run
				engineOfTile[o.Tile] = engineOf(o)
			}
		}
	}
	type seen struct {
		version string
		in      int
	}
	mods := map[string]*seen{}
	for i, text := range texts {
		f, err := modfile.Parse("go.mod", []byte(text), nil)
		if err != nil || f.Module == nil {
			out.Errors = append(out.Errors, fmt.Sprintf("file %d is not a go.mod: it has no module line tilegen can read", i+1))
			continue
		}
		out.Modules = append(out.Modules, f.Module.Mod.Path)
		for _, r := range f.Require {
			if r.Indirect {
				continue // not a choice anyone made
			}
			s := mods[r.Mod.Path]
			if s == nil {
				s = &seen{}
				mods[r.Mod.Path] = s
			}
			s.in++
			if s.version < r.Mod.Version {
				s.version = r.Mod.Version
			}
		}
	}
	engines := map[string]*RunningJSON{}
	engineFiles := map[string]int{}
	for _, m := range sortedKeys(mods) {
		s := mods[m]
		tiles := tilesFor(m, covered)
		mj := ModuleJSON{Module: m, Version: s.version, Tiles: tiles, In: s.in}
		if len(tiles) == 0 {
			out.Unknown = append(out.Unknown, mj)
			continue
		}
		out.Known = append(out.Known, mj)
		for _, t := range tiles {
			e := engineOfTile[t]
			if e == "" {
				continue // a library, not something to operate
			}
			r := engines[e]
			if r == nil {
				r = &RunningJSON{Engine: e}
				engines[e] = r
			}
			if !contains(r.Modules, m) {
				r.Modules = append(r.Modules, m)
				if s.in > engineFiles[e] {
					engineFiles[e] = s.in
				}
			}
		}
	}
	for _, e := range sortedKeys(engines) {
		r := engines[e]
		for _, t := range sortedKeys(engineOfTile) { // every tile that would share it, not only the one whose library was seen
			if engineOfTile[t] == e {
				r.Tiles = append(r.Tiles, t)
			}
		}
		r.In = engineFiles[e]
		out.Running = append(out.Running, *r)
	}
	sort.SliceStable(out.Running, func(i, j int) bool { return out.Running[i].In > out.Running[j].In })
	return out
}

// handleGoMods answers POST /gomods: {"files": ["module x ...", ...]}.
func handleGoMods(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("the request is not valid JSON: %w", err))
		return
	}
	if len(req.Files) > maxGoMods {
		writeError(w, http.StatusBadRequest, fmt.Errorf(`send {"files": [...]} with at most %d go.mod files as text`, maxGoMods))
		return
	}
	respondJSON(w, scanGoMods(req.Files))
}

// maxGoMods bounds one request: enough for a team's services, not a crawl.
const maxGoMods = 50
