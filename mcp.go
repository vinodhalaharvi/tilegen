//go:build !js

package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/vinodhalaharvi/weft/mcp"
	"github.com/vinodhalaharvi/weft/weft"
)

// tilegen as an MCP server.
//
// Every stage of the compiler is a weft.Arrow, and every tool is a
// composition of those arrows:
//
//	generate = place ; readText ; decideTiles ; layFiles ; confine ; writeFiles ; report
//	explain  = readText ; decideTiles ; describe
//	many     = traverse (name △ attempt generate) ; tally
//
// (";" is weft.Compose, "△" is weft.Fanout.) Nothing here decides
// anything: the arrows wrap the same functions the command line calls,
// so a project made through MCP is byte for byte the one `tilegen -out`
// makes. mcp_test.go holds that, and the laws the composition relies on.
//
// The server only scaffolds. It never fills a hole and never calls a
// model: a client can ask for one project or a thousand and no token is
// spent here.

// SpecIn is a spec as text, with the optional files that go beside it.
type SpecIn struct {
	Spec   string `json:"spec"`
	Config string `json:"config,omitempty"`
	Policy string `json:"policy,omitempty"`
}

// ProjectIn is a spec and the folder, under the server's root, to write it to.
type ProjectIn struct {
	Name string `json:"name"`
	SpecIn
}

// ManyIn is a batch of projects.
type ManyIn struct {
	Projects []ProjectIn `json:"projects"`
}

// Decision is one need and the tile chosen for it.
type Decision struct {
	Need       string `json:"need"`
	Capability string `json:"capability"`
	Tile       string `json:"tile"`
	Cost       int    `json:"cost"`
	ChosenBy   string `json:"chosen_by"` // auto | config | lock
}

// Project is what generating wrote.
type Project struct {
	Name      string     `json:"name"`
	Dir       string     `json:"dir"`
	Decisions []Decision `json:"decisions"`
	Written   int        `json:"files_written"`
	Unchanged int        `json:"files_unchanged"`
	Kept      []string   `json:"files_kept,omitempty"` // yours: never overwritten
	Holes     int        `json:"holes_open"`
	Next      []string   `json:"next"` // the commands that make it build, in order
	Warnings  []string   `json:"warnings,omitempty"`
}

// Outcome is one project of a batch: made, or not, with the reason.
type Outcome struct {
	Name    string   `json:"name"`
	OK      bool     `json:"ok"`
	Project *Project `json:"project,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// ManyOut is a batch's result. A project that fails does not stop the rest.
type ManyOut struct {
	Root     string    `json:"root"`
	Created  int       `json:"created"`
	Failed   int       `json:"failed"`
	Holes    int       `json:"holes_open"`
	Projects []Outcome `json:"projects"`
}

// The values that flow between stages.
type (
	placed struct { // a request, and where it is allowed to go
		in  ProjectIn
		dir string
	}
	parsed struct { // the spec as forms
		placed
		forms []*Node
	}
	decided struct { // every pass has run: the tiles are chosen
		parsed
		cp *compiled
	}
	laid struct { // the files are planned against what is on disk; nothing is written
		decided
		rep *Report
	}
)

var projectNameOK = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// place: a request to a folder under root, or a refusal. It is the only
// stage that knows where files may go.
func place(root string) weft.Arrow[ProjectIn, placed] {
	return func(_ context.Context, in ProjectIn) (placed, error) {
		if !projectNameOK.MatchString(in.Name) || strings.Contains(in.Name, "..") {
			return placed{}, fmt.Errorf("the name %q cannot be a folder here: use lower-case letters, digits, '-', '_' and '.', starting with a letter or digit, at most 63 characters", in.Name)
		}
		dir := filepath.Join(root, in.Name)
		if rel, err := filepath.Rel(root, dir); err != nil || rel != in.Name {
			return placed{}, fmt.Errorf("the name %q would leave the root folder %s", in.Name, root)
		}
		if st, err := os.Lstat(dir); err == nil && !st.IsDir() {
			return placed{}, fmt.Errorf("%s exists and is not a folder (a file or a link); choose another name", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return placed{}, fmt.Errorf("the folder %s cannot be read: %w", dir, err)
		}
		if len(entries) > 0 {
			if _, err := os.Stat(filepath.Join(dir, lockFile)); err != nil {
				return placed{}, fmt.Errorf("the folder %s already exists and is not a tilegen project (it has no %s); choose another name", dir, lockFile)
			}
		}
		return placed{in: in, dir: dir}, nil
	}
}

// readText: text to forms.
func readText(_ context.Context, p placed) (parsed, error) {
	if strings.TrimSpace(p.in.Spec) == "" {
		return parsed{}, errors.New(`the spec is empty; send the S-expression, starting "(project ..."`)
	}
	forms, err := parseParts(p.in.Spec, p.in.Config, p.in.Policy)
	if err != nil {
		return parsed{}, err
	}
	return parsed{placed: p, forms: forms}, nil
}

// decideTiles: every pass. This is where each need gets its tile; a lock file
// already in the folder pins the choices it records.
func decideTiles(_ context.Context, p parsed) (decided, error) {
	cp, err := compileForms(p.forms, Options{Out: p.dir}, io.Discard)
	if err != nil {
		return decided{}, err
	}
	return decided{parsed: p, cp: cp}, nil
}

// layFiles: plan every file against what the folder holds. Nothing is written.
func layFiles(_ context.Context, d decided) (laid, error) {
	rep, err := Emit(d.cp.nodes, d.dir, d.cp.c, false)
	if err != nil {
		return laid{}, err
	}
	return laid{decided: d, rep: rep}, nil
}

// confine: refuse a plan with any path that is not inside the project's
// folder, whatever the spec said about where its packages live.
func confine(_ context.Context, l laid) (laid, error) {
	paths := make([]string, 0, len(l.rep.staged)+len(l.rep.unlinked))
	for rel := range l.rep.staged {
		paths = append(paths, rel)
	}
	for rel := range l.rep.unlinked {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return laid{}, fmt.Errorf("this spec would put %q outside its project folder %s; a (dir ...) must stay inside the project", rel, l.dir)
		}
	}
	return l, nil
}

// writeFiles: put the plan on disk. The only stage with an effect outside memory.
func writeFiles(_ context.Context, l laid) (laid, error) {
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return laid{}, err
	}
	return l, l.rep.Apply()
}

// report: what a caller is told.
func report(l laid) Project {
	c := l.cp.c
	p := Project{Name: l.in.Name, Dir: l.dir, Decisions: decisions(c), Written: len(l.rep.Written), Unchanged: l.rep.Unchanged,
		Kept: l.rep.Kept, Holes: l.rep.Tasks, Warnings: c.Warnings}
	for _, st := range toolSteps(c) {
		p.Next = append(p.Next, st.Cmd)
	}
	p.Next = append(p.Next, "go mod tidy", "go build ./...")
	return p
}

func decisions(c *Ctx) []Decision {
	ids := make([]string, 0, len(c.Cover))
	for id := range c.Cover {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Decision, 0, len(ids))
	for _, id := range ids {
		cv := c.Cover[id]
		out = append(out, Decision{Need: id, Capability: cv.Offer.Capability, Tile: cv.Offer.Tile, Cost: cv.Score, ChosenBy: cv.By})
	}
	return out
}

// recovering turns a panic in a stage into that arrow's error, so one spec
// that finds a bug in tilegen cannot take the server, or a batch, with it.
func recovering[A, B any]() weft.Transform[A, B] {
	return func(f weft.Arrow[A, B]) weft.Arrow[A, B] {
		return func(ctx context.Context, a A) (b B, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("tilegen stopped unexpectedly on this spec (a bug in tilegen): %v", r)
				}
			}()
			return f(ctx, a)
		}
	}
}

// generate is the whole compiler, one stage after another.
func generate(root string) weft.Arrow[ProjectIn, Project] {
	return weft.Apply(
		weft.Map(weft.Pipe6(place(root), weft.Arrow[placed, parsed](readText), weft.Arrow[parsed, decided](decideTiles), weft.Arrow[decided, laid](layFiles), weft.Arrow[laid, laid](confine), weft.Arrow[laid, laid](writeFiles)), report),
		recovering[ProjectIn, Project]())
}

// attempt makes an arrow total: its error becomes the left of an Either,
// so a traversal over it has nothing to fail on.
func attempt[A, B any](f weft.Arrow[A, B]) weft.Arrow[A, weft.Either[string, B]] {
	return func(ctx context.Context, a A) (weft.Either[string, B], error) {
		b, err := f(ctx, a)
		if err != nil {
			return weft.Left[string, B](err.Error()), nil
		}
		return weft.Right[string, B](b), nil
	}
}

func nameOf(in ProjectIn) string { return in.Name }

// maxBatch bounds one call. A client that wants more makes more calls.
const maxBatch = 500

// many generates a batch: each project attempted on its own, in the order
// given, a bounded number at a time.
func many(root string) weft.Arrow[ManyIn, ManyOut] {
	each := weft.Fanout(weft.Pure(nameOf), attempt(generate(root)))
	all := weft.Traverse(each, weft.WithConcurrency(min(8, runtime.NumCPU())))
	return func(ctx context.Context, in ManyIn) (ManyOut, error) {
		if len(in.Projects) == 0 {
			return ManyOut{}, errors.New(`no projects; send {"projects": [{"name": "...", "spec": "(project ...)"}]}`)
		}
		if len(in.Projects) > maxBatch {
			return ManyOut{}, fmt.Errorf("%d projects in one call; the most is %d, so send them in several calls", len(in.Projects), maxBatch)
		}
		seen := map[string]bool{}
		for _, p := range in.Projects {
			if seen[p.Name] {
				return ManyOut{}, fmt.Errorf("the name %q appears twice; two projects cannot share a folder", p.Name)
			}
			seen[p.Name] = true
		}
		pairs, err := all(ctx, in.Projects)
		if err != nil {
			return ManyOut{}, err
		}
		out := ManyOut{Root: root}
		for _, p := range pairs {
			o := Outcome{Name: p.Fst}
			if p.Snd.IsRight() {
				o.OK, o.Project = true, p.Snd.Right
				out.Created++
				out.Holes += p.Snd.Right.Holes
			} else {
				o.Error = *p.Snd.Left
				out.Failed++
			}
			out.Projects = append(out.Projects, o)
		}
		return out, nil
	}
}

// explainMu guards explainJSON, which sets a package variable while it runs.
var explainMu sync.Mutex

// explain: the decisions and the reasons, and nothing on disk.
func explain() weft.Arrow[SpecIn, any] {
	here := weft.Pure(func(in SpecIn) placed { return placed{in: ProjectIn{SpecIn: in}, dir: "project"} })
	describe := func(d decided) any {
		explainMu.Lock()
		defer explainMu.Unlock()
		return explainJSON(d.cp, nil)
	}
	return weft.Apply(weft.Map(weft.Pipe3(here, weft.Arrow[placed, parsed](readText), weft.Arrow[parsed, decided](decideTiles)), describe), recovering[SpecIn, any]())
}

// Catalogue is what there is to ask for.
type Catalogue struct {
	Version string `json:"version"`
	TilesJSON
}

func catalogue() weft.Arrow[struct{}, Catalogue] {
	return weft.Pure(func(struct{}) Catalogue { return Catalogue{Version: version, TilesJSON: tilesJSON(Registry())} })
}

//go:embed README.md
var readmeText string

// Reference is how to write a spec.
type Reference struct {
	Version   string `json:"version"`
	Reference string `json:"reference"`
}

// reference is the README's own sections on the spec, the config and
// choosing tiles: the same text a person reads, so it cannot drift.
func reference() weft.Arrow[struct{}, Reference] {
	text := readmeSections("## The spec", "## The config", "## Choosing tiles")
	return weft.Pure(func(struct{}) Reference { return Reference{Version: version, Reference: text} })
}

// readmeSections returns the named level-two sections, each up to the next.
func readmeSections(titles ...string) string {
	var out []string
	lines := strings.Split(readmeText, "\n")
	for _, title := range titles {
		on := false
		for _, l := range lines {
			if strings.HasPrefix(l, "## ") {
				on = l == title
			}
			if on {
				out = append(out, l)
			}
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

const (
	specProps = `"spec": {"type": "string", "minLength": 1, "description": "The spec, as S-expression text starting with (project NAME ...). It states requirements such as (durable) and (cross-process); it does not name a database or a broker. Call tilegen_reference for the language."},
"config": {"type": "string", "description": "Optional. A (config ...) form that pins choices, for example (config (store postgres) (events nats)). Leave it out to let tilegen choose every tile."},
"policy": {"type": "string", "description": "Optional. A (policy ...) form: cost weights, tiles to avoid, and engines already running."}`
	noInput     = `{"type": "object", "properties": {}, "additionalProperties": false}`
	specSchema  = `{"type": "object", "properties": {` + specProps + `}, "required": ["spec"], "additionalProperties": false}`
	nameProp    = `"name": {"type": "string", "pattern": "^[a-z0-9][a-z0-9._-]{0,62}$", "description": "The folder to create under the server's root, for example orders-api. One project per name."}`
	oneSchema   = `{"type": "object", "properties": {` + nameProp + `, ` + specProps + `}, "required": ["name", "spec"], "additionalProperties": false}`
	manySchema  = `{"type": "object", "properties": {"projects": {"type": "array", "minItems": 1, "maxItems": 500, "description": "The projects to create, each in its own folder.", "items": {"type": "object", "properties": {` + nameProp + `, ` + specProps + `}, "required": ["name", "spec"], "additionalProperties": false}}}, "required": ["projects"], "additionalProperties": false}`
	scaffoldSay = " Scaffolding only: types, interfaces, SQL, protobuf, wiring and a lock file are written, and method bodies that need judgment are left as typed holes. No model is called and no token is spent."
)

// mcpServer is tilegen's tools, each an arrow lifted out through weft.
func mcpServer(root string) *mcp.Server {
	return mcp.Serve(
		mcp.ServeAsTool("tilegen_reference", reference(),
			mcp.WithDescription("How to write a tilegen spec: the spec language with examples, the config that pins choices, and how tiles are chosen. Read this before writing a spec. Takes no input; returns {version, reference} where reference is Markdown."),
			mcp.WithInputSchema([]byte(noInput))),
		mcp.ServeAsTool("tilegen_catalogue", catalogue(),
			mcp.WithDescription("What there is to ask for: every capability with the requirements a spec may state for it, and every tile with what it guarantees, what rules it out, what it costs and what it needs running. Takes no input. Read-only."),
			mcp.WithInputSchema([]byte(noInput))),
		mcp.ServeAsTool("tilegen_explain", explain(),
			mcp.WithDescription("Decide, and write nothing. For a spec, returns each need, the tile chosen for it and its score, every other tile that could do it, and every tile ruled out with the reason. Use it to check a spec, or to see why a tile won, before generating. An invalid spec comes back as an error that names the line and the fix."),
			mcp.WithInputSchema([]byte(specSchema))),
		mcp.ServeAsTool("tilegen_generate", generate(root),
			mcp.WithDescription("Create one Go project from a spec, in the folder NAME under the server's root."+scaffoldSay+" Returns the tile chosen for each need, how many files were written, how many holes are open, and the commands that make the project build. Calling it again with the same name and spec changes nothing; a folder that exists and is not a tilegen project is refused."),
			mcp.WithInputSchema([]byte(oneSchema))),
		mcp.ServeAsTool("tilegen_generate_many", many(root),
			mcp.WithDescription("Create many Go projects in one call, each from its own spec, each in its own folder under the server's root (up to 500 per call)."+scaffoldSay+" A spec that fails is reported with its error and the others are still created. Returns, per project, either what was written or why it was not."),
			mcp.WithInputSchema([]byte(manySchema))),
	)
}

// mcpCmd runs the server on standard input and output until the client leaves.
func mcpCmd(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", ".", "the folder projects are created under; nothing is written outside it")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: tilegen mcp [-root DIR]\n\nServe tilegen to an MCP client (an editor's assistant) on standard input and output.\nThe client can read the spec language, ask why a spec gets the tiles it does, and\ncreate one project or many under DIR. It only scaffolds: no hole is filled and no\nmodel is called.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("mcp takes no arguments, only -root")
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return fmt.Errorf("the root %s is not a folder; create it first, or pass -root", root)
	}
	if err := checkRegistry(); err != nil {
		return fmt.Errorf("the tile registry is inconsistent (this is a tilegen bug): %w", err)
	}
	return mcp.RunStdioServer(mcpServer(root), mcp.WithServerInfo("tilegen", version))
}
