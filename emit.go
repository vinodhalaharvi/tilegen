package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	goversion "go/version"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/imports"
)

// Report summarizes what Emit did.
type Report struct {
	Written, Kept []string // Written = New + Changed
	New, Changed  []string
	Unchanged     int
	Tasks, Done   int
	TaskList      []Task

	// The plan (see plan.go).
	out      string
	staged   map[string][]byte
	order    []string
	unlinked map[string]bool

	// Reconciliation (see reconcile.go).
	Produced        map[string]bool // every file this run produces, written or kept
	Updated         []string        // kept files that had stubs appended
	Added           []string        // "Method to file": appended stubs
	Restubbed       []string        // "Method in file": untouched stubs given the new signature
	Dropped         []string        // "Method from file": untouched stubs the spec no longer has
	Removed         []string        // stale generated files deleted
	RemovedScaffold []string        // scaffolded files deleted: still pure scaffolding
	Notes           []Note          // orphans and drift
}

// Emit turns target forms into files. It is deliberately dumb: every
// decision was made by the passes, and every format is handled by the
// tool that owns it.
func Emit(nodes []*Node, out string, c *Ctx, apply bool) (*Report, error) {
	r := &Report{Produced: map[string]bool{}, out: out, staged: map[string][]byte{}, unlinked: map[string]bool{}}
	var tables, queries, tasks []*Node
	var sqlc *Node
	for _, n := range nodes {
		var err error
		switch n.Head() {
		case "gomod":
			err = emitGoMod(n, out, r)
		case "go/file":
			err = emitGoFile(n, out, r)
		case "sql/table":
			tables = append(tables, n)
		case "sql/query":
			queries = append(queries, n)
		case "sqlc/config":
			sqlc = n
		case "llm/task":
			tasks = append(tasks, n)
		case "text/file":
			err = emitTextFile(n, out, r)
		case "git/repo":
			// acted on by run() around Emit when -git is set
		default:
			err = fmt.Errorf("%s: no emitter for %s (tilegen bug: a pass left a non-target form)", n.Pos, short(n))
		}
		if err != nil {
			return nil, err
		}
	}
	if len(tables) > 0 {
		if err := writeFile(out, "db/schema.sql", joinSQL(tables), r); err != nil {
			return nil, err
		}
		if len(queries) > 0 {
			if err := writeFile(out, "db/query.sql", joinSQL(queries), r); err != nil {
				return nil, err
			}
		}
	}
	if sqlc != nil {
		if err := writeFile(out, "sqlc.yaml", sqlcYAML(sqlc), r); err != nil {
			return nil, err
		}
	}
	r.Produced["tilegen.tasks.json"] = true
	sweep(out, r)
	if err := emitTasks(tasks, out, c, r); err != nil {
		return nil, err
	}
	r.finalize()
	if apply {
		return r, r.Apply()
	}
	return r, nil
}

func writeFile(out, rel string, data []byte, r *Report) error {
	r.stage(rel, data)
	return nil
}

// emitTextFile writes (text/file path (mode keep|generated) content).
func emitTextFile(n *Node, out string, r *Report) error {
	rel := n.List[1].Atom
	if n.Text("mode") == "keep" {
		if r.exists(rel) {
			r.Produced[rel] = true
			r.Kept = append(r.Kept, rel)
			return nil
		}
	}
	return writeFile(out, rel, []byte(n.List[len(n.List)-1].Atom), r)
}

// emitGoMod edits go.mod with x/mod/modfile. If go.mod exists (say after
// `go mod tidy` added indirect requirements) it is merged, not clobbered,
// and versions tidy raised are kept.
func emitGoMod(n *Node, out string, r *Report) error {
	p := filepath.Join(out, "go.mod")
	f := new(modfile.File)
	if data, err := r.read("go.mod"); err == nil {
		if f, err = modfile.Parse(p, data, nil); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := f.AddModuleStmt(n.Text("module")); err != nil {
		return err
	}
	// The spec's versions are minimums. `go mod tidy` may raise them (say, a
	// dependency needs a newer Go), and lowering them again would break the
	// build until the next tidy - so tilegen only ever raises versions.
	if want := n.Text("go"); f.Go == nil || goversion.Compare("go"+want, "go"+f.Go.Version) > 0 {
		if err := f.AddGoStmt(want); err != nil {
			return err
		}
	}
	have := map[string]string{}
	for _, r := range f.Require {
		have[r.Mod.Path] = r.Mod.Version
	}
	for _, req := range n.FindAll("require") {
		path, want := req.List[1].Atom, req.List[2].Atom
		if cur, ok := have[path]; ok && semver.Compare(cur, want) >= 0 {
			continue
		}
		if err := f.AddRequire(path, want); err != nil {
			return err
		}
	}
	f.Cleanup()
	return writeFile(out, "go.mod", modfile.Format(f.Syntax), r)
}

// emitGoFile: text -> go/parser (AST) -> astutil (imports) -> go/format ->
// x/tools/imports (goimports: stdlib imports, grouping, final gofmt).
// A kept file that already exists is reconciled instead: see reconcile.go.
func emitGoFile(n *Node, out string, r *Report) error {
	rel := n.List[1].Atom
	full := filepath.Join(out, filepath.FromSlash(rel))
	if n.Text("mode") == "keep" {
		if r.exists(rel) {
			r.Produced[rel] = true
			return reconcileKeep(n, full, rel, r)
		}
	}
	final, err := finishGo(n, full, rel, renderGo(n))
	if err != nil {
		return err
	}
	return writeFile(out, rel, final, r)
}

// finishGo parses src, adds the (import ...) forms of n, and formats with
// go/format and goimports.
func finishGo(n *Node, full, rel, src string) ([]byte, error) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("%s: tilegen rendered invalid Go for %s (tilegen bug): %v\n%s", n.Pos, rel, err, numbered(src))
	}
	for _, im := range n.FindAll("import") {
		alias, p := im.List[1].Atom, im.List[2].Atom
		if alias == path.Base(p) {
			alias = ""
		}
		astutil.AddNamedImport(fset, af, alias, p)
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, af); err != nil {
		return nil, err
	}
	final, err := imports.Process(full, buf.Bytes(), &imports.Options{Comments: true, TabIndent: true, TabWidth: 8})
	if err != nil {
		return nil, fmt.Errorf("goimports %s: %v", rel, err)
	}
	return final, nil
}

func renderGo(f *Node) string {
	var b strings.Builder
	if f.Text("mode") == "generated" {
		b.WriteString("// Code generated by tilegen. DO NOT EDIT.\n\n")
	} else {
		b.WriteString("// Scaffolded by tilegen. This file is yours: tilegen never overwrites it.\n\n")
	}
	b.WriteString(docLines(f, ""))
	fmt.Fprintf(&b, "package %s\n\n", f.Text("package"))
	for _, d := range f.Args() {
		switch d.Head() {
		case "go/struct":
			b.WriteString(docLines(d, ""))
			fmt.Fprintf(&b, "type %s struct {\n", d.List[1].Atom)
			for _, fl := range d.FindAll("field") {
				b.WriteString(docLines(fl, "\t"))
				fmt.Fprintf(&b, "\t%s %s", fl.List[1].Atom, fl.List[2].Atom)
				if tag := fl.Text("tag"); tag != "" {
					if strings.Contains(tag, "`") {
						fmt.Fprintf(&b, " %s", strconv.Quote(tag))
					} else {
						fmt.Fprintf(&b, " `%s`", tag)
					}
				}
				b.WriteString("\n")
			}
			b.WriteString("}\n\n")
		case "go/interface":
			b.WriteString(docLines(d, ""))
			fmt.Fprintf(&b, "type %s interface {\n", d.List[1].Atom)
			for _, m := range d.Args() {
				switch m.Head() {
				case "embed":
					fmt.Fprintf(&b, "\t%s\n", m.List[1].Atom)
				case "method":
					b.WriteString(docLines(m, "\t"))
					fmt.Fprintf(&b, "\t%s\n", signature(m))
				}
			}
			b.WriteString("}\n\n")
		case "go/func":
			b.WriteString(renderFunc(d))
		case "go/raw": // fixed helper code a tile ships verbatim
			b.WriteString(d.List[1].Atom + "\n\n")
		case "go/assert":
			fmt.Fprintf(&b, "// Compile-time check that %s satisfies %s.\nvar _ %s = (*%s)(nil)\n\n",
				d.List[2].Atom, d.List[1].Atom, d.List[1].Atom, d.List[2].Atom)
		case "go/type":
			b.WriteString(docLines(d, ""))
			fmt.Fprintf(&b, "type %s %s\n\n", d.List[1].Atom, d.List[2].Atom)
		case "go/consts":
			b.WriteString("const (\n")
			for _, c := range d.FindAll("const") {
				fmt.Fprintf(&b, "\t%s %s = %s\n", c.List[1].Atom, c.List[2].Atom, strconv.Quote(c.List[3].Atom))
			}
			b.WriteString(")\n\n")
		case "go/var":
			b.WriteString(docLines(d, ""))
			fmt.Fprintf(&b, "var %s = %s\n\n", d.List[1].Atom, d.List[2].Atom)
		}
	}
	return b.String()
}

// renderFunc renders a (go/func ...) declaration.
func renderFunc(d *Node) string {
	var b strings.Builder
	b.WriteString(docLines(d, ""))
	b.WriteString("func ")
	if rv := d.Find("recv"); rv != nil {
		fmt.Fprintf(&b, "(%s %s) ", rv.List[1].Atom, rv.List[2].Atom)
	}
	fmt.Fprintf(&b, "%s {\n%s\n}\n\n", signature(d), d.Text("body"))
	return b.String()
}

// signature renders Name(a A, b B) (R1, R2) from (x Name (params ...) (returns ...)).
func signature(m *Node) string {
	var ps, rs []string
	for _, p := range m.Find("params").Args() {
		ps = append(ps, p.List[0].Atom+" "+p.List[1].Atom)
	}
	for _, t := range m.Find("returns").Args() {
		rs = append(rs, t.Atom)
	}
	s := m.List[1].Atom + "(" + strings.Join(ps, ", ") + ")"
	switch len(rs) {
	case 0:
	case 1:
		s += " " + rs[0]
	default:
		s += " (" + strings.Join(rs, ", ") + ")"
	}
	return s
}

func docLines(n *Node, indent string) string {
	doc := n.Text("doc")
	if doc == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(doc, "\n") {
		b.WriteString(indent + "// " + line + "\n")
	}
	return b.String()
}

func numbered(src string) string {
	var b strings.Builder
	for i, l := range strings.Split(src, "\n") {
		fmt.Fprintf(&b, "%4d  %s\n", i+1, l)
	}
	return b.String()
}

func joinSQL(nodes []*Node) []byte {
	var b strings.Builder
	b.WriteString("-- Code generated by tilegen. DO NOT EDIT.\n")
	if len(nodes) > 0 && nodes[0].Head() == "sql/query" {
		// sqlc copies the comments before a query into the generated Go, so
		// end the header with an empty statement: it stays out of sqlc's
		// output, where it would mislabel sqlc's file as tilegen's.
		b.WriteString("-- (the empty statement below keeps this header out of sqlc's Go)\n;\n")
	}
	for _, n := range nodes {
		b.WriteString("\n" + n.List[2].Atom + "\n")
	}
	return []byte(b.String())
}

func sqlcYAML(n *Node) []byte {
	q := strconv.Quote
	var b strings.Builder
	b.WriteString("# Code generated by tilegen. DO NOT EDIT.\n")
	b.WriteString("version: \"2\"\nsql:\n  - engine: \"postgresql\"\n")
	fmt.Fprintf(&b, "    schema: %s\n    queries: %s\n", q(n.Text("schema")), q(n.Text("queries")))
	fmt.Fprintf(&b, "    gen:\n      go:\n        package: \"db\"\n        out: %s\n        sql_package: \"pgx/v5\"\n", q(n.Text("out")))
	if ovs := n.FindAll("override"); len(ovs) > 0 {
		b.WriteString("        overrides:\n")
		for _, o := range ovs {
			fmt.Fprintf(&b, "          - db_type: %s\n            go_type: %s\n", q(o.List[1].Atom), q(o.List[2].Atom))
		}
	}
	return []byte(b.String())
}

// Task is one hole for the LLM, as written to tilegen.tasks.json.
type Task struct {
	ID           string   `json:"id"`
	Status       string   `json:"status,omitempty"` // "drift": the method exists but its signature is wrong
	File         string   `json:"file,omitempty"`
	Line         int      `json:"line,omitempty"`
	Symbol       string   `json:"symbol,omitempty"`
	Contract     string   `json:"contract,omitempty"`
	Intent       string   `json:"intent"`
	Constraints  []string `json:"constraints,omitempty"`
	ContextFiles []string `json:"context_files,omitempty"`
	SExpr        string   `json:"sexpr,omitempty"`
}

const llmInstructions = `Implement each task by replacing the panic("tilegen:hole <id>") at file:line ` +
	`with a real body that honors the contract, intent and constraints. For a task with ` +
	`status "drift", the method exists but its signature no longer matches: change it to ` +
	`the contract and adapt the body. Items under "reconcile" with kind "orphan" are no ` +
	`longer in the spec: delete them if nothing uses them. Never edit files that say ` +
	`"DO NOT EDIT": change the spec and re-run tilegen instead. Verify with: ` +
	`go build ./... && go vet ./...`

// emitTasks lists the holes that still exist on disk. Holes are found by
// parsing the real files with go/parser, so a hole the LLM (or you) already
// filled drops off the list, and line numbers are always current.
func emitTasks(tasks []*Node, out string, c *Ctx, r *Report) error {
	holes := map[string]map[string]int{}
	var list []Task
	driftByID := map[string]Note{}
	var orphans []Note
	for _, nt := range r.Notes {
		if nt.Kind == "drift" {
			driftByID[nt.id] = nt
		} else {
			orphans = append(orphans, nt)
		}
	}
	for _, n := range tasks {
		t := Task{
			ID: n.Text("id"), File: n.Text("file"), Symbol: n.Text("symbol"),
			Contract: n.Text("contract"), Intent: n.Text("intent"), SExpr: n.Text("sexpr"),
		}
		for _, k := range n.FindAll("constraint") {
			t.Constraints = append(t.Constraints, k.List[1].Atom)
		}
		for _, k := range n.FindAll("context-file") {
			t.ContextFiles = append(t.ContextFiles, k.List[1].Atom)
		}
		if t.File != "" {
			h, ok := holes[t.File]
			if !ok {
				var err error
				src, err := r.read(t.File)
				if err != nil {
					return err
				}
				if h, err = findHolesSrc(t.File, src); err != nil {
					return err
				}
				holes[t.File] = h
			}
			line, open := h[t.ID]
			if drift, ok := driftByID[t.ID]; ok {
				t.Status, t.Line = "drift", drift.line
				t.Intent = "Signature drift: " + drift.Detail + ". " + t.Intent
				list = append(list, t)
				continue
			}
			if !open {
				r.Done++
				continue
			}
			t.Line = line
		}
		list = append(list, t)
	}
	r.Tasks = len(list)
	r.TaskList = list
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep <id> and && readable for the LLM
	enc.SetIndent("", "  ")
	doc := map[string]any{
		"generator":    "tilegen " + version,
		"module":       c.Module,
		"instructions": llmInstructions,
		"tasks":        list,
	}
	if len(orphans) > 0 {
		doc["reconcile"] = orphans
	}
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return writeFile(out, "tilegen.tasks.json", buf.Bytes(), r)
}

func findHoles(file string) (map[string]int, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return findHolesSrc(file, src)
}

func findHolesSrc(file string, src []byte) (map[string]int, error) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot scan %s for holes: %v", file, err)
	}
	holes := map[string]int{}
	ast.Inspect(af, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "panic" {
			return true
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && strings.HasPrefix(s, holePrefix) {
				holes[strings.TrimPrefix(s, holePrefix)] = fset.Position(lit.Pos()).Line
			}
		}
		return true
	})
	return holes, nil
}
