// Command tilegen compiles an S-expression spec into Go scaffolding by
// successive lowering passes and tree tiling, leaving well-described holes
// for an LLM (or a human) to fill.
//
//	spec file or dir --parse--> --merge--> validate --expand--> --concretize(config)-->
//	--select--> target forms --emit--> go.mod, *.go, db/*.sql, sqlc.yaml, tasks,
//	starter files; with -git: clone or init, commit, gh repo create, topics
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const version = "v0.1.0"

// Pipeline is the ordered list of local tiling passes. Each maps
// S-expressions to S-expressions; run with -dump to see every stage.
var Pipeline = []*Pass{Expand, Concretize, Select}

// Options are the command-line settings.
type Options struct {
	Spec   string // a .sexp file or a directory of them
	Config string // config file; overrides a (config ...) form in the spec
	Out    string
	Name   string // overrides the project (and repository) name
	Dump   bool
	Strict bool
	Git    bool // clone the GitHub repo if it exists, else create it
	DryRun bool // run every pass, print the -git plan, write nothing
	Runner Runner
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
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
	flag.StringVar(&o.Out, "out", "", "output directory (default: the workspace's (out ...), else ./out)")
	flag.StringVar(&o.Name, "name", "", "project name (default: the workspace's (name ...)); also the repository name, and the module path when (module ...) is omitted")
	flag.BoolVar(&o.Dump, "dump", false, "write the S-expression after every pass to <out>/.tilegen/")
	flag.BoolVar(&o.Strict, "strict", false, "fail on spec forms no tile covers instead of creating LLM tasks")
	flag.BoolVar(&o.Git, "git", false, "make <out> a git repository: clone the (repo (github ...)) if it exists, else git init, commit, and create it with gh")
	flag.BoolVar(&o.DryRun, "dry-run", false, "run every pass and print the git/gh commands -git would run, but write nothing")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tilegen [flags] SPEC        generate\n"+
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

func run(o Options, log io.Writer) error {
	if o.Runner == nil {
		o.Runner = execRunner{log: log, dry: o.DryRun}
	}
	forms, err := ReadSpec(o.Spec)
	if err != nil {
		return err
	}
	stages := []string{Dump(forms)}
	linked, err := Merge(forms)
	if err != nil {
		return err
	}
	project, inlineCfg := linked.Project, linked.Config
	if linked.Workspace != nil {
		ws, err := ParseWorkspace(linked.Workspace, specBase(o.Spec))
		if err != nil {
			return err
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
			return err
		}
	case inlineCfg != nil:
		if cfg, err = ParseConfig(inlineCfg); err != nil {
			return err
		}
	}
	c := &Ctx{Cfg: cfg, Strict: o.Strict}
	if err := resolveProject(project, o.Name, o.Git, o.Runner, c); err != nil {
		return err
	}
	nodes := []*Node{project}
	stages = append(stages, Dump(nodes))
	if err := Validate(nodes, c); err != nil {
		return err
	}
	for _, p := range Pipeline {
		if nodes, err = p.Run(c, nodes); err != nil {
			return err
		}
		stages = append(stages, Dump(nodes))
	}

	var gitRepo *Node
	for _, n := range nodes {
		if n.Head() == "git/repo" {
			gitRepo = n
		}
	}
	if o.Git && gitRepo == nil {
		gitRepo = L(Sym("git/repo"), L(Sym("visibility"), Sym("private"))) // local repository only
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

	rep, err := Emit(nodes, o.Out, c)
	for _, w := range c.Warnings {
		fmt.Fprintln(log, "warning:", w)
	}
	if err != nil {
		return err
	}
	for _, f := range rep.Written {
		fmt.Fprintln(log, "  wrote ", f)
	}
	for _, f := range rep.Kept {
		fmt.Fprintln(log, "  kept  ", f, "(yours, not overwritten)")
	}
	fmt.Fprintf(log, "%d open LLM task(s), %d hole(s) already filled -> %s\n",
		rep.Tasks, rep.Done, filepath.Join(o.Out, "tilegen.tasks.json"))

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
	fs.Parse(args)
	spec := "spec"
	if fs.NArg() > 0 {
		spec = fs.Arg(0)
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
