package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// noToolchain makes tilegen finish Go files without the go command even
// where one is installed, so the two paths can be compared.
var noToolchain = os.Getenv("TILEGEN_NO_TOOLCHAIN") != ""

// fixImports does for a generated file what goimports does, without the
// go command: it drops the imports the file does not use, adds the
// standard-library ones it uses and lacks, and groups them, standard
// library first. goimports needs the toolchain to find packages it has to
// guess at; tilegen never guesses, since every other import comes from the
// spec, so in a browser or an image with no Go installed this is enough,
// and its output is the same.
func fixImports(rel string, src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	// The package names the file refers to: x in x.Y where x is not
	// declared in the file.
	used := map[string]map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
				if used[id.Name] == nil {
					used[id.Name] = map[string]bool{}
				}
				used[id.Name][sel.Sel.Name] = true
			}
		}
		return true
	})
	type imp struct{ name, path string }
	var keep []imp
	have := map[string]bool{}
	for _, s := range f.Imports {
		p, _ := strconv.Unquote(s.Path.Value)
		name, local := "", importName(p)
		if s.Name != nil {
			name, local = s.Name.Name, s.Name.Name
		}
		if local == "_" || local == "." || used[local] != nil {
			keep = append(keep, imp{name, p})
			have[local] = true
		}
	}
	for name, syms := range used {
		if have[name] {
			continue
		}
		if p := stdFor(name, syms); p != "" {
			keep = append(keep, imp{"", p})
		}
	}
	// Take the import declarations out and print the rest.
	var decls []ast.Decl
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			continue
		}
		decls = append(decls, d)
	}
	f.Decls, f.Imports = decls, nil
	// Comments that sat inside the import block have nowhere to go.
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return nil, err
	}
	if len(keep) == 0 {
		return format.Source(buf.Bytes())
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].path < keep[j].path })
	var block strings.Builder
	line := func(i imp) string {
		if i.name != "" {
			return fmt.Sprintf("%s %q", i.name, i.path)
		}
		return fmt.Sprintf("%q", i.path)
	}
	if len(keep) == 1 {
		fmt.Fprintf(&block, "import %s\n", line(keep[0]))
	} else {
		block.WriteString("import (\n")
		for pass, std := range []bool{true, false} {
			wrote := false
			for _, i := range keep {
				if isStdImport(i.path) != std {
					continue
				}
				if pass == 1 && !wrote && strings.Contains(block.String(), "\"") {
					block.WriteString("\n")
				}
				fmt.Fprintf(&block, "\t%s\n", line(i))
				wrote = true
			}
		}
		block.WriteString(")\n")
	}
	// Put the block after the package clause.
	out := buf.Bytes()
	pf, err := parser.ParseFile(token.NewFileSet(), rel, out, parser.PackageClauseOnly)
	if err != nil {
		return nil, err
	}
	at := int(pf.Name.End()) - 1
	if nl := bytes.IndexByte(out[at:], '\n'); nl >= 0 {
		at += nl + 1
	} else {
		at = len(out)
	}
	joined := string(out[:at]) + "\n" + block.String() + "\n" + strings.TrimLeft(string(out[at:]), "\n")
	return format.Source([]byte(joined))
}

// importName is the name an import path is used by when it has no alias:
// its last element, or the one before a version suffix.
func importName(p string) string {
	base := path.Base(p)
	if len(base) > 1 && base[0] == 'v' && strings.Trim(base[1:], "0123456789") == "" {
		base = path.Base(path.Dir(p))
	}
	return base
}

// stdFor is the standard-library package a file means by name, given the
// names it uses from it. Where two share a name the symbols decide, as
// they do for goimports.
func stdFor(name string, syms map[string]bool) string {
	loadStd()
	ps := stdPaths[name]
	switch len(ps) {
	case 0:
		return ""
	case 1:
		return ps[0]
	}
	switch name {
	case "rand":
		if syms["Reader"] || syms["Read"] || syms["Prime"] || syms["Text"] {
			return "crypto/rand"
		}
		return "math/rand"
	case "template":
		if syms["HTML"] || syms["JS"] || syms["URL"] || syms["HTMLEscapeString"] {
			return "html/template"
		}
		return "text/template"
	case "scanner":
		return "text/scanner"
	}
	sort.Slice(ps, func(i, j int) bool { return len(ps[i]) < len(ps[j]) })
	return ps[0]
}
