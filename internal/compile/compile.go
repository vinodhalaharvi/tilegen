package compile

//go:generate go run gen_std.go

import (
	"errors"
	"fmt"
	"io"

	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

const Version = "v0.3.6"

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
	JSON       bool // Print structured output instead of a report
	AllowHoles bool // with Check: open holes are not a failure
	Runner     Runner
	Tiles      string // (tile ...) drafts given with the spec, as text
}

// Compiled is a spec after every pass: the target forms, the shared
// context, the -dump stages, and the options with workspace defaults.
type Compiled struct {
	Nodes     []*sexp.Node
	Stages    []string
	Ctx       *Ctx
	Opts      Options
	Workspace *Workspace // nil when the spec has no (workspace ...)
}

// Compile runs parse, merge, validation and every pass. It writes nothing.
func Compile(o Options, log io.Writer) (*Compiled, error) {
	if err := CheckRegistry(); err != nil {
		return nil, fmt.Errorf("the tile registry is inconsistent (this is a tilegen bug): %w", err)
	}
	if o.Runner == nil {
		o.Runner = ExecRunner{Log: log, Dry: o.DryRun}
	}
	forms, err := ReadSpec(o.Spec)
	if err != nil {
		return nil, err
	}
	return CompileForms(forms, o, log)
}

// CompileForms is compile once the spec has been read, so a caller that
// already has the forms - the HTTP service, whose "files" are request
// fields - runs exactly the same compiler.
func CompileForms(forms []*sexp.Node, o Options, log io.Writer) (*Compiled, error) {
	if err := CheckRegistry(); err != nil {
		return nil, fmt.Errorf("the tile registry is inconsistent (this is a tilegen bug): %w", err)
	}
	if o.Runner == nil {
		o.Runner = ExecRunner{Log: log, Dry: o.DryRun}
	}
	var err error
	stages := []string{sexp.Dump(forms)}
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
	if err := pol.CheckTileNames(); err != nil {
		return nil, err
	}
	c := &Ctx{Cfg: cfg, Strict: o.Strict, Policy: pol}
	if c.Extra, err = ParseDrafts(o.Tiles); err != nil {
		return nil, err
	}
	if err := resolveProject(project, o.Name, o.Out, o.Git, o.Runner, c); err != nil {
		return nil, err
	}
	nodes := []*sexp.Node{project}
	stages = append(stages, sexp.Dump(nodes))
	if err := Validate(nodes, c); err != nil {
		return nil, err
	}
	if c.Lock, c.Pins, err = LoadLock(o.Out); err != nil {
		var stale staleLock
		if !errors.As(err, &stale) {
			return nil, err
		}
		c.warn(sexp.Pos{}, "%v", err)
	}
	c.Reselect = o.Reselect
	if err := errors.Join(coverAll(project, c), checkReserved(project, c)); err != nil {
		return nil, err
	}
	var drafted []string
	for _, id := range SortedKeys(c.Cover) {
		if cov := c.Cover[id]; cov.Offer.Draft {
			drafted = append(drafted, id+": "+cov.Offer.Tile)
		}
	}
	if len(drafted) > 0 { // decided, and that is as far as a draft goes
		return nil, &DraftsChosen{Compiled: &Compiled{Nodes: nodes, Stages: stages, Ctx: c, Opts: o, Workspace: ws}, Needs: drafted}
	}
	for _, p := range Pipeline {
		if nodes, err = p.Run(c, nodes); err != nil {
			return nil, err
		}
		stages = append(stages, sexp.Dump(nodes))
	}
	return &Compiled{Nodes: nodes, Stages: stages, Ctx: c, Opts: o, Workspace: ws}, nil
}

// workspaceHint points at the commands that work without a project, when
// the spec has a workspace but nothing to generate yet.
func workspaceHint(l *Linked) string {
	if l.Workspace == nil {
		return ""
	}
	return " (the workspace is there, so `tilegen up` still works)"
}
