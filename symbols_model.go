package main

import (
	"fmt"
	"sort"
	"strings"
)

// A module's symbols: what it exports, as data.
//
// Everything here comes from the type checker (go/types), never from
// source text. So an interface's methods are its whole method set,
// embedded ones included; a type's methods are the ones callable on it;
// "implements" is types.Implements, and a sentinel error is a variable
// whose type satisfies error. A reader of this data is told what the
// compiler knows, not what a file looks like.

// ModuleSymbols is one module at one version.
type ModuleSymbols struct {
	Module   string           `json:"module"`
	Version  string           `json:"version"`
	Packages []PackageSymbols `json:"packages"`
	// NotRead is the importable packages left out to keep the answer a
	// size someone can use; ask for one by its import path.
	NotRead []string `json:"packages_not_read,omitempty"`
}

// PackageSymbols is one package's exported API.
type PackageSymbols struct {
	Path       string         `json:"path"`
	Name       string         `json:"name"`
	Doc        string         `json:"doc,omitempty"`
	Interfaces []InterfaceSym `json:"interfaces,omitempty"`
	Types      []TypeSym      `json:"types,omitempty"`
	Funcs      []FuncSym      `json:"funcs,omitempty"`
	Values     []ValueSym     `json:"values,omitempty"`
}

// InterfaceSym is an interface and its whole method set.
type InterfaceSym struct {
	Name          string    `json:"name"`
	Doc           string    `json:"doc,omitempty"`
	TypeParams    string    `json:"type_params,omitempty"`
	Methods       []FuncSym `json:"methods,omitempty"`
	ImplementedBy []string  `json:"implemented_by,omitempty"` // this package's types that satisfy it
	Constraint    bool      `json:"constraint,omitempty"`     // a type set, usable only as a constraint
}

// TypeSym is a defined type that is not an interface.
type TypeSym struct {
	Name         string     `json:"name"`
	Doc          string     `json:"doc,omitempty"`
	Kind         string     `json:"kind"` // struct, func, map, slice, array, chan, pointer, or a basic type's name
	TypeParams   string     `json:"type_params,omitempty"`
	Alias        string     `json:"alias_of,omitempty"`
	Underlying   string     `json:"underlying,omitempty"` // when it is not a struct
	Fields       []ParamSym `json:"fields,omitempty"`     // exported fields of a struct
	Methods      []FuncSym  `json:"methods,omitempty"`    // callable on a *T
	Implements   []string   `json:"implements,omitempty"` // this package's interfaces it satisfies
	Constructors []string   `json:"constructors,omitempty"`
	IsError      bool       `json:"is_error,omitempty"`
}

// FuncSym is a function or a method.
type FuncSym struct {
	Name       string     `json:"name"`
	Doc        string     `json:"doc,omitempty"`
	TypeParams string     `json:"type_params,omitempty"`
	Params     []ParamSym `json:"params,omitempty"`
	Results    []string   `json:"results,omitempty"`
	Variadic   bool       `json:"variadic,omitempty"`
}

// ParamSym is a parameter or a field.
type ParamSym struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type"`
}

// ValueSym is a package-level variable or constant.
type ValueSym struct {
	Name    string `json:"name"`
	Doc     string `json:"doc,omitempty"`
	Kind    string `json:"kind"` // var | const
	Type    string `json:"type"`
	Value   string `json:"value,omitempty"`
	IsError bool   `json:"is_error,omitempty"` // a sentinel: compare with errors.Is
}

// only keeps the symbols of the kinds asked for: interface, type, func, value.
func (m *ModuleSymbols) only(kinds []string) {
	if len(kinds) == 0 {
		return
	}
	keep := map[string]bool{}
	for _, k := range kinds {
		keep[k] = true
	}
	for i := range m.Packages {
		p := &m.Packages[i]
		if !keep["interface"] {
			p.Interfaces = nil
		}
		if !keep["type"] {
			p.Types = nil
		}
		if !keep["func"] {
			p.Funcs = nil
		}
		if !keep["value"] {
			p.Values = nil
		}
	}
}

var symbolKinds = []string{"interface", "type", "func", "value"}

// has reports whether the module exports a symbol, written pkg.Name or
// pkg.Type.Method, where pkg is a package's name or its import path.
func (m *ModuleSymbols) has(ref string) bool {
	for _, p := range m.Packages {
		for _, prefix := range []string{p.Path + ".", p.Name + "."} {
			rest, ok := strings.CutPrefix(ref, prefix)
			if !ok || rest == "" {
				continue
			}
			name, member, _ := strings.Cut(rest, ".")
			for _, i := range p.Interfaces {
				if i.Name == name && (member == "" || hasFunc(i.Methods, member)) {
					return true
				}
			}
			for _, t := range p.Types {
				if t.Name == name && (member == "" || hasFunc(t.Methods, member) || hasField(t.Fields, member)) {
					return true
				}
			}
			if member == "" && (hasFunc(p.Funcs, name) || hasValue(p.Values, name)) {
				return true
			}
		}
	}
	return false
}

func hasFunc(fs []FuncSym, name string) bool {
	for _, f := range fs {
		if f.Name == name {
			return true
		}
	}
	return false
}

func hasField(fs []ParamSym, name string) bool {
	for _, f := range fs {
		if f.Name == name {
			return true
		}
	}
	return false
}

func hasValue(vs []ValueSym, name string) bool {
	for _, v := range vs {
		if v.Name == name {
			return true
		}
	}
	return false
}

// signature writes a function the way Go does, without the keyword.
func (f FuncSym) signature() string {
	params := make([]string, len(f.Params))
	for i, p := range f.Params {
		params[i] = strings.TrimSpace(p.Name + " " + p.Type)
	}
	s := f.Name + f.TypeParams + "(" + strings.Join(params, ", ") + ")"
	switch len(f.Results) {
	case 0:
	case 1:
		s += " " + f.Results[0]
	default:
		s += " (" + strings.Join(f.Results, ", ") + ")"
	}
	return s
}

// text is the symbols as a short listing for a prompt: interfaces and
// what satisfies them, then types and how to get one, then sentinel
// errors, within a budget of characters. It says so when it stops early.
func (m *ModuleSymbols) text(budget int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s, as the Go type checker reads it:\n", m.Module, m.Version)
	full := false
	line := func(format string, a ...any) {
		if full {
			return
		}
		s := fmt.Sprintf(format, a...)
		if b.Len()+len(s) > budget {
			full = true
			b.WriteString("... (more symbols than fit here)\n")
			return
		}
		b.WriteString(s)
	}
	for _, p := range m.Packages {
		line("\npackage %s // %s\n", p.Name, p.Path)
		if p.Doc != "" {
			line("  %s\n", p.Doc)
		}
		for _, i := range p.Interfaces {
			if i.Constraint {
				continue
			}
			line("  interface %s%s", i.Name, i.TypeParams)
			if len(i.ImplementedBy) > 0 {
				line(" (implemented by %s)", strings.Join(i.ImplementedBy, ", "))
			}
			line("\n")
			for _, mth := range i.Methods {
				line("    %s\n", mth.signature())
			}
		}
		for _, t := range p.Types {
			line("  type %s%s %s", t.Name, t.TypeParams, t.Kind)
			if len(t.Constructors) > 0 {
				line(", from %s", strings.Join(t.Constructors, ", "))
			}
			if len(t.Implements) > 0 {
				line(", implements %s", strings.Join(t.Implements, ", "))
			}
			if len(t.Methods) > 0 {
				names := make([]string, len(t.Methods))
				for k, mth := range t.Methods {
					names[k] = mth.Name
				}
				line("; methods: %s", strings.Join(names, ", "))
			}
			line("\n")
		}
		for _, v := range p.Values {
			if v.IsError {
				line("  error %s\n", v.Name)
			}
		}
	}
	if len(m.NotRead) > 0 {
		line("\nnot read: %s\n", strings.Join(m.NotRead, ", "))
	}
	return b.String()
}

// checkCited holds a draft to the module it names: every symbol it cites
// is one the module exports.
func checkCited(o *Offer, m *ModuleSymbols) error {
	var missing []string
	for _, ref := range o.Symbols {
		if !m.has(ref) {
			missing = append(missing, ref+didYouMean(ref, m.names()))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s %s does not export %s; cite only symbols it has", m.Module, m.Version, strings.Join(missing, ", "))
}

// names is every reference has would accept for a top-level symbol,
// by package name.
func (m *ModuleSymbols) names() []string {
	var out []string
	for _, p := range m.Packages {
		for _, i := range p.Interfaces {
			out = append(out, p.Name+"."+i.Name)
		}
		for _, t := range p.Types {
			out = append(out, p.Name+"."+t.Name)
		}
		for _, f := range p.Funcs {
			out = append(out, p.Name+"."+f.Name)
		}
		for _, v := range p.Values {
			out = append(out, p.Name+"."+v.Name)
		}
	}
	sort.Strings(out)
	return out
}

// SymbolIndex is a module's symbols by name: enough to see what is there
// and ask for the ones that matter.
type SymbolIndex struct {
	Module   string         `json:"module"`
	Version  string         `json:"version"`
	Packages []PackageIndex `json:"packages"`
	NotRead  []string       `json:"packages_not_read,omitempty"`
}

// PackageIndex is one package's names.
type PackageIndex struct {
	Path       string           `json:"path"`
	Name       string           `json:"name"`
	Doc        string           `json:"doc,omitempty"`
	Interfaces []InterfaceIndex `json:"interfaces,omitempty"`
	Types      []TypeIndex      `json:"types,omitempty"`
	Funcs      []string         `json:"funcs,omitempty"`
	Errors     []string         `json:"errors,omitempty"` // sentinel errors
	Values     int              `json:"other_values,omitempty"`
}

// InterfaceIndex is an interface without its method signatures.
type InterfaceIndex struct {
	Name          string   `json:"name"`
	Methods       int      `json:"methods"`
	ImplementedBy []string `json:"implemented_by,omitempty"`
}

// TypeIndex is a type without its fields and method signatures.
type TypeIndex struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Methods      int      `json:"methods,omitempty"`
	Constructors []string `json:"constructors,omitempty"`
	Implements   []string `json:"implements,omitempty"`
	IsError      bool     `json:"is_error,omitempty"`
}

func (m *ModuleSymbols) index() SymbolIndex {
	out := SymbolIndex{Module: m.Module, Version: m.Version, NotRead: m.NotRead}
	for _, p := range m.Packages {
		pi := PackageIndex{Path: p.Path, Name: p.Name, Doc: p.Doc}
		for _, i := range p.Interfaces {
			pi.Interfaces = append(pi.Interfaces, InterfaceIndex{Name: i.Name, Methods: len(i.Methods), ImplementedBy: i.ImplementedBy})
		}
		for _, t := range p.Types {
			pi.Types = append(pi.Types, TypeIndex{Name: t.Name, Kind: t.Kind, Methods: len(t.Methods), Constructors: t.Constructors, Implements: t.Implements, IsError: t.IsError})
		}
		for _, f := range p.Funcs {
			pi.Funcs = append(pi.Funcs, f.Name)
		}
		for _, v := range p.Values {
			if v.IsError {
				pi.Errors = append(pi.Errors, v.Name)
			} else {
				pi.Values++
			}
		}
		out.Packages = append(out.Packages, pi)
	}
	return out
}

// SymbolDetail is one symbol in full. Exactly one of the four is set.
type SymbolDetail struct {
	Ref       string        `json:"ref"`
	Package   string        `json:"package"`
	Interface *InterfaceSym `json:"interface,omitempty"`
	Type      *TypeSym      `json:"type,omitempty"`
	Func      *FuncSym      `json:"func,omitempty"`
	Value     *ValueSym     `json:"value,omitempty"`
}

// detail finds a symbol written pkg.Name, where pkg is a package's name
// or its import path.
func (m *ModuleSymbols) detail(ref string) (SymbolDetail, bool) {
	for pi := range m.Packages {
		p := &m.Packages[pi]
		for _, prefix := range []string{p.Path + ".", p.Name + "."} {
			name, ok := strings.CutPrefix(ref, prefix)
			if !ok || name == "" || strings.Contains(name, ".") {
				continue
			}
			d := SymbolDetail{Ref: ref, Package: p.Path}
			for i := range p.Interfaces {
				if p.Interfaces[i].Name == name {
					d.Interface = &p.Interfaces[i]
					return d, true
				}
			}
			for i := range p.Types {
				if p.Types[i].Name == name {
					d.Type = &p.Types[i]
					return d, true
				}
			}
			for i := range p.Funcs {
				if p.Funcs[i].Name == name {
					d.Func = &p.Funcs[i]
					return d, true
				}
			}
			for i := range p.Values {
				if p.Values[i].Name == name {
					d.Value = &p.Values[i]
					return d, true
				}
			}
		}
	}
	return SymbolDetail{}, false
}
