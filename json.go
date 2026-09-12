package main

import (
	"bytes"
	"encoding/json"
	"io"
)

// -json on the read-only commands, so a program does not have to parse
// tables meant for people. The shapes here are the contract: they name
// what tilegen decided and why, not how it printed it.
//
// Everything is derived from the same structures the text output uses, so
// the two can never disagree.

// writeJSON prints v as indented JSON, with < and & left alone so ids and
// intents stay readable.
func writeJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// ---- explain ----

// ExplainJSON is why every need got the tile it did.
type ExplainJSON struct {
	Policy   PolicyJSON     `json:"policy"`
	Coverage []CoverageJSON `json:"coverage"`
}

type PolicyJSON struct {
	Weights map[string]int    `json:"weights"`
	Prefer  []string          `json:"prefer,omitempty"`
	Margin  int               `json:"margin,omitempty"`
	Avoid   map[string]string `json:"avoid,omitempty"`
}

type CoverageJSON struct {
	Need         string          `json:"need"`
	Capability   string          `json:"capability"`
	Requirements []string        `json:"requirements,omitempty"`
	Chosen       string          `json:"chosen"`
	Score        int             `json:"score"`
	ChosenBy     string          `json:"chosen_by"` // auto | config | lock
	Why          string          `json:"why,omitempty"`
	Candidates   []CandidateJSON `json:"candidates"`
	Illegal      []IllegalJSON   `json:"illegal,omitempty"`
	Children     []CoverageJSON  `json:"children,omitempty"`
	Position     string          `json:"position,omitempty"`
}

type CandidateJSON struct {
	Tile  string      `json:"tile"`
	Score int         `json:"score"`
	Own   int         `json:"own"`
	Terms string      `json:"terms"`
	Chain []ChainJSON `json:"chain,omitempty"`
}

type ChainJSON struct {
	Rule   string `json:"rule"`
	Score  int    `json:"score"`
	Detail string `json:"detail"`
}

type IllegalJSON struct {
	Tile   string `json:"tile"`
	Reason string `json:"reason"`
}

func policyJSON(p *Policy) PolicyJSON {
	out := PolicyJSON{Weights: map[string]int{}, Margin: p.Margin}
	for _, d := range weightOrder {
		out.Weights[d] = p.Weights[d]
	}
	if len(p.Prefer) > 0 {
		out.Prefer = sortedKeys(p.Prefer)
	}
	if len(p.Avoid) > 0 {
		out.Avoid = p.Avoid
	}
	return out
}

func coverageJSON(cov *Covering) CoverageJSON {
	out := CoverageJSON{
		Need: cov.Need.ID, Capability: cov.Need.Capability, Requirements: cov.Need.Requirements,
		Chosen: cov.Offer.Tile, Score: cov.Score, ChosenBy: cov.By, Why: cov.Why,
	}
	if cov.Need.Pos.Line > 0 {
		out.Position = cov.Need.Pos.String()
	}
	for _, cand := range cov.Ranked {
		c := CandidateJSON{Tile: cand.Offer.Tile, Score: cand.Score, Own: cand.Own, Terms: cand.Terms}
		for _, st := range cand.Via {
			c.Chain = append(c.Chain, ChainJSON{Rule: st.Chain.Name, Score: st.Score, Detail: st.Detail})
		}
		out.Candidates = append(out.Candidates, c)
	}
	for _, r := range cov.Illegal {
		out.Illegal = append(out.Illegal, IllegalJSON{Tile: r.Tile, Reason: r.Reason})
	}
	for _, k := range cov.Children {
		out.Children = append(out.Children, coverageJSON(k))
	}
	return out
}

// ---- plan ----

// PlanJSON is what a run makes, in dependency order.
type PlanJSON struct {
	Levels [][]PlanNodeJSON `json:"levels"`
	Cycle  []string         `json:"cycle,omitempty"`
}

type PlanNodeJSON struct {
	ID    string   `json:"id"`
	Kind  string   `json:"kind"` // file | tool | task | need | tile
	Name  string   `json:"name"`
	By    string   `json:"by,omitempty"`
	Note  string   `json:"note,omitempty"`
	Needs []string `json:"needs,omitempty"`
}

func planJSON(g *PlanGraph, levels [][]string, cycleErr error) PlanJSON {
	out := PlanJSON{}
	for _, level := range levels {
		var nodes []PlanNodeJSON
		for _, id := range level {
			n := g.Nodes[id]
			nodes = append(nodes, PlanNodeJSON{ID: n.ID, Kind: n.Kind, Name: n.Name, By: n.By, Note: n.Note, Needs: n.Deps})
		}
		out.Levels = append(out.Levels, nodes)
	}
	if cycleErr != nil {
		placed := map[string]bool{}
		for _, level := range levels {
			for _, id := range level {
				placed[id] = true
			}
		}
		for _, id := range g.Order {
			if !placed[id] {
				out.Cycle = append(out.Cycle, id)
			}
		}
	}
	return out
}

// ---- tiles ----

// TilesJSON is the registry: what exists, and what it costs.
type TilesJSON struct {
	Capabilities []CapabilityJSON `json:"capabilities"`
	Tiles        []TileJSON       `json:"tiles"`
}

type CapabilityJSON struct {
	Name         string   `json:"name"`
	Doc          string   `json:"doc,omitempty"`
	Requirements []string `json:"requirements,omitempty"`
	OfferedBy    []string `json:"offered_by"`
}

type TileJSON struct {
	Name        string            `json:"name"`
	Pass        string            `json:"pass"`
	Doc         string            `json:"doc,omitempty"`
	Produces    string            `json:"produces,omitempty"`
	Form        string            `json:"form,omitempty"`
	Cost        map[string]int    `json:"cost,omitempty"`
	Requires    []string          `json:"requires,omitempty"`
	IllegalWhen map[string]string `json:"illegal_when,omitempty"`
	Covers      []string          `json:"covers,omitempty"`
}

func tilesJSON(rows []TileInfo) TilesJSON {
	out := TilesJSON{}
	for _, name := range capabilityNames() {
		c := capabilities[name]
		cj := CapabilityJSON{Name: c.Name, Doc: c.Doc, Requirements: c.Requirements}
		for _, o := range offersOf(name) {
			cj.OfferedBy = append(cj.OfferedBy, o.Tile)
		}
		out.Capabilities = append(out.Capabilities, cj)
	}
	for _, t := range rows {
		tj := TileJSON{Name: t.Name, Pass: t.Pass, Doc: t.Doc, Produces: t.Produces, Requires: t.Requires, IllegalWhen: t.IllegalWhen}
		if t.Form != domainForm {
			tj.Form = t.Form
		}
		if len(t.Cost) > 0 {
			tj.Cost = map[string]int{}
			for _, c := range t.Cost {
				tj.Cost[c.Dim] = c.Value
			}
		}
		for _, n := range t.Covers {
			tj.Covers = append(tj.Covers, n.Flat())
		}
		out.Tiles = append(out.Tiles, tj)
	}
	return out
}

// ---- check ----

// CheckJSON is what generating would change, and whether that is a failure.
type CheckJSON struct {
	OK        bool       `json:"ok"`
	Missing   []string   `json:"missing,omitempty"`
	Stale     []string   `json:"stale,omitempty"`
	Remove    []string   `json:"remove,omitempty"`
	Stubs     []string   `json:"stubs,omitempty"`
	Drift     []NoteJSON `json:"drift,omitempty"`
	Orphans   []NoteJSON `json:"orphans,omitempty"`
	OpenHoles int        `json:"open_holes"`
	UpToDate  int        `json:"up_to_date"`
	Problems  []string   `json:"problems,omitempty"`
}

type NoteJSON struct {
	File   string `json:"file"`
	Symbol string `json:"symbol,omitempty"`
	Detail string `json:"detail"`
}
