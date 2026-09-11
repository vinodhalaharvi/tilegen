package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ReadSpec parses a spec file, or every *.sexp file in a directory in
// name order, keeping each form's own file:line:col.
func ReadSpec(path string) ([]*Node, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	files := []string{path}
	if info.IsDir() {
		if files, err = filepath.Glob(filepath.Join(path, "*.sexp")); err != nil {
			return nil, err
		}
		sort.Strings(files)
		if len(files) == 0 {
			return nil, fmt.Errorf("%s: no .sexp files in directory", path)
		}
	}
	var forms []*Node
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		fs, err := Parse(f, string(src))
		if err != nil {
			return nil, err
		}
		forms = append(forms, fs...)
	}
	return forms, nil
}

// Linked is the result of Merge: one project plus the forms that sit
// beside it and never change generated code.
type Linked struct {
	Project   *Node
	Config    *Node // (config ...): house style
	Workspace *Node // (workspace ...): how tilegen runs on this machine
}

// Merge is a whole-program pass, the linker of this compiler. A spec may be
// split across files: one (project ...) header, plus any number of
// top-level (package ...), (require ...), (repo ...) and (config ...)
// forms. Merge links them into a single (project ...):
//
//   - packages with the same name merge, items in file order;
//   - identical requires dedupe; conflicting ones are errors;
//   - (config ...) and (workspace ...) forms are returned separately.
//
// Unlike the other passes it is not a set of local tiles: deciding that
// two (package orders ...) forms are one package needs the whole program.
func Merge(forms []*Node) (*Linked, error) {
	var errs []error
	bad := func(n *Node, f string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", n.Pos, fmt.Sprintf(f, a...)))
	}
	var project, config, workspace, repo *Node
	var items []*Node // project items in order, top-level contributions after
	var other []*Node // unknown top-level forms, left for the validator

	for _, f := range forms {
		switch f.Head() {
		case "project":
			if project != nil {
				bad(f, "second (project ...) form; the first is at %s", project.Pos)
				continue
			}
			project = f
			items = append(f.List[2:len(f.List):len(f.List)], items...)
		case "config":
			if config != nil {
				bad(f, "second (config ...) form; the first is at %s", config.Pos)
				continue
			}
			config = f
		case "workspace":
			if workspace != nil {
				bad(f, "second (workspace ...) form; the first is at %s", workspace.Pos)
				continue
			}
			workspace = f
		case "package", "require", "repo":
			items = append(items, f)
		default:
			other = append(other, f)
		}
	}
	if project == nil || len(project.List) < 2 {
		errs = append(errs, errors.New("spec needs exactly one (project NAME ...) form"))
		return nil, errors.Join(errs...)
	}

	merged := &Node{IsList: true, Pos: project.Pos, List: []*Node{project.List[0], project.List[1]}}
	pkgs := map[string]*Node{}
	reqs := map[string]*Node{} // alias -> entry
	var reqForm *Node
	for _, it := range items {
		switch it.Head() {
		case "package":
			if len(it.List) < 2 || it.List[1].IsList {
				merged.List = append(merged.List, it) // malformed; the validator reports it
				continue
			}
			name := it.List[1].Atom
			if p, ok := pkgs[name]; ok {
				if it.Find("doc") != nil && p.Find("doc") != nil {
					bad(it.Find("doc"), "package %s already has a (doc ...) at %s", name, p.Find("doc").Pos)
				}
				p.List = append(p.List, it.List[2:]...)
				continue
			}
			p := &Node{IsList: true, Pos: it.Pos, List: append([]*Node{}, it.List...)}
			pkgs[name] = p
			merged.List = append(merged.List, p)
		case "require":
			if reqForm == nil {
				reqForm = &Node{IsList: true, Pos: it.Pos, List: []*Node{it.List[0]}}
				merged.List = append(merged.List, reqForm)
			}
			for _, r := range it.Args() {
				if !r.IsList || len(r.List) == 0 || r.List[0].IsList {
					reqForm.List = append(reqForm.List, r) // the validator reports it
					continue
				}
				alias := r.List[0].Atom
				if prev, ok := reqs[alias]; ok {
					if prev.Flat() != r.Flat() {
						bad(r, "require %s conflicts with %s at %s", r.Flat(), prev.Flat(), prev.Pos)
					}
					continue
				}
				reqs[alias] = r
				reqForm.List = append(reqForm.List, r)
			}
		case "repo":
			if repo != nil {
				bad(it, "second (repo ...) form; the first is at %s", repo.Pos)
				continue
			}
			repo = it
			merged.List = append(merged.List, it)
		default:
			merged.List = append(merged.List, it)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &Linked{Project: merged, Config: config, Workspace: workspace}, errorsFor(other)
}

func errorsFor(other []*Node) error {
	var errs []error
	for _, f := range other {
		errs = append(errs, fmt.Errorf("%s: unknown top-level form %s (want project, package, require, repo, config, workspace)", f.Pos, short(f)))
	}
	return errors.Join(errs...)
}
