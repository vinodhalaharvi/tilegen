package main

import (
	"fmt"
	"sort"
	"strings"
)

// The capabilities tiles compete to offer, and the shared vocabulary a
// node uses to say what it wants. (durable) means the same thing for a
// store and for an event bus.
func init() {
	RegisterCapability(&Capability{
		Name: "store",
		Doc:  "persists an entity's values",
		// lookup-by-field is not written by hand: it is implied by asking
		// for (list-by X) and friends, so a store that can only find things
		// by their key can say so and be ruled out rather than chosen and
		// then worked around.
		// no-broker is the other direction: not a capability a tile must
		// have, but a constraint a heavy one fails. Without it nothing ever
		// rules out the most capable answer, and the most capable answer is
		// usually the most expensive thing to run.
		Requirements: []string{"durable", "cross-process", "lookup-by-field", "no-broker", "outbox", "inbox"},
	})
	RegisterCapability(&Capability{
		Name: "event-bus",
		Doc:  "delivers a package's events to handlers",
		Requirements: []string{"durable", "cross-process", "replay", "ordered",
			"at-least-once", "fan-out", "consumer-groups", "no-broker"},
	})
}

// needsOf reads a package's spec nodes and returns what each one needs.
// A need's ID is stable and is the key in tilegen.lock.
func needsOf(pkg *Node, pkgName string) []*Need {
	var out []*Need
	for _, e := range pkg.FindAll("entity") {
		store := e.Find("store")
		if store == nil {
			continue
		}
		out = append(out, &Need{
			ID:           pkgName + "." + e.List[1].Atom + "Store",
			Capability:   "store",
			Requirements: append(append(append(requirementsOf(store), lookupRequirements(store)...), outboxRequirements(pkg, e.List[1].Atom)...), inboxRequirements(pkg)...),
			Pos:          store.Pos,
			Node:         store,
			Data:         e,
		})
	}
	for _, ev := range pkg.FindAll("events") {
		out = append(out, &Need{
			ID:           pkgName + ".Bus",
			Capability:   "event-bus",
			Requirements: requirementsOf(ev),
			Pos:          ev.Pos,
			Node:         ev,
		})
	}
	for _, a := range pkg.FindAll("auth") {
		out = append(out, &Need{
			ID:           pkgName + ".Auth",
			Capability:   "auth",
			Requirements: requirementsOf(a),
			Pos:          a.Pos,
			Node:         a,
		})
	}
	for _, sec := range pkg.FindAll("secrets") {
		out = append(out, &Need{
			ID:           pkgName + ".Secrets",
			Capability:   "secrets",
			Requirements: requirementsOf(sec),
			Pos:          sec.Pos,
			Node:         sec,
		})
	}
	for _, a := range pkg.FindAll("api") {
		out = append(out, &Need{
			ID:           pkgName + ".API",
			Capability:   "api",
			Requirements: append(requirementsOf(a), watchRequirements(a)...),
			Pos:          a.Pos,
			Node:         a,
		})
	}
	return out
}

// requirementsOf reads the bare requirement forms of a node: (durable).
// lookupRequirements reads the operations a store was asked for and adds
// what they imply. Asking to find an entity by anything other than its key
// is a requirement even though nobody wrote it as one.
func lookupRequirements(store *Node) []string {
	for _, op := range store.Args() {
		if op.IsList && strings.HasSuffix(op.Head(), "-by") {
			return []string{"lookup-by-field"}
		}
	}
	return nil
}

func requirementsOf(n *Node) []string {
	var out []string
	for _, it := range n.Args() {
		if it.IsList && len(it.List) == 1 && contains(knownRequirements(), it.Head()) {
			out = append(out, it.Head())
		}
	}
	return out
}

// aliasOf is the short name a config may use for a tile: postgres-sqlc is
// (storage postgres). Registered by the tile itself.
var tileAliases = map[string]string{}

func aliasOf(tile string) string { return tileAliases[tile] }

func registerAlias(tile, alias string) {
	if prev, dup := tileAliases[tile]; dup && prev != alias {
		panic(fmt.Sprintf("tilegen: tile %s already has alias %s", tile, prev))
	}
	tileAliases[tile] = alias
}

// aliasesFor lists the names a config may use for one capability.
func aliasesFor(capability string) []string {
	var out []string
	for _, o := range offersOf(capability) {
		if a := aliasOf(o.Tile); a != "" {
			out = append(out, a)
		} else {
			out = append(out, o.Tile)
		}
	}
	return out
}

// usedBackends are the storage backends among the tiles a project uses.
// Code that needs backend details (reserved packages, imports, topics, the
// generate step) asks for these rather than every offer.
// toolStep is a command a chosen tile needs run after tilegen and before
// the project builds: sqlc generate, buf generate.
type toolStep struct {
	Cmd, Doc, Tile string
	Reads          func(rel string) bool // the files tilegen writes for it
	Writes         []string              // the directories it fills
}

// Target is the make target that runs the step: the tool's name.
func (s toolStep) Target() string { return strings.Fields(s.Cmd)[0] }

// toolSteps lists the steps the chosen tiles need, each command once.
func toolSteps(c *Ctx) []toolStep {
	var out []toolStep
	seen := map[string]int{}
	for _, o := range c.Used {
		var s toolStep
		switch impl := o.Impl.(type) {
		case *Backend:
			if impl.Generate == "" {
				continue
			}
			s = toolStep{Cmd: impl.Generate, Doc: impl.GenerateDoc, Tile: impl.Tile,
				Reads: func(rel string) bool { return strings.HasPrefix(rel, "db/") || rel == "sqlc.yaml" }}
			for _, dir := range impl.Packages {
				s.Writes = append(s.Writes, dir)
			}
			sort.Strings(s.Writes)
		case *APITransport:
			if impl.Plugin == "" {
				continue
			}
			s = toolStep{Cmd: "buf generate", Doc: "writes gen/ from the .proto files", Tile: o.Tile, Writes: []string{"gen"},
				Reads: func(rel string) bool { return strings.HasPrefix(rel, "proto/") || strings.HasPrefix(rel, "buf.") }}
		default:
			continue
		}
		if i, dup := seen[s.Cmd]; dup { // two tiles, one tool: one step that writes for both
			for _, dir := range s.Writes {
				if !contains(out[i].Writes, dir) {
					out[i].Writes = append(out[i].Writes, dir)
				}
			}
			continue
		}
		seen[s.Cmd] = len(out)
		out = append(out, s)
	}
	return out
}

func usedBackends(c *Ctx) []*Backend {
	var out []*Backend
	for _, o := range c.Used {
		if b, ok := o.Impl.(*Backend); ok {
			out = append(out, b)
		}
	}
	return out
}
