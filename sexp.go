package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Pos is a 1-based line:col in a source file. Generated nodes inherit the
// position of the node they were lowered from, so errors in late passes
// still point at the line the human wrote.
type Pos struct {
	File      string
	Line, Col int
}

func (p Pos) String() string {
	if p.Line == 0 {
		return "<generated>"
	}
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}

// Node is an S-expression: an atom (symbol or quoted string) or a list.
// Every pass in tilegen consumes and produces Nodes; only the final
// emitter leaves S-expression land.
type Node struct {
	Atom   string
	Quoted bool // atom was written as "..."
	IsList bool
	List   []*Node
	Pos    Pos
}

func Sym(s string) *Node     { return &Node{Atom: s} }
func Str(s string) *Node     { return &Node{Atom: s, Quoted: true} }
func L(items ...*Node) *Node { return &Node{IsList: true, List: items} }
func Syms(ss ...string) []*Node {
	out := make([]*Node, len(ss))
	for i, s := range ss {
		out[i] = Sym(s)
	}
	return out
}

// Head returns the leading symbol of a list, or "".
func (n *Node) Head() string {
	if n != nil && n.IsList && len(n.List) > 0 && !n.List[0].IsList {
		return n.List[0].Atom
	}
	return ""
}

// Args returns everything after the head.
func (n *Node) Args() []*Node {
	if n != nil && n.IsList && len(n.List) > 0 {
		return n.List[1:]
	}
	return nil
}

// Find returns the first child list whose head is h.
func (n *Node) Find(h string) *Node {
	for _, c := range n.Args() {
		if c.Head() == h {
			return c
		}
	}
	return nil
}

// FindAll returns every child list whose head is h.
func (n *Node) FindAll(h string) []*Node {
	var out []*Node
	for _, c := range n.Args() {
		if c.Head() == h {
			out = append(out, c)
		}
	}
	return out
}

// Text returns the atom of the first argument of the child list h,
// e.g. (doc "hi") -> "hi".
func (n *Node) Text(h string) string {
	if c := n.Find(h); c != nil && len(c.Args()) > 0 && !c.Args()[0].IsList {
		return c.Args()[0].Atom
	}
	return ""
}

// Parse reads every top-level form in src.
func Parse(file, src string) ([]*Node, error) {
	p := &reader{file: file, src: src, line: 1, col: 1}
	var out []*Node
	for {
		p.skip()
		if p.eof() {
			return out, nil
		}
		n, err := p.node()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
}

type reader struct {
	file, src    string
	i, line, col int
}

func (p *reader) eof() bool { return p.i >= len(p.src) }
func (p *reader) pos() Pos  { return Pos{p.file, p.line, p.col} }

func (p *reader) adv() byte {
	c := p.src[p.i]
	p.i++
	if c == '\n' {
		p.line, p.col = p.line+1, 1
	} else {
		p.col++
	}
	return c
}

func (p *reader) skip() {
	for !p.eof() {
		switch c := p.src[p.i]; {
		case c == ';': // comment to end of line
			for !p.eof() && p.src[p.i] != '\n' {
				p.adv()
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.adv()
		default:
			return
		}
	}
}

func (p *reader) node() (*Node, error) {
	pos := p.pos()
	switch p.src[p.i] {
	case '(':
		p.adv()
		n := &Node{IsList: true, List: []*Node{}, Pos: pos}
		for {
			p.skip()
			if p.eof() {
				return nil, fmt.Errorf("%s: unclosed '('", pos)
			}
			if p.src[p.i] == ')' {
				p.adv()
				return n, nil
			}
			child, err := p.node()
			if err != nil {
				return nil, err
			}
			n.List = append(n.List, child)
		}
	case ')':
		return nil, fmt.Errorf("%s: unexpected ')'", pos)
	case '"':
		start := p.i
		p.adv()
		for {
			if p.eof() {
				return nil, fmt.Errorf("%s: unterminated string", pos)
			}
			c := p.adv()
			if c == '\\' && !p.eof() {
				p.adv()
				continue
			}
			if c == '"' {
				break
			}
		}
		s, err := strconv.Unquote(p.src[start:p.i])
		if err != nil {
			return nil, fmt.Errorf("%s: bad string literal: %v", pos, err)
		}
		return &Node{Atom: s, Quoted: true, Pos: pos}, nil
	default:
		start := p.i
		for !p.eof() && !strings.ContainsRune(" \t\r\n();\"", rune(p.src[p.i])) {
			p.adv()
		}
		return &Node{Atom: p.src[start:p.i], Pos: pos}, nil
	}
}

// Flat prints n on one line.
func (n *Node) Flat() string {
	if !n.IsList {
		if n.Quoted || n.Atom == "" || strings.ContainsAny(n.Atom, " \t\n();\"") {
			return strconv.Quote(n.Atom)
		}
		return n.Atom
	}
	parts := make([]string, len(n.List))
	for i, c := range n.List {
		parts[i] = c.Flat()
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// String pretty-prints n: short forms stay on one line, long forms put
// leading atoms on the first line and each sub-list on its own line.
func (n *Node) String() string {
	var b strings.Builder
	n.pretty(&b, 0)
	return b.String()
}

func (n *Node) pretty(b *strings.Builder, indent int) {
	flat := n.Flat()
	if !n.IsList || indent+len(flat) <= 80 {
		b.WriteString(flat)
		return
	}
	b.WriteByte('(')
	i := 0
	for ; i < len(n.List) && !n.List[i].IsList; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(n.List[i].Flat())
	}
	for ; i < len(n.List); i++ {
		b.WriteString("\n" + strings.Repeat(" ", indent+2))
		n.List[i].pretty(b, indent+2)
	}
	b.WriteByte(')')
}

// Dump pretty-prints a sequence of top-level forms.
func Dump(nodes []*Node) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(n.String())
		b.WriteString("\n\n")
	}
	return b.String()
}

// inheritPos gives every position-less node in n the position p, so
// errors about generated nodes point back at the source form.
func inheritPos(n *Node, p Pos) {
	if n.Pos.Line == 0 {
		n.Pos = p
	}
	for _, c := range n.List {
		inheritPos(c, p)
	}
}
