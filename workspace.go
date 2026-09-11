package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
)

// A (workspace ...) form describes how tilegen runs on this machine. It
// never changes generated code, so the golden tests do not see it.
//
//	(workspace
//	  (name myshop)                  ; default for -name
//	  (out ..)                       ; default for -out, relative to the spec folder
//	  (worktrees                     ; checkouts at <out>.wt/<name>
//	    (worktree billing (branch feat/billing)))
//	  (tmux
//	    (session myshop
//	      (window code (dir .) (run "$EDITOR ."))
//	      (window billing (worktree billing) (run "claude")))))
//
// `tilegen up` creates what is missing and attaches; `status` reports;
// `down` closes the session and, with -prune, removes clean worktrees.

// Workspace is a parsed (workspace ...) form with absolute paths.
type Workspace struct {
	Name      string
	Out       string // the main checkout; "" when not declared
	Worktrees []Worktree
	Session   string
	Windows   []Window
}

type Worktree struct{ Name, Branch, Path string }

type Window struct{ Name, Dir, Run string }

var (
	tmuxNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`) // no '.' or ':', which tmux targets use
	branchRE   = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

// specBase is the folder that relative paths in a spec are resolved from.
func specBase(spec string) string {
	if info, err := os.Stat(spec); err == nil && info.IsDir() {
		return spec
	}
	return filepath.Dir(spec)
}

func expandPath(base, p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	} else if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Abs(p)
}

// ParseWorkspace validates a (workspace ...) form, reporting every problem
// with its spec position, and resolves paths against base.
func ParseWorkspace(n *Node, base string) (*Workspace, error) {
	ws := &Workspace{}
	if n == nil {
		return ws, nil
	}
	var errs []error
	bad := func(at *Node, f string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", at.Pos, fmt.Sprintf(f, a...)))
	}
	one := func(it *Node) string {
		if len(it.List) != 2 || it.List[1].IsList {
			bad(it, "expected (%s value), got %s", it.Head(), short(it))
			return ""
		}
		return it.List[1].Atom
	}
	seen := map[string]bool{}
	var tmux *Node
	for _, it := range n.Args() {
		h := it.Head()
		if seen[h] {
			bad(it, "duplicate (%s ...) in workspace", h)
		}
		seen[h] = true
		switch h {
		case "name":
			if ws.Name = one(it); ws.Name != "" && !repoNameRE.MatchString(ws.Name) {
				bad(it, "name must be letters, digits, '.', '_' or '-'")
			}
		case "out":
			if o := one(it); o != "" {
				p, err := expandPath(base, o)
				if err != nil {
					bad(it, "%v", err)
				}
				ws.Out = p
			}
		case "worktrees":
			names := map[string]bool{}
			for _, w := range it.Args() {
				b := Bindings{}
				if !Match(Pat("(worktree ?name ?opts...)"), w, b) || b.One("name").IsList {
					bad(w, "expected (worktree NAME (branch BRANCH)), got %s", short(w))
					continue
				}
				wt := Worktree{Name: b.Atom("name"), Branch: b.Atom("name")}
				if !tmuxNameRE.MatchString(wt.Name) {
					bad(w, "worktree name %q must be letters, digits, '_' or '-'", wt.Name)
				}
				if names[wt.Name] {
					bad(w, "duplicate worktree %q", wt.Name)
				}
				names[wt.Name] = true
				for _, o := range b.Rest("opts") {
					if o.Head() != "branch" {
						bad(o, "worktree options are (branch NAME), got %s", short(o))
						continue
					}
					wt.Branch = one(o)
				}
				if br := wt.Branch; !branchRE.MatchString(br) || strings.Contains(br, "..") || strings.HasPrefix(br, "-") ||
					strings.HasPrefix(br, "/") || strings.HasSuffix(br, "/") || strings.HasSuffix(br, ".lock") {
					bad(w, "invalid branch name %q", br)
				}
				ws.Worktrees = append(ws.Worktrees, wt)
			}
		case "tmux":
			tmux = it
		default:
			bad(it, "unknown workspace item %s (want name, out, worktrees, tmux)", short(it))
		}
	}
	if len(ws.Worktrees) > 0 && ws.Out == "" {
		bad(n, "worktrees need (out ...): they live next to the main checkout, in <out>.wt/")
	}
	for i := range ws.Worktrees {
		ws.Worktrees[i].Path = filepath.Join(ws.Out+".wt", ws.Worktrees[i].Name)
	}
	if tmux != nil {
		parseTmux(tmux, ws, base, bad)
	}
	return ws, errors.Join(errs...)
}

func parseTmux(t *Node, ws *Workspace, base string, bad func(*Node, string, ...any)) {
	b := Bindings{}
	if len(t.Args()) != 1 || !Match(Pat("(session ?name ?windows...)"), t.Args()[0], b) || b.One("name").IsList {
		bad(t, "expected (tmux (session NAME (window ...)...))")
		return
	}
	ws.Session = b.Atom("name")
	if !tmuxNameRE.MatchString(ws.Session) {
		bad(b.One("name"), "tmux session name %q must be letters, digits, '_' or '-'", ws.Session)
	}
	root := ws.Out
	if root == "" {
		root = base
	}
	names := map[string]bool{}
	for _, w := range b.Rest("windows") {
		wb := Bindings{}
		if !Match(Pat("(window ?name ?opts...)"), w, wb) || wb.One("name").IsList {
			bad(w, "expected (window NAME (dir PATH)|(worktree NAME) (run \"...\")), got %s", short(w))
			continue
		}
		win := Window{Name: wb.Atom("name"), Dir: root}
		if !tmuxNameRE.MatchString(win.Name) {
			bad(w, "window name %q must be letters, digits, '_' or '-'", win.Name)
		}
		if names[win.Name] {
			bad(w, "duplicate window %q", win.Name)
		}
		names[win.Name] = true
		var hasDir, hasWT bool
		for _, o := range wb.Rest("opts") {
			val := ""
			if len(o.List) == 2 && !o.List[1].IsList {
				val = o.List[1].Atom
			} else {
				bad(o, "expected (%s value), got %s", o.Head(), short(o))
				continue
			}
			switch o.Head() {
			case "dir":
				hasDir = true
				if p, err := expandPath(root, val); err == nil {
					win.Dir = p
				}
			case "worktree":
				hasWT = true
				found := false
				for _, wt := range ws.Worktrees {
					if wt.Name == val {
						win.Dir, found = wt.Path, true
					}
				}
				if !found {
					bad(o, "no worktree named %q in (worktrees ...)", val)
				}
			case "run":
				win.Run = val
			default:
				bad(o, "window options are (dir ...), (worktree ...) and (run ...), got %s", short(o))
			}
		}
		if hasDir && hasWT {
			bad(w, "window %s takes (dir ...) or (worktree ...), not both", win.Name)
		}
		ws.Windows = append(ws.Windows, win)
	}
	if len(ws.Windows) == 0 {
		bad(t, "tmux session needs at least one (window ...)")
	}
}

// loadWorkspace reads a spec file or folder and returns its workspace.
func loadWorkspace(spec string) (*Workspace, error) {
	forms, err := ReadSpec(spec)
	if err != nil {
		return nil, err
	}
	linked, err := Merge(forms)
	if err != nil {
		return nil, err
	}
	if linked.Workspace == nil {
		return nil, fmt.Errorf("%s has no (workspace ...) form", spec)
	}
	ws, err := ParseWorkspace(linked.Workspace, specBase(spec))
	if err != nil {
		return nil, err
	}
	if ws.Out == "" {
		return nil, fmt.Errorf("%s: the workspace needs (out ...) to find the main checkout", linked.Workspace.Pos)
	}
	return ws, nil
}

// canonical resolves symlinks so paths compare equal to what git prints.
func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "/" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// worktreesOf lists the checkouts git knows for the repository at main.
func worktreesOf(main string, r Runner) (map[string]bool, error) {
	out, err := r.Query(main, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("%s is not a git repository yet (create it with `tilegen -git`): %s", main, out)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			set[canonical(p)] = true
		}
	}
	return set, nil
}

// Up creates missing worktrees, then creates or completes the tmux session
// and attaches to it. Running it again reuses everything that exists.
func Up(ws *Workspace, r Runner, log io.Writer, noAttach bool) error {
	have, err := worktreesOf(ws.Out, r)
	if err != nil {
		return err
	}
	for _, wt := range ws.Worktrees {
		if have[canonical(wt.Path)] {
			fmt.Fprintf(log, "worktree %s: exists at %s\n", wt.Name, tilde(wt.Path))
			continue
		}
		if _, err := os.Stat(wt.Path); err == nil {
			return fmt.Errorf("%s exists but is not a worktree of %s; move it away first", wt.Path, ws.Out)
		}
		args := []string{"worktree", "add", "-q"}
		if _, err := r.Query(ws.Out, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+wt.Branch); err == nil {
			args = append(args, wt.Path, wt.Branch)
		} else {
			args = append(args, "-b", wt.Branch, wt.Path)
		}
		if err := r.Do(ws.Out, "git", args...); err != nil {
			return err
		}
	}
	if ws.Session == "" {
		return nil
	}
	return tmuxUp(ws, r, log, noAttach)
}

func tmuxUp(ws *Workspace, r Runner, log io.Writer, noAttach bool) error {
	target := "=" + ws.Session // '=' means exact match; tmux otherwise matches prefixes
	existing := map[string]bool{}
	running := false
	if _, err := r.Query("", "tmux", "has-session", "-t", target); err == nil {
		running = true
		out, _ := r.Query("", "tmux", "list-windows", "-t", target, "-F", "#{window_name}")
		for _, w := range strings.Fields(out) {
			existing[w] = true
		}
		fmt.Fprintf(log, "tmux session %s: running\n", ws.Session)
	}
	for _, w := range ws.Windows {
		if existing[w.Name] {
			continue
		}
		var err error
		if !running {
			err = r.Do("", "tmux", "new-session", "-d", "-s", ws.Session, "-n", w.Name, "-c", w.Dir)
			running = true
		} else {
			err = r.Do("", "tmux", "new-window", "-d", "-t", target+":", "-n", w.Name, "-c", w.Dir)
		}
		if err != nil {
			return err
		}
		if w.Run != "" { // typed into the window's shell, which stays open afterwards
			if err := r.Do("", "tmux", "send-keys", "-t", target+":"+w.Name, w.Run, "Enter"); err != nil {
				return err
			}
		}
	}
	switch {
	case noAttach:
		fmt.Fprintf(log, "attach with: tmux attach -t %s\n", ws.Session)
		return nil
	case os.Getenv("TMUX") != "": // already inside tmux: switch this client over
		return r.Do("", "tmux", "switch-client", "-t", target)
	case !isTerminal(os.Stdin):
		fmt.Fprintf(log, "no terminal to attach; attach with: tmux attach -t %s\n", ws.Session)
		return nil
	}
	return attach("tmux", "attach-session", "-t", target)
}

// attach hands the terminal to tmux. A variable so tests can replace it.
var attach = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Down closes the tmux session. With prune it also removes worktrees; git
// refuses to remove any with uncommitted changes, and branches are kept.
func Down(ws *Workspace, r Runner, log io.Writer, prune bool) error {
	if ws.Session != "" {
		if _, err := r.Query("", "tmux", "has-session", "-t", "="+ws.Session); err == nil {
			if err := r.Do("", "tmux", "kill-session", "-t", "="+ws.Session); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(log, "tmux session %s: not running\n", ws.Session)
		}
	}
	if !prune {
		if len(ws.Worktrees) > 0 {
			fmt.Fprintln(log, "worktrees kept; remove them with: tilegen down -prune")
		}
		return nil
	}
	have, err := worktreesOf(ws.Out, r)
	if err != nil {
		return err
	}
	var errs []error
	for _, wt := range ws.Worktrees {
		if !have[canonical(wt.Path)] {
			continue
		}
		if err := r.Do(ws.Out, "git", "worktree", "remove", wt.Path); err != nil {
			if strings.Contains(err.Error(), "modified or untracked files") {
				err = errors.New("it has uncommitted changes; commit or stash them, then run `tilegen down -prune` again")
			}
			errs = append(errs, fmt.Errorf("kept worktree %s at %s: %w", wt.Name, tilde(wt.Path), err))
		}
	}
	return errors.Join(errs...)
}

// Status prints the session and, for the main checkout and each worktree,
// its state, open tilegen holes, and branch.
func Status(ws *Workspace, r Runner, w io.Writer) error {
	title := ws.Name
	if title == "" {
		title = filepath.Base(ws.Out)
	}
	session := "no tmux session declared"
	if ws.Session != "" {
		session = "tmux " + ws.Session + ": not running"
		if out, err := r.Query("", "tmux", "list-sessions", "-F", "#{session_name} #{session_attached} #{session_windows}"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				if f := strings.Fields(line); len(f) == 3 && f[0] == ws.Session {
					state := "detached"
					if f[1] != "0" {
						state = "attached"
					}
					session = fmt.Sprintf("tmux %s: %s, %s windows", ws.Session, state, f[2])
				}
			}
		}
	}
	fmt.Fprintf(w, "%s   %s\n", title, session)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, wt := range append([]Worktree{{Name: "main", Path: ws.Out}}, ws.Worktrees...) {
		if _, err := os.Stat(wt.Path); err != nil {
			fmt.Fprintf(tw, "  %s\t%s\tmissing (run tilegen up)\t\t\n", wt.Name, tilde(wt.Path))
			continue
		}
		state := "clean"
		porcelain, err := r.Query(wt.Path, "git", "status", "--porcelain")
		switch {
		case err != nil:
			state = "not a git checkout"
		case porcelain != "":
			state = fmt.Sprintf("%d changed", len(strings.Split(porcelain, "\n")))
		}
		branch, _ := r.Query(wt.Path, "git", "branch", "--show-current")
		holes := countHoles(wt.Path)
		unit := "holes"
		if holes == 1 {
			unit = "hole"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%d %s\t%s\n", wt.Name, tilde(wt.Path), state, holes, unit, branch)
	}
	return tw.Flush()
}

// countHoles counts tilegen hole markers in the Go files under root.
func countHoles(root string) int {
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && p != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir // .git, .tilegen, editor folders
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") {
			if h, err := findHoles(p); err == nil {
				n += len(h)
			}
		}
		return nil
	})
	return n
}
