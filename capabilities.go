package main

import "fmt"

// The capabilities tiles compete to offer, and the shared vocabulary a
// node uses to say what it wants. (durable) means the same thing for a
// store and for an event bus.
func init() {
	RegisterCapability(&Capability{
		Name:         "store",
		Doc:          "persists an entity's values",
		Requirements: []string{"durable"},
	})
	RegisterCapability(&Capability{
		Name:         "event-bus",
		Doc:          "delivers a package's events to handlers",
		Requirements: []string{"durable", "cross-process"},
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
			Requirements: requirementsOf(store),
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
	return out
}

// requirementsOf reads the bare requirement forms of a node: (durable).
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
func usedBackends(c *Ctx) []*Backend {
	var out []*Backend
	for _, o := range c.Used {
		if b, ok := o.Impl.(*Backend); ok {
			out = append(out, b)
		}
	}
	return out
}
