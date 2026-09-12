package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// An LLM filling a hole writes against the APIs the surrounding code
// imports. Left to its training data it guesses at them, and libraries
// move. So a prompt carries the real API surface of the packages that
// task's files import: exported declarations with their doc comments,
// signatures only, printed from go/types.
//
// Scope is deliberately narrow. Only what the task's own files import,
// not the whole dependency tree, and never the standard library, which
// models know well. The result is cached under <out>/.tilegen/api.

const (
	apiCacheDir    = ".tilegen/api"
	apiTokenBudget = 1200 // per package, in rough tokens (4 chars each)
	apiMaxPackages = 4
)

// apiSurface returns the API surface sections for the packages the given
// files import, ready to append to a prompt. Failures are silent: a
// prompt without an API section is worse, not broken.
func apiSurface(out string, files []string, r *Report, self string, mentions ...string) string {
	paths := importsOf(files, r)
	// A mention that is already an import path (a tile naming a package a
	// hole will need) is taken as is; prose is resolved through go.mod.
	var prose []string
	for _, m := range mentions {
		if strings.Contains(m, "/") && !strings.ContainsAny(m, " \t") {
			paths = append(paths, m)
		} else {
			prose = append(prose, m)
		}
	}
	paths = append(paths, mentionedPackages(prose, paths, out)...)
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	shown := 0
	for _, p := range paths {
		if shown == apiMaxPackages {
			break
		}
		if strings.HasPrefix(p, self) {
			continue // the project's own packages are already in the context files
		}
		text, err := packageAPI(out, p)
		if err != nil || text == "" {
			continue
		}
		if shown == 0 {
			b.WriteString("\n## API surface\n\nThe exported API of the packages this code imports, as it is on disk.\nUse these signatures; do not guess at them.\n")
		}
		fmt.Fprintf(&b, "\n### %s\n\n```go\n%s\n```\n", p, strings.TrimRight(text, "\n"))
		shown++
	}
	return b.String()
}

// mentionedPackages resolves package names named in a task's intent or
// contract but not imported by its files yet: the LLM is about to import
// them, so it needs their API (a postgres store told to map pgx.ErrNoRows
// has no pgx import until it writes one).
func mentionedPackages(texts, have []string, dir string) []string {
	seen := map[string]bool{}
	for _, p := range have {
		seen[path.Base(p)] = true
	}
	known := knownPackagePaths(dir)
	var out []string
	for _, text := range texts {
		for _, w := range qualifierRE.FindAllStringSubmatch(text, -1) {
			name := w[1]
			if seen[name] || isStd(name) {
				continue
			}
			if p, ok := known[name]; ok {
				seen[name] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// qualifierRE finds pkg.Name references in prose: "map pgx.ErrNoRows".
var qualifierRE = regexp.MustCompile(`\b([a-z][a-z0-9]{1,15})\.[A-Z]\w+`)

// knownPackagePaths maps a package name to an import path, from the
// module's own go.mod requires. Only modules the project already depends
// on are considered, so nothing is fetched.
func knownPackagePaths(dir string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return out
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return out
	}
	for _, r := range f.Require {
		p := r.Mod.Path
		base := path.Base(p)
		if v := path.Base(p); strings.HasPrefix(v, "v") && len(v) <= 3 { // .../v5
			base = path.Base(path.Dir(p))
		}
		if _, dup := out[base]; !dup {
			out[base] = p
		}
	}
	return out
}

// importsOf reads the import paths of the given files (as the plan will
// leave them), skipping the standard library.
func importsOf(files []string, r *Report) []string {
	seen := map[string]bool{}
	var paths []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		src, err := r.read(f)
		if err != nil {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, im := range af.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			if seen[p] || !strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
				continue // seen, or standard library (no dot in the first element)
			}
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths
}

// packageAPI returns one package's exported API, from the cache when it
// is there.
func packageAPI(out, importPath string) (string, error) {
	cache := filepath.Join(out, apiCacheDir, apiCacheName(importPath))
	if b, err := os.ReadFile(cache); err == nil {
		return string(b), nil
	}
	text, err := loadPackageAPI(out, importPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err == nil {
		os.WriteFile(cache, []byte(text), 0o644)
	}
	return text, nil
}

func apiCacheName(importPath string) string {
	sum := sha256.Sum256([]byte(importPath))
	base := strings.ReplaceAll(strings.ReplaceAll(importPath, "/", "_"), ".", "-")
	if len(base) > 60 {
		base = base[:60]
	}
	return base + "-" + hex.EncodeToString(sum[:4]) + ".txt"
}

// loadPackageAPI type-checks a package and prints its exported
// declarations, signatures only, within a token budget.
func loadPackageAPI(dir, importPath string) (string, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo, Dir: dir}
	pkgs, err := packages.Load(cfg, importPath)
	if err != nil || len(pkgs) == 0 || pkgs[0].Types == nil {
		return "", fmt.Errorf("cannot load %s: %v", importPath, err)
	}
	p := pkgs[0]
	if len(p.Errors) > 0 {
		return "", fmt.Errorf("%s does not type-check", importPath)
	}
	docs := docComments(p)
	scope := p.Types.Scope()
	qual := func(other *types.Package) string {
		if other == p.Types {
			return ""
		}
		return other.Name()
	}
	var types_, funcs, values []string
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		doc := docs[name]
		switch o := obj.(type) {
		case *types.TypeName:
			types_ = append(types_, doc+typeDecl(o, qual))
		case *types.Func:
			funcs = append(funcs, doc+"func "+o.Name()+strings.TrimPrefix(types.TypeString(o.Type(), qual), "func"))
		case *types.Var:
			values = append(values, doc+"var "+o.Name()+" "+types.TypeString(o.Type(), qual))
		case *types.Const:
			values = append(values, doc+"const "+o.Name()+" = "+o.Val().String())
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "package %s // %s\n", p.Types.Name(), importPath)
	budget := apiTokenBudget * 4
	for _, group := range [][]string{types_, funcs, values} {
		for _, d := range group {
			if b.Len()+len(d) > budget {
				b.WriteString("\n// ... trimmed to fit the prompt\n")
				return b.String(), nil
			}
			b.WriteString("\n" + d + "\n")
		}
	}
	return b.String(), nil
}

// typeDecl prints a type and, for named types, its exported methods.
func typeDecl(o *types.TypeName, qual types.Qualifier) string {
	var b strings.Builder
	named, ok := o.Type().(*types.Named)
	if !ok {
		fmt.Fprintf(&b, "type %s = %s", o.Name(), types.TypeString(o.Type(), qual))
		return b.String()
	}
	switch u := named.Underlying().(type) {
	case *types.Struct:
		fmt.Fprintf(&b, "type %s struct {", o.Name())
		n := 0
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if !f.Exported() {
				continue
			}
			n++
			fmt.Fprintf(&b, "\n\t%s %s", f.Name(), types.TypeString(f.Type(), qual))
		}
		if n == 0 {
			b.Reset()
			fmt.Fprintf(&b, "type %s struct{}", o.Name())
		} else {
			b.WriteString("\n}")
		}
	case *types.Interface:
		fmt.Fprintf(&b, "type %s interface {", o.Name())
		for i := 0; i < u.NumMethods(); i++ {
			m := u.Method(i)
			fmt.Fprintf(&b, "\n\t%s%s", m.Name(), strings.TrimPrefix(types.TypeString(m.Type(), qual), "func"))
		}
		b.WriteString("\n}")
		return b.String()
	default:
		fmt.Fprintf(&b, "type %s %s", o.Name(), types.TypeString(named.Underlying(), qual))
	}
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		if !m.Exported() {
			continue
		}
		recv := types.TypeString(m.Type().(*types.Signature).Recv().Type(), qual)
		fmt.Fprintf(&b, "\nfunc (%s) %s%s", recv, m.Name(), strings.TrimPrefix(types.TypeString(m.Type(), qual), "func"))
	}
	return b.String()
}

// docComments returns the first line of each exported declaration's doc
// comment: the contract, without the prose.
func docComments(p *packages.Package) map[string]string {
	out := map[string]string{}
	add := func(name string, doc *ast.CommentGroup) {
		if doc == nil || name == "" {
			return
		}
		if line, _, _ := strings.Cut(strings.TrimSpace(doc.Text()), "\n"); line != "" {
			out[name] = "// " + line + "\n"
		}
	}
	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					add(d.Name.Name, d.Doc)
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch s := sp.(type) {
					case *ast.TypeSpec:
						doc := s.Doc
						if doc == nil {
							doc = d.Doc
						}
						add(s.Name.Name, doc)
					case *ast.ValueSpec:
						for _, n := range s.Names {
							doc := s.Doc
							if doc == nil {
								doc = d.Doc
							}
							add(n.Name, doc)
						}
					}
				}
			}
		}
	}
	return out
}

// tilegen api [-out DIR] IMPORT-PATH... prints what a prompt would carry
// for those packages.
func apiCmd(args []string, stdout, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen api", flag.ExitOnError)
	out := fs.String("out", ".", "the module to resolve imports from")
	noCache := fs.Bool("no-cache", false, "ignore the cache under .tilegen/api")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen api [-out DIR] IMPORT-PATH...\n\nPrints the exported API surface a prompt would carry for those packages.\n\n")
		fs.PrintDefaults()
	}
	paths := parseAnywhere(fs, args)
	if len(paths) == 0 {
		fs.Usage()
		return fmt.Errorf("no import paths given")
	}
	for _, p := range paths {
		var text string
		var err error
		if *noCache {
			text, err = loadPackageAPI(*out, p)
		} else {
			text, err = packageAPI(*out, p)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "### %s\n\n```go\n%s\n```\n\n", p, strings.TrimRight(text, "\n"))
	}
	return nil
}
