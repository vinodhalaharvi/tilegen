package main

import (
	"fmt"
	"strings"
)

// Forms and chain rules. A backend's output has a form: memory and pgx hand
// back domain values (*Order) directly; sqlc hands back its own row types
// (db.Order), form db-rows. Stores return domain values, so a db-rows
// backend needs a converter. Converters are chain rules: registered tiles
// that turn one form into another at a cost that depends on the entity, so
// selection can count the mapper, not just the backend. This is how
// compilers let a tile's result feed a tile expecting another form.

// domainForm is the form every store must end in.
const domainForm = "domain"

// Chain is a registered converter from one form to another.
type Chain struct {
	Name     string // row-mapper
	From, To string // db-rows -> domain
	Doc      string
	CostRule string // the rule, for `tilegen tiles`
	// Cost prices converting one entity, with a breakdown for explain.
	Cost func(e EntityShape) (Cost, string)
}

// EntityShape is what a chain needs to know about an entity.
type EntityShape struct {
	Name   string
	Fields []FieldShape
}

type FieldShape struct {
	Name, Type            string
	Enum, Nullable, JSONB bool
}

var chains []*Chain

// RegisterChain adds a converter to the registry.
func RegisterChain(ch *Chain) { chains = append(chains, ch) }

// Step is one converter on a chosen path.
type Step struct {
	Chain  *Chain
	Score  int
	Detail string
}

// convert finds the cheapest sequence of converters from one form to
// another for an entity (Dijkstra over forms; the graph is tiny).
func convert(from, to string, e EntityShape, p *Policy) ([]Step, int, bool) {
	if from == to {
		return nil, 0, true
	}
	type best struct {
		score int
		path  []Step
	}
	dist := map[string]best{from: {}}
	done := map[string]bool{}
	for {
		cur, found := "", false
		for f, b := range dist {
			if !done[f] && (!found || b.score < dist[cur].score) {
				cur, found = f, true
			}
		}
		if !found {
			return nil, 0, false
		}
		if cur == to {
			return dist[cur].path, dist[cur].score, true
		}
		done[cur] = true
		for _, ch := range chains {
			if ch.From != cur {
				continue
			}
			cost, detail := ch.Cost(e)
			s, _ := p.score(cost)
			next := dist[cur].score + s
			if b, ok := dist[ch.To]; !ok || next < b.score {
				path := append(append([]Step{}, dist[cur].path...), Step{ch, s, detail})
				dist[ch.To] = best{next, path}
			}
		}
	}
}

// entityShape reads an entity's fields, marking enums (declared anywhere in
// the project), nullable pointers, and types that would land in JSONB.
func entityShape(e *Node, pkg string, enums map[string]bool) EntityShape {
	shape := EntityShape{Name: e.List[1].Atom}
	for _, f := range e.FindAll("field") {
		typ := f.List[2].Atom
		base := strings.TrimPrefix(typ, "*")
		key := base
		if !strings.Contains(key, ".") {
			key = pkg + "." + key
		}
		fs := FieldShape{Name: f.List[1].Atom, Type: typ, Nullable: strings.HasPrefix(typ, "*"), Enum: enums[key]}
		if _, ok := sqlTypes[base]; !ok && !fs.Enum {
			fs.JSONB = true
		}
		shape.Fields = append(shape.Fields, fs)
	}
	return shape
}

func init() {
	RegisterChain(&Chain{
		Name: "row-mapper", From: "db-rows", To: domainForm,
		Doc:      "converts sqlc's row types to domain types, by hand",
		CostRule: "1 per entity, +1 per enum field (ParseX), +1 per nullable field (pgtype), +2 per JSONB field",
		Cost: func(e EntityShape) (Cost, string) {
			units, enums, nulls, jsonb := 1, 0, 0, 0
			for _, f := range e.Fields {
				switch {
				case f.JSONB:
					jsonb++
					units += 2
				default:
					if f.Enum {
						enums++
						units++
					}
					if f.Nullable {
						nulls++
						units++
					}
				}
			}
			parts := []string{"1 entity"}
			for _, p := range []struct {
				n    int
				what string
			}{{enums, "enum"}, {nulls, "nullable"}, {jsonb, "JSONB"}} {
				if p.n > 0 {
					parts = append(parts, fmt.Sprintf("%d %s field%s", p.n, p.what, map[bool]string{true: "s"}[p.n > 1]))
				}
			}
			return Cost{{Dim: "llm-work", Value: units}, {Dim: "maintenance", Value: (units + 1) / 2}}, strings.Join(parts, ", ")
		},
	})
}
