// Command tilegen compiles an S-expression spec into Go scaffolding by
// successive lowering passes and tree tiling, leaving well-described holes
// for an LLM (or a human) to fill.
//
//	spec file or dir --parse--> --merge--> validate --expand--> --concretize(config)-->
//	--select--> target forms --emit--> go.mod, *.go, db/*.sql, sqlc.yaml, tasks,
//	starter files; with -git: clone or init, commit, gh repo create, topics
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const version = "v0.3.0"

// Pipeline is the ordered list of local tiling passes. Each maps
// S-expressions to S-expressions; run with -dump to see every stage.
var Pipeline = []*Pass{Expand, Concretize, Select}

// Options are the command-line settings.
type Options struct {
	Spec       string // a .sexp file or a directory of them
	Config     string // config file; overrides a (config ...) form in the spec
	PolicyFile string // policy file; overrides a (policy ...) form in the spec
	Out        string
	Name       string // overrides the project (and repository) name
	Dump       bool
	Strict     bool
	Git        bool // clone the GitHub repo if it exists, else create it
	DryRun     bool // run every pass, print the -git plan, write nothing

	Reselect   bool // ignore tilegen.lock and choose every backend again
	Check      bool // plan only, and fail if the output differs or holes remain
	JSON       bool // print structured output instead of a report
	AllowHoles bool // with Check: open holes are not a failure
	Runner     Runner
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "plan":
			if err := planCmd(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		case "api":
			if err := apiCmd(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		case "import":
			if err := importCmd(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		case "explain":
			if err := explainCmd(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		case "tiles":
			if err := tilesCmd(os.Args[2:], os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		case "fill":
			if err := fillCmd(os.Args[2:], os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		case "prompt":
			if err := promptCmd(os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		case "check":
			if err := checkCmd(os.Args[2:], os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		case "up", "status", "down":
			if err := workspaceCmd(os.Args[1], os.Args[2:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, "tilegen:", err)
				os.Exit(1)
			}
			return
		}
	}
	var o Options
	flag.StringVar(&o.Config, "config", "", "config .sexp file (default: a (config ...) form in the spec, else built-in defaults)")
	flag.StringVar(&o.PolicyFile, "policy", "", "policy .sexp file: weights, prefer and avoid (default: a (policy ...) form in the spec)")
	flag.StringVar(&o.Out, "out", "", "output directory (default: the workspace's (out ...), else ./out)")
	flag.StringVar(&o.Name, "name", "", "project name (default: the workspace's (name ...)); also the repository name, and the module path when (module ...) is omitted")
	flag.BoolVar(&o.Dump, "dump", false, "write the S-expression after every pass to <out>/.tilegen/")
	flag.BoolVar(&o.Strict, "strict", false, "fail on spec forms no tile covers instead of creating LLM tasks")
	flag.BoolVar(&o.Reselect, "reselect", false, "ignore tilegen.lock and choose every store's backend again")
	flag.BoolVar(&o.Git, "git", false, "make <out> a git repository: clone the (repo (github ...)) if it exists, else git init, commit, and create it with gh")
	flag.BoolVar(&o.DryRun, "dry-run", false, "run every pass and print the git/gh commands -git would run, but write nothing")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen [flags] SPEC        generate\n"+
			"       tilegen check [SPEC]         fail if generated code is out of date or holes remain\n"+
			"       tilegen prompt [SPEC] [ID]   print a self-contained LLM prompt for a task (no ID: list)\n"+
			"       tilegen fill [SPEC] [ID]     fill holes with an LLM CLI; build; retry with the errors\n"+
			"       tilegen tiles [-sexp]        list the tile registry: passes, capabilities, costs\n"+
			"       tilegen explain [SPEC]       why each store got its backend: needs, scores, legality\n"+
			"       tilegen import [DIR]         lift an existing Go module into a spec\n"+
			"       tilegen api IMPORT-PATH      the API surface a prompt carries for a package\n"+
			"       tilegen plan [-dot] [SPEC]   what the plan makes, in dependency order\n"+
			"       tilegen up [-detach] [SPEC]  create worktrees, open or attach the tmux session\n"+
			"       tilegen status [SPEC]        worktrees, changes, open holes, session\n"+
			"       tilegen down [-prune] [SPEC] close the session (and remove clean worktrees)\n\n"+
			"SPEC is a .sexp file or a directory of them (default for up/status/down: ./spec).\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("tilegen", version)
		return
	}
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	o.Spec = flag.Arg(0)
	if err := run(o, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tilegen:", err)
		os.Exit(1)
	}
}

// compiled is a spec after every pass: the target forms, the shared
// context, the -dump stages, and the options with workspace defaults.
type compiled struct {
	nodes  []*Node
	stages []string
	c      *Ctx
	o      Options
	ws     *Workspace // nil when the spec has no (workspace ...)
}

// compile runs parse, merge, validation and every pass. It writes nothing.
func compile(o Options, log io.Writer) (*compiled, error) {
	if err := checkRegistry(); err != nil {
		return nil, fmt.Errorf("the tile registry is inconsistent (this is a tilegen bug): %w", err)
	}
	if o.Runner == nil {
		o.Runner = execRunner{log: log, dry: o.DryRun}
	}
	forms, err := ReadSpec(o.Spec)
	if err != nil {
		return nil, err
	}
	stages := []string{Dump(forms)}
	linked, err := Merge(forms)
	if err != nil {
		return nil, err
	}
	if linked.Project == nil {
		return nil, fmt.Errorf("%s has no (project NAME ...) form, so there is nothing to generate%s", o.Spec, workspaceHint(linked))
	}
	project, inlineCfg := linked.Project, linked.Config
	var ws *Workspace
	if linked.Workspace != nil {
		if ws, err = ParseWorkspace(linked.Workspace, specBase(o.Spec)); err != nil {
			return nil, err
		}
		if o.Name == "" {
			o.Name = ws.Name
		}
		if o.Out == "" {
			o.Out = ws.Out
		}
	}
	if o.Out == "" {
		o.Out = "out"
	}
	cfg := DefaultConfig()
	switch {
	case o.Config != "":
		if cfg, err = LoadConfig(o.Config); err != nil {
			return nil, err
		}
	case inlineCfg != nil:
		if cfg, err = ParseConfig(inlineCfg); err != nil {
			return nil, err
		}
	}
	pol := DefaultPolicy()
	switch {
	case o.PolicyFile != "":
		if pol, err = LoadPolicy(o.PolicyFile); err != nil {
			return nil, err
		}
	case linked.Policy != nil:
		if pol, err = ParsePolicy(linked.Policy); err != nil {
			return nil, err
		}
	}
	if err := pol.checkTileNames(); err != nil {
		return nil, err
	}
	c := &Ctx{Cfg: cfg, Strict: o.Strict, Policy: pol}
	if err := resolveProject(project, o.Name, o.Out, o.Git, o.Runner, c); err != nil {
		return nil, err
	}
	nodes := []*Node{project}
	stages = append(stages, Dump(nodes))
	if err := Validate(nodes, c); err != nil {
		return nil, err
	}
	if c.Lock, err = loadLock(o.Out); err != nil {
		var stale staleLock
		if !errors.As(err, &stale) {
			return nil, err
		}
		c.warn(Pos{}, "%v", err)
	}
	c.Reselect = o.Reselect
	if err := errors.Join(coverAll(project, c), checkReserved(project, c)); err != nil {
		return nil, err
	}
	for _, p := range Pipeline {
		if nodes, err = p.Run(c, nodes); err != nil {
			return nil, err
		}
		stages = append(stages, Dump(nodes))
	}
	return &compiled{nodes: nodes, stages: stages, c: c, o: o, ws: ws}, nil
}

func run(o Options, log io.Writer) error {
	cp, err := compile(o, log)
	if err != nil {
		return err
	}
	nodes, stages, c := cp.nodes, cp.stages, cp.c
	o = cp.o

	var gitRepo *Node
	for _, n := range nodes {
		if n.Head() == "git/repo" {
			gitRepo = n
		}
	}
	if o.Git && gitRepo == nil {
		gitRepo = L(Sym("git/repo"), L(Sym("visibility"), Sym("private"))) // local repository only
	}
	if o.Check {
		rep, err := Emit(nodes, o.Out, c, false)
		if err != nil {
			return err
		}
		if o.JSON {
			return checkJSON(rep, c, o, log)
		}
		return checkReport(rep, c, o, log)
	}

	mode := ""
	if o.Git {
		if mode, err = gitPrepare(o.Out, gitRepo, o.Runner, o.DryRun); err != nil {
			return err
		}
	}
	if o.DryRun {
		for _, w := range c.Warnings {
			fmt.Fprintln(log, "warning:", w)
		}
		if o.Git {
			if err := gitFinish(o.Out, mode, gitRepo, c, o.Runner, io.Discard); err != nil {
				return err
			}
		}
		fmt.Fprintf(log, "dry run: the spec is valid; nothing was written to %s\n", o.Out)
		return nil
	}

	if o.Dump {
		names := []string{"parse", "merge"}
		for _, p := range Pipeline {
			names = append(names, p.Name)
		}
		dir := filepath.Join(o.Out, ".tilegen")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for i, s := range stages {
			f := filepath.Join(dir, fmt.Sprintf("%02d-%s.sexp", i, names[i]))
			if err := os.WriteFile(f, []byte(s), 0o644); err != nil {
				return err
			}
		}
	}

	rep, err := Emit(nodes, o.Out, c, true)
	for _, w := range c.Warnings {
		fmt.Fprintln(log, "warning:", w)
	}
	if err != nil {
		return err
	}
	for _, f := range rep.Written {
		fmt.Fprintln(log, "  wrote ", f)
	}
	if rep.Unchanged > 0 {
		fmt.Fprintf(log, "  (%d file(s) already up to date)\n", rep.Unchanged)
	}
	for _, f := range rep.Kept {
		fmt.Fprintln(log, "  kept  ", f, "(yours, not overwritten)")
	}
	for _, a := range rep.Added {
		fmt.Fprintln(log, "  added ", a, "(new in the spec; your code untouched)")
	}
	for _, a := range rep.Restubbed {
		fmt.Fprintln(log, "  updated", a, "(untouched stub follows the new signature)")
	}
	for _, a := range rep.Dropped {
		fmt.Fprintln(log, "  dropped", a, "(untouched stub, no longer in the spec)")
	}
	for _, f := range rep.Removed {
		fmt.Fprintln(log, "  removed", f, "(no longer in the spec)")
	}
	for _, f := range rep.RemovedScaffold {
		fmt.Fprintln(log, "  removed", f, "(untouched scaffolding, no longer in the spec)")
	}
	for _, nt := range rep.Notes {
		what := nt.File
		if nt.Symbol != "" {
			what = nt.Symbol + " in " + nt.File
		}
		fmt.Fprintf(log, "  %-7s %s: %s\n", nt.Kind, what, nt.Detail)
	}
	fmt.Fprintf(log, "%d open LLM task(s), %d hole(s) already filled -> %s\n",
		rep.Tasks, rep.Done, filepath.Join(o.Out, "tilegen.tasks.json"))

	if cp.ws != nil && cp.ws.Session != "" {
		fmt.Fprintf(log, "run `tilegen up %s` to open the %s session (%d window(s))\n", o.Spec, cp.ws.Session, len(cp.ws.Windows))
	}
	switch {
	case o.Git:
		return gitFinish(o.Out, mode, gitRepo, c, o.Runner, log)
	case gitRepo != nil && gitRepo.Text("github") != "":
		fmt.Fprintf(log, "run with -git to clone or create github.com/%s\n", gitRepo.Text("github"))
	}
	return nil
}

// workspaceCmd runs up, status or down.
func workspaceCmd(cmd string, args []string, stdout, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen "+cmd, flag.ExitOnError)
	var dry, detach, prune bool
	if cmd != "status" {
		fs.BoolVar(&dry, "dry-run", false, "print the git and tmux commands that change things instead of running them")
	}
	switch cmd {
	case "up":
		fs.BoolVar(&detach, "detach", false, "create everything but do not attach to the tmux session")
	case "down":
		fs.BoolVar(&prune, "prune", false, "also remove worktrees (git refuses if they have uncommitted changes)")
	}
	pos := parseAnywhere(fs, args)
	spec := "spec"
	if len(pos) > 0 {
		spec = pos[0]
	}
	ws, err := loadWorkspace(spec)
	if err != nil {
		return err
	}
	r := execRunner{log: log, dry: dry}
	switch cmd {
	case "up":
		return Up(ws, r, log, detach || dry)
	case "down":
		return Down(ws, r, log, prune)
	}
	return Status(ws, r, stdout)
}

// checkCmd runs `tilegen check`: the same plan as generation, compared
// with the disk. Nothing is written.
func checkCmd(args []string, log io.Writer) error {
	fs := flag.NewFlagSet("tilegen check", flag.ExitOnError)
	var o Options
	fs.StringVar(&o.Config, "config", "", "config .sexp file (default: a (config ...) form in the spec)")
	fs.StringVar(&o.Out, "out", "", "the generated project (default: the workspace's (out ...), else ./out)")
	fs.StringVar(&o.Name, "name", "", "project name, as given to generation")
	fs.BoolVar(&o.AllowHoles, "allow-holes", false, "do not fail on open holes, only on out-of-date files and drift")
	fs.BoolVar(&o.Strict, "strict", false, "fail on spec forms no tile covers")
	fs.BoolVar(&o.JSON, "json", false, "print what would change as JSON")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen check [flags] [SPEC]\n\nExit status 1 if generating would change any file, if a method you wrote\nhas drifted from the spec, or (without -allow-holes) if holes remain.\n\n")
		fs.PrintDefaults()
	}
	pos := parseAnywhere(fs, args)
	o.Spec = "spec"
	if len(pos) > 0 {
		o.Spec = pos[0]
	}
	o.Check = true
	return run(o, log)
}

// checkResult is what generating would change, computed once and rendered
// either as a report or as JSON.
func checkResult(rep *Report, o Options) CheckJSON {
	updated := map[string]bool{}
	for _, f := range rep.Updated {
		updated[f] = true
	}
	out := CheckJSON{Missing: rep.New, OpenHoles: rep.Tasks, UpToDate: rep.Unchanged}
	for _, f := range rep.Changed {
		if !updated[f] {
			out.Stale = append(out.Stale, f)
		}
	}
	out.Remove = append(append([]string{}, rep.Removed...), rep.RemovedScaffold...)
	for _, a := range rep.Added {
		out.Stubs = append(out.Stubs, a+": new in the spec")
	}
	for _, a := range rep.Restubbed {
		out.Stubs = append(out.Stubs, a+": signature changed in the spec")
	}
	for _, a := range rep.Dropped {
		out.Stubs = append(out.Stubs, a+": no longer in the spec")
	}
	for _, nt := range rep.Notes {
		n := NoteJSON{File: nt.File, Symbol: nt.Symbol, Detail: nt.Detail}
		if nt.Kind == "drift" {
			out.Drift = append(out.Drift, n)
		} else {
			out.Orphans = append(out.Orphans, n)
		}
	}
	if stale := len(out.Missing) + len(out.Stale) + len(out.Remove); stale > 0 {
		out.Problems = append(out.Problems, fmt.Sprintf("%d file(s) out of date", stale))
	}
	if len(out.Drift) > 0 {
		out.Problems = append(out.Problems, fmt.Sprintf("%d drifted method(s)", len(out.Drift)))
	}
	if rep.Tasks > 0 && !o.AllowHoles {
		out.Problems = append(out.Problems, fmt.Sprintf("%d open hole(s)", rep.Tasks))
	}
	out.OK = len(out.Problems) == 0
	return out
}

func checkFailure(res CheckJSON, o Options) error {
	if res.OK {
		return nil
	}
	return fmt.Errorf("check failed: %s. Run `tilegen %s` to update, then fill the holes", strings.Join(res.Problems, ", "), o.Spec)
}

// checkJSON prints the same result as structured data.
func checkJSON(rep *Report, c *Ctx, o Options, log io.Writer) error {
	res := checkResult(rep, o)
	if err := writeJSON(os.Stdout, res); err != nil {
		return err
	}
	return checkFailure(res, o)
}

// checkReport prints what generation would change and decides pass/fail.
func checkReport(rep *Report, c *Ctx, o Options, log io.Writer) error {
	for _, w := range c.Warnings {
		fmt.Fprintln(log, "warning:", w)
	}
	res := checkResult(rep, o)
	for _, f := range res.Missing {
		fmt.Fprintf(log, "  missing  %s\n", f)
	}
	for _, f := range res.Stale {
		fmt.Fprintf(log, "  stale    %s: differs from what the spec generates\n", f)
	}
	for _, s := range res.Stubs {
		fmt.Fprintf(log, "  stub     %s\n", s)
	}
	for _, f := range res.Remove {
		fmt.Fprintf(log, "  remove   %s: no longer in the spec\n", f)
	}
	for _, d := range res.Drift {
		fmt.Fprintf(log, "  drift    %s in %s: %s\n", d.Symbol, d.File, d.Detail)
	}
	for _, orph := range res.Orphans {
		what := orph.File
		if orph.Symbol != "" {
			what = orph.Symbol + " in " + orph.File
		}
		fmt.Fprintf(log, "  orphan   %s: %s (warning)\n", what, orph.Detail)
	}
	if res.OpenHoles > 0 {
		fmt.Fprintf(log, "  holes    %d open (listed in tilegen.tasks.json)\n", res.OpenHoles)
	}
	if err := checkFailure(res, o); err != nil {
		return err
	}
	holes := "no open holes"
	if res.OpenHoles > 0 {
		holes = fmt.Sprintf("%d open hole(s) allowed", res.OpenHoles)
	}
	fmt.Fprintf(log, "ok: %d generated file(s) match the spec; %s\n", res.UpToDate, holes)
	return nil
}

// parseAnywhere parses flags that may come before, between or after the
// positional arguments (Go's flag package stops at the first non-flag, so
// `tilegen check spec/ -allow-holes` would otherwise ignore the flag).
func parseAnywhere(fs *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		fs.Parse(args)
		if fs.NArg() == 0 {
			return pos
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// workspaceHint points at the commands that work without a project, when
// the spec has a workspace but nothing to generate yet.
func workspaceHint(l *Linked) string {
	if l.Workspace == nil {
		return ""
	}
	return " (the workspace is there, so `tilegen up` still works)"
}
