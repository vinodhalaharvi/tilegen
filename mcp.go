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
//	symbols  = (id △ resolve) ; pick
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

// ExplainIn is a spec, and the drafted tiles to weigh beside tilegen's own.
type ExplainIn struct {
	SpecIn
	Tiles string `json:"tiles,omitempty"`
}

// decideWith is decideTiles with drafts in the running. A draft that wins
// cannot generate, and the decision still stands: explaining it is the point.
func decideWith(tiles string) weft.Arrow[parsed, decided] {
	return func(_ context.Context, p parsed) (decided, error) {
		cp, err := compileForms(p.forms, Options{Out: p.dir, Tiles: tiles}, io.Discard)
		var won *draftsChosen
		if errors.As(err, &won) {
			return decided{parsed: p, cp: won.cp}, nil
		}
		if err != nil {
			return decided{}, err
		}
		return decided{parsed: p, cp: cp}, nil
	}
}

// explain: the decisions and the reasons, and nothing on disk.
func explain() weft.Arrow[ExplainIn, any] {
	here := weft.Pure(func(in ExplainIn) placed { return placed{in: ProjectIn{SpecIn: in.SpecIn}, dir: "project"} })
	describe := func(d decided) any {
		explainMu.Lock()
		defer explainMu.Unlock()
		return explainJSON(d.cp, nil)
	}
	decide := func(ctx context.Context, in ExplainIn) (decided, error) {
		return weft.Pipe3(here, weft.Arrow[placed, parsed](readText), decideWith(in.Tiles))(ctx, in)
	}
	return weft.Apply(weft.Map(weft.Arrow[ExplainIn, decided](decide), describe), recovering[ExplainIn, any]())
}

// GoModIn is the text of a go.mod.
type GoModIn struct {
	GoMod string `json:"gomod"`
}

// gomod: which of a go.mod's direct requirements a tile already speaks,
// which engines that shows are in use, and which requirements no tile knows.
func gomod() weft.Arrow[GoModIn, GoModsJSON] {
	return func(_ context.Context, in GoModIn) (GoModsJSON, error) {
		if strings.TrimSpace(in.GoMod) == "" {
			return GoModsJSON{}, errors.New(`no go.mod; send its text as {"gomod": "module ...\n\nrequire (...)"}`)
		}
		out := scanGoMods([]string{in.GoMod})
		if len(out.Errors) > 0 {
			return GoModsJSON{}, fmt.Errorf("that is not a go.mod: %s", strings.TrimPrefix(out.Errors[0], "file 1: "))
		}
		return out, nil
	}
}

// SymbolsIn asks what a module exports, or for some of its symbols in full.
type SymbolsIn struct {
	Module  string   `json:"module"`
	Version string   `json:"version,omitempty"`
	Symbols []string `json:"symbols,omitempty"`
}

// SymbolsOut is the index of a module, or the symbols asked for.
type SymbolsOut struct {
	Module  string         `json:"module"`
	Version string         `json:"version"`
	Index   *SymbolIndex   `json:"index,omitempty"`
	Details []SymbolDetail `json:"details,omitempty"`
	Missing []string       `json:"not_exported,omitempty"`
}

// maxSymbolDetails bounds how many symbols one call returns in full.
const maxSymbolDetails = 25

// resolve: a module and a version to what the type checker reads there.
func resolve(read func(mod, version string) (*ModuleSymbols, error)) weft.Arrow[SymbolsIn, *ModuleSymbols] {
	return func(ctx context.Context, in SymbolsIn) (*ModuleSymbols, error) {
		if strings.TrimSpace(in.Module) == "" {
			return nil, errors.New(`no module; send {"module": "github.com/nats-io/nats.go"}, with "version" if it matters`)
		}
		type result struct {
			m   *ModuleSymbols
			err error
		}
		done := make(chan result, 1)
		go func() {
			m, err := read(strings.TrimSpace(in.Module), strings.TrimSpace(in.Version))
			done <- result{m, err}
		}()
		select {
		case r := <-done:
			return r.m, r.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// symbols: the index of a module, or the named symbols in full.
func symbols(read func(mod, version string) (*ModuleSymbols, error)) weft.Arrow[SymbolsIn, SymbolsOut] {
	pick := func(p weft.Pair[SymbolsIn, *ModuleSymbols]) (SymbolsOut, error) {
		in, m := p.Fst, p.Snd
		out := SymbolsOut{Module: m.Module, Version: m.Version}
		if len(in.Symbols) == 0 {
			idx := m.index()
			out.Index = &idx
			return out, nil
		}
		if len(in.Symbols) > maxSymbolDetails {
			return SymbolsOut{}, fmt.Errorf("%d symbols in one call; the most is %d, so ask in several calls", len(in.Symbols), maxSymbolDetails)
		}
		for _, ref := range in.Symbols {
			if d, ok := m.detail(ref); ok {
				out.Details = append(out.Details, d)
			} else {
				out.Missing = append(out.Missing, ref+didYouMean(ref, m.names()))
			}
		}
		return out, nil
	}
	both := weft.Fanout(weft.Id[SymbolsIn](), resolve(read))
	return weft.Apply(weft.Compose(both, weft.Arrow[weft.Pair[SymbolsIn, *ModuleSymbols], SymbolsOut](func(_ context.Context, p weft.Pair[SymbolsIn, *ModuleSymbols]) (SymbolsOut, error) {
		return pick(p)
	})), recovering[SymbolsIn, SymbolsOut]())
}

// CheckTileIn is a drafted tile to hold to tilegen's rules and to its module.
type CheckTileIn struct {
	Tile    string `json:"tile"`
	Version string `json:"version,omitempty"`
}

// CheckTileOut says whether a draft stands, and everything wrong with it if not.
type CheckTileOut struct {
	OK         bool     `json:"ok"`
	Name       string   `json:"name,omitempty"`
	Capability string   `json:"capability,omitempty"`
	Module     string   `json:"module,omitempty"`
	Version    string   `json:"version,omitempty"`
	Cited      int      `json:"symbols_cited"`
	Verified   bool     `json:"symbols_verified"`
	Problems   []string `json:"problems,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// checkTile: the form, by the parser every tile passes; then each cited
// symbol, by the module itself.
func checkTile(read func(mod, version string) (*ModuleSymbols, error)) weft.Arrow[CheckTileIn, CheckTileOut] {
	form := func(_ context.Context, in CheckTileIn) (*Offer, error) {
		if strings.TrimSpace(in.Tile) == "" {
			return nil, errors.New(`no tile; send {"tile": "(tile NAME (offers ...) ...)"}`)
		}
		offers, err := parseDrafts(in.Tile)
		if err != nil {
			return nil, err
		}
		if len(offers) != 1 {
			return nil, fmt.Errorf("one tile per call; this is %d", len(offers))
		}
		return offers[0], nil
	}
	return weft.Apply(weft.Arrow[CheckTileIn, CheckTileOut](func(ctx context.Context, in CheckTileIn) (CheckTileOut, error) {
		o, err := form(ctx, in)
		if err != nil {
			return CheckTileOut{Problems: strings.Split(err.Error(), "\n")}, nil
		}
		out := CheckTileOut{Name: o.Tile, Capability: o.Capability, Module: o.Module, Cited: len(o.Symbols)}
		if len(o.Symbols) == 0 {
			out.OK = true
			out.Note = "the form is right, and it cites no symbols, so nothing ties its claims to the module: add (symbols ...) naming what each satisfies rests on"
			return out, nil
		}
		m, err := resolve(read)(ctx, SymbolsIn{Module: o.Module, Version: in.Version})
		if err != nil {
			out.Problems = []string{"the module's symbols could not be read, so the cited ones are unchecked: " + err.Error()}
			return out, nil
		}
		out.Version = m.Version
		if err := checkCited(o, m); err != nil {
			out.Problems = []string{err.Error()}
			return out, nil
		}
		out.OK, out.Verified = true, true
		return out, nil
	}), recovering[CheckTileIn, CheckTileOut]())
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
	noInput       = `{"type": "object", "properties": {}, "additionalProperties": false}`
	nameProp      = `"name": {"type": "string", "pattern": "^[a-z0-9][a-z0-9._-]{0,62}$", "description": "The folder to create under the server's root, for example orders-api. One project per name."}`
	oneSchema     = `{"type": "object", "properties": {` + nameProp + `, ` + specProps + `}, "required": ["name", "spec"], "additionalProperties": false}`
	manySchema    = `{"type": "object", "properties": {"projects": {"type": "array", "minItems": 1, "maxItems": 500, "description": "The projects to create, each in its own folder.", "items": {"type": "object", "properties": {` + nameProp + `, ` + specProps + `}, "required": ["name", "spec"], "additionalProperties": false}}}, "required": ["projects"], "additionalProperties": false}`
	explainSchema = `{"type": "object", "properties": {` + specProps + `, "tiles": {"type": "string", "description": "Optional. Drafted (tile ...) forms to weigh beside tilegen's own, to see whether a tool you already use would win. A draft can win a decision; it cannot generate code."}}, "required": ["spec"], "additionalProperties": false}`
	gomodSchema   = `{"type": "object", "properties": {"gomod": {"type": "string", "minLength": 1, "description": "The text of a go.mod file."}}, "required": ["gomod"], "additionalProperties": false}`
	symbolsSchema = `{"type": "object", "properties": {"module": {"type": "string", "minLength": 1, "description": "A Go module path, for example github.com/nats-io/nats.go."}, "version": {"type": "string", "description": "Optional. The version, as in go.mod, for example v1.55.0. Left out, the latest is read."}, "symbols": {"type": "array", "maxItems": 25, "items": {"type": "string"}, "description": "Optional. Symbols to return in full, written pkg.Name, for example nats.JetStream. Left out, the answer is the index: every package's names."}}, "required": ["module"], "additionalProperties": false}`
	checkSchema   = `{"type": "object", "properties": {"tile": {"type": "string", "minLength": 1, "description": "One (tile NAME (offers CAPABILITY) (doc ...) (module ...) (symbols pkg.Name ...) (satisfies ...) (cost ...)) form."}, "version": {"type": "string", "description": "Optional. The module version to check the cited symbols against. Left out, the latest."}}, "required": ["tile"], "additionalProperties": false}`
	scaffoldSay   = " Scaffolding only: types, interfaces, SQL, protobuf, wiring and a lock file are written, and method bodies that need judgment are left as typed holes. No model is called and no token is spent."
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
			mcp.WithDescription("Decide, and write nothing. For a spec, returns each need, the tile chosen for it and its score, every other tile that could do it, and every tile ruled out with the reason. Use it to check a spec, or to see why a tile won, before generating. With \"tiles\", drafted tiles are weighed beside tilegen's own. An invalid spec comes back as an error that names the line and the fix."),
			mcp.WithInputSchema([]byte(explainSchema))),
		mcp.ServeAsTool("tilegen_gomod", gomod(),
			mcp.WithDescription("Read a go.mod: which direct requirements a tile already speaks, which engines (postgres, nats, kafka ...) they show are in use, and which requirements no tile knows. Use it inside an existing project to see what tilegen covers before writing a spec, and to turn what is already running into a policy. Reads only the text sent; touches nothing."),
			mcp.WithInputSchema([]byte(gomodSchema))),
		mcp.ServeAsTool("tilegen_symbols", symbols(readSymbols),
			mcp.WithDescription("What a Go module exports, as data read by the Go type checker (not from source text and not from memory): each interface with its whole method set and the types that satisfy it, each type with its methods and constructors, and the sentinel errors. Without \"symbols\" it returns the index, names only; with \"symbols\": [\"nats.JetStream\"] it returns those in full, with every signature. Use it before drafting a tile for a library, so each claim rests on something the library has. Fetches the module with the go tool if it is not on this machine; runs nothing from it."),
			mcp.WithInputSchema([]byte(symbolsSchema))),
		mcp.ServeAsTool("tilegen_check_tile", checkTile(readSymbols),
			mcp.WithDescription("Hold a drafted tile to tilegen's rules: the form, by the parser every tile passes, and each symbol it cites in (symbols ...), by the module itself. Returns ok, or every problem with how to fix it. A tile that passes can be given to tilegen_explain as \"tiles\"."),
			mcp.WithInputSchema([]byte(checkSchema))),
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
