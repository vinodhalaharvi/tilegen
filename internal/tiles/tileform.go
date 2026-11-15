package tiles

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// A tile is written as a (tile NAME (offers CAPABILITY) ...): what it is, what each
// requirement means for it, what it costs, what the generated store holds
// and a hint for each method. This file is the form and its checks;
// datatile.go turns a checked one into a registered tile.

func init() {
	compile.Elsewhere["tile"] = "a tile is a file of its own, in a folder tilegen is given with -tiles DIR"
}

// Loadable are the capabilities a tile written as a file can offer, in
// order.
func Loadable() []string { return compile.SortedKeys(shapes) }

// Unanswered reports whether a form still carries a TODO: the atom, or a
// string that starts with it. A skeleton is full of them, and none may
// reach a decision.
func Unanswered(n *sexp.Node) bool {
	if !n.IsList {
		return n.Atom == "TODO" || strings.HasPrefix(n.Atom, "TODO:") || strings.HasPrefix(n.Atom, "TODO ")
	}
	for _, k := range n.List {
		if Unanswered(k) {
			return true
		}
	}
	return false
}

// costDims are the dimensions a cost term may name, in the order the
// registry prints them.
var costDims = []string{"llm-work", "maintenance", "dependency", "runtime", "operations", "uncertainty"}

var costSources = []string{"default", "measured", "derived", "user"}

func validateTileSpec(v *compile.Validator, n *sexp.Node) {
	b := v.Shape(n, "(tile ?name ?items...)")
	if b == nil {
		return
	}
	name := b.One("name")
	if name == nil || name.IsList || !ValidTileName(name.Atom) {
		v.Bad(n, "a tile name is lower-case letters, digits and hyphens, like postgres-sqlc, got %s", compile.Short(n))
		return
	}

	// A skeleton's TODOs are questions. Until each is answered there is
	// nothing here to check, and nothing that may be generated from.
	open := false
	for _, it := range b.Rest("items") {
		if Unanswered(it) {
			v.Bad(it, "(tile %s ...): %s is still a TODO; answer it before this tile can be used", name.Atom, compile.Short(it))
			open = true
		}
	}
	if open {
		return
	}

	offers := ""
	seen := map[string]bool{}
	claimed := map[string]string{} // requirement -> the heading it is under
	hinted := map[string]bool{}    // op -> it has a hint
	for _, it := range b.Rest("items") {
		switch it.Head() {
		case "offers":
			if kb := v.Shape(it, "(offers ?name)"); kb != nil {
				offers = kb.Atom("name")
				if shapes[offers] == nil {
					v.Bad(it, "a tile written as a file cannot offer %q%s; it can offer %s", offers,
						compile.DidYouMean(offers, Loadable()), strings.Join(Loadable(), ", "))
				}
			}
		case "doc":
			v.Shape(it, "(doc ?text)")
		case "alias":
			v.Shape(it, "(alias ?name)")
		case "error-package":
			v.Shape(it, "(error-package ?qualifier)")
		case "import":
			v.Shape(it, "(import ?qualifier ?path)")
		case "topics":
			if len(it.Args()) == 0 {
				v.Bad(it, "(topics ...) needs at least one topic")
			}
		case "form":
			v.Shape(it, "(form ?name)")
		case "engine":
			v.Shape(it, "(engine ?name)")
		case "requires":
			for _, r := range it.Args() {
				v.Shape(r, "(tool ?name)")
			}
		case "illegal-when", "satisfies", "unverified":
			if req := validateClaim(v, it); req != "" {
				if was := claimed[req]; was != "" {
					v.Bad(it, "(%s) is already under (%s ...): a requirement is met, ruled out or unchecked, and only one of them", req, was)
				}
				claimed[req] = it.Head()
			}
		case "module":
			v.Shape(it, "(module ?path)")
		case "symbols":
			// What the tile's text relies on in its module, as a draft cites
			// it: checked against the module by tilegen's check_tile tool.
			for _, sym := range it.Args() {
				if sym.IsList || sym.Atom == "" {
					v.Bad(it, "(symbols ...) lists names like pebble.DB.Get, got %s", compile.Short(sym))
				}
			}
		case "cost":
			validateTileCost(v, it)
		case "holds":
			validateHolds(v, it)
		case "new":
			validateNew(v, it)
		case "hint":
			if op := validateHint(v, it); op != "" {
				if hinted[op] {
					v.Bad(it, "duplicate (hint %s ...) in tile %s", op, name.Atom)
				}
				hinted[op] = true
			}
		default:
			known := []string{"offers", "doc", "module", "symbols", "alias", "import", "error-package", "topics", "form", "engine", "requires", "satisfies", "illegal-when", "unverified", "cost", "holds", "new", "hint"}
			v.Bad(it, "a tile contains %s, got %s%s",
				strings.Join(known, ", "), compile.Short(it), compile.DidYouMean(it.Head(), known))
			continue
		}
		if it.Head() != "illegal-when" && it.Head() != "satisfies" && it.Head() != "unverified" && it.Head() != "import" && it.Head() != "hint" && seen[it.Head()] {
			v.Bad(it, "duplicate (%s ...) in tile %s", it.Head(), name.Atom)
		}
		seen[it.Head()] = true
	}
	// A store's short name is what a config says after (storage ...), and
	// the front of every type the tile generates: PostgresOrderStore.
	if offers == "store" {
		short := n.Text("alias")
		if short == "" {
			short = name.Atom
		}
		if !shortName.MatchString(short) {
			v.Bad(n, "(tile %s ...): a store's short name becomes part of a Go type, so it is lower-case letters and digits only; %q is not. Add (alias NAME), as postgres-pgx has (alias pgx)", name.Atom, short)
		}
	}
	if offers == "" {
		v.Bad(n, "(tile %s ...) needs an (offers ...): one of %s", name.Atom, strings.Join(Loadable(), ", "))
	}
	// What the store holds and how it is made: without them there is
	// nothing to generate a store from.
	if !seen["holds"] || !seen["new"] {
		v.Bad(n, "(tile %s ...) needs (holds ...) and (new ...): what the generated store keeps, and the constructor that fills it", name.Atom)
	}
	// A hint for a lookup the tile has ruled out is the tile arguing with
	// itself.
	if claimed["lookup-by-field"] == "illegal-when" {
		for _, op := range fieldOps {
			if hinted[op] {
				v.Bad(n, "(tile %s ...) rules out (lookup-by-field), so no store it serves has a %s method: remove (hint %s ...)", name.Atom, op, op)
			}
		}
	}
	if n.Text("doc") == "" {
		v.Bad(n, "(tile %s ...) needs a (doc \"...\"): it is the one line `tilegen tiles` prints", name.Atom)
	}
	if n.Find("cost") == nil {
		v.Bad(n, "(tile %s ...) needs a (cost ...): a tile that declares no cost can never be compared with one that does", name.Atom)
	}
	// Silence is not a yes. Each thing a spec can ask of this kind of tile
	// is answered: met, ruled out, or said to be unchecked.
	if c := compile.Capabilities[offers]; c != nil && shapes[offers] != nil {
		var open []string
		for _, req := range c.Requirements {
			if claimed[req] == "" {
				open = append(open, "("+req+")")
			}
		}
		if len(open) > 0 {
			v.Bad(n, "(tile %s ...) says nothing about %s: put each under (satisfies ...), (illegal-when ...) or (unverified ...), with the reason",
				name.Atom, strings.Join(open, ", "))
		}
	}
}

// validateClaim checks (satisfies (durable) "reason"), and the same shape
// under illegal-when and unverified, and returns the requirement named.
// The reason is not optional: it is what a user reads in explain.
func validateClaim(v *compile.Validator, it *sexp.Node) string {
	head := it.Head()
	b := v.Shape(it, "("+head+" ?req ?reason)")
	if b == nil {
		return ""
	}
	req := b.One("req")
	if req == nil || !req.IsList || len(req.List) != 1 || req.List[0].IsList {
		v.Bad(it, "(%s ...) takes a bare requirement form like (durable), got %s", head, compile.Short(it))
		return ""
	}
	name := req.List[0].Atom
	if !compile.Contains(compile.KnownRequirements(), name) {
		v.Bad(req, "unknown requirement (%s)%s; the shared vocabulary is %s", name,
			compile.DidYouMean(name, compile.KnownRequirements()), strings.Join(compile.KnownRequirements(), ", "))
		return ""
	}
	if reason := b.One("reason"); reason == nil || strings.TrimSpace(reason.Atom) == "" {
		v.Bad(it, "(%s (%s) ...) needs a reason: it is what explain prints for this tile", head, name)
	}
	return name
}

// validateTileCost checks (cost (llm-work 4 (source derived "...")) ...).
func validateTileCost(v *compile.Validator, it *sexp.Node) {
	if len(it.Args()) == 0 {
		v.Bad(it, "(cost ...) needs at least one dimension: %s", strings.Join(costDims, ", "))
		return
	}
	seen := map[string]bool{}
	for _, term := range it.Args() {
		if !term.IsList || len(term.List) < 2 {
			v.Bad(term, "a cost term is (DIMENSION N [(source ...)]), got %s", compile.Short(term))
			continue
		}
		dim := term.List[0].Atom
		if !compile.Contains(costDims, dim) {
			v.Bad(term, "unknown cost dimension %q%s; tilegen weighs %s", dim,
				compile.DidYouMean(dim, costDims), strings.Join(costDims, ", "))
		}
		if seen[dim] {
			v.Bad(term, "duplicate cost dimension %q", dim)
		}
		seen[dim] = true
		if _, err := strconv.Atoi(term.List[1].Atom); err != nil {
			v.Bad(term, "cost %s must be a whole number, got %q", dim, term.List[1].Atom)
		}
		for _, extra := range term.List[2:] {
			if !extra.IsList && extra.Quoted {
				continue // (DIMENSION N "why"), as a draft writes it
			}
			sb := v.Shape(extra, "(source ?kind ?note...)")
			if sb == nil {
				continue
			}
			if kind := sb.Atom("kind"); !compile.Contains(costSources, kind) {
				v.Bad(extra, "unknown cost source %q%s; tilegen records %s", kind,
					compile.DidYouMean(kind, costSources), strings.Join(costSources, ", "))
			}
		}
	}
}

// shortName is a store's short name: what a config says, and the front of
// a Go type name.
var shortName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// ValidTileName accepts the shape every tile in the registry already has:
// lower-case letters, digits and hyphens, starting with a letter.
func ValidTileName(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return !strings.HasSuffix(s, "-")
}

// importPairs reads every (import qualifier path), in order.
func importPairs(n *sexp.Node) [][2]string {
	var out [][2]string
	for _, it := range n.FindAll("import") {
		if len(it.List) == 3 {
			out = append(out, [2]string{it.List[1].Atom, it.List[2].Atom})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
